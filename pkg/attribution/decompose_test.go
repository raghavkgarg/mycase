package attribution

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/marketdata"
)

// decomposeConfig is a shared window for the decompose tests.
func decomposeConfig() Config {
	return Config{
		InitialCapital: 10000,
		From:           time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		To:             time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
		Benchmark:      "US:SPY",
		Location:       time.UTC,
	}
}

func TestDecompose_SelectionAndRebalancingIdentity(t *testing.T) {
	// Two tickers, three trading days.
	// AAA: 100 → 110 → 121   BBB: 50 → 55 → 60   SPY: 400 → 404 → 408
	ts := []int64{day(2026, 1, 5), day(2026, 1, 6), day(2026, 1, 7)}
	f := &mockFetcher{data: map[string]*marketdata.HistoricalData{
		"US:AAA": {Timestamps: ts, Closes: []float64{100, 110, 121}},
		"US:BBB": {Timestamps: ts, Closes: []float64{50, 55, 60}},
		"US:SPY": {Timestamps: ts, Closes: []float64{400, 404, 408}},
	}}
	tr := NewTracker(f, quietLogger())

	// Current (actual) basket = 50/50 AAA/BBB.
	holdings := []Holding{{"US:AAA", 0.5}, {"US:BBB", 0.5}}
	// History: a single in-window rebalance whose basket equals the current one,
	// so buy-and-hold == actual → Rebalancing effect ≈ 0.
	history := []RebalanceEvent{
		{When: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), RunID: "run_1",
			Weights: map[string]float64{"US:AAA": 0.5, "US:BBB": 0.5}},
	}

	d, err := tr.Decompose(context.Background(), DecomposeInput{Holdings: holdings, History: history}, decomposeConfig())
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}

	// Identity: ActiveReturn == Selection + Rebalancing (tax reported separately).
	if math.Abs(d.ActiveReturn-(d.Selection+d.Rebalancing)) > 1e-9 {
		t.Errorf("identity broken: active=%.6f selection=%.6f rebalancing=%.6f",
			d.ActiveReturn, d.Selection, d.Rebalancing)
	}
	// Same basket held vs actual → rebalancing ~0.
	if math.Abs(d.Rebalancing) > 1e-9 {
		t.Errorf("Rebalancing = %.6f, want ~0 (identical baskets)", d.Rebalancing)
	}
	// Portfolio beat SPY, so selection (== active here) should be positive.
	if d.Selection <= 0 {
		t.Errorf("Selection = %.6f, want > 0", d.Selection)
	}
	if d.Rebalances != 1 {
		t.Errorf("Rebalances = %d, want 1", d.Rebalances)
	}
}

func TestDecompose_RebalancingEffectNonZero(t *testing.T) {
	// The buy-and-hold-first basket differs from the current basket, producing a
	// measurable rebalancing effect.
	ts := []int64{day(2026, 1, 5), day(2026, 1, 6), day(2026, 1, 7)}
	f := &mockFetcher{data: map[string]*marketdata.HistoricalData{
		"US:WIN": {Timestamps: ts, Closes: []float64{100, 130, 160}}, // strong
		"US:LAG": {Timestamps: ts, Closes: []float64{100, 100, 100}}, // flat
		"US:SPY": {Timestamps: ts, Closes: []float64{400, 404, 408}},
	}}
	tr := NewTracker(f, quietLogger())

	// Actual now: all-in the winner.
	holdings := []Holding{{"US:WIN", 1.0}}
	// First basket: all-in the laggard (held untouched → flat).
	history := []RebalanceEvent{
		{When: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), RunID: "run_1",
			Weights: map[string]float64{"US:LAG": 1.0}},
	}

	d, err := tr.Decompose(context.Background(), DecomposeInput{Holdings: holdings, History: history}, decomposeConfig())
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	// Actual (WIN, +60%) beats buy-and-hold (LAG, flat) → positive rebalancing.
	if d.Rebalancing <= 0 {
		t.Errorf("Rebalancing = %.6f, want > 0", d.Rebalancing)
	}
	// Buy-and-hold LAG was flat but SPY rose → selection negative.
	if d.Selection >= 0 {
		t.Errorf("Selection = %.6f, want < 0 (flat laggard vs rising SPY)", d.Selection)
	}
	if math.Abs(d.ActiveReturn-(d.Selection+d.Rebalancing)) > 1e-9 {
		t.Errorf("identity broken: %.6f != %.6f", d.ActiveReturn, d.Selection+d.Rebalancing)
	}
}

func TestDecompose_TaxEffect(t *testing.T) {
	ts := []int64{day(2026, 1, 5), day(2026, 1, 6)}
	f := &mockFetcher{data: map[string]*marketdata.HistoricalData{
		"US:AAA": {Timestamps: ts, Closes: []float64{100, 110}},
		"US:SPY": {Timestamps: ts, Closes: []float64{400, 404}},
	}}
	tr := NewTracker(f, quietLogger())
	holdings := []Holding{{"US:AAA", 1.0}}

	// $500 tax saving on $10000 initial → 5% tax effect.
	d, err := tr.Decompose(context.Background(),
		DecomposeInput{Holdings: holdings, TaxSaving: 500}, decomposeConfig())
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if math.Abs(d.Tax-0.05) > 1e-9 {
		t.Errorf("Tax = %.6f, want 0.05", d.Tax)
	}
}

func TestDecompose_EmptyHistoryUsesCurrentAsFirstBasket(t *testing.T) {
	// With no history, buy-and-hold == actual (both use current holdings) →
	// rebalancing must be ~0 and selection == active.
	ts := []int64{day(2026, 1, 5), day(2026, 1, 6), day(2026, 1, 7)}
	f := &mockFetcher{data: map[string]*marketdata.HistoricalData{
		"US:AAA": {Timestamps: ts, Closes: []float64{100, 110, 120}},
		"US:SPY": {Timestamps: ts, Closes: []float64{400, 404, 408}},
	}}
	tr := NewTracker(f, quietLogger())
	holdings := []Holding{{"US:AAA", 1.0}}

	d, err := tr.Decompose(context.Background(), DecomposeInput{Holdings: holdings}, decomposeConfig())
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if math.Abs(d.Rebalancing) > 1e-9 {
		t.Errorf("Rebalancing = %.6f, want ~0 (no history)", d.Rebalancing)
	}
	if math.Abs(d.Selection-d.ActiveReturn) > 1e-9 {
		t.Errorf("Selection %.6f should equal ActiveReturn %.6f with no rebalancing", d.Selection, d.ActiveReturn)
	}
	if d.Rebalances != 0 {
		t.Errorf("Rebalances = %d, want 0", d.Rebalances)
	}
}

func TestFirstInWindowBasket(t *testing.T) {
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	fallback := []Holding{{"FALLBACK", 1.0}}

	// Empty history → fallback.
	if got := firstInWindowBasket(nil, from, fallback); len(got) != 1 || got[0].Ticker != "FALLBACK" {
		t.Errorf("empty history should return fallback, got %+v", got)
	}

	hist := []RebalanceEvent{
		{When: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Weights: map[string]float64{"OLD": 1}},
		{When: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Weights: map[string]float64{"MID": 1}},
		{When: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Weights: map[string]float64{"NEW": 1}},
	}
	// Earliest event on/after `from` (2026-06-01) is the 2026-07-01 "MID" event.
	got := firstInWindowBasket(hist, from, fallback)
	if len(got) != 1 || got[0].Ticker != "MID" {
		t.Errorf("expected MID basket, got %+v", got)
	}

	// If all events predate the window, use the earliest known basket.
	lateFrom := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	got = firstInWindowBasket(hist, lateFrom, fallback)
	if len(got) != 1 || got[0].Ticker != "OLD" {
		t.Errorf("expected earliest (OLD) basket when all predate window, got %+v", got)
	}
}

func TestCountInWindow(t *testing.T) {
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	hist := []RebalanceEvent{
		{When: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}, // before
		{When: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)}, // in
		{When: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}, // in
		{When: time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)}, // after
	}
	if n := countInWindow(hist, from, to); n != 2 {
		t.Errorf("countInWindow = %d, want 2", n)
	}
}

// fakeRunReader implements runReader for LoadRebalanceHistory tests.
type fakeRunReader struct {
	runs      []cache.PipelineRun
	proposals map[string][]cache.Proposal // runID → optimized proposals
}

func (f *fakeRunReader) ListRunsByPortfolio(_ context.Context, _ string, _ int) ([]cache.PipelineRun, error) {
	return f.runs, nil
}

func (f *fakeRunReader) GetProposals(_ context.Context, runID, stage string) ([]cache.Proposal, error) {
	if stage != "optimized" {
		return nil, nil
	}
	return f.proposals[runID], nil
}

func TestLoadRebalanceHistory(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

	r := &fakeRunReader{
		// Newest-first, as ListRunsByPortfolio returns.
		runs: []cache.PipelineRun{
			{RunID: "run_3", StartedAt: t3, Status: cache.RunStatusCompleted, Portfolio: "us_sp500"},
			{RunID: "run_2", StartedAt: t2, Status: cache.RunStatusFailed, Portfolio: "us_sp500"}, // skipped: not completed
			{RunID: "run_1", StartedAt: t1, Status: cache.RunStatusCompleted, Portfolio: "us_sp500"},
			{RunID: "run_0", StartedAt: t1.AddDate(-1, 0, 0), Status: cache.RunStatusCompleted}, // skipped: no proposals
		},
		proposals: map[string][]cache.Proposal{
			"run_3": {{Ticker: "AAA", Weight: 0.6}, {Ticker: "BBB", Weight: 0.4}},
			"run_1": {{Ticker: "AAA", Weight: 1.0}},
			// run_0 has none.
		},
	}

	events, err := LoadRebalanceHistory(context.Background(), r, "us_sp500", time.Time{})
	if err != nil {
		t.Fatalf("LoadRebalanceHistory: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events (completed + has proposals), got %d", len(events))
	}
	// Oldest-first ordering.
	if events[0].RunID != "run_1" || events[1].RunID != "run_3" {
		t.Errorf("expected [run_1, run_3], got [%s, %s]", events[0].RunID, events[1].RunID)
	}
	if events[1].Weights["AAA"] != 0.6 || events[1].Weights["BBB"] != 0.4 {
		t.Errorf("run_3 weights wrong: %+v", events[1].Weights)
	}
}

func TestLoadRebalanceHistory_SinceFilter(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	r := &fakeRunReader{
		runs: []cache.PipelineRun{
			{RunID: "run_2", StartedAt: t2, Status: cache.RunStatusCompleted},
			{RunID: "run_1", StartedAt: t1, Status: cache.RunStatusCompleted},
		},
		proposals: map[string][]cache.Proposal{
			"run_2": {{Ticker: "AAA", Weight: 1.0}},
			"run_1": {{Ticker: "BBB", Weight: 1.0}},
		},
	}
	since := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	events, err := LoadRebalanceHistory(context.Background(), r, "x", since)
	if err != nil {
		t.Fatalf("LoadRebalanceHistory: %v", err)
	}
	if len(events) != 1 || events[0].RunID != "run_2" {
		t.Errorf("since filter failed: %+v", events)
	}
}

func TestLoadRebalanceHistory_NilReader(t *testing.T) {
	events, err := LoadRebalanceHistory(context.Background(), nil, "x", time.Time{})
	if err != nil || events != nil {
		t.Errorf("nil reader should return (nil, nil), got (%v, %v)", events, err)
	}
}
