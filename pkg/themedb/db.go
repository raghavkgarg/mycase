package themedb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/raghavkgarg/mycase/pkg/cache"
)

const DefaultDBPath = "data/mycase.db"

// ThemeRebalance represents the header metadata for a portfolio rebalance event.
type ThemeRebalance struct {
	ThemeName      string
	Version        int
	EffectiveDate  time.Time
	CreatedAt      time.Time
	ExecutedAt     *time.Time
	RunID          string
	TotalNAV       float64
	CashWeight     float64
	TurnoverPct    float64
	BenchmarkPrice float64
	Status         string // "PROPOSED", "COMMITTED", "ROLLED_BACK"
	Notes          string
}

// ThemeHistoryItem represents a constituent holding transition during a rebalance.
type ThemeHistoryItem struct {
	ThemeName         string
	Version           int
	Symbol            string
	ISIN              string
	Action            string // "NEW_ENTRY", "REWEIGHT", "EXITED", "UNCHANGED"
	CycleNumber       int
	TargetWeight      float64
	PrevWeight        float64
	ActualWeight      float64
	TargetShares      int
	DeltaShares       int
	ExecutedShares    int
	DecisionPrice     float64
	ExecutionAvgPrice float64
	Rank              int
	CompositeScore    float64
	ExitCategory      string
	ExitReason        string
	ExecutionStatus   string // "PENDING", "FILLED", "PARTIAL", "SKIPPED"
}

// DB wraps a DuckDB connection to data/mycase.db.
type DB struct {
	db     *sql.DB
	dbPath string
	ownsDB bool
}

const ddl = `
CREATE TABLE IF NOT EXISTS theme_rebalances (
    theme_name          VARCHAR NOT NULL,
    version             INTEGER NOT NULL,
    effective_date      DATE NOT NULL,
    created_at          TIMESTAMP NOT NULL,
    executed_at         TIMESTAMP,
    run_id              VARCHAR,
    total_nav           DOUBLE,
    cash_weight         DOUBLE DEFAULT 0.0,
    turnover_pct        DOUBLE,
    benchmark_price     DOUBLE,
    status              VARCHAR DEFAULT 'COMMITTED',
    notes               VARCHAR,
    PRIMARY KEY (theme_name, version)
);

CREATE TABLE IF NOT EXISTS theme_history (
    theme_name          VARCHAR NOT NULL,
    version             INTEGER NOT NULL,
    symbol              VARCHAR NOT NULL,
    isin                VARCHAR,
    action              VARCHAR NOT NULL,
    cycle_number        INTEGER DEFAULT 1,
    target_weight       DOUBLE NOT NULL,
    prev_weight         DOUBLE DEFAULT 0.0,
    actual_weight       DOUBLE,
    target_shares       INTEGER,
    delta_shares        INTEGER,
    executed_shares     INTEGER,
    decision_price      DOUBLE,
    execution_avg_price DOUBLE,
    rank                INTEGER,
    composite_score     DOUBLE,
    exit_category       VARCHAR,
    exit_reason         VARCHAR,
    execution_status    VARCHAR DEFAULT 'FILLED',
    PRIMARY KEY (theme_name, version, symbol)
);
`

// ResolveDBPath resolves the database path using explicit path, env var, or local directory check.
func ResolveDBPath(explicitPath string) string {
	if explicitPath != "" {
		return explicitPath
	}
	if envPath := os.Getenv("MYCASE_DB"); envPath != "" {
		return envPath
	}
	candidates := []string{
		DefaultDBPath,
		filepath.Join("..", DefaultDBPath),
		filepath.Join("..", "..", DefaultDBPath),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return DefaultDBPath
}

// Open opens (or creates) the DuckDB database at path and initializes the schema.
func Open(path string) (*DB, error) {
	resolved := ResolveDBPath(path)
	if global := cache.GetDB(); global != nil && (path == "" || resolved == DefaultDBPath || resolved == filepath.Clean(DefaultDBPath)) {
		tdb := &DB{db: global.Conn(), dbPath: resolved, ownsDB: false}
		if err := tdb.initSchema(context.Background()); err != nil {
			return nil, fmt.Errorf("init schema: %w", err)
		}
		return tdb, nil
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return nil, fmt.Errorf("create dir for mycase db: %w", err)
	}

	db, err := sql.Open("duckdb", resolved)
	if err != nil {
		return nil, fmt.Errorf("open mycase db at %s: %w", resolved, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping mycase db: %w", err)
	}

	themedb := &DB{db: db, dbPath: resolved, ownsDB: true}
	if err := themedb.initSchema(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return themedb, nil
}

// Close closes the underlying DuckDB connection.
func (d *DB) Close() error {
	if d.ownsDB && d.db != nil {
		return d.db.Close()
	}
	return nil
}

// initSchema creates the theme lifecycle tables.
func (d *DB) initSchema(ctx context.Context) error {
	_, err := d.db.ExecContext(ctx, ddl)
	return err
}

// RecordRebalance records a rebalance header and its constituent items atomically.
func (d *DB) RecordRebalance(ctx context.Context, r ThemeRebalance, items []ThemeHistoryItem) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if r.Status == "" {
		r.Status = "COMMITTED"
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}

	// Insert or replace rebalance header
	headerQuery := `
INSERT OR REPLACE INTO theme_rebalances (
    theme_name, version, effective_date, created_at, executed_at,
    run_id, total_nav, cash_weight, turnover_pct, benchmark_price,
    status, notes
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
`
	var execTime interface{}
	if r.ExecutedAt != nil && !r.ExecutedAt.IsZero() {
		execTime = *r.ExecutedAt
	}

	effDateStr := r.EffectiveDate.Format("2006-01-02")
	_, err = tx.ExecContext(ctx, headerQuery,
		r.ThemeName, r.Version, effDateStr, r.CreatedAt, execTime,
		r.RunID, r.TotalNAV, r.CashWeight, r.TurnoverPct, r.BenchmarkPrice,
		r.Status, r.Notes,
	)
	if err != nil {
		return fmt.Errorf("insert theme_rebalance header: %w", err)
	}

	// Insert or replace each line item
	itemQuery := `
INSERT OR REPLACE INTO theme_history (
    theme_name, version, symbol, isin, action, cycle_number,
    target_weight, prev_weight, actual_weight, target_shares, delta_shares, executed_shares,
    decision_price, execution_avg_price, rank, composite_score,
    exit_category, exit_reason, execution_status
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
`
	for _, it := range items {
		action := it.Action
		if action == "" {
			if it.TargetWeight <= 0 {
				action = "EXITED"
			} else if it.PrevWeight <= 0 {
				action = "NEW_ENTRY"
			} else if it.TargetWeight != it.PrevWeight {
				action = "REWEIGHT"
			} else {
				action = "UNCHANGED"
			}
		}

		execStatus := it.ExecutionStatus
		if execStatus == "" {
			execStatus = "FILLED"
		}
		cycleNum := it.CycleNumber
		if cycleNum <= 0 {
			cycleNum = 1
		}

		cleanSym := strings.ToUpper(strings.TrimSpace(it.Symbol))
		_, err = tx.ExecContext(ctx, itemQuery,
			r.ThemeName, r.Version, cleanSym, it.ISIN, action, cycleNum,
			it.TargetWeight, it.PrevWeight, it.ActualWeight, it.TargetShares, it.DeltaShares, it.ExecutedShares,
			it.DecisionPrice, it.ExecutionAvgPrice, it.Rank, it.CompositeScore,
			it.ExitCategory, it.ExitReason, execStatus,
		)
		if err != nil {
			return fmt.Errorf("insert theme_history item (%s): %w", cleanSym, err)
		}
	}

	return tx.Commit()
}

// GetLatestVersion returns the highest committed version for a given theme (0 if none exist).
func (d *DB) GetLatestVersion(ctx context.Context, themeName string) (int, error) {
	row := d.db.QueryRowContext(ctx, `
SELECT COALESCE(MAX(version), 0)
FROM theme_rebalances
WHERE theme_name = ? AND status = 'COMMITTED';
`, themeName)
	var v int
	if err := row.Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

// HasTheme checks whether the database has any committed records for the specified theme.
func (d *DB) HasTheme(ctx context.Context, themeName string) (bool, error) {
	v, err := d.GetLatestVersion(ctx, themeName)
	if err != nil {
		return false, err
	}
	return v > 0, nil
}

// GetActiveHoldings returns all constituents with target_weight > 0 for the latest committed version.
func (d *DB) GetActiveHoldings(ctx context.Context, themeName string) ([]ThemeHistoryItem, error) {
	query := `
WITH latest AS (
    SELECT MAX(version) AS max_v
    FROM theme_rebalances
    WHERE theme_name = ? AND status = 'COMMITTED'
)
SELECT 
    th.theme_name, th.version, th.symbol, COALESCE(th.isin, ''), th.action, th.cycle_number,
    th.target_weight, th.prev_weight, COALESCE(th.actual_weight, 0.0),
    COALESCE(th.target_shares, 0), COALESCE(th.delta_shares, 0), COALESCE(th.executed_shares, 0),
    COALESCE(th.decision_price, 0.0), COALESCE(th.execution_avg_price, 0.0),
    COALESCE(th.rank, 0), COALESCE(th.composite_score, 0.0),
    COALESCE(th.exit_category, ''), COALESCE(th.exit_reason, ''), COALESCE(th.execution_status, 'FILLED')
FROM theme_history th
JOIN latest ON th.version = latest.max_v
WHERE th.theme_name = ? AND th.target_weight > 0
ORDER BY th.target_weight DESC, th.symbol ASC;
`
	rows, err := d.db.QueryContext(ctx, query, themeName, themeName)
	if err != nil {
		return nil, fmt.Errorf("querying active holdings: %w", err)
	}
	defer rows.Close()

	var items []ThemeHistoryItem
	for rows.Next() {
		var it ThemeHistoryItem
		if err := rows.Scan(
			&it.ThemeName, &it.Version, &it.Symbol, &it.ISIN, &it.Action, &it.CycleNumber,
			&it.TargetWeight, &it.PrevWeight, &it.ActualWeight,
			&it.TargetShares, &it.DeltaShares, &it.ExecutedShares,
			&it.DecisionPrice, &it.ExecutionAvgPrice,
			&it.Rank, &it.CompositeScore,
			&it.ExitCategory, &it.ExitReason, &it.ExecutionStatus,
		); err != nil {
			return nil, fmt.Errorf("scanning active holding row: %w", err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// GetExitedHoldings returns all symbols that were recorded as EXITED in any committed version,
// and are NOT present with target_weight > 0 in the latest active version.
func (d *DB) GetExitedHoldings(ctx context.Context, themeName string) ([]ThemeHistoryItem, error) {
	query := `
WITH latest_active AS (
    SELECT symbol
    FROM theme_history th
    WHERE th.theme_name = ? AND th.version = (
        SELECT MAX(version) FROM theme_rebalances WHERE theme_name = ? AND status = 'COMMITTED'
    ) AND th.target_weight > 0
)
SELECT 
    th.theme_name, th.version, th.symbol, COALESCE(th.isin, ''), th.action, th.cycle_number,
    th.target_weight, th.prev_weight, COALESCE(th.actual_weight, 0.0),
    COALESCE(th.target_shares, 0), COALESCE(th.delta_shares, 0), COALESCE(th.executed_shares, 0),
    COALESCE(th.decision_price, 0.0), COALESCE(th.execution_avg_price, 0.0),
    COALESCE(th.rank, 0), COALESCE(th.composite_score, 0.0),
    COALESCE(th.exit_category, ''), COALESCE(th.exit_reason, ''), COALESCE(th.execution_status, 'FILLED')
FROM theme_history th
JOIN theme_rebalances tr ON th.theme_name = tr.theme_name AND th.version = tr.version
WHERE th.theme_name = ? 
  AND tr.status = 'COMMITTED'
  AND th.action = 'EXITED'
  AND th.symbol NOT IN (SELECT symbol FROM latest_active)
ORDER BY th.version DESC, th.symbol ASC;
`
	rows, err := d.db.QueryContext(ctx, query, themeName, themeName, themeName)
	if err != nil {
		return nil, fmt.Errorf("querying exited holdings: %w", err)
	}
	defer rows.Close()

	seen := make(map[string]bool)
	var items []ThemeHistoryItem
	for rows.Next() {
		var it ThemeHistoryItem
		if err := rows.Scan(
			&it.ThemeName, &it.Version, &it.Symbol, &it.ISIN, &it.Action, &it.CycleNumber,
			&it.TargetWeight, &it.PrevWeight, &it.ActualWeight,
			&it.TargetShares, &it.DeltaShares, &it.ExecutedShares,
			&it.DecisionPrice, &it.ExecutionAvgPrice,
			&it.Rank, &it.CompositeScore,
			&it.ExitCategory, &it.ExitReason, &it.ExecutionStatus,
		); err != nil {
			return nil, fmt.Errorf("scanning exited holding row: %w", err)
		}
		if !seen[it.Symbol] {
			seen[it.Symbol] = true
			items = append(items, it)
		}
	}
	return items, rows.Err()
}

// GetRebalances returns all rebalances for a theme sorted by version ascending.
func (d *DB) GetRebalances(ctx context.Context, themeName string) ([]ThemeRebalance, error) {
	query := `
SELECT 
    theme_name, version, effective_date, created_at, executed_at,
    COALESCE(run_id, ''), COALESCE(total_nav, 0.0), COALESCE(cash_weight, 0.0),
    COALESCE(turnover_pct, 0.0), COALESCE(benchmark_price, 0.0),
    status, COALESCE(notes, '')
FROM theme_rebalances
WHERE theme_name = ?
ORDER BY version ASC;
`
	rows, err := d.db.QueryContext(ctx, query, themeName)
	if err != nil {
		return nil, fmt.Errorf("querying rebalances: %w", err)
	}
	defer rows.Close()

	var rebalances []ThemeRebalance
	for rows.Next() {
		var r ThemeRebalance
		var execTime sql.NullTime
		if err := rows.Scan(
			&r.ThemeName, &r.Version, &r.EffectiveDate, &r.CreatedAt, &execTime,
			&r.RunID, &r.TotalNAV, &r.CashWeight, &r.TurnoverPct, &r.BenchmarkPrice,
			&r.Status, &r.Notes,
		); err != nil {
			return nil, fmt.Errorf("scanning rebalance: %w", err)
		}
		if execTime.Valid {
			r.ExecutedAt = &execTime.Time
		}
		rebalances = append(rebalances, r)
	}
	return rebalances, rows.Err()
}

// RecordPipelineRebalance computes transitions between the latest state and an incoming proposal CSV,
// and records a new version into theme_rebalances and theme_history.
func (d *DB) RecordPipelineRebalance(ctx context.Context, themeName, sourceCSV, runID string) error {
	latestV, err := d.GetLatestVersion(ctx, themeName)
	if err != nil {
		return fmt.Errorf("getting latest version: %w", err)
	}

	newV := latestV + 1
	now := time.Now()

	// Load incoming positions from source CSV
	newPositions, err := loadCSVPositions(sourceCSV)
	if err != nil {
		return fmt.Errorf("loading proposal CSV %s: %w", sourceCSV, err)
	}

	// Fetch previous active holdings
	prevMap := make(map[string]float64)
	if latestV > 0 {
		activeItems, err := d.GetActiveHoldings(ctx, themeName)
		if err == nil {
			for _, it := range activeItems {
				prevMap[CleanTicker(it.Symbol)] = it.TargetWeight
			}
		}
	}

	allSymbols := make(map[string]bool)
	for s := range prevMap {
		allSymbols[s] = true
	}
	for s := range newPositions {
		allSymbols[s] = true
	}

	turnover := 0.0
	var items []ThemeHistoryItem

	for s := range allSymbols {
		pW := prevMap[s]
		cW := newPositions[s]

		action := "UNCHANGED"
		if pW <= 0 && cW > 0 {
			action = "NEW_ENTRY"
			turnover += cW
		} else if pW > 0 && cW <= 0 {
			action = "EXITED"
			turnover += pW
		} else if pW > 0 && cW > 0 && pW != cW {
			action = "REWEIGHT"
			if cW > pW {
				turnover += (cW - pW)
			}
		} else if pW <= 0 && cW <= 0 {
			continue
		}

		items = append(items, ThemeHistoryItem{
			ThemeName:       themeName,
			Version:         newV,
			Symbol:          s,
			Action:          action,
			TargetWeight:    cW,
			PrevWeight:      pW,
			ExecutionStatus: "FILLED",
		})
	}

	rebalance := ThemeRebalance{
		ThemeName:     themeName,
		Version:       newV,
		EffectiveDate: now,
		CreatedAt:     now,
		RunID:         runID,
		Status:        "COMMITTED",
		TurnoverPct:   turnover * 100.0,
		Notes:         fmt.Sprintf("Pipeline rebalance via %s", filepath.Base(sourceCSV)),
	}

	return d.RecordRebalance(ctx, rebalance, items)
}
