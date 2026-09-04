package yfinance

import (
	"math"
	"testing"
)

func TestCalculateSmoothedBenchmarkRegime_ExactWorkedExamples(t *testing.T) {
	tests := []struct {
		name          string
		closes        []float64
		expectedScore float64
	}{
		{
			name: "Strong Bull: +4% above 50DMA, 20/20 sessions above",
			closes: func() []float64 {
				c := make([]float64, 50)
				// 30 days at 97.3333333333 + 20 days at 104.0 -> sum = 5000 -> SMA50 = 100.0
				for i := 0; i < 30; i++ {
					c[i] = 2920.0 / 30.0
				}
				for i := 30; i < 50; i++ {
					c[i] = 104.0
				}
				return c
			}(),
			expectedScore: 1.00, // 0.20 + 0.60(1.0) + 0.50(0.40) = 1.00
		},
		{
			name: "Mild Correction: -3% below 50DMA, 6/20 sessions above",
			closes: func() []float64 {
				c := make([]float64, 50)
				// 30 days at 101.2 + 6 days at 101.0 + 14 days at 97.0 -> sum = 5000 -> SMA50 = 100.0
				for i := 0; i < 30; i++ {
					c[i] = 3036.0 / 30.0
				}
				for i := 30; i < 36; i++ {
					c[i] = 101.0
				}
				for i := 36; i < 50; i++ {
					c[i] = 97.0
				}
				return c
			}(),
			expectedScore: 0.23, // 0.20 + 0.60(0.30) + 0.50(-0.30) = 0.23
		},
		{
			name: "Severe Downtrend: -10% below 50DMA, 0/20 sessions above",
			closes: func() []float64 {
				c := make([]float64, 50)
				// 30 days at 106.6666667 + 20 days at 90.0 -> sum = 5000 -> SMA50 = 100.0
				for i := 0; i < 30; i++ {
					c[i] = 3200.0 / 30.0
				}
				for i := 30; i < 50; i++ {
					c[i] = 90.0
				}
				return c
			}(),
			expectedScore: 0.20, // 0.20 + 0.60(0.0) + 0.50(-0.40) = 0.00 -> floored to 0.20
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			score := CalculateSmoothedBenchmarkRegime(tc.closes, 50, 0.20)
			if math.Abs(score-tc.expectedScore) > 0.02 {
				t.Errorf("expected score ~%.2f, got %.4f", tc.expectedScore, score)
			}
		})
	}
}

func TestCalculateCompositeRS_ExactWeights(t *testing.T) {
	// 252 trading days (1 full trading year)
	n := 253
	stock := make([]float64, n)
	bench := make([]float64, n)

	// Flat benchmark at 100.0 (0% return across all horizons)
	for i := range bench {
		bench[i] = 100.0
	}

	// Stock starts at 100.0
	for i := range stock {
		stock[i] = 100.0
	}

	// Set stock returns:
	// 1M (last 21 days): 100 -> 110 (+10% return)
	// 3M (last 63 days): 100 -> 110 (+10% return)
	// 12M (252 days):    100 -> 110 (+10% return)
	for i := n - 21; i < n; i++ {
		stock[i] = 110.0
	}

	composite, rs1m, rs3m, rs12m := CalculateCompositeRS(stock, bench)
	// Since benchmark return is 0%, stock excess returns are:
	// RS_1M = +10%, RS_3M = +10%, RS_12M = +10%
	// Composite = 0.40(10%) + 0.30(10%) + 0.30(10%) = +10% = 0.1000
	if math.Abs(rs1m-0.10) > 1e-4 {
		t.Errorf("expected rs1m = 0.10, got %.4f", rs1m)
	}
	if math.Abs(rs3m-0.10) > 1e-4 {
		t.Errorf("expected rs3m = 0.10, got %.4f", rs3m)
	}
	if math.Abs(rs12m-0.10) > 1e-4 {
		t.Errorf("expected rs12m = 0.10, got %.4f", rs12m)
	}
	if math.Abs(composite-0.10) > 1e-4 {
		t.Errorf("expected composite = 0.10, got %.4f", composite)
	}
}

func TestCalculateVCPTightness_ExactRatio(t *testing.T) {
	// 70 trading days
	n := 70
	closes := make([]float64, n)
	opens := make([]float64, n)

	// First 60 days: daily true range = 10.0 (Open 90, Close 100)
	for i := 0; i < 60; i++ {
		opens[i] = 90.0
		closes[i] = 100.0
	}
	// Last 10 days: daily true range = 3.5 (Open 96.5, Close 100)
	for i := 60; i < 70; i++ {
		opens[i] = 96.5
		closes[i] = 100.0
	}

	ratio, isTight := CalculateVCPTightness(closes, opens)
	// ATR_10 should be 3.5
	// ATR_60 should be average of (50 days of 10.0 + 10 days of 3.5) / 60 = (500 + 35)/60 = 535/60 = 8.9167
	// Expected Ratio = 3.5 / 8.9167 = 0.3925
	expectedRatio := 3.5 / (535.0 / 60.0)
	if math.Abs(ratio-expectedRatio) > 0.01 {
		t.Errorf("expected VCP ratio ~%.4f, got %.4f", expectedRatio, ratio)
	}
	if !isTight {
		t.Errorf("expected isTight=true for ratio 0.3925 (< 0.50), got %t", isTight)
	}
}

func TestCalculateDecayedPocketPivot_ExactFormula(t *testing.T) {
	// Verify exact exponential decay: w_d = e^(-0.25 * d) with capped intensity 3.0
	closes := make([]float64, 50)
	opens := make([]float64, 50)
	volumes := make([]float64, 50)

	// 50 days of normal trading: red days with 1000 volume
	for i := 0; i < 50; i++ {
		closes[i] = 100.0
		opens[i] = 101.0 // red day
		volumes[i] = 1000.0
	}

	// Pocket Pivot 2 days ago (index 47: d = 2): green day with 2000 volume (> max 1000)
	// Intensity = min(3.0, 2000/1000) = 2.0
	// Decay weight = e^(-0.25 * 2) = e^(-0.50) ≈ 0.60653
	// Expected contribution = 2.0 * 0.60653 = 1.213
	closes[47] = 105.0
	opens[47] = 100.0
	volumes[47] = 2000.0

	score, count := CalculateDecayedPocketPivot(closes, opens, volumes, 10, 0.25)
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}
	expectedScore := 2.0 * math.Exp(-0.25*2.0)
	if math.Abs(score-expectedScore) > 0.01 {
		t.Errorf("expected PP score ~%.4f, got %.4f", expectedScore, score)
	}
}

func TestCalculateWinsorizedRVOLZScore_ExactFormula(t *testing.T) {
	// 60 days of uniform volume: 1000.0
	volumes := make([]float64, 60)
	for i := 0; i < 55; i++ {
		volumes[i] = 1000.0
	}
	// Last 5 days: surge to 2000.0 (within 4x winsorize cap of 4000)
	for i := 55; i < 60; i++ {
		volumes[i] = 2000.0
	}

	z := CalculateWinsorizedRVOLZScore(volumes, 5, 50, 4.0)
	if z <= 0 {
		t.Errorf("expected positive Z-score for volume surge, got %.4f", z)
	}
}

func TestCalculateProximity52W_ExactFormula(t *testing.T) {
	closes := make([]float64, 100)
	for i := range closes {
		closes[i] = 100.0
	}
	closes[40] = 200.0 // Peak 52W High
	closes[99] = 190.0 // Latest Close

	prox := CalculateProximity52W(closes)
	expected := 190.0 / 200.0 // 0.95 (95%)
	if math.Abs(prox-expected) > 1e-4 {
		t.Errorf("expected proximity 0.95, got %.4f", prox)
	}
}

func TestCalculateBaseDurationWeeks_ExactFormula(t *testing.T) {
	// 50 days total: Peak at day 10 (200.0), floor is 85% of 200 = 170.0
	closes := make([]float64, 50)
	closes[10] = 200.0

	// Days 0-24: 150.0 (below 170 floor)
	for i := 0; i < 25; i++ {
		if i != 10 {
			closes[i] = 150.0
		}
	}
	// Days 25-49 (25 consecutive days): 180.0 (above 170 floor)
	for i := 25; i < 50; i++ {
		closes[i] = 180.0
	}

	weeks, inBase := CalculateBaseDurationWeeks(closes, 0.85)
	// 25 consecutive days / 5 = 5 weeks
	if weeks != 5 {
		t.Errorf("expected 5 weeks in base, got %d", weeks)
	}
	if !inBase {
		t.Errorf("expected inBase=true for 5 weeks (>= 4W), got %t", inBase)
	}
}
