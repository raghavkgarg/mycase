package yfinance

import (
	"math"
	"testing"
)

func TestCalculateRSI_Insufficient(t *testing.T) {
	if got := CalculateRSI(nil); got != 50.0 {
		t.Errorf("nil input: want 50.0, got %f", got)
	}
	if got := CalculateRSI([]float64{100, 101, 102}); got != 50.0 {
		t.Errorf("< 15 prices: want 50.0, got %f", got)
	}
}

func TestCalculateRSI_AllUp(t *testing.T) {
	prices := make([]float64, 20)
	prices[0] = 100.0
	for i := 1; i < 20; i++ {
		prices[i] = prices[i-1] * 1.01
	}
	rsi := CalculateRSI(prices)
	if rsi <= 90.0 {
		t.Errorf("all-up prices: expected RSI > 90, got %f", rsi)
	}
	if rsi > 100.0 {
		t.Errorf("RSI exceeded 100: %f", rsi)
	}
}

func TestCalculateRSI_AllDown(t *testing.T) {
	prices := make([]float64, 20)
	prices[0] = 100.0
	for i := 1; i < 20; i++ {
		prices[i] = prices[i-1] * 0.99
	}
	rsi := CalculateRSI(prices)
	if rsi >= 10.0 {
		t.Errorf("all-down prices: expected RSI < 10, got %f", rsi)
	}
	if rsi < 0.0 {
		t.Errorf("RSI below 0: %f", rsi)
	}
}

func TestCalculateRSI_Alternating(t *testing.T) {
	prices := make([]float64, 30)
	prices[0] = 100.0
	for i := 1; i < 30; i++ {
		if i%2 == 1 {
			prices[i] = prices[i-1] * 1.01
		} else {
			prices[i] = prices[i-1] * 0.99
		}
	}
	rsi := CalculateRSI(prices)
	if rsi < 40.0 || rsi > 60.0 {
		t.Errorf("alternating prices: expected RSI near 50, got %f", rsi)
	}
}

func TestCalculateRSI_AlwaysInBounds(t *testing.T) {
	cases := [][]float64{
		{100, 101, 99, 102, 98, 103, 97, 104, 96, 105, 95, 106, 94, 107, 93},
		{100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100},
	}
	for _, prices := range cases {
		rsi := CalculateRSI(prices)
		if rsi < 0 || rsi > 100 {
			t.Errorf("RSI out of bounds [0,100]: %f for prices %v", rsi, prices)
		}
	}
}

func TestCalculateSalesGrowth_InsufficientHistory(t *testing.T) {
	f := Fundamentals{
		AnnualRevenue: []AnnualMetric{
			{Date: "2025-12-31", Value: 1000},
			{Date: "2026-12-31", Value: 1100},
		},
	}
	passed, ttm, cagr := CalculateSalesGrowth(&f)
	if passed {
		t.Error("expected false for < 3 annual revenue points")
	}
	if ttm != 0 || cagr != 0 {
		t.Errorf("expected 0,0 for insufficient history, got ttm=%f cagr=%f", ttm, cagr)
	}
}

func TestCalculateSalesGrowth_Accelerating(t *testing.T) {
	// CAGR over 2 years: (1200/1000)^(1/2)-1 ≈ 9.5%. TTM: 1400/1200-1 ≈ 16.7%.
	f := Fundamentals{
		TTMRevenue: 1400,
		AnnualRevenue: []AnnualMetric{
			{Date: "2024-12-31", Value: 1000},
			{Date: "2025-12-31", Value: 1100},
			{Date: "2026-12-31", Value: 1200},
		},
	}
	passed, ttm, cagr := CalculateSalesGrowth(&f)
	if !passed {
		t.Errorf("expected accelerating growth to pass: ttm=%f cagr=%f", ttm, cagr)
	}
	if ttm <= cagr {
		t.Errorf("ttm should exceed cagr when accelerating: ttm=%f cagr=%f", ttm, cagr)
	}
}

func TestCalculateSalesGrowth_Decelerating(t *testing.T) {
	// CAGR over 2 years: (1200/1000)^(1/2)-1 ≈ 9.5%.
	// TTMRevenue is well below latest annual (pctDiff > 2%), so baseRev = latest = 1200.
	// TTM growth: (1050/1200)-1 = -12.5% → clearly < CAGR.
	f := Fundamentals{
		TTMRevenue: 1050,
		AnnualRevenue: []AnnualMetric{
			{Date: "2024-12-31", Value: 1000},
			{Date: "2025-12-31", Value: 1100},
			{Date: "2026-12-31", Value: 1200},
		},
	}
	passed, ttm, cagr := CalculateSalesGrowth(&f)
	if passed {
		t.Errorf("expected decelerating growth to fail: ttm=%f cagr=%f", ttm, cagr)
	}
}

func TestCalculateDSO_InsufficientData(t *testing.T) {
	f := Fundamentals{}
	passed, prev, latest := CalculateDSO(&f)
	if passed || prev != 0 || latest != 0 {
		t.Errorf("empty fundamentals: want (false,0,0), got (%v,%f,%f)", passed, prev, latest)
	}
}

func TestCalculateDSO_Improving(t *testing.T) {
	// AR/Rev is lower in latest year → faster cash collection → DSO improves
	f := Fundamentals{
		AnnualRevenue: []AnnualMetric{
			{Date: "2024-12-31", Value: 1000},
			{Date: "2025-12-31", Value: 1100},
			{Date: "2026-12-31", Value: 1200},
		},
		AnnualAccountsReceivable: []AnnualMetric{
			{Date: "2024-12-31", Value: 200},  // DSO = 73 days
			{Date: "2025-12-31", Value: 198},  // DSO = 66 days
			{Date: "2026-12-31", Value: 180},  // DSO = 55 days
		},
	}
	passed, dsoPrev, dsoLatest := CalculateDSO(&f)
	if !passed {
		t.Errorf("improving DSO should pass: prev=%f latest=%f", dsoPrev, dsoLatest)
	}
	if dsoLatest >= dsoPrev {
		t.Errorf("expected dsoLatest < dsoPrev for improving DSO: %f >= %f", dsoLatest, dsoPrev)
	}
}

func TestCheckVolumeBreakout_ShortData(t *testing.T) {
	// Fewer than 2 data points → always false
	if CheckVolumeBreakout([]float64{100}, []float64{99}, []float64{1000}, 60, 2.0) {
		t.Error("single data point should return false")
	}
	if CheckVolumeBreakout(nil, nil, nil, 60, 2.0) {
		t.Error("nil slices should return false")
	}
}

func TestCheckVolumeBreakout_Clear(t *testing.T) {
	// 10 alternating red days (avg vol ~1000), then one massive green day (vol 5000 = 5× avg red)
	n := 11
	closes := make([]float64, n)
	opens := make([]float64, n)
	volumes := make([]float64, n)
	closes[0] = 100.0
	opens[0] = 101.0 // red day
	volumes[0] = 1000.0
	for i := 1; i < n-1; i++ {
		opens[i] = closes[i-1] + 0.5
		closes[i] = closes[i-1] - 0.5 // red
		volumes[i] = 1000.0
	}
	// Last day: big green on heavy volume
	opens[n-1] = closes[n-2]
	closes[n-1] = closes[n-2] + 5.0 // green
	volumes[n-1] = 5000.0

	if !CheckVolumeBreakout(closes, opens, volumes, 60, 2.0) {
		t.Error("clear volume breakout should return true")
	}
}

func TestCalculateRSI_ExactlyFifteen(t *testing.T) {
	// 15 prices is the minimum to get a real RSI (not the 50.0 default)
	prices := make([]float64, 15)
	prices[0] = 100.0
	for i := 1; i < 15; i++ {
		prices[i] = prices[i-1] + 1.0
	}
	rsi := CalculateRSI(prices)
	if rsi == 50.0 {
		t.Error("exactly 15 prices should compute real RSI, not return neutral 50.0")
	}
	if rsi < 0 || rsi > 100 {
		t.Errorf("RSI out of [0,100]: %f", rsi)
	}
}

func absF(x float64) float64 {
	return math.Abs(x)
}
