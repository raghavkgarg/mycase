package pithistory

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
)

const DefaultDBPath = "data/mycase.db"

// DB wraps a DuckDB connection dedicated to Point-in-Time research and calibration data.
type DB struct {
	db     *sql.DB
	ownsDB bool
}

// Open opens (or creates) the DuckDB database at path and initializes the schema.
func Open(path string) (*DB, error) {
	if (path == "" || path == DefaultDBPath) && cache.GetDB() != nil {
		p := &DB{db: cache.GetDB().Conn(), ownsDB: false}
		if err := p.initSchema(context.Background()); err != nil {
			return nil, fmt.Errorf("init pit schema: %w", err)
		}
		return p, nil
	}

	if path == "" {
		path = DefaultDBPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create dir for pit db: %w", err)
	}

	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, fmt.Errorf("open pit db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping pit db: %w", err)
	}

	p := &DB{db: db, ownsDB: true}
	if err := p.initSchema(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("init pit schema: %w", err)
	}
	return p, nil
}

// Close closes the underlying DuckDB connection.
func (p *DB) Close() error {
	if p.ownsDB && p.db != nil {
		return p.db.Close()
	}
	return nil
}

const schemaDDL = `
CREATE TABLE IF NOT EXISTS pit_runs (
    as_of_date         DATE,
    index_name         VARCHAR,
    method             VARCHAR,
    regime_multiplier  DOUBLE,
    total_constituents INTEGER,
    stage1_survivors   INTEGER,
    selected_count     INTEGER,
    pillar4_uncalibrated BOOLEAN DEFAULT false,
    created_at         TIMESTAMP,
    PRIMARY KEY (as_of_date, index_name, method)
);

CREATE TABLE IF NOT EXISTS pit_candidate_scores (
    as_of_date         DATE,
    index_name         VARCHAR,
    method             VARCHAR,
    ticker             VARCHAR,
    sector             VARCHAR,
    passed_stage1      BOOLEAN,
    data_fetch_failed  BOOLEAN DEFAULT false,
    rejection_reason   VARCHAR,
    raw_score          DOUBLE,
    effective_score    DOUBLE,
    composite_rs       DOUBLE,
    vcp_ratio          DOUBLE,
    rvol_z_score       DOUBLE,
    decayed_pp         DOUBLE,
    delivery_delta     DOUBLE,
    selected           BOOLEAN,
    final_weight       DOUBLE,
    forward_return_21d DOUBLE,
    pillar4_uncalibrated BOOLEAN DEFAULT false,
    pillar4_insufficient_history BOOLEAN DEFAULT false,
    PRIMARY KEY (as_of_date, index_name, method, ticker)
);
`

func (p *DB) initSchema(ctx context.Context) error {
	if _, err := p.db.ExecContext(ctx, schemaDDL); err != nil {
		return err
	}
	// Migrate existing database tables gracefully
	_, _ = p.db.ExecContext(ctx, "ALTER TABLE pit_candidate_scores ADD COLUMN IF NOT EXISTS data_fetch_failed BOOLEAN DEFAULT false;")
	_, _ = p.db.ExecContext(ctx, "ALTER TABLE pit_candidate_scores ADD COLUMN IF NOT EXISTS pillar4_uncalibrated BOOLEAN DEFAULT false;")
	_, _ = p.db.ExecContext(ctx, "ALTER TABLE pit_candidate_scores ADD COLUMN IF NOT EXISTS pillar4_insufficient_history BOOLEAN DEFAULT false;")
	_, _ = p.db.ExecContext(ctx, "ALTER TABLE pit_runs ADD COLUMN IF NOT EXISTS pillar4_uncalibrated BOOLEAN DEFAULT false;")
	_, _ = p.db.ExecContext(ctx, "UPDATE pit_candidate_scores SET pillar4_uncalibrated = TRUE WHERE as_of_date <= '2026-09-10';")
	_, _ = p.db.ExecContext(ctx, "UPDATE pit_runs SET pillar4_uncalibrated = TRUE WHERE as_of_date <= '2026-09-10';")
	// Clean out any artificial dummy index placeholder rows (e.g. DUMMYINXGN, DUMMYTRVN)
	_, _ = p.db.ExecContext(ctx, "DELETE FROM pit_candidate_scores WHERE UPPER(ticker) LIKE '%DUMMY%';")
	_ = p.initIndexConstituents(ctx)
	_ = p.initViews(ctx)
	return nil
}

// SaveRunSnapshot inserts or replaces a run snapshot and all constituent candidate scores.
func (p *DB) SaveRunSnapshot(ctx context.Context, snap *stockpicker.PITRunSnapshot) error {
	if snap == nil {
		return fmt.Errorf("nil snapshot")
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 1. Upsert Run Record
	runQuery := `
INSERT OR REPLACE INTO pit_runs (
    as_of_date, index_name, method, regime_multiplier, 
    total_constituents, stage1_survivors, selected_count, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);
`
	_, err = tx.ExecContext(ctx, runQuery,
		snap.AsOfDate,
		snap.IndexName,
		snap.Method,
		snap.RegimeMultiplier,
		snap.TotalConstituents,
		snap.Stage1Count,
		snap.SelectedCount,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("insert pit_run: %w", err)
	}

	// 2. Upsert Candidate Records
	candidateQuery := `
INSERT OR REPLACE INTO pit_candidate_scores (
    as_of_date, index_name, method, ticker, sector,
    passed_stage1, data_fetch_failed, rejection_reason, raw_score, effective_score,
    composite_rs, vcp_ratio, rvol_z_score, decayed_pp, delivery_delta,
    selected, final_weight, forward_return_21d, pillar4_uncalibrated, pillar4_insufficient_history
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
`
	stmt, err := tx.PrepareContext(ctx, candidateQuery)
	if err != nil {
		return fmt.Errorf("prepare candidate insert: %w", err)
	}
	defer stmt.Close()

	isUncalibrated := (snap.AsOfDate <= "2026-09-10")

	for _, c := range snap.Candidates {
		_, err := stmt.ExecContext(ctx,
			snap.AsOfDate,
			snap.IndexName,
			snap.Method,
			c.Ticker,
			c.Sector,
			c.PassedStage1,
			c.DataFetchFailed,
			c.RejectionReason,
			c.RawScore,
			c.EffectiveScore,
			c.CompositeRS,
			c.VCPRatio,
			c.RVOLZScore,
			c.DecayedPP,
			c.DeliveryDelta,
			c.Selected,
			c.FinalWeight,
			0.0, // forward return initialized to 0.0, backfilled after 21 days
			isUncalibrated,
			c.Pillar4InsufficientHistory,
		)
		if err != nil {
			return fmt.Errorf("insert candidate %s: %w", c.Ticker, err)
		}
	}

	return tx.Commit()
}

// UpdateForwardReturns updates the realized forward return for a specific candidate at a historical date.
func (p *DB) UpdateForwardReturns(ctx context.Context, asOfDate, indexName, method, ticker string, fwdRet float64) error {
	query := `
UPDATE pit_candidate_scores 
SET forward_return_21d = ?
WHERE as_of_date = ? AND index_name = ? AND method = ? AND ticker = ?;
`
	_, err := p.db.ExecContext(ctx, query, fwdRet, asOfDate, indexName, method, ticker)
	return err
}

// HasRun returns whether a run snapshot exists for the given asOfDate, indexName, and method.
func (p *DB) HasRun(ctx context.Context, asOfDate, indexName, method string) (bool, error) {
	var count int64
	cleanIndex := strings.NewReplacer(",", "_", " ", "_", "^", "").Replace(indexName)
	query := `SELECT count(*) FROM v_pit_runs WHERE as_of_date = ? AND (index_name = ? OR index_name = ? OR index_name = 'niftytotalmarket') AND method = ?;`
	err := p.db.QueryRowContext(ctx, query, asOfDate, cleanIndex, indexName, method).Scan(&count)
	if err != nil {
		// Fallback to base pit_runs if view not ready
		query = `SELECT count(*) FROM pit_runs WHERE as_of_date = ? AND (index_name = ? OR index_name = ? OR index_name = 'niftytotalmarket') AND method = ?;`
		err = p.db.QueryRowContext(ctx, query, asOfDate, cleanIndex, indexName, method).Scan(&count)
		if err != nil {
			return false, err
		}
	}
	return count > 0, nil
}

// GetLatestRunDate returns the most recent as_of_date for the given indexName and method.
func (p *DB) GetLatestRunDate(ctx context.Context, indexName, method string) (string, error) {
	var dt string
	cleanIndex := strings.NewReplacer(",", "_", " ", "_", "^", "").Replace(indexName)
	query := `SELECT as_of_date::VARCHAR FROM v_pit_runs WHERE (index_name = ? OR index_name = ? OR index_name = 'niftytotalmarket') AND method = ? ORDER BY as_of_date DESC LIMIT 1;`
	err := p.db.QueryRowContext(ctx, query, cleanIndex, indexName, method).Scan(&dt)
	if err != nil {
		return "", err
	}
	return dt, nil
}

// GetRunSyncTime returns the creation/sync timestamp for a run snapshot.
func (p *DB) GetRunSyncTime(ctx context.Context, asOfDate, indexName, method string) (time.Time, error) {
	var t time.Time
	cleanIndex := strings.NewReplacer(",", "_", " ", "_", "^", "").Replace(indexName)
	query := `SELECT created_at FROM v_pit_runs WHERE as_of_date = ? AND (index_name = ? OR index_name = ? OR index_name = 'niftytotalmarket') AND method = ? ORDER BY created_at DESC LIMIT 1;`
	err := p.db.QueryRowContext(ctx, query, asOfDate, cleanIndex, indexName, method).Scan(&t)
	if err != nil {
		// Fallback to base pit_runs
		query = `SELECT created_at FROM pit_runs WHERE as_of_date = ? AND (index_name = ? OR index_name = ? OR index_name = 'niftytotalmarket') AND method = ? ORDER BY created_at DESC LIMIT 1;`
		err = p.db.QueryRowContext(ctx, query, asOfDate, cleanIndex, indexName, method).Scan(&t)
		if err != nil {
			return time.Time{}, err
		}
	}
	ist := time.FixedZone("IST", 5*3600+30*60)
	return t.In(ist), nil
}

// GetCandidateTemporalVelocities queries DuckDB for chronological score trajectories and survival streaks.
func (p *DB) GetCandidateTemporalVelocities(
	ctx context.Context,
	indexName, method, excludeDate string,
	lookbackRuns int,
) (map[string]stockpicker.TemporalVelocity, error) {
	if lookbackRuns <= 0 {
		lookbackRuns = 5
	}

	// 1. Find the latest distinct dates (excluding excludeDate if specified)
	dateQuery := `
SELECT DISTINCT as_of_date 
FROM v_pit_runs 
WHERE index_name = ? AND method = ? AND (? = '' OR as_of_date != ?)
ORDER BY as_of_date DESC 
LIMIT ?;
`
	rows, err := p.db.QueryContext(ctx, dateQuery, indexName, method, excludeDate, excludeDate, lookbackRuns)
	if err != nil {
		return nil, fmt.Errorf("query past run dates: %w", err)
	}
	defer rows.Close()

	var datesDesc []string
	for rows.Next() {
		var dt string
		if err := rows.Scan(&dt); err == nil {
			datesDesc = append(datesDesc, dt)
		}
	}
	rows.Close()

	if len(datesDesc) == 0 {
		return make(map[string]stockpicker.TemporalVelocity), nil
	}

	// Reverse to ascending chronological order [T-2, T-1, ...]
	var datesAsc []string
	for _, d := range slices.Backward(datesDesc) {
		datesAsc = append(datesAsc, d)
	}

	datePlaceholders := make([]string, len(datesAsc))
	dateArgs := make([]any, len(datesAsc)+2)
	dateArgs[0] = indexName
	dateArgs[1] = method
	for i, dt := range datesAsc {
		datePlaceholders[i] = "?"
		dateArgs[i+2] = dt
	}

	scoreQuery := fmt.Sprintf(`
SELECT 
    as_of_date,
    ticker,
    passed_stage1,
    COALESCE(data_fetch_failed, false),
    raw_score,
    COALESCE(delivery_delta, 0.0)
FROM v_pit_candidate_scores
WHERE index_name = ? AND method = ? AND as_of_date IN (%s)
ORDER BY as_of_date ASC;
`, strings.Join(datePlaceholders, ","))

	sRows, err := p.db.QueryContext(ctx, scoreQuery, dateArgs...)
	if err != nil {
		return nil, fmt.Errorf("query candidate scores: %w", err)
	}
	defer sRows.Close()

	type runPoint struct {
		Date         string
		PassedStage1 bool
		DataFailed   bool
		RawScore     float64
		DelivDelta   float64
	}
	tickerHistory := make(map[string][]runPoint)
	for sRows.Next() {
		var dt, t string
		var pass, dataFailed bool
		var score, deliv float64
		if err := sRows.Scan(&dt, &t, &pass, &dataFailed, &score, &deliv); err == nil {
			tickerHistory[t] = append(tickerHistory[t], runPoint{
				Date:         dt,
				PassedStage1: pass,
				DataFailed:   dataFailed,
				RawScore:     score,
				DelivDelta:   deliv,
			})
		}
	}

	result := make(map[string]stockpicker.TemporalVelocity)
	for t, points := range tickerHistory {
		tv := stockpicker.TemporalVelocity{
			Ticker:        t,
			RunsEvaluated: len(points),
		}

		consec := 0
		// Walk backwards from most recent past point
		for _, point := range slices.Backward(points) {
			if point.PassedStage1 && !point.DataFailed && point.RawScore > 0 {
				consec++
			} else {
				break
			}
		}
		tv.ConsecutivePass = consec

		sumScore := 0.0
		for _, pt := range points {
			tv.Dates = append(tv.Dates, pt.Date)
			tv.ScoreTrajectory = append(tv.ScoreTrajectory, pt.RawScore)
			sumScore += pt.RawScore
		}
		if len(points) > 0 {
			tv.AvgScore = sumScore / float64(len(points))
			tv.LatestDelivDelta = points[len(points)-1].DelivDelta
		}
		if len(points) >= 2 {
			tv.VelocityDelta = points[len(points)-1].RawScore - points[len(points)-2].RawScore
		}

		result[t] = tv
	}

	return result, nil
}

func (p *DB) initIndexConstituents(ctx context.Context) error {
	schema := `
CREATE TABLE IF NOT EXISTS index_constituents (
    index_name VARCHAR NOT NULL,
    ticker VARCHAR NOT NULL,
    PRIMARY KEY (index_name, ticker)
);
`
	if _, err := p.db.ExecContext(ctx, schema); err != nil {
		return err
	}

	// Check if already populated
	var count int64
	_ = p.db.QueryRowContext(ctx, "SELECT count(*) FROM index_constituents;").Scan(&count)
	if count > 0 {
		return nil
	}

	files := []struct {
		indexName string
		path      string
	}{
		{"NIFTY50", "data/universe_snapshots/NIFTY50.csv"},
		{"microcap250", "data/universe_snapshots/microcap250.csv"},
		{"smallcap250", "data/universe_snapshots/smallcap250.csv"},
		{"microcap250_smallcap250", "data/universe_snapshots/microcap250_smallcap250.csv"},
	}

	for _, f := range files {
		candidates := []string{
			f.path,
			filepath.Join("..", f.path),
			filepath.Join("..", "..", f.path),
		}
		for _, cp := range candidates {
			if _, err := os.Stat(cp); err == nil {
				query := fmt.Sprintf(`
INSERT OR REPLACE INTO index_constituents
SELECT '%s' as index_name, 'NSE:' || trim(symbol) as ticker 
FROM read_csv_auto('%s', header=false, names=['symbol'])
WHERE trim(symbol) != '' AND trim(symbol) NOT LIKE '%%%%DUMMY%%%%';
`, f.indexName, cp)
				_, _ = p.db.ExecContext(ctx, query)
				break
			}
		}
	}
	return nil
}

func (p *DB) initViews(ctx context.Context) error {
	viewDDL := `
CREATE OR REPLACE VIEW v_pit_candidate_scores AS
SELECT * FROM pit_candidate_scores
UNION ALL
SELECT 
    p.as_of_date,
    c.index_name,
    p.method,
    p.ticker,
    p.sector,
    p.passed_stage1,
    p.data_fetch_failed,
    p.rejection_reason,
    p.raw_score,
    p.effective_score,
    p.composite_rs,
    p.vcp_ratio,
    p.rvol_z_score,
    p.decayed_pp,
    p.delivery_delta,
    p.selected,
    p.final_weight,
    p.forward_return_21d,
    p.pillar4_uncalibrated,
    p.pillar4_insufficient_history
FROM pit_candidate_scores p
JOIN index_constituents c ON p.ticker = c.ticker
WHERE p.index_name = 'niftytotalmarket'
  AND NOT EXISTS (
      SELECT 1 FROM pit_candidate_scores existing
      WHERE existing.as_of_date = p.as_of_date 
        AND existing.index_name = c.index_name 
        AND existing.method = p.method 
        AND existing.ticker = p.ticker
  )
UNION ALL
SELECT 
    p.as_of_date,
    'small250' as index_name,
    p.method,
    p.ticker,
    p.sector,
    p.passed_stage1,
    p.data_fetch_failed,
    p.rejection_reason,
    p.raw_score,
    p.effective_score,
    p.composite_rs,
    p.vcp_ratio,
    p.rvol_z_score,
    p.decayed_pp,
    p.delivery_delta,
    p.selected,
    p.final_weight,
    p.forward_return_21d,
    p.pillar4_uncalibrated,
    p.pillar4_insufficient_history
FROM pit_candidate_scores p
JOIN index_constituents c ON p.ticker = c.ticker
WHERE p.index_name = 'niftytotalmarket'
  AND c.index_name = 'smallcap250'
  AND NOT EXISTS (
      SELECT 1 FROM pit_candidate_scores existing
      WHERE existing.as_of_date = p.as_of_date 
        AND existing.index_name = 'small250' 
        AND existing.method = p.method 
        AND existing.ticker = p.ticker
  );

CREATE OR REPLACE VIEW v_pit_runs AS
SELECT * FROM pit_runs
UNION ALL
SELECT 
    p.as_of_date,
    c.index_name,
    p.method,
    r.regime_multiplier,
    count(*)::INT as total_constituents,
    count(CASE WHEN p.passed_stage1 THEN 1 END)::INT as stage1_survivors,
    count(CASE WHEN p.selected THEN 1 END)::INT as selected_count,
    r.pillar4_uncalibrated,
    r.created_at
FROM pit_candidate_scores p
JOIN index_constituents c ON p.ticker = c.ticker
JOIN pit_runs r ON p.as_of_date = r.as_of_date AND r.index_name = 'niftytotalmarket' AND r.method = p.method
WHERE p.index_name = 'niftytotalmarket'
  AND NOT EXISTS (
      SELECT 1 FROM pit_runs existing
      WHERE existing.as_of_date = p.as_of_date
        AND existing.index_name = c.index_name
        AND existing.method = p.method
  )
GROUP BY p.as_of_date, c.index_name, p.method, r.regime_multiplier, r.pillar4_uncalibrated, r.created_at
UNION ALL
SELECT 
    p.as_of_date,
    'small250' as index_name,
    p.method,
    r.regime_multiplier,
    count(*)::INT as total_constituents,
    count(CASE WHEN p.passed_stage1 THEN 1 END)::INT as stage1_survivors,
    count(CASE WHEN p.selected THEN 1 END)::INT as selected_count,
    r.pillar4_uncalibrated,
    r.created_at
FROM pit_candidate_scores p
JOIN index_constituents c ON p.ticker = c.ticker
JOIN pit_runs r ON p.as_of_date = r.as_of_date AND r.index_name = 'niftytotalmarket' AND r.method = p.method
WHERE p.index_name = 'niftytotalmarket'
  AND c.index_name = 'smallcap250'
  AND NOT EXISTS (
      SELECT 1 FROM pit_runs existing
      WHERE existing.as_of_date = p.as_of_date
        AND existing.index_name = 'small250'
        AND existing.method = p.method
  )
GROUP BY p.as_of_date, p.method, r.regime_multiplier, r.pillar4_uncalibrated, r.created_at;
`
	_, err := p.db.ExecContext(ctx, viewDDL)
	return err
}
