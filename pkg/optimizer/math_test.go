package optimizer

import (
	"math"
	"testing"
)

func TestCalculateMean_Empty(t *testing.T) {
	if got := CalculateMean(nil); got != 0 {
		t.Errorf("nil: want 0, got %f", got)
	}
	if got := CalculateMean([]float64{}); got != 0 {
		t.Errorf("empty: want 0, got %f", got)
	}
}

func TestCalculateMean_Known(t *testing.T) {
	want := 3.0
	if got := CalculateMean([]float64{1, 2, 3, 4, 5}); math.Abs(got-want) > 1e-9 {
		t.Errorf("want %f, got %f", want, got)
	}
}

func TestCalculateMean_Single(t *testing.T) {
	if got := CalculateMean([]float64{42.5}); got != 42.5 {
		t.Errorf("single value: want 42.5, got %f", got)
	}
}

func TestCalculateCovariance_MismatchedLengths(t *testing.T) {
	if got := CalculateCovariance([]float64{1, 2}, []float64{1}); got != 0 {
		t.Errorf("mismatched lengths: want 0, got %f", got)
	}
}

func TestCalculateCovariance_ShortSlice(t *testing.T) {
	if got := CalculateCovariance([]float64{1}, []float64{1}); got != 0 {
		t.Errorf("single-element: want 0, got %f", got)
	}
}

func TestCalculateCovariance_Known(t *testing.T) {
	// cov([1,2,3],[1,2,3]) = var([1,2,3]) = 1.0 (sample)
	x := []float64{1, 2, 3}
	got := CalculateCovariance(x, x)
	if math.Abs(got-1.0) > 1e-9 {
		t.Errorf("cov(x,x) should equal sample variance: want 1.0, got %f", got)
	}
}

func TestCalculateCovariance_Negative(t *testing.T) {
	// x goes up while y goes down → negative covariance
	x := []float64{1, 2, 3, 4}
	y := []float64{4, 3, 2, 1}
	got := CalculateCovariance(x, y)
	if got >= 0 {
		t.Errorf("inversely related series: covariance should be negative, got %f", got)
	}
}

func TestCalculateDownsideDeviation_AllPositive(t *testing.T) {
	// All returns above 0 target → falls back to 0.005
	returns := []float64{0.01, 0.02, 0.03, 0.04}
	got := CalculateDownsideDeviation(returns, 0.0)
	if got != 0.005 {
		t.Errorf("all positive returns: want fallback 0.005, got %f", got)
	}
}

func TestCalculateDownsideDeviation_AllNegative(t *testing.T) {
	returns := []float64{-0.01, -0.02, -0.03, -0.04}
	got := CalculateDownsideDeviation(returns, 0.0)
	if got <= 0 {
		t.Errorf("all negative returns: expected positive downside deviation, got %f", got)
	}
}

func TestCalculateDownsideDeviation_Short(t *testing.T) {
	if got := CalculateDownsideDeviation(nil, 0); got != 0 {
		t.Errorf("nil: want 0, got %f", got)
	}
	if got := CalculateDownsideDeviation([]float64{-0.01}, 0); got != 0 {
		t.Errorf("single element: want 0, got %f", got)
	}
}

func TestCalculateTotalReturn_Known(t *testing.T) {
	// 100 → 150 = +50%
	got := CalculateTotalReturn([]float64{100, 150})
	if math.Abs(got-0.5) > 1e-9 {
		t.Errorf("want 0.5, got %f", got)
	}
}

func TestCalculateTotalReturn_Edge(t *testing.T) {
	if got := CalculateTotalReturn(nil); got != 0 {
		t.Errorf("nil: want 0, got %f", got)
	}
	if got := CalculateTotalReturn([]float64{100}); got != 0 {
		t.Errorf("single price: want 0, got %f", got)
	}
}

func TestCalculateDailyReturns_ZeroPrice(t *testing.T) {
	// Zero price at i-1 should not divide by zero (returns 0 for that day)
	prices := []float64{0.0, 100.0, 110.0}
	returns := CalculateDailyReturns(prices)
	if returns == nil || len(returns) != 2 {
		t.Fatalf("expected 2 returns, got %v", returns)
	}
	if math.IsNaN(returns[0]) || math.IsInf(returns[0], 0) {
		t.Errorf("zero starting price should not produce NaN/Inf: got %f", returns[0])
	}
}

func TestCalculateUlcerIndex_AllDecline(t *testing.T) {
	// Continuously declining prices → significant ulcer index
	prices := []float64{100, 90, 80, 70, 60}
	got := CalculateUlcerIndex(prices)
	if got <= 0 {
		t.Errorf("declining prices: expected positive ulcer index, got %f", got)
	}
}

func TestCalculateUlcerIndex_AllRising(t *testing.T) {
	// Continuously rising: always at new high → drawdown = 0 at each step → ulcer = 0
	prices := []float64{100, 110, 120, 130, 140}
	got := CalculateUlcerIndex(prices)
	if got != 0.0 {
		t.Errorf("monotonically rising prices: expected ulcer=0, got %f", got)
	}
}
