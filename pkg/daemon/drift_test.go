package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/broker"
)

// stubBroker implements broker.Broker with configurable quotes and holdings.
type stubBroker struct {
	quotes   map[string]float64
	holdings []broker.Holding
}

func (s *stubBroker) GetQuotes(keys []string) (map[string]float64, error) {
	out := make(map[string]float64, len(keys))
	for _, k := range keys {
		out[k] = s.quotes[k]
	}
	return out, nil
}
func (s *stubBroker) GetHoldings() ([]broker.Holding, error) { return s.holdings, nil }
func (s *stubBroker) PlaceOrder(_ string, _ broker.Order) (broker.OrderResult, error) {
	return broker.OrderResult{}, nil
}
func (s *stubBroker) PlaceGTT(_ broker.Order) (broker.OrderResult, error) {
	return broker.OrderResult{}, nil
}
func (s *stubBroker) IsMock() bool { return true }

var _ broker.Broker = (*stubBroker)(nil)

func TestCalculateDrift_ExactMatch(t *testing.T) {
	// Actual weights == target weights → drift must be 0.
	b := &stubBroker{
		quotes: map[string]float64{"NSE:AAA": 100, "NSE:BBB": 100},
		holdings: []broker.Holding{
			{Exchange: "NSE", TradingSymbol: "AAA", Quantity: 60},
			{Exchange: "NSE", TradingSymbol: "BBB", Quantity: 40},
		},
	}
	target := map[string]float64{"NSE:AAA": 0.60, "NSE:BBB": 0.40}
	keys := []string{"NSE:AAA", "NSE:BBB"}

	res, err := CalculateDrift(context.Background(), b, target, keys)
	if err != nil {
		t.Fatal(err)
	}
	if res.DriftIndex > 1e-9 {
		t.Errorf("drift = %.6f, want 0", res.DriftIndex)
	}
}

func TestCalculateDrift_NoHoldings(t *testing.T) {
	// Zero holdings → actual weights all 0 → drift = 0.5 × Σ|target| = 0.5.
	b := &stubBroker{
		quotes:   map[string]float64{"NSE:AAA": 100, "NSE:BBB": 100},
		holdings: nil,
	}
	target := map[string]float64{"NSE:AAA": 0.60, "NSE:BBB": 0.40}
	keys := []string{"NSE:AAA", "NSE:BBB"}

	res, err := CalculateDrift(context.Background(), b, target, keys)
	if err != nil {
		t.Fatal(err)
	}
	const want = 0.5
	if diff := res.DriftIndex - want; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("drift = %.6f, want %.6f", res.DriftIndex, want)
	}
}

func TestCalculateDrift_PartialDrift(t *testing.T) {
	// Equal-weight 2-stock portfolio; actual is 75/25 instead of 50/50.
	// drift = ½(|0.75−0.5| + |0.25−0.5|) = ½ × 0.5 = 0.25
	b := &stubBroker{
		quotes: map[string]float64{"NSE:AAA": 100, "NSE:BBB": 100},
		holdings: []broker.Holding{
			{Exchange: "NSE", TradingSymbol: "AAA", Quantity: 75},
			{Exchange: "NSE", TradingSymbol: "BBB", Quantity: 25},
		},
	}
	target := map[string]float64{"NSE:AAA": 0.50, "NSE:BBB": 0.50}
	keys := []string{"NSE:AAA", "NSE:BBB"}

	res, err := CalculateDrift(context.Background(), b, target, keys)
	if err != nil {
		t.Fatal(err)
	}
	const want = 0.25
	if diff := res.DriftIndex - want; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("drift = %.6f, want %.6f", res.DriftIndex, want)
	}
}

func TestCalculateDrift_T1T2QuantityCounted(t *testing.T) {
	// T1 and T2 quantities must be included in the holding value.
	// qty=3, T1=2, T2=1 → total 6 shares at 100 = 600; same as qty=6 alone.
	b := &stubBroker{
		quotes: map[string]float64{"NSE:AAA": 100},
		holdings: []broker.Holding{
			{Exchange: "NSE", TradingSymbol: "AAA", Quantity: 3, T1Quantity: 2, T2Quantity: 1},
		},
	}
	target := map[string]float64{"NSE:AAA": 1.0}
	keys := []string{"NSE:AAA"}

	res, err := CalculateDrift(context.Background(), b, target, keys)
	if err != nil {
		t.Fatal(err)
	}
	if res.DriftIndex > 1e-9 {
		t.Errorf("drift = %.6f, want 0 (T1/T2 quantities not counted)", res.DriftIndex)
	}
}

func TestCalculateDrift_SingleStockPerfect(t *testing.T) {
	b := &stubBroker{
		quotes:   map[string]float64{"NSE:AAA": 250},
		holdings: []broker.Holding{{Exchange: "NSE", TradingSymbol: "AAA", Quantity: 10}},
	}
	target := map[string]float64{"NSE:AAA": 1.0}
	keys := []string{"NSE:AAA"}

	res, err := CalculateDrift(context.Background(), b, target, keys)
	if err != nil {
		t.Fatal(err)
	}
	if res.DriftIndex > 1e-9 {
		t.Errorf("drift = %.6f, want 0", res.DriftIndex)
	}
}

func TestCalculateDrift_Bounded(t *testing.T) {
	// Drift must always be in [0, 1.0].
	// The formula ½ Σ|w_actual − w_target| is bounded by 1.0 (total variation distance).
	// The no-holdings case yields exactly 0.5; a fully-concentrated actual far from target
	// can approach 1.0. The cases below verify boundary behaviour.
	cases := []struct {
		quotes      map[string]float64
		holdings    []broker.Holding
		target      map[string]float64
		keys        []string
		wantAtMost  float64
		wantAtLeast float64
	}{
		{
			// One stock overweight, one underweight → intermediate drift.
			quotes:      map[string]float64{"NSE:X": 100, "NSE:Y": 50},
			holdings:    []broker.Holding{{Exchange: "NSE", TradingSymbol: "X", Quantity: 5}},
			target:      map[string]float64{"NSE:X": 0.7, "NSE:Y": 0.3},
			keys:        []string{"NSE:X", "NSE:Y"},
			wantAtLeast: 0, wantAtMost: 1.0,
		},
		{
			// No holdings → actual all-zero, drift exactly 0.5.
			quotes:      map[string]float64{"NSE:X": 100},
			holdings:    nil,
			target:      map[string]float64{"NSE:X": 1.0},
			keys:        []string{"NSE:X"},
			wantAtLeast: 0.5 - 1e-9, wantAtMost: 0.5 + 1e-9,
		},
		{
			// All weight in Z (target 20%), none in X/Y (target 40% each) → drift 0.8.
			// Verifies the formula is not artificially capped at 0.5.
			quotes:      map[string]float64{"NSE:X": 100, "NSE:Y": 100, "NSE:Z": 100},
			holdings:    []broker.Holding{{Exchange: "NSE", TradingSymbol: "Z", Quantity: 100}},
			target:      map[string]float64{"NSE:X": 0.4, "NSE:Y": 0.4, "NSE:Z": 0.2},
			keys:        []string{"NSE:X", "NSE:Y", "NSE:Z"},
			wantAtLeast: 0.8 - 1e-9, wantAtMost: 0.8 + 1e-9,
		},
	}
	for _, tc := range cases {
		res, err := CalculateDrift(context.Background(), &stubBroker{quotes: tc.quotes, holdings: tc.holdings}, tc.target, tc.keys)
		if err != nil {
			t.Fatal(err)
		}
		if res.DriftIndex < tc.wantAtLeast || res.DriftIndex > tc.wantAtMost {
			t.Errorf("drift %.6f not in [%.6f, %.6f]", res.DriftIndex, tc.wantAtLeast, tc.wantAtMost)
		}
	}
}

func TestCalculateDrift_CheckedAtSet(t *testing.T) {
	before := time.Now().Add(-time.Second)
	b := &stubBroker{quotes: map[string]float64{"NSE:X": 100}}
	res, err := CalculateDrift(context.Background(), b, map[string]float64{"NSE:X": 1.0}, []string{"NSE:X"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.CheckedAt.After(before) {
		t.Errorf("CheckedAt %v not after %v", res.CheckedAt, before)
	}
}

func TestNextMarketClose_AlwaysFuture(t *testing.T) {
	mktCfg := broker.LoadMarketConfig()
	next := nextMarketClose(mktCfg)
	if !next.After(time.Now()) {
		t.Errorf("nextMarketClose() returned %v which is not in the future", next)
	}
}

func TestNextMarketClose_CorrectTime(t *testing.T) {
	mktCfg := broker.MarketConfigForName("india")
	next := nextMarketClose(mktCfg)
	ist, _ := time.LoadLocation("Asia/Kolkata")
	inIST := next.In(ist)
	// India market close is 15:30, check fires at 15:45
	if inIST.Hour() != 15 || inIST.Minute() != 45 || inIST.Second() != 0 {
		t.Errorf("nextMarketClose(india) in IST = %02d:%02d:%02d, want 15:45:00",
			inIST.Hour(), inIST.Minute(), inIST.Second())
	}
}

func TestNextMarketClose_WithinOneDay(t *testing.T) {
	mktCfg := broker.LoadMarketConfig()
	next := nextMarketClose(mktCfg)
	if d := time.Until(next); d > 25*time.Hour {
		t.Errorf("nextMarketClose() is %v away, expected ≤ 25h", d.Round(time.Minute))
	}
}
