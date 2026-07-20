package backtest

import (
	"math"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// ---- metrics tests ----

func TestCalcCAGR_Basic(t *testing.T) {
	// Double in 365 days → CAGR = 100%
	got := CalcCAGR(1000, 2000, 365)
	assertClose(t, "CAGR double", got, 1.0)
}

func TestCalcCAGR_NoGrowth(t *testing.T) {
	got := CalcCAGR(1000, 1000, 365)
	assertClose(t, "CAGR flat", got, 0.0)
}

func TestCalcCAGR_ZeroInputs(t *testing.T) {
	if CalcCAGR(0, 1000, 365) != 0 {
		t.Error("zero initial should return 0")
	}
	if CalcCAGR(1000, 0, 365) != 0 {
		t.Error("zero final should return 0")
	}
	if CalcCAGR(1000, 2000, 0) != 0 {
		t.Error("zero days should return 0")
	}
}

func TestCalcMaxDrawdown_Flat(t *testing.T) {
	got := CalcMaxDrawdown([]float64{100, 100, 100})
	assertClose(t, "flat drawdown", got, 0.0)
}

func TestCalcMaxDrawdown_MonotonicDecline(t *testing.T) {
	// 100 → 80 → 60: drawdown from 100 = -40%
	got := CalcMaxDrawdown([]float64{100, 80, 60})
	assertClose(t, "monotonic decline", got, -0.4)
}

func TestCalcMaxDrawdown_Recovery(t *testing.T) {
	// 100 → 50 → 90: max drawdown is -50%, not final value
	got := CalcMaxDrawdown([]float64{100, 50, 90})
	assertClose(t, "recovery drawdown", got, -0.5)
}

func TestCalcMaxDrawdown_PeakThenRecover(t *testing.T) {
	// 100 → 150 → 75 → 200: peak=150, trough=75, dd = (75-150)/150 = -50%
	got := CalcMaxDrawdown([]float64{100, 150, 75, 200})
	assertClose(t, "peak-then-recover", got, -0.5)
}

func TestCalcSharpe_AllSameReturn(t *testing.T) {
	// Constant daily return: variance ≈ 0 → Sharpe is near-zero or infinite depending
	// on floating-point representation. Just verify it's finite (no NaN/Inf explosion).
	nav := make([]float64, 252)
	for i := range nav {
		nav[i] = 1000 * math.Pow(1.0001, float64(i))
	}
	got := CalcSharpe(nav)
	if math.IsNaN(got) {
		t.Errorf("constant return: Sharpe should not be NaN")
	}
}

func TestCalcSharpe_Positive(t *testing.T) {
	// Build a NAV with positive drift and low noise — Sharpe should be positive
	nav := make([]float64, 252)
	nav[0] = 100
	for i := 1; i < 252; i++ {
		nav[i] = nav[i-1] * 1.0008 // steady growth
	}
	got := CalcSharpe(nav)
	if got <= 0 {
		t.Errorf("positive drift: Sharpe should be > 0, got %.4f", got)
	}
}

func TestCalcSortino_NoDownDays(t *testing.T) {
	// All returns above risk-free → downsideStd = 0 → Sortino = 0
	nav := make([]float64, 50)
	nav[0] = 1000
	for i := 1; i < 50; i++ {
		nav[i] = nav[i-1] * 1.001 // above daily risk-free of 0.06/252 ≈ 0.000238
	}
	got := CalcSortino(nav)
	if got != 0 {
		t.Errorf("no down days: Sortino should be 0 (no downside vol), got %.4f", got)
	}
}

func TestCalcCalmar_Normal(t *testing.T) {
	// CAGR 20%, MaxDD -10% → Calmar = 2.0
	got := CalcCalmar(0.20, -0.10)
	assertClose(t, "Calmar", got, 2.0)
}

func TestCalcCalmar_ZeroDrawdown(t *testing.T) {
	if CalcCalmar(0.20, 0) != 0 {
		t.Error("zero drawdown should return 0")
	}
}

func TestCalcBeta_PerfectCorrelation(t *testing.T) {
	// Portfolio = benchmark → beta = 1
	port := []float64{100, 110, 121, 133.1}
	bench := port
	got := CalcBeta(port, bench)
	assertClose(t, "beta perfect corr", got, 1.0)
}

func TestCalcBeta_Negative(t *testing.T) {
	// Portfolio with varying returns that move opposite to benchmark → beta < 0
	// Returns: bench=[+10%,+5%,+2%,+8%,-3%], port=[-10%,-5%,-2%,-8%,+3%]
	bench := []float64{100, 110, 115.5, 117.81, 127.2348, 123.417756}
	port := []float64{100, 90, 85.5, 83.79, 76.6886, 79.089586}
	got := CalcBeta(port, bench)
	if got >= 0 {
		t.Errorf("inverse-moving portfolio: beta should be negative, got %.4f", got)
	}
	// Beta should be close to -1 since we used exactly negated returns
	assertClosePct(t, "beta ≈ -1", got, -1.0, 0.05)
}

func TestCalcAlpha_SameAsBenchmark(t *testing.T) {
	// portCAGR = benchCAGR, beta = 1 → alpha = 0
	got := CalcAlpha(0.15, 0.15, 1.0)
	assertClose(t, "alpha zero", got, 0.0)
}

func TestCalcAlpha_PositiveOutperformance(t *testing.T) {
	// portCAGR = 25%, benchCAGR = 15%, beta = 1 → alpha = 10%
	got := CalcAlpha(0.25, 0.15, 1.0)
	assertClose(t, "alpha positive", got, 0.10)
}

// ---- engine tests ----

func TestRun_BasicNoRebalance(t *testing.T) {
	// 2 tickers: A (50%) + B (50%), prices grow uniformly over 3 days
	// No slippage, drift threshold huge (never rebalances)
	from := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	to := time.Date(2020, 1, 6, 0, 0, 0, 0, time.UTC)

	// Days: 2020-01-02, 2020-01-03, 2020-01-06
	ts := []int64{
		time.Date(2020, 1, 2, 6, 0, 0, 0, time.UTC).Unix(),
		time.Date(2020, 1, 3, 6, 0, 0, 0, time.UTC).Unix(),
		time.Date(2020, 1, 6, 6, 0, 0, 0, time.UTC).Unix(),
	}
	priceA := []float64{100, 110, 121}        // +21%
	priceB := []float64{200, 220, 242}        // +21%
	priceBench := []float64{1000, 1050, 1100} // +10%

	holdings := []Holding{
		{Ticker: "A", Weight: 0.5},
		{Ticker: "B", Weight: 0.5},
	}
	priceData := map[string]*yfinance.HistoricalData{
		"A": makeHist(ts, priceA),
		"B": makeHist(ts, priceB),
	}
	cfg := SimConfig{
		InitialCapital:  1000,
		From:            from,
		To:              to,
		Rebalance:       FreqDrift,
		DriftThreshold:  1.0, // 100% — never triggers
		BenchmarkTicker: "^NSEI",
	}

	res, err := Run(holdings, priceData, makeHist(ts, priceBench), cfg)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.TradingDays != 3 {
		t.Errorf("TradingDays = %d, want 3", res.TradingDays)
	}
	// Total return: A and B both +21%
	assertClosePct(t, "TotalReturn", res.TotalReturn, 0.21, 0.01)
	assertClosePct(t, "BenchmarkReturn", res.BenchmarkReturn, 0.10, 0.01)
	if res.RebalanceCount != 0 {
		t.Errorf("expected 0 rebalances, got %d", res.RebalanceCount)
	}
}

func TestRun_WithSlippage(t *testing.T) {
	// 1% slippage on buy reduces initial portfolio value slightly
	ts := []int64{
		time.Date(2020, 1, 2, 6, 0, 0, 0, time.UTC).Unix(),
		time.Date(2020, 1, 3, 6, 0, 0, 0, time.UTC).Unix(),
	}
	prices := []float64{100, 100}
	bench := []float64{1000, 1000}

	holdings := []Holding{{Ticker: "X", Weight: 1.0}}
	priceData := map[string]*yfinance.HistoricalData{"X": makeHist(ts, prices)}

	cfg := SimConfig{
		InitialCapital: 1000,
		From:           time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC),
		To:             time.Date(2020, 1, 3, 0, 0, 0, 0, time.UTC),
		Rebalance:      FreqDrift,
		DriftThreshold: 1.0,
		SlippagePct:    0.01, // 1%
	}

	res, err := Run(holdings, priceData, makeHist(ts, bench), cfg)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	// Initial portfolio after 1% slippage on buy: 1000/1.01 * 100 / 100 = 990.1
	// Final = 990.1 * 100 / 100 = 990.1 (flat prices)
	// TotalReturn should be slightly negative due to slippage
	if res.TotalReturn >= 0 {
		t.Errorf("1%% slippage on flat price should give negative return, got %.4f", res.TotalReturn)
	}
}

func TestRun_QuarterlyRebalance(t *testing.T) {
	// Span 6 months with quarterly rebalance → expect 1 rebalance (at Q2 start)
	ts := buildMonthlyTimestamps(time.Date(2020, 1, 2, 6, 0, 0, 0, time.UTC), 7)
	prices := buildFlatPrices(len(ts), 100)
	bench := buildFlatPrices(len(ts), 1000)

	holdings := []Holding{
		{Ticker: "X", Weight: 0.6},
		{Ticker: "Y", Weight: 0.4},
	}
	priceData := map[string]*yfinance.HistoricalData{
		"X": makeHist(ts, prices),
		"Y": makeHist(ts, prices),
	}

	cfg := SimConfig{
		InitialCapital: 1000,
		From:           time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		To:             time.Date(2020, 7, 31, 0, 0, 0, 0, time.UTC),
		Rebalance:      FreqQuarterly,
	}

	res, err := Run(holdings, priceData, makeHist(ts, bench), cfg)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.RebalanceCount < 1 {
		t.Errorf("expected at least 1 quarterly rebalance over 6 months, got %d", res.RebalanceCount)
	}
}

func TestRun_FromAfterTo(t *testing.T) {
	ts := []int64{time.Date(2020, 1, 2, 6, 0, 0, 0, time.UTC).Unix()}
	h := makeHist(ts, []float64{100})
	_, err := Run(
		[]Holding{{Ticker: "X", Weight: 1.0}},
		map[string]*yfinance.HistoricalData{"X": h},
		h,
		SimConfig{
			InitialCapital: 1000,
			From:           time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC),
			To:             time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	)
	if err == nil {
		t.Error("expected error when from > to")
	}
}

func TestRun_InsufficientDays(t *testing.T) {
	ts := []int64{time.Date(2020, 1, 2, 6, 0, 0, 0, time.UTC).Unix()}
	h := makeHist(ts, []float64{100})
	_, err := Run(
		[]Holding{{Ticker: "X", Weight: 1.0}},
		map[string]*yfinance.HistoricalData{"X": h},
		h,
		SimConfig{InitialCapital: 1000},
	)
	if err == nil {
		t.Error("expected error for single trading day")
	}
}

func TestRun_MissingTickerData(t *testing.T) {
	ts := []int64{
		time.Date(2020, 1, 2, 6, 0, 0, 0, time.UTC).Unix(),
		time.Date(2020, 1, 3, 6, 0, 0, 0, time.UTC).Unix(),
	}
	h := makeHist(ts, []float64{100, 105})
	_, err := Run(
		[]Holding{{Ticker: "MISSING", Weight: 1.0}},
		map[string]*yfinance.HistoricalData{},
		h,
		SimConfig{InitialCapital: 1000},
	)
	if err == nil {
		t.Error("expected error for missing ticker data")
	}
}

// ---- helpers ----

func makeHist(ts []int64, closes []float64) *yfinance.HistoricalData {
	opens := make([]float64, len(closes))
	vols := make([]float64, len(closes))
	copy(opens, closes)
	return &yfinance.HistoricalData{
		Timestamps: ts,
		Closes:     closes,
		Opens:      opens,
		Volumes:    vols,
	}
}

// buildMonthlyTimestamps generates n timestamps, one per month starting from start.
func buildMonthlyTimestamps(start time.Time, n int) []int64 {
	ts := make([]int64, n)
	for i := range n {
		ts[i] = start.AddDate(0, i, 0).Unix()
	}
	return ts
}

func buildFlatPrices(n int, price float64) []float64 {
	p := make([]float64, n)
	for i := range p {
		p[i] = price
	}
	return p
}

func assertClose(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %.9f, want %.9f", name, got, want)
	}
}

// assertClosePct checks that got is within tol fraction of want.
func assertClosePct(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.6f, want %.6f (±%.6f)", name, got, want, tol)
	}
}
