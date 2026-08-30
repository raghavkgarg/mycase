package pithistory

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/raghavkgarg/mycase/pkg/stockpicker"
)

const DefaultDBPath = "data/pit_history.db"

// DB wraps a DuckDB connection dedicated to Point-in-Time research and calibration data.
type DB struct {
	db *sql.DB
}

// Open opens (or creates) the DuckDB database at path and initializes the schema.
func Open(path string) (*DB, error) {
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

	p := &DB{db: db}
	if err := p.initSchema(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("init pit schema: %w", err)
	}
	return p, nil
}

// Close closes the underlying DuckDB connection.
func (p *DB) Close() error {
	if p.db != nil {
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
    PRIMARY KEY (as_of_date, index_name, method, ticker)
);
`

func (p *DB) initSchema(ctx context.Context) error {
	_, err := p.db.ExecContext(ctx, schemaDDL)
	return err
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
    passed_stage1, rejection_reason, raw_score, effective_score,
    composite_rs, vcp_ratio, rvol_z_score, decayed_pp, delivery_delta,
    selected, final_weight, forward_return_21d
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
`
	stmt, err := tx.PrepareContext(ctx, candidateQuery)
	if err != nil {
		return fmt.Errorf("prepare candidate insert: %w", err)
	}
	defer stmt.Close()

	for _, c := range snap.Candidates {
		_, err := stmt.ExecContext(ctx,
			snap.AsOfDate,
			snap.IndexName,
			snap.Method,
			c.Ticker,
			c.Sector,
			c.PassedStage1,
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
