package themedb

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// TableMigrationResult summarizes the status and row count of a migrated table.
type TableMigrationResult struct {
	TableName string
	Rows      int64
	Source    string
	Status    string
}

const cacheDDL = `
CREATE TABLE IF NOT EXISTS prices (
    ticker  VARCHAR NOT NULL,
    date    DATE    NOT NULL,
    ts      BIGINT  NOT NULL,
    close   DOUBLE  NOT NULL,
    open    DOUBLE,
    volume  DOUBLE,
    source  VARCHAR,
    PRIMARY KEY (ticker, date)
);
CREATE TABLE IF NOT EXISTS fundamentals (
    ticker     VARCHAR PRIMARY KEY,
    fetched_at BIGINT  NOT NULL,
    raw_json   VARCHAR NOT NULL,
    source     VARCHAR
);
CREATE TABLE IF NOT EXISTS cache_meta (
    ticker     VARCHAR NOT NULL,
    range_key  VARCHAR NOT NULL,
    fetched_at BIGINT  NOT NULL,
    PRIMARY KEY (ticker, range_key)
);
CREATE TABLE IF NOT EXISTS pipeline_runs (
    run_id       VARCHAR PRIMARY KEY,
    started_at   BIGINT  NOT NULL,
    completed_at BIGINT,
    status       VARCHAR NOT NULL,
    portfolio    VARCHAR NOT NULL,
    method       VARCHAR NOT NULL,
    config_json  VARCHAR
);
CREATE TABLE IF NOT EXISTS index_picks (
    run_id     VARCHAR NOT NULL,
    index_name VARCHAR NOT NULL,
    ticker     VARCHAR NOT NULL,
    score      DOUBLE,
    rank       INTEGER,
    weight     DOUBLE,
    sector     VARCHAR,
    PRIMARY KEY (run_id, index_name, ticker)
);
CREATE TABLE IF NOT EXISTS proposals (
    run_id VARCHAR NOT NULL,
    stage  VARCHAR NOT NULL,
    ticker VARCHAR NOT NULL,
    weight DOUBLE  NOT NULL,
    score  DOUBLE,
    rank   INTEGER,
    sector VARCHAR,
    PRIMARY KEY (run_id, stage, ticker)
);
CREATE TABLE IF NOT EXISTS selections (
    run_id       VARCHAR NOT NULL,
    ticker       VARCHAR NOT NULL,
    weight       DOUBLE  NOT NULL,
    score        DOUBLE,
    rank         INTEGER,
    sector       VARCHAR,
    ttm_growth   DOUBLE,
    revenue_cagr DOUBLE,
    dso_delta    DOUBLE,
    rsi          DOUBLE,
    momentum_1y  DOUBLE,
    fcf_yield    DOUBLE,
    roic         DOUBLE,
    action       VARCHAR,
    prev_rank    INTEGER,
    prev_weight  DOUBLE,
    PRIMARY KEY (run_id, ticker)
);
`

const pitDDL = `
CREATE TABLE IF NOT EXISTS pit_runs (
    as_of_date         DATE,
    index_name         VARCHAR,
    method             VARCHAR,
    regime_multiplier  DOUBLE,
    total_constituents INTEGER,
    stage1_survivors   INTEGER,
    selected_count     INTEGER,
    created_at         TIMESTAMP,
    pillar4_uncalibrated BOOLEAN DEFAULT false,
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
    data_fetch_failed  BOOLEAN DEFAULT false,
    pillar4_uncalibrated BOOLEAN DEFAULT false,
    pillar4_insufficient_history BOOLEAN DEFAULT false,
    PRIMARY KEY (as_of_date, index_name, method, ticker)
);
`

// MigrateAllToMycaseDB attaches cache.db and pit_history.db read-only and copies all tables into mycase.db.
func (d *DB) MigrateAllToMycaseDB(ctx context.Context, cacheDBPath, pitDBPath string) ([]TableMigrationResult, error) {
	if cacheDBPath == "" {
		cacheDBPath = "data/cache.db"
	}
	if pitDBPath == "" {
		pitDBPath = "data/pit_history.db"
	}

	// 1. Ensure all schemas with proper PRIMARY KEY definitions exist
	if _, err := d.db.ExecContext(ctx, cacheDDL); err != nil {
		return nil, fmt.Errorf("initializing cache schema: %w", err)
	}
	if _, err := d.db.ExecContext(ctx, pitDDL); err != nil {
		return nil, fmt.Errorf("initializing pit schema: %w", err)
	}

	var results []TableMigrationResult

	// 2. Migrate cache.db tables
	if _, err := os.Stat(cacheDBPath); err == nil {
		attachQuery := fmt.Sprintf("ATTACH '%s' AS src_cache (READ_ONLY);", cacheDBPath)
		if _, err := d.db.ExecContext(ctx, attachQuery); err != nil {
			return nil, fmt.Errorf("attaching cache db (%s): %w", cacheDBPath, err)
		}

		cacheTables := []string{
			"prices",
			"fundamentals",
			"cache_meta",
			"pipeline_runs",
			"index_picks",
			"proposals",
			"selections",
		}

		for _, tbl := range cacheTables {
			copySQL := fmt.Sprintf("INSERT OR IGNORE INTO %s BY NAME SELECT * FROM src_cache.%s;", tbl, tbl)
			if _, err := d.db.ExecContext(ctx, copySQL); err != nil {
				if strings.Contains(err.Error(), "does not exist") {
					continue
				}
				d.db.ExecContext(ctx, "DETACH src_cache;")
				return nil, fmt.Errorf("copying table %s: %w", tbl, err)
			}

			var cnt int64
			d.db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s;", tbl)).Scan(&cnt)
			results = append(results, TableMigrationResult{
				TableName: tbl,
				Rows:      cnt,
				Source:    cacheDBPath,
				Status:    "MIGRATED",
			})
		}

		d.db.ExecContext(ctx, "DETACH src_cache;")
	}

	// 3. Migrate pit_history.db tables
	if _, err := os.Stat(pitDBPath); err == nil {
		attachQuery := fmt.Sprintf("ATTACH '%s' AS src_pit (READ_ONLY);", pitDBPath)
		if _, err := d.db.ExecContext(ctx, attachQuery); err != nil {
			return nil, fmt.Errorf("attaching pit db (%s): %w", pitDBPath, err)
		}

		pitTables := []string{
			"pit_runs",
			"pit_candidate_scores",
		}

		for _, tbl := range pitTables {
			copySQL := fmt.Sprintf("INSERT OR IGNORE INTO %s BY NAME SELECT * FROM src_pit.%s;", tbl, tbl)
			if _, err := d.db.ExecContext(ctx, copySQL); err != nil {
				if strings.Contains(err.Error(), "does not exist") {
					continue
				}
				d.db.ExecContext(ctx, "DETACH src_pit;")
				return nil, fmt.Errorf("copying pit table %s: %w", tbl, err)
			}

			var cnt int64
			d.db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s;", tbl)).Scan(&cnt)
			results = append(results, TableMigrationResult{
				TableName: tbl,
				Rows:      cnt,
				Source:    pitDBPath,
				Status:    "MIGRATED",
			})
		}

		d.db.ExecContext(ctx, "DETACH src_pit;")
	}

	// 4. Include theme tables in result
	var themeRebCount, themeHistCount int64
	d.db.QueryRowContext(ctx, "SELECT count(*) FROM theme_rebalances;").Scan(&themeRebCount)
	results = append(results, TableMigrationResult{
		TableName: "theme_rebalances",
		Rows:      themeRebCount,
		Source:    d.dbPath,
		Status:    "ACTIVE",
	})

	d.db.QueryRowContext(ctx, "SELECT count(*) FROM theme_history;").Scan(&themeHistCount)
	results = append(results, TableMigrationResult{
		TableName: "theme_history",
		Rows:      themeHistCount,
		Source:    d.dbPath,
		Status:    "ACTIVE",
	})

	return results, nil
}

// GetDatabaseStats returns row counts for all tables currently in mycase.db.
func (d *DB) GetDatabaseStats(ctx context.Context) ([]TableMigrationResult, error) {
	rows, err := d.db.QueryContext(ctx, "SHOW TABLES;")
	if err != nil {
		return nil, fmt.Errorf("querying tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tbl string
		if err := rows.Scan(&tbl); err != nil {
			return nil, err
		}
		tables = append(tables, tbl)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	var stats []TableMigrationResult
	for _, tbl := range tables {
		var cnt int64
		if err := d.db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s;", tbl)).Scan(&cnt); err == nil {
			stats = append(stats, TableMigrationResult{
				TableName: tbl,
				Rows:      cnt,
				Source:    d.dbPath,
				Status:    "ONLINE",
			})
		}
	}
	return stats, nil
}
