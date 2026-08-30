package attribution

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/cache"
)

// openStore opens a fresh DuckDB cache in a temp dir and returns a Store over it.
func openStore(t *testing.T) *Store {
	t.Helper()
	c, err := cache.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return NewStore(c.Conn())
}

func mkPoints(base time.Time, n int) []NAVPoint {
	pts := make([]NAVPoint, n)
	for i := range n {
		pts[i] = NAVPoint{
			Date:           base.AddDate(0, 0, i),
			PortfolioValue: 1000 + float64(i)*10,
			BenchmarkValue: 1000 + float64(i)*5,
		}
	}
	return pts
}

func TestStore_RoundTrip(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	pts := mkPoints(base, 5)

	if err := s.InsertNAVPoints(ctx, "microsmall", pts); err != nil {
		t.Fatalf("InsertNAVPoints: %v", err)
	}
	got, err := s.GetNAVHistory(ctx, "microsmall", time.Time{})
	if err != nil {
		t.Fatalf("GetNAVHistory: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("expected 5 points, got %d", len(got))
	}
	// Ordered ascending; values round-trip; timestamps to second precision.
	for i, p := range got {
		if p.Date.Unix() != pts[i].Date.Unix() {
			t.Errorf("point %d date = %v, want %v", i, p.Date, pts[i].Date)
		}
		if p.PortfolioValue != pts[i].PortfolioValue || p.BenchmarkValue != pts[i].BenchmarkValue {
			t.Errorf("point %d values mismatch: %+v vs %+v", i, p, pts[i])
		}
	}
}

func TestStore_UpsertIdempotent(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)

	if err := s.InsertNAVPoints(ctx, "p", mkPoints(base, 3)); err != nil {
		t.Fatal(err)
	}
	// Re-insert same days with new values → overwrite, not duplicate.
	updated := []NAVPoint{{Date: base, PortfolioValue: 9999, BenchmarkValue: 8888}}
	if err := s.InsertNAVPoints(ctx, "p", updated); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetNAVHistory(ctx, "p", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 rows after upsert (no dupes), got %d", len(got))
	}
	if got[0].PortfolioValue != 9999 || got[0].BenchmarkValue != 8888 {
		t.Errorf("upsert did not overwrite: %+v", got[0])
	}
}

func TestStore_SinceFilterAndPortfolioScope(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)

	if err := s.InsertNAVPoints(ctx, "p", mkPoints(base, 5)); err != nil {
		t.Fatal(err)
	}
	// Different portfolio must not leak in.
	if err := s.InsertNAVPoints(ctx, "other", mkPoints(base, 2)); err != nil {
		t.Fatal(err)
	}

	since := base.AddDate(0, 0, 2)
	got, err := s.GetNAVHistory(ctx, "p", since)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 { // days 2,3,4 (0-indexed)
		t.Fatalf("since filter: expected 3, got %d", len(got))
	}
	if got[0].Date.Unix() != since.Unix() {
		t.Errorf("first point after since = %v, want %v", got[0].Date, since)
	}
}

func TestStore_NilAndEmpty(t *testing.T) {
	ctx := context.Background()
	// NewStore(nil) → nil Store; methods are safe no-ops.
	var ns *Store = NewStore(nil)
	if ns != nil {
		t.Fatal("NewStore(nil) should return nil")
	}
	if err := ns.InsertNAVPoints(ctx, "p", mkPoints(time.Now(), 2)); err != nil {
		t.Errorf("nil Store InsertNAVPoints should be no-op, got %v", err)
	}
	got, err := ns.GetNAVHistory(ctx, "p", time.Time{})
	if err != nil || got != nil {
		t.Errorf("nil Store GetNAVHistory should return (nil,nil), got (%v,%v)", got, err)
	}

	// Empty insert on a real store is a no-op.
	s := openStore(t)
	if err := s.InsertNAVPoints(ctx, "p", nil); err != nil {
		t.Errorf("empty insert should be no-op, got %v", err)
	}
}
