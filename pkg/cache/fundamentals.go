package cache

import (
	"context"
	"database/sql"
	"time"
)

// GetFundamentalsJSON returns the cached JSON blob for ticker if it was
// fetched within the last 24 hours. Returns (nil, false, nil) on miss/stale.
func (c *Cache) GetFundamentalsJSON(ctx context.Context, ticker string) ([]byte, bool, error) {
	data, _, ok, err := c.GetFundamentalsJSONWithTime(ctx, ticker)
	return data, ok, err
}

// GetFundamentalsJSONWithTime returns the cached JSON blob and the fetched timestamp for ticker
// if it was fetched within the last 24 hours. Returns (nil, time.Time{}, false, nil) on miss/stale.
func (c *Cache) GetFundamentalsJSONWithTime(ctx context.Context, ticker string) ([]byte, time.Time, bool, error) {
	var fetchedAtUnix int64
	var rawJSON string
	err := c.db.QueryRowContext(ctx,
		`SELECT fetched_at, raw_json FROM fundamentals WHERE ticker = ?`,
		ticker,
	).Scan(&fetchedAtUnix, &rawJSON)
	if err == sql.ErrNoRows {
		return nil, time.Time{}, false, nil
	}
	if err != nil {
		return nil, time.Time{}, false, err
	}
	fetchedAt := time.Unix(fetchedAtUnix, 0)
	if !isFreshFundamentals(fetchedAt) {
		return nil, time.Time{}, false, nil
	}
	return []byte(rawJSON), fetchedAt, true, nil
}

// StoreFundamentalsJSON upserts the JSON blob for ticker with the current timestamp.
func (c *Cache) StoreFundamentalsJSON(ctx context.Context, ticker string, data []byte) error {
	_, err := c.db.ExecContext(ctx, `
		INSERT INTO fundamentals (ticker, fetched_at, raw_json) VALUES (?, ?, ?)
		ON CONFLICT (ticker) DO UPDATE SET fetched_at = EXCLUDED.fetched_at, raw_json = EXCLUDED.raw_json`,
		ticker, time.Now().Unix(), string(data),
	)
	return err
}

func isFreshFundamentals(fetchedAt time.Time) bool {
	return time.Since(fetchedAt) < 24*time.Hour
}
