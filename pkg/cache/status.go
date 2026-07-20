package cache

import (
	"context"
	"time"
)

// StatusEntry summarises one cache entry for display.
type StatusEntry struct {
	Kind      string // "prices" or "fundamentals"
	Ticker    string
	RangeKey  string
	FetchedAt time.Time
	Rows      int
}

// Status returns a summary of all cached entries.
func (c *Cache) Status(ctx context.Context) ([]StatusEntry, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT m.ticker, m.range_key, m.fetched_at,
		       (SELECT COUNT(*) FROM prices p
		        WHERE p.ticker = m.ticker
		          AND p.date >= CAST(? AS DATE)) AS row_count
		FROM cache_meta m
		ORDER BY m.ticker, m.range_key`,
		time.Now().AddDate(-5, 0, 0).Format("2006-01-02"),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []StatusEntry
	for rows.Next() {
		var e StatusEntry
		var fetchedAtUnix int64
		e.Kind = "prices"
		if err := rows.Scan(&e.Ticker, &e.RangeKey, &fetchedAtUnix, &e.Rows); err != nil {
			return nil, err
		}
		e.FetchedAt = time.Unix(fetchedAtUnix, 0)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	frows, err := c.db.QueryContext(ctx,
		`SELECT ticker, fetched_at FROM fundamentals ORDER BY ticker`,
	)
	if err != nil {
		return nil, err
	}
	defer frows.Close()
	for frows.Next() {
		var e StatusEntry
		var fetchedAtUnix int64
		e.Kind = "fundamentals"
		e.RangeKey = "—"
		e.Rows = 1
		if err := frows.Scan(&e.Ticker, &fetchedAtUnix); err != nil {
			return nil, err
		}
		e.FetchedAt = time.Unix(fetchedAtUnix, 0)
		entries = append(entries, e)
	}
	return entries, frows.Err()
}

// ClearTicker removes all cached data for a specific ticker.
func (c *Cache) ClearTicker(ctx context.Context, ticker string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM prices WHERE ticker = ?`,
		`DELETE FROM fundamentals WHERE ticker = ?`,
		`DELETE FROM cache_meta WHERE ticker = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, ticker); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClearAll removes all data from the cache.
func (c *Cache) ClearAll(ctx context.Context) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM prices`,
		`DELETE FROM fundamentals`,
		`DELETE FROM cache_meta`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return tx.Commit()
}
