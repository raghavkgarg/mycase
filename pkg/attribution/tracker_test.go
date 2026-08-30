package attribution

import (
	"context"
	"errors"
	"io"
	"math"
	"testing"
	"time"

	"log/slog"

	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// mockFetcher returns canned HistoricalData per ticker, or an error.
type mockFetcher struct {
	data map[string]*yfinance.HistoricalData
	errs map[string]error
}

func (m *mockFetcher) FetchHistoricalByDateRange(_ context.Context, ticker string, _, _ time.Time) (*yfinance.HistoricalData, error) {
	if e, ok := m.errs[ticker]; ok {
		return nil, e
	}
	return m.data[ticker], nil
}

// day builds a Unix timestamp (seconds) for a UTC calendar date at noon, so
// that converting to America/New_York keeps the same calendar day.
func day(y int, mo time.Month, d int) int64 {
	return time.Date(y, mo, d, 17, 0, 0, 0, time.UTC).Unix() // 17:00 UTC = 12/13:00 ET
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildNAVSeries_BasicTwoTicker(t *testing.T) {
	// AAA: 100 → 110 → 121 (+10%/day). BBB: 50 → 50 → 50 (flat).
	// Benchmark SPY: 400 → 404 → 408.
	ts := []int64{day(2026, time.January, 5), day(2026, time.January, 6), day(2026, time.January, 7)}
	f := &mockFetcher{data: map[string]*yfinance.HistoricalData{
		"US:AAA": {Timestamps: ts, Closes: []float64{100, 110, 121}},
		"US:BBB": {Timestamps: ts, Closes: []float64{50, 50, 50}},
		"US:SPY": {Timestamps: ts, Closes: []float64{400, 404, 408}},
	}}
	tr := NewTracker(f, quietLogger())

	cfg := Config{
		InitialCapital: 10000,
		From:           time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		To:             time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC),
		Benchmark:      "US:SPY",
		Location:       time.UTC, // deterministic day-keying
	}
	holdings := []Holding{{"US:AAA", 0.5}, {"US:BBB", 0.5}}

	points, err := tr.BuildNAVSeries(context.Background(), holdings, cfg)
	if err != nil {
		t.Fatalf("BuildNAVSeries: %v", err)
	}
	if len(points) != 3 {
		t.Fatalf("expected 3 points, got %d", len(points))
	}

	// $5000 into AAA @100 = 50 shares; $5000 into BBB @50 = 100 shares.
	// Day0: 50*100 + 100*50 = 10000. Day1: 50*110 + 100*50 = 10500.
	// Day2: 50*121 + 100*50 = 11050.
	wantPort := []float64{10000, 10500, 11050}
	for i, w := range wantPort {
		if math.Abs(points[i].PortfolioValue-w) > 1e-6 {
			t.Errorf("port[%d] = %.4f, want %.4f", i, points[i].PortfolioValue, w)
		}
	}
	// Benchmark: 10000/400 = 25 units. 25*[400,404,408] = [10000,10100,10200].
	wantBench := []float64{10000, 10100, 10200}
	for i, w := range wantBench {
		if math.Abs(points[i].BenchmarkValue-w) > 1e-6 {
			t.Errorf("bench[%d] = %.4f, want %.4f", i, points[i].BenchmarkValue, w)
		}
	}
}

func TestBuildNAVSeries_SkipsFailedTickerAndRenormalizes(t *testing.T) {
	ts := []int64{day(2026, time.January, 5), day(2026, time.January, 6)}
	f := &mockFetcher{
		data: map[string]*yfinance.HistoricalData{
			"US:AAA": {Timestamps: ts, Closes: []float64{100, 110}},
			"US:SPY": {Timestamps: ts, Closes: []float64{400, 404}},
		},
		errs: map[string]error{"US:BBB": errors.New("fetch failed")},
	}
	tr := NewTracker(f, quietLogger())
	cfg := Config{InitialCapital: 10000, From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), Location: time.UTC}
	holdings := []Holding{{"US:AAA", 0.5}, {"US:BBB", 0.5}}

	points, err := tr.BuildNAVSeries(context.Background(), holdings, cfg)
	if err != nil {
		t.Fatalf("BuildNAVSeries: %v", err)
	}
	// BBB dropped; AAA renormalized to weight 1.0. $10000/100 = 100 shares.
	// Day0: 10000, Day1: 100*110 = 11000.
	if math.Abs(points[0].PortfolioValue-10000) > 1e-6 || math.Abs(points[1].PortfolioValue-11000) > 1e-6 {
		t.Errorf("renormalized NAV wrong: %+v", points)
	}
}

func TestBuildNAVSeries_IntersectionOfTradingDays(t *testing.T) {
	// AAA trades on days 5,6,7; SPY only on 5,6. Intersection = {5,6}.
	f := &mockFetcher{data: map[string]*yfinance.HistoricalData{
		"US:AAA": {Timestamps: []int64{day(2026, 1, 5), day(2026, 1, 6), day(2026, 1, 7)}, Closes: []float64{100, 110, 120}},
		"US:SPY": {Timestamps: []int64{day(2026, 1, 5), day(2026, 1, 6)}, Closes: []float64{400, 404}},
	}}
	tr := NewTracker(f, quietLogger())
	cfg := Config{InitialCapital: 1000, From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), Location: time.UTC}
	points, err := tr.BuildNAVSeries(context.Background(), []Holding{{"US:AAA", 1.0}}, cfg)
	if err != nil {
		t.Fatalf("BuildNAVSeries: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("expected 2 common days, got %d", len(points))
	}
}

func TestBuildNAVSeries_Errors(t *testing.T) {
	tr := NewTracker(&mockFetcher{data: map[string]*yfinance.HistoricalData{}}, quietLogger())
	base := Config{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), Location: time.UTC}

	// No holdings.
	if _, err := tr.BuildNAVSeries(context.Background(), nil, base); err == nil {
		t.Error("expected error for no holdings")
	}
	// Invalid range (from == to).
	bad := base
	bad.To = bad.From
	if _, err := tr.BuildNAVSeries(context.Background(), []Holding{{"US:AAA", 1}}, bad); err == nil {
		t.Error("expected error for invalid range")
	}
	// Benchmark returns nothing.
	if _, err := tr.BuildNAVSeries(context.Background(), []Holding{{"US:AAA", 1}}, base); err == nil {
		t.Error("expected error when benchmark empty")
	}
}

func TestAttribution_KnownSeries(t *testing.T) {
	// Portfolio strictly beats a flat benchmark.
	base := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	pts := []NAVPoint{
		{Date: base, PortfolioValue: 1000, BenchmarkValue: 1000},
		{Date: base.AddDate(0, 0, 1), PortfolioValue: 1010, BenchmarkValue: 1000},
		{Date: base.AddDate(0, 0, 2), PortfolioValue: 1020, BenchmarkValue: 1000},
	}
	r := Attribution(pts, 0.045)

	if r.TradingDays != 3 {
		t.Errorf("TradingDays = %d", r.TradingDays)
	}
	if math.Abs(r.TotalReturn-0.02) > 1e-9 {
		t.Errorf("TotalReturn = %.6f, want 0.02", r.TotalReturn)
	}
	if math.Abs(r.BenchmarkReturn-0.0) > 1e-9 {
		t.Errorf("BenchmarkReturn = %.6f, want 0", r.BenchmarkReturn)
	}
	// Portfolio up, benchmark flat → positive alpha.
	if r.Alpha <= 0 {
		t.Errorf("expected positive alpha, got %.4f", r.Alpha)
	}
	// Benchmark has zero variance → beta defaults to 1 (from CalcBeta).
	if r.Beta != 1 {
		t.Errorf("Beta = %.4f, want 1 (zero-variance benchmark)", r.Beta)
	}
	// Active returns are all positive & constant-ish → positive IR.
	if r.InformationRatio <= 0 {
		t.Errorf("expected positive information ratio, got %.4f", r.InformationRatio)
	}
	// No drawdown on a monotonically rising series.
	if r.MaxDrawdown != 0 {
		t.Errorf("MaxDrawdown = %.4f, want 0", r.MaxDrawdown)
	}
}

func TestAttribution_Degenerate(t *testing.T) {
	if r := Attribution(nil, 0.045); r.TradingDays != 0 {
		t.Error("nil series should give zero-value result")
	}
	one := []NAVPoint{{Date: time.Now(), PortfolioValue: 100, BenchmarkValue: 100}}
	if r := Attribution(one, 0.045); r.TradingDays != 1 || r.TotalReturn != 0 {
		t.Error("single-point series should give no returns")
	}
}

func TestAttribution_DefaultRiskFreeApplied(t *testing.T) {
	base := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	pts := []NAVPoint{
		{Date: base, PortfolioValue: 1000, BenchmarkValue: 1000},
		{Date: base.AddDate(0, 0, 1), PortfolioValue: 1010, BenchmarkValue: 1005},
	}
	// riskFree=0 should fall back to DefaultRiskFree (no panic, finite result).
	r := Attribution(pts, 0)
	if math.IsNaN(r.Alpha) || math.IsInf(r.Alpha, 0) {
		t.Errorf("alpha not finite: %v", r.Alpha)
	}
}

func TestNewTracker_NilLoggerUsesDefault(t *testing.T) {
	tr := NewTracker(&mockFetcher{}, nil)
	if tr.log == nil {
		t.Error("NewTracker(nil) should install slog.Default()")
	}
}

func TestConfig_WithDefaults(t *testing.T) {
	c := Config{}.withDefaults()
	if c.InitialCapital != DefaultInitialCapital {
		t.Errorf("InitialCapital = %v, want %v", c.InitialCapital, DefaultInitialCapital)
	}
	if c.Benchmark != DefaultBenchmark {
		t.Errorf("Benchmark = %q, want %q", c.Benchmark, DefaultBenchmark)
	}
	if c.RiskFree != DefaultRiskFree {
		t.Errorf("RiskFree = %v, want %v", c.RiskFree, DefaultRiskFree)
	}
	if c.Location == nil {
		t.Error("Location should default to a non-nil zone")
	}
	// Explicit values are preserved.
	custom := Config{InitialCapital: 5000, Benchmark: "US:VOO", RiskFree: 0.03, Location: time.UTC}.withDefaults()
	if custom.InitialCapital != 5000 || custom.Benchmark != "US:VOO" || custom.RiskFree != 0.03 || custom.Location != time.UTC {
		t.Errorf("withDefaults overwrote explicit values: %+v", custom)
	}
}

func TestBuildNAVSeries_SkipsNonPositiveCloses(t *testing.T) {
	// A zero/negative close on a day is skipped; that day then isn't in the
	// ticker's map, so it drops out of the intersection.
	f := &mockFetcher{data: map[string]*yfinance.HistoricalData{
		"US:AAA": {Timestamps: []int64{day(2026, 1, 5), day(2026, 1, 6), day(2026, 1, 7)}, Closes: []float64{100, 0, 120}},
		"US:SPY": {Timestamps: []int64{day(2026, 1, 5), day(2026, 1, 6), day(2026, 1, 7)}, Closes: []float64{400, 404, 408}},
	}}
	tr := NewTracker(f, quietLogger())
	cfg := Config{InitialCapital: 1000, From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), Location: time.UTC}
	points, err := tr.BuildNAVSeries(context.Background(), []Holding{{"US:AAA", 1.0}}, cfg)
	if err != nil {
		t.Fatalf("BuildNAVSeries: %v", err)
	}
	// Day 6 (zero close) dropped → only days 5 and 7 remain.
	if len(points) != 2 {
		t.Fatalf("expected 2 points (zero-close day dropped), got %d", len(points))
	}
}
