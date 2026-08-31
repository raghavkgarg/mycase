package monitoring

import (
	"math"
	"testing"

	"github.com/raghavkgarg/mycase/pkg/yfinance"
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

func makeSimData(nDays int) ([]StockInfo, map[string]*yfinance.HistoricalData, *yfinance.HistoricalData, map[string]yfinance.Fundamentals) {
	closes1 := make([]float64, nDays)
	closes2 := make([]float64, nDays)
	closesBench := make([]float64, nDays)
	vols := make([]float64, nDays)
	timestamps := make([]int64, nDays)
	for i := range nDays {
		closes1[i] = 100.0 + float64(i)*0.2
		closes2[i] = 100.0 - float64(i)*0.1
		closesBench[i] = 100.0 + float64(i)*0.1
		vols[i] = 10000.0
		timestamps[i] = int64(1700000000 + i*86400)
	}
	portfolio := []StockInfo{
		{Ticker: "NSE:TEST1", Weight: 0.50},
		{Ticker: "NSE:TEST2", Weight: 0.50},
	}
	histData := map[string]*yfinance.HistoricalData{
		"NSE:TEST1": {Closes: closes1, Opens: closes1, Volumes: vols, Timestamps: timestamps},
		"NSE:TEST2": {Closes: closes2, Opens: closes2, Volumes: vols, Timestamps: timestamps},
	}
	benchData := &yfinance.HistoricalData{Closes: closesBench, Opens: closesBench, Volumes: vols, Timestamps: timestamps}
	fundamentals := map[string]yfinance.Fundamentals{
		"NSE:TEST1": {Sector: "Tech", AnnualRevenue: []yfinance.AnnualMetric{{Date: "2021-03-31", Value: 100}, {Date: "2022-03-31", Value: 120}, {Date: "2023-03-31", Value: 144}}, TTMRevenue: 180},
		"NSE:TEST2": {Sector: "Finance", AnnualRevenue: []yfinance.AnnualMetric{{Date: "2021-03-31", Value: 100}, {Date: "2022-03-31", Value: 90}, {Date: "2023-03-31", Value: 80}}, TTMRevenue: 70},
	}
	return portfolio, histData, benchData, fundamentals
}

func defaultParams() PolicyParams {
	return PolicyParams{
		ConsecutiveQuartersExit:   2,
		DSODeteriorationThreshold: 0.15,
		SMADays:                   10,
		RebalanceMonths:           6,
		MaxWeightDrift:            0.15,
	}
}

func TestRunSimulation(t *testing.T) {
	portfolio, histData, benchData, fundamentals := makeSimData(250)
	res, err := RunSimulation(portfolio, defaultParams(), histData, benchData, fundamentals, 100000.0)
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

func TestRunSimulation_Determinism(t *testing.T) {
	// RunSimulation must be stable: same inputs must produce results within float tolerance.
	// The simulator uses map iteration internally which can cause sub-epsilon variation.
	const tol = 1e-4
	portfolio, histData, benchData, fundamentals := makeSimData(300)
	params := defaultParams()
	res1, err1 := RunSimulation(portfolio, params, histData, benchData, fundamentals, 100000.0)
	res2, err2 := RunSimulation(portfolio, params, histData, benchData, fundamentals, 100000.0)
	if (err1 == nil) != (err2 == nil) {
		t.Fatalf("error mismatch: %v vs %v", err1, err2)
	}
	if err1 != nil {
		return
	}
	if math.Abs(res1.FinalValue-res2.FinalValue) > tol {
		t.Errorf("FinalValue unstable: %.10f vs %.10f (diff %.2e)", res1.FinalValue, res2.FinalValue, math.Abs(res1.FinalValue-res2.FinalValue))
	}
	if math.Abs(res1.PortfolioReturn-res2.PortfolioReturn) > tol {
		t.Errorf("PortfolioReturn unstable: %.10f vs %.10f", res1.PortfolioReturn, res2.PortfolioReturn)
	}
	if len(res1.Verdicts) != len(res2.Verdicts) {
		t.Errorf("Verdict count differs: %d vs %d", len(res1.Verdicts), len(res2.Verdicts))
	}
}

func TestRunSimulation_InsufficientHistory(t *testing.T) {
	// < 200 days of history should return an error, not panic.
	portfolio, histData, benchData, fundamentals := makeSimData(150)
	_, err := RunSimulation(portfolio, defaultParams(), histData, benchData, fundamentals, 100000.0)
	if err == nil {
		t.Error("expected error for insufficient history, got nil")
	}
}

func TestRunSimulation_EmptyPortfolio(t *testing.T) {
	_, histData, benchData, fundamentals := makeSimData(300)
	_, err := RunSimulation([]StockInfo{}, defaultParams(), histData, benchData, fundamentals, 100000.0)
	if err == nil {
		t.Error("expected error for empty portfolio, got nil")
	}
}

func TestRunSimulation_NoNaNInResult(t *testing.T) {
	portfolio, histData, benchData, fundamentals := makeSimData(300)
	res, err := RunSimulation(portfolio, defaultParams(), histData, benchData, fundamentals, 100000.0)
	if err != nil {
		t.Fatalf("simulation returned unexpected error: %v", err)
	}
	checks := map[string]float64{
		"InitialValue":    res.InitialValue,
		"FinalValue":      res.FinalValue,
		"PortfolioReturn": res.PortfolioReturn,
		"BenchmarkReturn": res.BenchmarkReturn,
	}
	for name, v := range checks {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("%s is NaN/Inf: %f", name, v)
		}
	}
}

func TestGetCapStallSeverity_Boundary(t *testing.T) {
	// ttmGrowth exactly equal to cagr3y — should still be "None" if dsoDelta is <= 5%.
	got := GetCapStallSeverity(0.15, 0.15, 0.04)
	if got != "None" {
		t.Errorf("equal TTM/CAGR with low DSO delta: want None, got %s", got)
	}
}

func TestGetCapStallSeverity_NegativeGrowth(t *testing.T) {
	// Both negative — classification should still work without panic.
	got := GetCapStallSeverity(-0.10, -0.05, 0.25)
	if got == "" {
		t.Error("expected a severity string, got empty")
	}
	// -0.10 < -0.05, TTM < CAGR and DSO > 20% → Severe
	if got != "Severe" {
		t.Errorf("expected Severe, got %s", got)
	}
}
