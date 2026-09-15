package cache

import (
	"context"
	"database/sql"
	"time"

	"github.com/raghavkgarg/mycase/pkg/marketcal"
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
	if !isFreshFundamentals(ticker, fetchedAt) {
		return nil, time.Time{}, false, nil
	}
	return []byte(rawJSON), fetchedAt, true, nil
}

// StoreFundamentalsJSON upserts the JSON blob for ticker with the current
// timestamp. source records which provider produced the blob (e.g. "yahoo",
// "schwab"); an empty string is stored as NULL (R17 / Phase 10 provenance).
func (c *Cache) StoreFundamentalsJSON(ctx context.Context, ticker string, data []byte, source string) error {
	var src any
	if source != "" {
		src = source
	}
	_, err := c.db.ExecContext(ctx, `
		INSERT INTO fundamentals (ticker, fetched_at, raw_json, source) VALUES (?, ?, ?, ?)
		ON CONFLICT (ticker) DO UPDATE SET fetched_at = EXCLUDED.fetched_at, raw_json = EXCLUDED.raw_json, source = EXCLUDED.source`,
		ticker, time.Now().Unix(), string(data), src,
	)
	return err
}

// isFreshFundamentals reports whether cached fundamentals fetched at fetchedAt
// are still fresh: either they already include the ticker market's most recent
// settled EOD (NYSE 16:00 ET for US, NSE 21:00 IST otherwise), or they were
// fetched within the last 24 hours.
func isFreshFundamentals(ticker string, fetchedAt time.Time) bool {
	if marketcal.ClockForTicker(ticker).IsFreshEOD(fetchedAt, time.Now()) {
		return true
	}
	return time.Since(fetchedAt) < 24*time.Hour
}
