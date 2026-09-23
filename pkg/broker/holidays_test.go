package broker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/raghavkgarg/mycase/pkg/cache"
)

func TestFileHolidayProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "holidays.json")
	body := `{"exchanges": {"NYSE": ["2026-01-01", "2026-07-03"], "NSE": ["2026-01-26"]}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write holidays.json: %v", err)
	}

	p := FileHolidayProvider{Path: path}
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

func TestFileHolidayProvider_MissingFile(t *testing.T) {
	p := FileHolidayProvider{Path: filepath.Join(t.TempDir(), "nope.json")}
	if got := p.Holidays("NYSE"); len(got) != 0 {
		t.Fatalf("missing file = %v, want empty (weekend-only degrade)", got)
	}
}

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
}

func TestSelectHolidayProvider_Precedence(t *testing.T) {
	// With no cache DB open and no env/flag, the default is the file provider.
	cache.SetGlobal(nil)
	t.Setenv(holidaySourceEnv, "")

	if _, ok := selectHolidayProvider("").(FileHolidayProvider); !ok {
		t.Fatalf("default source should be FileHolidayProvider")
	}

	// Env=db but no cache DB open → graceful fallback to file.
	t.Setenv(holidaySourceEnv, "db")
	if _, ok := selectHolidayProvider("").(FileHolidayProvider); !ok {
		t.Fatalf("db source with no cache DB should fall back to FileHolidayProvider")
	}

	// Env=db WITH a cache DB open → DB provider.
	c, err := cache.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(); cache.SetGlobal(nil) })
	if _, ok := selectHolidayProvider("").(*DBHolidayProvider); !ok {
		t.Fatalf("db source with cache DB open should be *DBHolidayProvider")
	}

	// Flag override wins over env: env=db, flag=file → file.
	if _, ok := selectHolidayProvider("file").(FileHolidayProvider); !ok {
		t.Fatalf("flag override 'file' should beat env 'db'")
	}
}
