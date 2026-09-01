// Package cache provides a persistent DuckDB-backed cache for price,
// fundamentals, and pipeline data. The global singleton is set once at startup
// and accessed via cache.GetDB() from any package.
package cache

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/duckdb/duckdb-go/v2"
)

// globalDB holds the application-wide DuckDB cache instance.
var globalDB *Cache

// SetGlobal sets the application-wide DuckDB cache singleton.
// Call once from main after Open.
func SetGlobal(c *Cache) { globalDB = c }

// GetDB returns the application-wide DuckDB cache (nil if not set).
func GetDB() *Cache { return globalDB }

// Cache wraps a DuckDB connection for price and fundamentals storage.
type Cache struct {
	db *sql.DB
}

// Open opens (or creates) the DuckDB cache at path and initialises the schema.
func Open(path string) (*Cache, error) {
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, fmt.Errorf("open cache db: %w", err)
	}
	// DuckDB supports one writer at a time; keep the pool at 1.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping cache db: %w", err)
	}
	c := &Cache{db: db}
	if err := c.initSchema(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("init cache schema: %w", err)
	}
	return c, nil
}

// Close closes the underlying database connection.
func (c *Cache) Close() error { return c.db.Close() }

// Conn returns the underlying *sql.DB so that domain packages can own their
// persistence (define their own tables + queries) without pkg/cache importing
// them. This keeps the dependency direction one-way: domain → cache, never
// cache → domain (see docs/refactor.md R16, problem P4). Callers must use
// CREATE TABLE IF NOT EXISTS and treat the single-writer connection accordingly.
func (c *Cache) Conn() *sql.DB { return c.db }

const ddl = `
CREATE TABLE IF NOT EXISTS prices (
    ticker  VARCHAR NOT NULL,
    date    DATE    NOT NULL,
    ts      BIGINT  NOT NULL,
    close   DOUBLE  NOT NULL,
    open    DOUBLE,
    volume  DOUBLE,
    PRIMARY KEY (ticker, date)
);
CREATE TABLE IF NOT EXISTS fundamentals (
    ticker     VARCHAR PRIMARY KEY,
    fetched_at BIGINT  NOT NULL,
    raw_json   VARCHAR NOT NULL
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

func (c *Cache) initSchema(ctx context.Context) error {
	_, err := c.db.ExecContext(ctx, ddl)
	return err
}
