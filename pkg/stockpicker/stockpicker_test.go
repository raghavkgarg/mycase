package stockpicker

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/selectiontracker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

func TestIsAbove200DaySMA(t *testing.T) {
	tests := []struct {
		name     string
		prices   []float64
		expected bool
	}{
		{
			name:     "Less than 200 days - bypass check",
			prices:   make([]float64, 199),
			expected: true,
		},
		{
			name:     "Exactly 200 days - latest price above average",
			prices:   append(make([]float64, 199), 10.0), // all others 0.0, average is 10/200 = 0.05. Latest is 10.0.
			expected: true,
		},
		{
			name: "Exactly 200 days - latest price below average",
			prices: func() []float64 {
				p := make([]float64, 200)
				for i := range 199 {
					p[i] = 100.0
				}
				p[199] = 50.0 // average is around ~99.75. Latest is 50.0.
				return p
			}(),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAbove200DaySMA(tt.prices)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestNormalizeValue(t *testing.T) {
	tests := []struct {
		name           string
		val            float64
		minVal         float64
		maxVal         float64
		maxPoints      float64
		higherIsBetter bool
		expected       float64
	}{
		{
			name:           "Min equals Max - returns max points",
			val:            10.0,
			minVal:         10.0,
			maxVal:         10.0,
			maxPoints:      50.0,
			higherIsBetter: true,
			expected:       50.0,
		},
		{
			name:           "Higher is better - midpoint",
			val:            15.0,
			minVal:         10.0,
			maxVal:         20.0,
			maxPoints:      100.0,
			higherIsBetter: true,
			expected:       50.0,
		},
		{
			name:           "Higher is better - max val",
			val:            20.0,
			minVal:         10.0,
			maxVal:         20.0,
			maxPoints:      100.0,
			higherIsBetter: true,
			expected:       100.0,
		},
		{
			name:           "Higher is better - min val",
			val:            10.0,
			minVal:         10.0,
			maxVal:         20.0,
			maxPoints:      100.0,
			higherIsBetter: true,
			expected:       0.0,
		},
		{
			name:           "Lower is better - midpoint",
			val:            15.0,
			minVal:         10.0,
			maxVal:         20.0,
			maxPoints:      100.0,
			higherIsBetter: false,
			expected:       50.0,
		},
		{
			name:           "Lower is better - min val gets max points",
			val:            10.0,
			minVal:         10.0,
			maxVal:         20.0,
			maxPoints:      100.0,
			higherIsBetter: false,
			expected:       100.0,
		},
		{
			name:           "Lower is better - max val gets zero points",
			val:            20.0,
			minVal:         10.0,
			maxVal:         20.0,
			maxPoints:      100.0,
			higherIsBetter: false,
			expected:       0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeValue(tt.val, tt.minVal, tt.maxVal, tt.maxPoints, tt.higherIsBetter)
			if math.Abs(result-tt.expected) > 1e-9 {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestLoadLocalCSVConstituents(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "stockpicker_pkg_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	csvPath := filepath.Join(tempDir, "tickers.csv")
	csvContent := `ticker,name
RELIANCE,Reliance Industries
NSE:TCS,Tata Consultancy Services
BSE:500112,State Bank of India
`
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write test CSV: %v", err)
	}

	tickers, err := loadLocalCSVConstituents(csvPath)
	if err != nil {
		t.Fatalf("unexpected error loading constituents: %v", err)
	}

	expectedTickers := []string{"NSE:RELIANCE", "NSE:TCS", "BSE:500112"}
	if len(tickers) != len(expectedTickers) {
		t.Fatalf("expected %d tickers, got %d", len(expectedTickers), len(tickers))
	}

	for i, expected := range expectedTickers {
		if tickers[i] != expected {
			t.Errorf("at index %d: expected %s, got %s", i, expected, tickers[i])
		}
	}
}

func TestIsEligible(t *testing.T) {
	mockFundamentals := yfinance.Fundamentals{
		MarketCap:               1000.0,
		RegularPrice:            100.0,
		AverageVolume:           10.0,
		HeldPercentInstitutions: 0.15,
		InsidersPercent:         0.60,
	}

	mockFilters := config.HardFilters{
		MinMarketCap: 500.0,
		MaxMarketCap: 5000.0,
		MinADV:       100.0,
	}

	stats := &FilterStats{}
	eligible, _ := isEligible(
		"NSE:TEST",
		mockFundamentals,
		"balanced",
		&mockFilters,
		[]float64{100, 101, 102},
		[]float64{99, 100, 101},
		[]float64{1000, 1000, 1000},
		nil,
		stats,
	)

	if !eligible {
		t.Errorf("expected stock to be eligible, but it was filtered out")
	}

	eligibleLowCap, _ := isEligible(
		"NSE:TEST",
		mockFundamentals,
		"balanced",
		&mockFilters,
		[]float64{100, 101, 102},
		[]float64{99, 100, 101},
		[]float64{1000, 1000, 1000},
		nil,
		stats,
	)

	// Since mockFilters.MinMarketCap is 500 and market cap is 1000, it passes.
	if !eligibleLowCap {
		t.Errorf("expected stock to pass, but it failed")
	}

	// Change filter to fail
	mockFilters.MinMarketCap = 2000.0
	eligibleLowCapFail, _ := isEligible(
		"NSE:TEST",
		mockFundamentals,
		"balanced",
		&mockFilters,
		[]float64{100, 101, 102},
		[]float64{99, 100, 101},
		[]float64{1000, 1000, 1000},
		nil,
		stats,
	)
	if eligibleLowCapFail {
		t.Errorf("expected stock to fail due to low market cap, but it passed")
	}
	if stats.EliminatedSize != 1 {
		t.Errorf("expected EliminatedSize to be 1, got %d", stats.EliminatedSize)
	}
}

func TestNormalizeValue_OutsideRange(t *testing.T) {
	// val < minVal: higherIsBetter → should return 0 (clamped)
	got := normalizeValue(5.0, 10.0, 20.0, 100.0, true)
	if got < 0 {
		t.Errorf("val below minVal with higherIsBetter: expected ≥ 0, got %f", got)
	}
	// val > maxVal: higherIsBetter → extrapolates above maxPoints but we just verify no panic
	_ = normalizeValue(25.0, 10.0, 20.0, 100.0, true)
}

func TestApplyHysteresisSelection_NoExisting(t *testing.T) {
	sorted := []string{"A", "B", "C", "D", "E"}
	existing := map[string]float64{}
	tracker := selectiontracker.New()
	selected := ApplyHysteresisSelection(sorted, existing, 3, 5, tracker)
	if len(selected) != 3 {
		t.Fatalf("expected 3 selected, got %d: %v", len(selected), selected)
	}
	// Must be the top 3
	for i, want := range []string{"A", "B", "C"} {
		if selected[i] != want {
			t.Errorf("position %d: want %s, got %s", i, want, selected[i])
		}
	}
}

func TestApplyHysteresisSelection_AllFit(t *testing.T) {
	sorted := []string{"A", "B", "C"}
	existing := map[string]float64{}
	tracker := selectiontracker.New()
	selected := ApplyHysteresisSelection(sorted, existing, 5, 7, tracker)
	// Fewer candidates than topN → all returned
	if len(selected) != 3 {
		t.Fatalf("expected all 3 returned when candidates < topN, got %d", len(selected))
	}
}

func TestApplyHysteresisSelection_RetainsExisting(t *testing.T) {
	// "E" is an existing holding at rank 5 (within buffer 6) → should be retained over rank-4 new candidate "D"
	sorted := []string{"A", "B", "C", "D", "E", "F"}
	existing := map[string]float64{"E": 0.2}
	tracker := selectiontracker.New()
	selected := ApplyHysteresisSelection(sorted, existing, 3, 6, tracker)
	if len(selected) != 3 {
		t.Fatalf("expected 3 selected, got %d", len(selected))
	}
	found := false
	for _, s := range selected {
		if s == "E" {
			found = true
		}
	}
	if !found {
		t.Errorf("existing holding E should be retained via hysteresis: got %v", selected)
	}
}

func TestApplyRebalancingBand_NoExisting(t *testing.T) {
	target := map[string]float64{"A": 0.6, "B": 0.4}
	result := ApplyRebalancingBand([]string{"A", "B"}, target, nil, 0.10)
	if result["A"] != 0.6 || result["B"] != 0.4 {
		t.Errorf("no existing holdings: target weights should be returned unchanged: %v", result)
	}
}

func TestApplyRebalancingBand_BeyondTolerance(t *testing.T) {
	// Difference is 0.30, tolerance is 0.10 (→ limit 0.001) → change exceeds tolerance → use target
	target := map[string]float64{"A": 0.7, "B": 0.3}
	existing := map[string]float64{"A": 0.4, "B": 0.6}
	result := ApplyRebalancingBand([]string{"A", "B"}, target, existing, 0.10)
	if math.Abs(result["A"]-0.7) > 1e-6 {
		t.Errorf("large diff should use target weight: want 0.7, got %f", result["A"])
	}
}
