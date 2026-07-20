// Package cache provides a persistent DuckDB-backed cache for Yahoo Finance
// price and fundamentals data. The caller API of pkg/yfinance is unchanged —
// the cache is wired in transparently via yfinance.SetCache.
package cache

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/duckdb/duckdb-go/v2"
)

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
`

func (c *Cache) initSchema(ctx context.Context) error {
	_, err := c.db.ExecContext(ctx, ddl)
	return err
}
