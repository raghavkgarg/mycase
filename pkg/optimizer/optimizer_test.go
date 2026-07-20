package optimizer

import (
	"reflect"
	"testing"
)

func TestOptimizeFreshBuy(t *testing.T) {
	basketKeys := []string{"NSE:SWSOLAR", "NSE:ADVAIT"}
	basket := map[string]float64{
		"NSE:SWSOLAR": 0.50,
		"NSE:ADVAIT":  0.50,
	}
	quoteData := map[string]float64{
		"NSE:SWSOLAR": 100.0,
		"NSE:ADVAIT":  100.0,
	}
	currentHoldings := map[string]int{
		"SWSOLAR": 0,
		"ADVAIT":  0,
	}

	// With budget of ₹1100, it should allocate 5 shares of each (accounting for 3% limit buffer)
	finalQtys := OptimizeFreshBuy(basketKeys, basket, quoteData, currentHoldings, 1100.0)
	expected := []int{5, 5}

	if !reflect.DeepEqual(finalQtys, expected) {
		t.Errorf("expected %v, got %v", expected, finalQtys)
	}
}

func TestVolatility(t *testing.T) {
	prices := []float64{100.0, 110.0, 121.0} // daily returns: +10% (+0.1), +10% (+0.1)
	returns := CalculateDailyReturns(prices)
	if len(returns) != 2 {
		t.Fatalf("expected 2 returns, got %d", len(returns))
	}
	for _, r := range returns {
		if mathAbs(r-0.1) > 0.0001 {
			t.Errorf("expected daily return of 0.1, got %f", r)
		}
	}

	vol := CalculateVolatility(returns)
	if vol != 0.0 {
		t.Errorf("expected volatility of 0.0 for constant returns, got %f", vol)
	}
}

func TestOptimizeInverseVolatility(t *testing.T) {
	tickers := []string{"NSE:STABLE", "NSE:VOLATILE"}
	// Stable stock: returns are constant
	stablePrices := []float64{100.0, 101.0, 102.0}
	// Volatile stock: returns fluctuate wildly
	volatilePrices := []float64{100.0, 150.0, 80.0}

	priceHistory := map[string][]float64{
		"NSE:STABLE":   stablePrices,
		"NSE:VOLATILE": volatilePrices,
	}

	weights := OptimizeInverseVolatility(tickers, priceHistory)
	if len(weights) != 2 {
		t.Fatalf("expected 2 weights, got %d", len(weights))
	}

	// Stable stock should get significantly higher weight than the volatile stock
	if weights["NSE:STABLE"] <= weights["NSE:VOLATILE"] {
		t.Errorf("expected stable stock to have higher weight than volatile stock. got stable=%f, volatile=%f", weights["NSE:STABLE"], weights["NSE:VOLATILE"])
	}
}

func mathAbs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestOptimizeFreshBuy_InsufficientBudget(t *testing.T) {
	// Budget covers baseline cost (2 shares at ~103 each) only barely — no greedy additions.
	basketKeys := []string{"NSE:EXPENSIVE", "NSE:ALSO_EXPENSIVE"}
	basket := map[string]float64{"NSE:EXPENSIVE": 0.50, "NSE:ALSO_EXPENSIVE": 0.50}
	quoteData := map[string]float64{"NSE:EXPENSIVE": 10000.0, "NSE:ALSO_EXPENSIVE": 10000.0}
	currentHoldings := map[string]int{"EXPENSIVE": 0, "ALSO_EXPENSIVE": 0}

	// Budget of ₹1 is way too small even for 1 share of each (baseline ~20600)
	// → falls back to current holdings (all zeros).
	result := OptimizeFreshBuy(basketKeys, basket, quoteData, currentHoldings, 1.0)
	for i, qty := range result {
		if qty != 0 {
			t.Errorf("index %d: expected 0 quantity (budget too small), got %d", i, qty)
		}
	}
}

func TestOptimizeFreshBuy_ExistingHoldings(t *testing.T) {
	// Already own shares → baseline cost is zero for owned stocks, greedy allocates more.
	basketKeys := []string{"NSE:SWSOLAR"}
	basket := map[string]float64{"NSE:SWSOLAR": 1.0}
	quoteData := map[string]float64{"NSE:SWSOLAR": 100.0}
	currentHoldings := map[string]int{"SWSOLAR": 5}

	result := OptimizeFreshBuy(basketKeys, basket, quoteData, currentHoldings, 500.0)
	if result[0] < 5 {
		t.Errorf("expected at least existing 5 shares, got %d", result[0])
	}
}

func TestCalculateDailyReturns_Empty(t *testing.T) {
	if r := CalculateDailyReturns(nil); r != nil {
		t.Errorf("expected nil for nil input, got %v", r)
	}
	if r := CalculateDailyReturns([]float64{}); r != nil {
		t.Errorf("expected nil for empty slice, got %v", r)
	}
}

func TestCalculateDailyReturns_SinglePrice(t *testing.T) {
	if r := CalculateDailyReturns([]float64{100.0}); r != nil {
		t.Errorf("expected nil for single-price slice, got %v", r)
	}
}

func TestCalculateVolatility_Empty(t *testing.T) {
	if v := CalculateVolatility(nil); v != 0.0 {
		t.Errorf("expected 0.0 for nil returns, got %f", v)
	}
	if v := CalculateVolatility([]float64{0.01}); v != 0.0 {
		t.Errorf("expected 0.0 for single-element returns, got %f", v)
	}
}

func TestCalculateUlcerIndex_Constant(t *testing.T) {
	prices := []float64{100, 100, 100, 100, 100}
	if u := CalculateUlcerIndex(prices); u != 0.0 {
		t.Errorf("constant prices should have ulcer=0.0, got %f", u)
	}
}

func TestCalculateUlcerIndex_AlwaysNonNegative(t *testing.T) {
	cases := [][]float64{
		{100, 90, 80, 70},
		{100, 110, 120, 130},
		{100, 50, 200, 10},
		{},
	}
	for _, prices := range cases {
		if u := CalculateUlcerIndex(prices); u < 0 {
			t.Errorf("ulcer index should be non-negative, got %f for %v", u, prices)
		}
	}
}

func TestOptimizeInverseVolatility_SumsToOne(t *testing.T) {
	tickers := []string{"A", "B", "C"}
	priceHistory := map[string][]float64{
		"A": {100, 101, 99, 102, 98},
		"B": {100, 105, 95, 110, 90},
		"C": {100, 100, 100, 100, 100},
	}
	weights := OptimizeInverseVolatility(tickers, priceHistory)
	var total float64
	for _, w := range weights {
		total += w
		if w <= 0 {
			t.Errorf("all weights should be positive, got %f", w)
		}
	}
	if mathAbs(total-1.0) > 1e-9 {
		t.Errorf("weights sum to %f, want 1.0", total)
	}
}

func TestOptimizeFreshBuy_ProportionalAllocation(t *testing.T) {
	// Tests expensive vs cheap stock allocation under budget
	basketKeys := []string{"NSE:EXPENSIVE", "NSE:CHEAP"}
	basket := map[string]float64{
		"NSE:EXPENSIVE": 0.50,
		"NSE:CHEAP":     0.50,
	}
	quoteData := map[string]float64{
		"NSE:EXPENSIVE": 1700.0,
		"NSE:CHEAP":     100.0,
	}
	currentHoldings := map[string]int{
		"EXPENSIVE": 0,
		"CHEAP":     0,
	}

	// With budget of ₹2000:
	// Expensive (limit ~1751): 1 share
	// Cheap (limit ~103): ~2 shares
	// Both stocks must receive non-zero allocations proportional to target weights
	finalQtys := OptimizeFreshBuy(basketKeys, basket, quoteData, currentHoldings, 2000.0)
	if finalQtys[0] < 1 {
		t.Errorf("expensive stock should receive at least 1 share, got %d", finalQtys[0])
	}
	if finalQtys[1] < 1 {
		t.Errorf("cheap stock should receive at least 1 share, got %d", finalQtys[1])
	}
}
