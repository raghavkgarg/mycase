package cache

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// openTestCache opens a fresh Cache in t.TempDir() and registers cleanup.
func openTestCache(t *testing.T) *Cache {
	t.Helper()
	c, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

var ctx = context.Background()

// ---------------------------------------------------------------------------
// Schema idempotence — data must survive Open → Close → Open
// ---------------------------------------------------------------------------

func TestOpen_SchemaIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	// First open: write a price row.
	c1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	err = c1.StorePrices(ctx, "NSE:TCS", "3mo", []PriceRecord{
		{Timestamp: 1_700_000_000, Close: 3500.0, Open: 3480.0, Volume: 50000},
	})
	if err != nil {
		t.Fatalf("StorePrices: %v", err)
	}
	c1.Close()

	// Second open on the same file: CREATE TABLE IF NOT EXISTS must NOT wipe the row.
	c2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer c2.Close()

	var n int
	err = c2.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM prices`).Scan(&n)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 price row to survive reopen, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Prices — basic round-trip, type accuracy, upsert, staleness, range filter
// ---------------------------------------------------------------------------

func TestStorePrices_Basic(t *testing.T) {
	c := openTestCache(t)

	now := time.Now()
	records := []PriceRecord{
		{Timestamp: now.Add(-48 * time.Hour).Unix(), Close: 3500.25, Open: 3480.50, Volume: 50_000.0},
		{Timestamp: now.Add(-24 * time.Hour).Unix(), Close: 3510.75, Open: 3501.00, Volume: 48_000.0},
	}
	if err := c.StorePrices(ctx, "NSE:TCS", "3mo", records); err != nil {
		t.Fatalf("StorePrices: %v", err)
	}

	got, fresh, err := c.GetPrices(ctx, "NSE:TCS", "3mo")
	if err != nil {
		t.Fatalf("GetPrices: %v", err)
	}
	if !fresh {
		t.Fatal("expected fresh=true for data just stored")
	}
	if len(got) != len(records) {
		t.Fatalf("expected %d records, got %d", len(records), len(got))
	}
}

func TestStorePrices_TypeAccuracy(t *testing.T) {
	c := openTestCache(t)

	// Use values that would expose int64 truncation or float precision loss.
	// Timestamp must be recent so it falls within the "1y" range window.
	want := PriceRecord{
		Timestamp: time.Now().Add(-24 * time.Hour).Unix(),
		Close:     1234567.89, // float64 with decimals
		Open:      1234567.12,
		Volume:    9_999_999_999.99, // large float
	}
	if err := c.StorePrices(ctx, "NSE:RELIANCE", "1y", []PriceRecord{want}); err != nil {
		t.Fatalf("StorePrices: %v", err)
	}

	got, fresh, err := c.GetPrices(ctx, "NSE:RELIANCE", "1y")
	if err != nil || !fresh || len(got) != 1 {
		t.Fatalf("GetPrices: fresh=%v err=%v len=%d", fresh, err, len(got))
	}
	r := got[0]
	if r.Timestamp != want.Timestamp {
		t.Errorf("Timestamp: got %d want %d", r.Timestamp, want.Timestamp)
	}
	if r.Close != want.Close {
		t.Errorf("Close: got %v want %v", r.Close, want.Close)
	}
	if r.Open != want.Open {
		t.Errorf("Open: got %v want %v", r.Open, want.Open)
	}
	if r.Volume != want.Volume {
		t.Errorf("Volume: got %v want %v", r.Volume, want.Volume)
	}
}

func TestStorePrices_Upsert(t *testing.T) {
	c := openTestCache(t)

	ts := int64(1_700_000_000)
	original := PriceRecord{Timestamp: ts, Close: 100.0, Open: 99.0, Volume: 1000}
	updated := PriceRecord{Timestamp: ts, Close: 200.0, Open: 198.0, Volume: 2000}

	if err := c.StorePrices(ctx, "NSE:TCS", "3mo", []PriceRecord{original}); err != nil {
		t.Fatalf("first StorePrices: %v", err)
	}

	// Force a fresh upsert by back-dating cache_meta so GetPrices would re-fetch,
	// but here we just verify the prices table row was updated.
	if err := c.StorePrices(ctx, "NSE:TCS", "3mo", []PriceRecord{updated}); err != nil {
		t.Fatalf("second StorePrices (upsert): %v", err)
	}

	// Directly check the prices table — should have exactly 1 row with the new values.
	var rowCount int
	c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM prices WHERE ticker = 'NSE:TCS'`).Scan(&rowCount)
	if rowCount != 1 {
		t.Errorf("expected 1 row after upsert, got %d (duplicate inserted instead of replaced)", rowCount)
	}

	var gotClose float64
	c.db.QueryRowContext(ctx, `SELECT close FROM prices WHERE ticker = 'NSE:TCS'`).Scan(&gotClose)
	if gotClose != 200.0 {
		t.Errorf("upsert did not update: got close=%v, want 200.0", gotClose)
	}
}

func TestGetPrices_Miss(t *testing.T) {
	c := openTestCache(t)

	records, fresh, err := c.GetPrices(ctx, "NSE:UNKNOWN", "3mo")
	if err != nil {
		t.Fatalf("unexpected error on cache miss: %v", err)
	}
	if fresh {
		t.Error("expected fresh=false on miss")
	}
	if records != nil {
		t.Errorf("expected nil records on miss, got %v", records)
	}
}

func TestGetPrices_Stale(t *testing.T) {
	c := openTestCache(t)

	ts := int64(time.Now().Add(-48 * time.Hour).Unix())
	if err := c.StorePrices(ctx, "NSE:TCS", "3mo", []PriceRecord{
		{Timestamp: ts, Close: 100.0, Open: 99.0, Volume: 1000},
	}); err != nil {
		t.Fatalf("StorePrices: %v", err)
	}

	// Back-date cache_meta to 2 days ago.
	_, err := c.db.ExecContext(ctx,
		`UPDATE cache_meta SET fetched_at = ? WHERE ticker = 'NSE:TCS' AND range_key = '3mo'`,
		time.Now().AddDate(0, 0, -2).Unix())
	if err != nil {
		t.Fatalf("backdating cache_meta: %v", err)
	}

	_, fresh, err := c.GetPrices(ctx, "NSE:TCS", "3mo")
	if err != nil {
		t.Fatalf("GetPrices: %v", err)
	}
	if fresh {
		t.Error("expected fresh=false for yesterday's data")
	}
}

func TestGetPrices_RangeFilter(t *testing.T) {
	c := openTestCache(t)

	now := time.Now().UTC()
	// One recent record (within 3mo) and one very old record (outside 3mo).
	recent := PriceRecord{
		Timestamp: now.AddDate(0, -1, 0).Unix(),
		Close:     100.0, Open: 99.0, Volume: 1000,
	}
	old := PriceRecord{
		Timestamp: now.AddDate(-1, 0, 0).Unix(), // 1 year ago — outside 3mo window
		Close:     50.0, Open: 49.0, Volume: 500,
	}

	if err := c.StorePrices(ctx, "NSE:TCS", "3mo", []PriceRecord{recent, old}); err != nil {
		t.Fatalf("StorePrices: %v", err)
	}

	got, fresh, err := c.GetPrices(ctx, "NSE:TCS", "3mo")
	if err != nil {
		t.Fatalf("GetPrices: %v", err)
	}
	if !fresh {
		t.Fatal("expected fresh=true")
	}
	if len(got) != 1 {
		t.Errorf("expected 1 record within 3mo window, got %d (range filter not working)", len(got))
	}
	if len(got) == 1 && got[0].Close != 100.0 {
		t.Errorf("wrong record returned: got close=%v, want 100.0", got[0].Close)
	}
}

func TestGetPrices_AllRowsOutsideRange(t *testing.T) {
	c := openTestCache(t)

	// Store a row that's 2 years old — outside every range window.
	ts := time.Now().AddDate(-2, -1, 0).Unix()
	if err := c.StorePrices(ctx, "NSE:TCS", "3mo", []PriceRecord{
		{Timestamp: ts, Close: 50.0, Open: 49.0, Volume: 500},
	}); err != nil {
		t.Fatalf("StorePrices: %v", err)
	}

	got, fresh, err := c.GetPrices(ctx, "NSE:TCS", "3mo")
	if err != nil {
		t.Fatalf("GetPrices: %v", err)
	}
	// cache_meta is fresh (stored today), but no rows match the date range.
	if fresh {
		t.Error("expected fresh=false when no rows fall within range window")
	}
	if got != nil {
		t.Errorf("expected nil records, got %v", got)
	}
}

// cache_meta upsert — repeated StorePrices must not grow cache_meta.
func TestCacheMeta_Upsert(t *testing.T) {
	c := openTestCache(t)

	ts := time.Now().Add(-24 * time.Hour).Unix()
	row := PriceRecord{Timestamp: ts, Close: 100.0, Open: 99.0, Volume: 1000}

	for range 3 {
		if err := c.StorePrices(ctx, "NSE:TCS", "3mo", []PriceRecord{row}); err != nil {
			t.Fatalf("StorePrices: %v", err)
		}
	}

	var n int
	c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cache_meta WHERE ticker = 'NSE:TCS'`).Scan(&n)
	if n != 1 {
		t.Errorf("expected 1 cache_meta row after 3 stores, got %d (upsert not working)", n)
	}
}

// ---------------------------------------------------------------------------
// Fundamentals — round-trip, upsert, staleness
// ---------------------------------------------------------------------------

func TestStoreFundamentals_Basic(t *testing.T) {
	c := openTestCache(t)

	want := []byte(`{"Sector":"Technology","ForwardPE":25.5,"ROE":0.42}`)
	if err := c.StoreFundamentalsJSON(ctx, "NSE:TCS", want); err != nil {
		t.Fatalf("StoreFundamentalsJSON: %v", err)
	}

	got, fresh, err := c.GetFundamentalsJSON(ctx, "NSE:TCS")
	if err != nil {
		t.Fatalf("GetFundamentalsJSON: %v", err)
	}
	if !fresh {
		t.Fatal("expected fresh=true for data just stored")
	}
	if string(got) != string(want) {
		t.Errorf("JSON mismatch: got %s, want %s", got, want)
	}
}

func TestStoreFundamentals_Upsert(t *testing.T) {
	c := openTestCache(t)

	if err := c.StoreFundamentalsJSON(ctx, "NSE:TCS", []byte(`{"ForwardPE":20.0}`)); err != nil {
		t.Fatalf("first store: %v", err)
	}
	if err := c.StoreFundamentalsJSON(ctx, "NSE:TCS", []byte(`{"ForwardPE":25.0}`)); err != nil {
		t.Fatalf("second store (upsert): %v", err)
	}

	// Must be exactly 1 row.
	var n int
	c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fundamentals WHERE ticker = 'NSE:TCS'`).Scan(&n)
	if n != 1 {
		t.Errorf("expected 1 row after upsert, got %d", n)
	}

	got, _, _ := c.GetFundamentalsJSON(ctx, "NSE:TCS")
	if string(got) != `{"ForwardPE":25.0}` {
		t.Errorf("upsert did not update JSON: got %s", got)
	}
}

func TestGetFundamentals_Miss(t *testing.T) {
	c := openTestCache(t)

	data, fresh, err := c.GetFundamentalsJSON(ctx, "NSE:UNKNOWN")
	if err != nil {
		t.Fatalf("unexpected error on miss: %v", err)
	}
	if fresh {
		t.Error("expected fresh=false on miss")
	}
	if data != nil {
		t.Errorf("expected nil data on miss, got %s", data)
	}
}

func TestGetFundamentals_Stale(t *testing.T) {
	c := openTestCache(t)

	if err := c.StoreFundamentalsJSON(ctx, "NSE:TCS", []byte(`{}`)); err != nil {
		t.Fatalf("StoreFundamentalsJSON: %v", err)
	}

	// Back-date to 25 hours ago.
	_, err := c.db.ExecContext(ctx,
		`UPDATE fundamentals SET fetched_at = ? WHERE ticker = 'NSE:TCS'`,
		time.Now().Add(-25*time.Hour).Unix())
	if err != nil {
		t.Fatalf("backdating fundamentals: %v", err)
	}

	_, fresh, err := c.GetFundamentalsJSON(ctx, "NSE:TCS")
	if err != nil {
		t.Fatalf("GetFundamentalsJSON: %v", err)
	}
	if fresh {
		t.Error("expected fresh=false for 25h-old fundamentals")
	}
}

// ---------------------------------------------------------------------------
// Status and clear
// ---------------------------------------------------------------------------

func TestStatus_Empty(t *testing.T) {
	c := openTestCache(t)

	entries, err := c.Status(ctx)
	if err != nil {
		t.Fatalf("Status on empty cache: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries on empty cache, got %d", len(entries))
	}
}

func TestStatus_WithData(t *testing.T) {
	c := openTestCache(t)

	now := time.Now().UTC()
	ts := now.Add(-48 * time.Hour).Unix()
	if err := c.StorePrices(ctx, "NSE:TCS", "3mo", []PriceRecord{
		{Timestamp: ts, Close: 100.0, Open: 99.0, Volume: 1000},
	}); err != nil {
		t.Fatalf("StorePrices: %v", err)
	}
	if err := c.StoreFundamentalsJSON(ctx, "NSE:RELIANCE", []byte(`{}`)); err != nil {
		t.Fatalf("StoreFundamentalsJSON: %v", err)
	}

	entries, err := c.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	var priceEntries, fundEntries int
	for _, e := range entries {
		switch e.Kind {
		case "prices":
			priceEntries++
			if e.Ticker != "NSE:TCS" || e.RangeKey != "3mo" {
				t.Errorf("unexpected price entry: %+v", e)
			}
		case "fundamentals":
			fundEntries++
			if e.Ticker != "NSE:RELIANCE" {
				t.Errorf("unexpected fundamentals entry: %+v", e)
			}
		}
	}
	if priceEntries != 1 {
		t.Errorf("expected 1 price entry, got %d", priceEntries)
	}
	if fundEntries != 1 {
		t.Errorf("expected 1 fundamentals entry, got %d", fundEntries)
	}
}

func TestClearTicker_RemovesOnlyTarget(t *testing.T) {
	c := openTestCache(t)

	ts := time.Now().Add(-24 * time.Hour).Unix()
	for _, ticker := range []string{"NSE:TCS", "NSE:INFY"} {
		if err := c.StorePrices(ctx, ticker, "3mo", []PriceRecord{
			{Timestamp: ts, Close: 100.0, Open: 99.0, Volume: 1000},
		}); err != nil {
			t.Fatalf("StorePrices %s: %v", ticker, err)
		}
		if err := c.StoreFundamentalsJSON(ctx, ticker, []byte(`{}`)); err != nil {
			t.Fatalf("StoreFundamentalsJSON %s: %v", ticker, err)
		}
	}

	if err := c.ClearTicker(ctx, "NSE:TCS"); err != nil {
		t.Fatalf("ClearTicker: %v", err)
	}

	// TCS rows must be gone from all tables.
	for _, q := range []string{
		`SELECT COUNT(*) FROM prices WHERE ticker = 'NSE:TCS'`,
		`SELECT COUNT(*) FROM fundamentals WHERE ticker = 'NSE:TCS'`,
		`SELECT COUNT(*) FROM cache_meta WHERE ticker = 'NSE:TCS'`,
	} {
		var n int
		c.db.QueryRowContext(ctx, q).Scan(&n)
		if n != 0 {
			t.Errorf("ClearTicker left %d rows for NSE:TCS in query: %s", n, q)
		}
	}

	// INFY must be untouched.
	for _, q := range []string{
		`SELECT COUNT(*) FROM prices WHERE ticker = 'NSE:INFY'`,
		`SELECT COUNT(*) FROM fundamentals WHERE ticker = 'NSE:INFY'`,
		`SELECT COUNT(*) FROM cache_meta WHERE ticker = 'NSE:INFY'`,
	} {
		var n int
		c.db.QueryRowContext(ctx, q).Scan(&n)
		if n != 1 {
			t.Errorf("ClearTicker removed NSE:INFY rows (should be untouched), query: %s", q)
		}
	}
}

func TestClearAll(t *testing.T) {
	c := openTestCache(t)

	ts := time.Now().Add(-24 * time.Hour).Unix()
	for _, ticker := range []string{"NSE:TCS", "NSE:INFY", "NSE:RELIANCE"} {
		c.StorePrices(ctx, ticker, "3mo", []PriceRecord{{Timestamp: ts, Close: 100.0}})
		c.StoreFundamentalsJSON(ctx, ticker, []byte(`{}`))
	}

	if err := c.ClearAll(ctx); err != nil {
		t.Fatalf("ClearAll: %v", err)
	}

	for _, q := range []string{
		`SELECT COUNT(*) FROM prices`,
		`SELECT COUNT(*) FROM fundamentals`,
		`SELECT COUNT(*) FROM cache_meta`,
	} {
		var n int
		c.db.QueryRowContext(ctx, q).Scan(&n)
		if n != 0 {
			t.Errorf("ClearAll left %d rows in: %s", n, q)
		}
	}
}

// ---------------------------------------------------------------------------
// Staleness helpers
// ---------------------------------------------------------------------------

func TestIsFreshToday(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+30*60)
	nowIST := time.Now().In(ist)

	tests := []struct {
		name      string
		fetchedAt time.Time
		want      bool
	}{
		{"just now", time.Now(), true},
		{"start of today IST", time.Date(nowIST.Year(), nowIST.Month(), nowIST.Day(), 0, 0, 0, 0, ist), true},
		{"yesterday", time.Now().AddDate(0, 0, -1), false},
		{"last week", time.Now().AddDate(0, 0, -7), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFreshToday(tc.fetchedAt); got != tc.want {
				t.Errorf("isFreshToday(%v) = %v, want %v", tc.fetchedAt, got, tc.want)
			}
		})
	}
}

func TestIsFreshFundamentals(t *testing.T) {
	tests := []struct {
		name      string
		fetchedAt time.Time
		want      bool
	}{
		{"just now", time.Now(), true},
		{"23h ago", time.Now().Add(-23 * time.Hour), true},
		{"exactly 24h ago", time.Now().Add(-24 * time.Hour), false},
		{"25h ago", time.Now().Add(-25 * time.Hour), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFreshFundamentals(tc.fetchedAt); got != tc.want {
				t.Errorf("isFreshFundamentals(%v) = %v, want %v", tc.fetchedAt, got, tc.want)
			}
		})
	}
}

func TestRangeKeyToStartDate(t *testing.T) {
	// rangeKeyToStartDate uses calendar arithmetic (time.AddDate), so the exact
	// number of days a key spans depends on which months/years it lands across
	// (a "6mo" range can be 181–184 days). Assert against the same calendar
	// arithmetic with a tight tolerance rather than fixed day-count windows,
	// which would drift with the calendar (this used to make "6mo" flaky).
	const tol = 2 * time.Hour // allowance for the now() gap between test and impl
	now := time.Now().UTC()
	tests := []struct {
		key  string
		want time.Time
	}{
		{"1d", now.AddDate(0, 0, -1)},
		{"7d", now.AddDate(0, 0, -7)},
		{"1mo", now.AddDate(0, -1, 0)},
		{"3mo", now.AddDate(0, -3, 0)},
		{"6mo", now.AddDate(0, -6, 0)},
		{"1y", now.AddDate(-1, 0, 0)},
		{"2y", now.AddDate(-2, 0, 0)},
		{"5y", now.AddDate(-5, 0, 0)},
		{"unknown", now.AddDate(0, -3, 0)}, // default falls back to 3mo
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			start := rangeKeyToStartDate(tc.key)
			if diff := start.Sub(tc.want); diff < -tol || diff > tol {
				t.Errorf("rangeKeyToStartDate(%q) = %v, want ~%v (diff %v, tol %v)",
					tc.key, start, tc.want, diff, tol)
			}
		})
	}
}
