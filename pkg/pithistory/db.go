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

// Conn returns the underlying *sql.DB connection.
func (p *DB) Conn() *sql.DB {
	return p.db
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

CREATE TABLE IF NOT EXISTS stage1_shadow_results (
    as_of_date             DATE,
    index_name             VARCHAR,
    method                 VARCHAR,
    ticker                 VARCHAR,
    sector                 VARCHAR,
    legacy_stage1_pass     BOOLEAN,
    legacy_rejection_cause VARCHAR,
    shadow_stage1_pass     BOOLEAN,
    shadow_relief_channel  VARCHAR,
    shadow_base_mult       DOUBLE,
    delivery_delta         DOUBLE,
    composite_rs           DOUBLE,
    vcp_ratio              DOUBLE,
    divergence_type        VARCHAR,
    created_at             TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (as_of_date, index_name, method, ticker)
);

CREATE TABLE IF NOT EXISTS pit_data_health (
    as_of_date                 DATE,
    index_name                 VARCHAR,
    method                     VARCHAR,
    total_candidates           INTEGER,
    prices_max_date            DATE,
    prices_stale_count         INTEGER,
    delivery_max_date          DATE,
    delivery_stale_count       INTEGER,
    paired_candidates_count    INTEGER,
    frozen_deliv_count         INTEGER,
    frozen_rs_count            INTEGER,
    frozen_vcp_count           INTEGER,
    frozen_rvol_count          INTEGER,
    zero_cfo_count             INTEGER,
    zero_pat_count             INTEGER,
    null_de_count              INTEGER,
    dropped_holdings_count     INTEGER,
    health_status              VARCHAR,
    created_at                 TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (as_of_date, index_name, method)
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
	// Clean out any artificial dummy index placeholder rows (e.g. DUMMYINXGN, DUMMYTRVN)
	_, _ = p.db.ExecContext(ctx, "DELETE FROM pit_candidate_scores WHERE UPPER(ticker) LIKE '%DUMMY%';")
	// Consolidate onto pure niftytotalmarket base: remove redundant sub-index physical rows
	_, _ = p.db.ExecContext(ctx, "DELETE FROM pit_candidate_scores WHERE index_name IN ('small250', 'smallcap250', 'microcap250', 'microsmall', 'microcap250_smallcap250');")
	_, _ = p.db.ExecContext(ctx, "DELETE FROM pit_runs WHERE index_name IN ('small250', 'smallcap250', 'microcap250', 'microsmall', 'microcap250_smallcap250');")
	_ = p.initIndexConstituents(ctx)
	_ = p.initViews(ctx)
	return nil
}

// SaveRunSnapshot inserts or replaces a run snapshot and all constituent candidate scores.
func (p *DB) SaveRunSnapshot(ctx context.Context, snap *stockpicker.PITRunSnapshot) error {
	if snap == nil {
		return fmt.Errorf("nil snapshot")
	}

	canonIndex := NormalizeIndexName(snap.IndexName)

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
		canonIndex,
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
			canonIndex,
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
			nil, // forward return initialized to NULL, backfilled after 21 trading sessions
			isUncalibrated,
			c.Pillar4InsufficientHistory,
		)
		if err != nil {
			return fmt.Errorf("insert candidate %s: %w", c.Ticker, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Synchronize shadow divergence results in background
	_ = p.SyncShadowResults(ctx, snap.AsOfDate, canonIndex, snap.Method)
	return nil
}

// SyncShadowResults computes and synchronizes Stage-1 shadow divergence for a given date, index, and method.
func (p *DB) SyncShadowResults(ctx context.Context, asOfDate, indexName, method string) error {
	indexName = NormalizeIndexName(indexName)
	syncQuery := `
INSERT OR REPLACE INTO stage1_shadow_results (
    as_of_date, index_name, method, ticker, sector,
    legacy_stage1_pass, legacy_rejection_cause,
    shadow_stage1_pass, shadow_relief_channel, shadow_base_mult,
    delivery_delta, composite_rs, vcp_ratio,
    divergence_type, created_at
)
SELECT 
    c.as_of_date,
    c.index_name,
    c.method,
    c.ticker,
    COALESCE(NULLIF(c.sector, ''), 'Unknown') AS sector,
    c.passed_stage1 AS legacy_stage1_pass,
    COALESCE(NULLIF(c.rejection_reason, ''), 'Stage-1 Qualified') AS legacy_rejection_cause,
    CASE 
        WHEN c.passed_stage1 THEN TRUE
        WHEN (c.rejection_reason LIKE '%ROCE%' OR c.rejection_reason LIKE '%Capital Efficiency%')
             AND c.delivery_delta >= 0.09 AND c.composite_rs >= 0.15 AND c.vcp_ratio <= 1.20 THEN TRUE
        ELSE FALSE
    END AS shadow_stage1_pass,
    CASE 
        WHEN c.passed_stage1 THEN 'legacy_pass'
        WHEN (c.rejection_reason LIKE '%ROCE%' OR c.rejection_reason LIKE '%Capital Efficiency%')
             AND c.delivery_delta >= 0.09 AND c.composite_rs >= 0.15 AND c.vcp_ratio <= 1.20 THEN 'delivery_override'
        ELSE 'blocked'
    END AS shadow_relief_channel,
    CASE 
        WHEN c.rejection_reason LIKE '%0 weeks%' OR c.rejection_reason LIKE '%1 weeks%' THEN 0.50
        WHEN c.rejection_reason LIKE '%2 weeks%' OR c.rejection_reason LIKE '%3 weeks%' THEN 0.75
        ELSE 1.00
    END AS shadow_base_mult,
    c.delivery_delta,
    c.composite_rs,
    c.vcp_ratio,
    CASE 
        WHEN c.passed_stage1 THEN 'ALIGNED_PASS'
        WHEN (c.rejection_reason LIKE '%ROCE%' OR c.rejection_reason LIKE '%Capital Efficiency%')
             AND c.delivery_delta >= 0.09 AND c.composite_rs >= 0.15 AND c.vcp_ratio <= 1.20
        THEN 'RESCUED'
        ELSE 'ALIGNED_FAIL'
    END AS divergence_type,
    CURRENT_TIMESTAMP
FROM v_pit_candidate_scores c
WHERE (? = '' OR c.as_of_date = ?)
  AND c.index_name = ?
  AND c.method = ?;
`
	_, err := p.db.ExecContext(ctx, syncQuery, asOfDate, asOfDate, indexName, method)
	return err
}

// BackfillForwardReturns backfills realized 21-trading-session forward returns
// for historical candidates from the prices table.
func (p *DB) BackfillForwardReturns(ctx context.Context) (int64, error) {
	query := `
WITH ranked_prices AS (
    SELECT ticker, date, close,
           ROW_NUMBER() OVER (PARTITION BY ticker ORDER BY date ASC) as rn
    FROM prices
),
fwd_returns AS (
    SELECT p0.ticker, p0.date as as_of_date,
           (p21.close - p0.close) / NULLIF(p0.close, 0) as fwd_ret
    FROM ranked_prices p0
    JOIN ranked_prices p21 ON p0.ticker = p21.ticker AND p21.rn = p0.rn + 21
)
UPDATE pit_candidate_scores
SET forward_return_21d = f.fwd_ret
FROM fwd_returns f
WHERE pit_candidate_scores.ticker = f.ticker
  AND pit_candidate_scores.as_of_date = f.as_of_date
  AND pit_candidate_scores.forward_return_21d IS NULL;
`
	res, err := p.db.ExecContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("backfill forward returns: %w", err)
	}
	rowsAffected, _ := res.RowsAffected()
	return rowsAffected, nil
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

CREATE OR REPLACE MACRO base_duration_multiplier(weeks_in_zone) AS (
    CASE
        WHEN weeks_in_zone >= 4 THEN 1.0
        WHEN weeks_in_zone >= 2 THEN 0.75
        WHEN weeks_in_zone >= 0 THEN 0.5
    END
);

CREATE OR REPLACE VIEW v_data_integrity_check AS
SELECT 
    as_of_date,
    index_name,
    method,
    count(*)::INT AS total_candidates,
    count(CASE WHEN data_fetch_failed THEN 1 END)::INT AS fetch_failed_count,
    count(CASE WHEN rejection_reason LIKE '%Unverified%' OR rejection_reason LIKE '%fetch%' THEN 1 END)::INT AS unverified_count
FROM pit_candidate_scores
GROUP BY as_of_date, index_name, method;

CREATE OR REPLACE VIEW v_strategy_consensus AS
SELECT 
    m.as_of_date,
    m.ticker,
    m.sector,
    m.effective_score AS mb_score,
    e.effective_score AS earlymb_score,
    (COALESCE(m.effective_score, 0) + COALESCE(e.effective_score, 0)) AS consensus_score,
    m.passed_stage1 AS mb_passed_stage1,
    e.passed_stage1 AS earlymb_passed_stage1,
    e.vcp_ratio,
    e.delivery_delta,
    e.rvol_z_score
FROM pit_candidate_scores m
JOIN pit_candidate_scores e 
  ON m.as_of_date = e.as_of_date 
 AND m.ticker = e.ticker
 AND m.index_name = e.index_name
WHERE m.method = 'multibagger' 
  AND e.method = 'earlymb'
  AND m.index_name = 'niftytotalmarket';

CREATE OR REPLACE VIEW v_pit_data_health AS
SELECT * FROM pit_data_health;
`
	_, err := p.db.ExecContext(ctx, viewDDL)
	return err
}
