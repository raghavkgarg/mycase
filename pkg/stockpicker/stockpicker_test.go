package stockpicker

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/gkgarg24/mycase/pkg/config"
	"github.com/gkgarg24/mycase/pkg/yfinance"
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
				for i := 0; i < 199; i++ {
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
