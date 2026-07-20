package cache

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// PriceRecord is a single daily price row as stored in the cache.
type PriceRecord struct {
	Timestamp int64
	Close     float64
	Open      float64
	Volume    float64
}

// GetPrices returns cached price records for (ticker, rangeKey) if the data
// is still fresh (fetched today IST). Returns (nil, false, nil) on a cache
// miss or stale entry.
func (c *Cache) GetPrices(ctx context.Context, ticker, rangeKey string) ([]PriceRecord, bool, error) {
	var fetchedAtUnix int64
	err := c.db.QueryRowContext(ctx,
		`SELECT fetched_at FROM cache_meta WHERE ticker = ? AND range_key = ?`,
		ticker, rangeKey,
	).Scan(&fetchedAtUnix)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !isFreshToday(time.Unix(fetchedAtUnix, 0)) {
		return nil, false, nil
	}

	startDate := rangeKeyToStartDate(rangeKey)
	rows, err := c.db.QueryContext(ctx,
		`SELECT ts, close, open, volume FROM prices
		 WHERE ticker = ? AND date >= ?
		 ORDER BY date ASC`,
		ticker, startDate.Format("2006-01-02"),
	)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var records []PriceRecord
	for rows.Next() {
		var r PriceRecord
		if err := rows.Scan(&r.Timestamp, &r.Close, &r.Open, &r.Volume); err != nil {
			return nil, false, err
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(records) == 0 {
		return nil, false, nil
	}
	return records, true, nil
}

// StorePrices upserts price rows for ticker and records the fetch time in cache_meta.
func (c *Cache) StorePrices(ctx context.Context, ticker, rangeKey string, records []PriceRecord) error {
	if len(records) == 0 {
		return nil
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, r := range records {
		date := time.Unix(r.Timestamp, 0).UTC().Format("2006-01-02")
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO prices (ticker, date, ts, close, open, volume) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (ticker, date) DO UPDATE SET
				ts = EXCLUDED.ts, close = EXCLUDED.close,
				open = EXCLUDED.open, volume = EXCLUDED.volume`,
			ticker, date, r.Timestamp, r.Close, r.Open, r.Volume,
		); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO cache_meta (ticker, range_key, fetched_at) VALUES (?, ?, ?)
		ON CONFLICT (ticker, range_key) DO UPDATE SET fetched_at = EXCLUDED.fetched_at`,
		ticker, rangeKey, time.Now().Unix(),
	); err != nil {
		return err
	}

	return tx.Commit()
}

func isFreshToday(fetchedAt time.Time) bool {
	ist := time.FixedZone("IST", 5*3600+30*60)
	nowIST := time.Now().In(ist)
	f := fetchedAt.In(ist)
	return f.Year() == nowIST.Year() && f.YearDay() == nowIST.YearDay()
}

// GetPricesByDateRange returns cached price records for [from, to] for a ticker.
// Historical ranges (to before today IST) never expire; ranges ending today use
// same-day freshness.
func (c *Cache) GetPricesByDateRange(ctx context.Context, ticker string, from, to time.Time) ([]PriceRecord, bool, error) {
	rangeKey := fmt.Sprintf("dr_%s_%s", from.Format("20060102"), to.Format("20060102"))

	var fetchedAt int64
	err := c.db.QueryRowContext(ctx,
		`SELECT fetched_at FROM cache_meta WHERE ticker = ? AND range_key = ?`,
		ticker, rangeKey,
	).Scan(&fetchedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	ist := time.FixedZone("IST", 5*3600+30*60)
	todayIST := time.Now().In(ist).Truncate(24 * time.Hour)
	toIST := to.In(ist).Truncate(24 * time.Hour)
	// Only apply freshness check when the range ends today (historical = always fresh)
	if !toIST.Before(todayIST) && !isFreshToday(time.Unix(fetchedAt, 0)) {
		return nil, false, nil
	}

	rows, err := c.db.QueryContext(ctx,
		`SELECT ts, close, open, volume FROM prices
		 WHERE ticker = ? AND date >= ? AND date <= ?
		 ORDER BY date ASC`,
		ticker, from.Format("2006-01-02"), to.Format("2006-01-02"),
	)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var records []PriceRecord
	for rows.Next() {
		var r PriceRecord
		if err := rows.Scan(&r.Timestamp, &r.Close, &r.Open, &r.Volume); err != nil {
			return nil, false, err
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(records) == 0 {
		return nil, false, nil
	}
	return records, true, nil
}

// StorePricesByDateRange upserts price rows and marks the date range as fetched in cache_meta.
func (c *Cache) StorePricesByDateRange(ctx context.Context, ticker string, from, to time.Time, records []PriceRecord) error {
	rangeKey := fmt.Sprintf("dr_%s_%s", from.Format("20060102"), to.Format("20060102"))
	return c.StorePrices(ctx, ticker, rangeKey, records)
}

func rangeKeyToStartDate(rangeKey string) time.Time {
	now := time.Now().UTC()
	switch rangeKey {
	case "1d":
		return now.AddDate(0, 0, -1)
	case "7d":
		return now.AddDate(0, 0, -7)
	case "1mo":
		return now.AddDate(0, -1, 0)
	case "3mo":
		return now.AddDate(0, -3, 0)
	case "6mo":
		return now.AddDate(0, -6, 0)
	case "1y":
		return now.AddDate(-1, 0, 0)
	case "2y":
		return now.AddDate(-2, 0, 0)
	case "5y":
		return now.AddDate(-5, 0, 0)
	default:
		return now.AddDate(0, -3, 0)
	}
}
