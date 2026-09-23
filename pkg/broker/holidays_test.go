package broker

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/raghavkgarg/mycase/pkg/cache"
)

func TestDBHolidayProvider_UpsertAndQuery(t *testing.T) {
	c, err := cache.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	p := NewDBHolidayProvider(c.Conn())
	ctx := context.Background()

	if err := p.UpsertHolidays(ctx, "NYSE", []string{"2026-01-01", "2026-07-03", "2026-01-01"}); err != nil {
		t.Fatalf("upsert NYSE: %v", err)
	}
	if err := p.UpsertHolidays(ctx, "NSE", []string{"2026-01-26"}); err != nil {
		t.Fatalf("upsert NSE: %v", err)
	}
	// Re-upsert a duplicate — ON CONFLICT DO NOTHING keeps the set stable.
	if err := p.UpsertHolidays(ctx, "NYSE", []string{"2026-01-01"}); err != nil {
		t.Fatalf("re-upsert NYSE: %v", err)
	}

	nyse := p.Holidays("NYSE")
	if len(nyse) != 2 || nyse[0] != "2026-01-01" || nyse[1] != "2026-07-03" {
		t.Fatalf("NYSE holidays = %v, want [2026-01-01 2026-07-03]", nyse)
	}
	if got := p.Holidays("NSE"); len(got) != 1 || got[0] != "2026-01-26" {
		t.Fatalf("NSE holidays = %v, want [2026-01-26]", got)
	}
	if got := p.Holidays("LSE"); len(got) != 0 {
		t.Fatalf("unknown exchange = %v, want empty", got)
	}
}

func TestDBHolidayProvider_NilHandle(t *testing.T) {
	p := NewDBHolidayProvider(nil)
	if got := p.Holidays("NYSE"); got != nil {
		t.Fatalf("nil-handle Holidays = %v, want nil (persistence disabled)", got)
	}
	// Upsert on a nil handle is a no-op, not an error.
	if err := p.UpsertHolidays(context.Background(), "NYSE", []string{"2026-01-01"}); err != nil {
		t.Fatalf("nil-handle upsert should be no-op, got %v", err)
	}
	if got := p.Count(context.Background(), "NYSE"); got != -1 {
		t.Fatalf("nil-handle Count = %d, want -1 (couldn't check)", got)
	}
}

func TestDBHolidayProvider_CountAndStatus(t *testing.T) {
	c, err := cache.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	p := NewDBHolidayProvider(c.Conn())
	ctx := context.Background()

	// Empty table: Count is 0 (checked, none), status has no rows.
	if got := p.Count(ctx, "NYSE"); got != 0 {
		t.Fatalf("empty Count = %d, want 0", got)
	}
	stats, err := p.HolidayStatus(ctx)
	if err != nil {
		t.Fatalf("HolidayStatus: %v", err)
	}
	if len(stats) != 0 {
		t.Fatalf("empty status = %v, want none", stats)
	}

	if err := p.UpsertHolidays(ctx, "NYSE", []string{"2026-12-25", "2026-01-01"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got := p.Count(ctx, "NYSE"); got != 2 {
		t.Fatalf("Count after seed = %d, want 2", got)
	}
	stats, err = p.HolidayStatus(ctx)
	if err != nil {
		t.Fatalf("HolidayStatus: %v", err)
	}
	if len(stats) != 1 || stats[0].Exchange != "NYSE" || stats[0].Count != 2 {
		t.Fatalf("status = %+v, want one NYSE row count 2", stats)
	}
	if stats[0].MinDate != "2026-01-01" || stats[0].MaxDate != "2026-12-25" {
		t.Errorf("status date range = %s..%s, want 2026-01-01..2026-12-25", stats[0].MinDate, stats[0].MaxDate)
	}
}

// selectHolidayProvider always yields a DB-backed provider. With no cache DB
// open it returns a nil-handle provider that degrades to weekend-only (no
// holidays), never a broken clock.
func TestSelectHolidayProvider_DBOnly(t *testing.T) {
	cache.SetGlobal(nil)
	if got := selectHolidayProvider().Holidays("NYSE"); got != nil {
		t.Fatalf("no cache DB should yield no holidays (weekend-only), got %v", got)
	}

	c, err := cache.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(); cache.SetGlobal(nil) })

	p, ok := selectHolidayProvider().(*DBHolidayProvider)
	if !ok {
		t.Fatalf("with cache DB open, provider should be *DBHolidayProvider")
	}
	// Round-trip through the selected provider.
	if err := p.UpsertHolidays(context.Background(), "NYSE", []string{"2026-12-25"}); err != nil {
		t.Fatalf("upsert via selected provider: %v", err)
	}
	if got := p.Holidays("NYSE"); len(got) != 1 || got[0] != "2026-12-25" {
		t.Fatalf("selected provider Holidays = %v, want [2026-12-25]", got)
	}
}
