package optimizer

import (
	"reflect"
	"testing"
)

func TestOptimizeFreshBuy(t *testing.T) {
	basketKeys := []string{"NSE:SWSOLAR", "NSE:ADVAIT"}
	basket := map[string]float64{
		"NSE:SWSOLAR": 0.50,
		"NSE:ADVAIT":   0.50,
	}
	quoteData := map[string]float64{
		"NSE:SWSOLAR": 100.0,
		"NSE:ADVAIT":   100.0,
	}
	currentHoldings := map[string]int{
		"SWSOLAR": 0,
		"ADVAIT":   0,
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

