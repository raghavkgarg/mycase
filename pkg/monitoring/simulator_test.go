package monitoring

import (
	"math"
	"testing"

	"github.com/gkgarg24/mycase/pkg/yfinance"
)

func TestGetCapStallSeverity(t *testing.T) {
	tests := []struct {
		name      string
		ttmGrowth float64
		cagr3y    float64
		dsoDelta  float64
		expected  string
	}{
		{"None - passed both", 0.15, 0.14, 0.03, "None"},
		{"Mild - DSO <= 10%", 0.12, 0.15, 0.08, "Mild"},
		{"Moderate - 10% < DSO <= 20%", 0.32, 0.35, 0.18, "Moderate"},
		{"Severe - DSO > 20%", 0.15, 0.25, 0.22, "Severe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetCapStallSeverity(tt.ttmGrowth, tt.cagr3y, tt.dsoDelta)
			if got != tt.expected {
				t.Errorf("GetCapStallSeverity() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRunSimulation(t *testing.T) {
	// Create minimal mock data for testing
	portfolio := []StockInfo{
		{Ticker: "NSE:TEST1", Weight: 0.50},
		{Ticker: "NSE:TEST2", Weight: 0.50},
	}

	nDays := 250
	closes1 := make([]float64, nDays)
	closes2 := make([]float64, nDays)
	closesBench := make([]float64, nDays)
	vols := make([]float64, nDays)
	timestamps := make([]int64, nDays)

	// Simulate prices
	for i := 0; i < nDays; i++ {
		closes1[i] = 100.0 + float64(i)*0.2    // growth
		closes2[i] = 100.0 - float64(i)*0.1    // decline
		closesBench[i] = 100.0 + float64(i)*0.1 // standard growth
		vols[i] = 10000.0
		timestamps[i] = int64(1700000000 + i*86400)
	}

	histData := map[string]*yfinance.HistoricalData{
		"NSE:TEST1": {
			Closes:     closes1,
			Opens:      closes1,
			Volumes:    vols,
			Timestamps: timestamps,
		},
		"NSE:TEST2": {
			Closes:     closes2,
			Opens:      closes2,
			Volumes:    vols,
			Timestamps: timestamps,
		},
	}

	benchData := &yfinance.HistoricalData{
		Closes:     closesBench,
		Opens:      closesBench,
		Volumes:    vols,
		Timestamps: timestamps,
	}

	fundamentals := map[string]yfinance.Fundamentals{
		"NSE:TEST1": {
			Sector: "Tech",
			AnnualRevenue: []yfinance.AnnualMetric{
				{Date: "2021-03-31", Value: 100},
				{Date: "2022-03-31", Value: 120},
				{Date: "2023-03-31", Value: 144},
			},
			TTMRevenue: 180, // TTM growth > CAGR3Y
		},
		"NSE:TEST2": {
			Sector: "Finance",
			AnnualRevenue: []yfinance.AnnualMetric{
				{Date: "2021-03-31", Value: 100},
				{Date: "2022-03-31", Value: 90},
				{Date: "2023-03-31", Value: 80},
			},
			TTMRevenue: 70, // slowdown
		},
	}

	params := PolicyParams{
		ConsecutiveQuartersExit:   2,
		DSODeteriorationThreshold: 0.15,
		SMADays:                   10,
		RebalanceMonths:           6,
		MaxWeightDrift:            0.15,
	}

	res, err := RunSimulation(portfolio, params, histData, benchData, fundamentals, 100000.0)
	if err != nil {
		t.Fatalf("RunSimulation returned error: %v", err)
	}

	if res.InitialValue != 100000.0 {
		t.Errorf("expected initial value 100000, got %f", res.InitialValue)
	}

	if math.IsNaN(res.PortfolioReturn) || math.IsInf(res.PortfolioReturn, 0) {
		t.Errorf("portfolio return is NaN/Inf")
	}

	if len(res.Verdicts) != 2 {
		t.Errorf("expected 2 verdicts, got %d", len(res.Verdicts))
	}
}
