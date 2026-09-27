package yfinance

import (
	"math"
	"testing"
)

func TestSectorWACC(t *testing.T) {
	tests := []struct {
		sector   string
		expected float64
	}{
		{"Consumer Defensive", 0.100},
		{"Healthcare", 0.100},
		{"Industrials", 0.110},
		{"Technology", 0.120},
		{"Energy", 0.130},
		{"Financial Services", 0.115},
		{"Unknown", 0.115},
	}

	for _, tt := range tests {
		got := SectorWACC(tt.sector)
		if math.Abs(got-tt.expected) > 1e-4 {
			t.Errorf("SectorWACC(%q) = %f, expected %f", tt.sector, got, tt.expected)
		}
	}
}

func TestCalculateDCFFairPrice(t *testing.T) {
	// Standard compounder stock
	f := &Fundamentals{
		RegularPrice: 500.0,
		MarketCap:    10000000000, // 1,000 Cr -> 20,000,000 shares
		TotalDebt:    500000000,   // 50 Cr
		AnnualFreeCashFlow: []AnnualMetric{
			{Date: "2024-03-31", Value: 600000000}, // 60 Cr
		},
		AnnualRevenue: []AnnualMetric{
			{Date: "2022-03-31", Value: 3000000000},
			{Date: "2023-03-31", Value: 3600000000},
			{Date: "2024-03-31", Value: 4320000000}, // ~20% CAGR
		},
		Sector: "Technology",
	}

	fp, ok := CalculateDCFFairPrice(f, 0.12, 0.05)
	if !ok || fp == nil {
		t.Fatalf("expected DCF fair price calculation to succeed")
	}
	if *fp <= 0 {
		t.Errorf("expected positive DCF fair price, got %f", *fp)
	}
}

func TestCalculateGrahamNumber(t *testing.T) {
	f := &Fundamentals{
		RegularPrice: 200.0,
		MarketCap:    1000000000, // 100 Cr -> 5,000,000 shares
		PBRatio:      2.0,        // BVPS = 100
		NetIncome:    100000000,  // 10 Cr -> EPS = 20
	}

	// Graham = sqrt(22.5 * 20 * 100) = sqrt(45000) ≈ 212.13
	expected := math.Sqrt(22.5 * 20.0 * 100.0)

	graham, ok := CalculateGrahamNumber(f)
	if !ok || graham == nil {
		t.Fatalf("expected Graham calculation to succeed")
	}
	if math.Abs(*graham-expected) > 0.1 {
		t.Errorf("expected Graham %f, got %f", expected, *graham)
	}

	// Test negative net income (should return false)
	fLoss := &Fundamentals{
		RegularPrice: 200.0,
		MarketCap:    1000000000,
		PBRatio:      2.0,
		NetIncome:    -50000000,
	}
	_, okLoss := CalculateGrahamNumber(fLoss)
	if okLoss {
		t.Errorf("expected Graham number to fail for loss-making stock")
	}
}

func TestCalculatePEGFairPrice(t *testing.T) {
	f := &Fundamentals{
		RegularPrice: 300.0,
		MarketCap:    3000000000, // 300 Cr -> 10,000,000 shares
		NetIncome:    200000000,  // 20 Cr -> EPS = 20
		ROE:          0.25,
		DebtToEquity: 0.20,
		AnnualRevenue: []AnnualMetric{
			{Date: "2022-03-31", Value: 1000000000},
			{Date: "2023-03-31", Value: 1200000000},
			{Date: "2024-03-31", Value: 1440000000}, // 20% CAGR
		},
	}

	pegPrice, ok := CalculatePEGFairPrice(f)
	if !ok || pegPrice == nil {
		t.Fatalf("expected PEG fair price to succeed")
	}
	if *pegPrice <= 0 {
		t.Errorf("expected positive PEG fair price, got %f", *pegPrice)
	}
}

func TestCalculateFairPrice_Ensemble(t *testing.T) {
	f := &Fundamentals{
		RegularPrice:      400.0,
		MarketCap:         4000000000, // 400 Cr -> 10,000,000 shares
		NetIncome:         300000000,  // 30 Cr -> EPS = 30
		PBRatio:           2.5,        // BVPS = 160
		TotalDebt:         200000000,  // 20 Cr
		OperatingCashflow: 350000000,  // 35 Cr
		AnnualFreeCashFlow: []AnnualMetric{
			{Date: "2024-03-31", Value: 250000000},
		},
		AnnualOperatingIncome: []AnnualMetric{
			{Date: "2022-03-31", Value: 280000000},
			{Date: "2023-03-31", Value: 340000000},
			{Date: "2024-03-31", Value: 420000000},
		},
		AnnualRevenue: []AnnualMetric{
			{Date: "2022-03-31", Value: 1500000000},
			{Date: "2023-03-31", Value: 1800000000},
			{Date: "2024-03-31", Value: 2200000000},
		},
		Sector: "Industrials",
	}

	medians := SectorMedians{
		PE:       20.0,
		PB:       3.0,
		EVEBITDA: 12.0,
	}

	res, err := CalculateFairPrice(f, medians)
	if err != nil {
		t.Fatalf("CalculateFairPrice failed: %v", err)
	}

	if res.ValidModelCount < 3 {
		t.Errorf("expected >= 3 valid models, got %d", res.ValidModelCount)
	}
	if res.EnsembleFairPrice <= 0 {
		t.Errorf("expected positive ensemble fair price, got %f", res.EnsembleFairPrice)
	}
	if res.PessimisticFairPrice >= res.EnsembleFairPrice {
		t.Errorf("expected pessimistic < ensemble, got pess=%f, ens=%f", res.PessimisticFairPrice, res.EnsembleFairPrice)
	}
	if res.OptimisticFairPrice <= res.EnsembleFairPrice {
		t.Errorf("expected optimistic > ensemble, got opt=%f, ens=%f", res.OptimisticFairPrice, res.EnsembleFairPrice)
	}
	if res.ConfidenceScore <= 0 || res.ConfidenceScore > 100 {
		t.Errorf("expected confidence score between 0 and 100, got %f", res.ConfidenceScore)
	}
}

func TestCalculateFairPrice_Clamping(t *testing.T) {
	// Artificially tiny CMP with huge cash flow to test upper clamp
	f := &Fundamentals{
		RegularPrice: 10.0,
		MarketCap:    10000000,
		NetIncome:    50000000,
		PBRatio:      0.5,
		AnnualFreeCashFlow: []AnnualMetric{
			{Date: "2024-03-31", Value: 40000000},
		},
		Sector: "Technology",
	}
	medians := SectorMedians{PE: 20.0, PB: 3.0, EVEBITDA: 12.0}

	res, err := CalculateFairPrice(f, medians)
	if err != nil {
		t.Fatalf("CalculateFairPrice failed: %v", err)
	}

	// Max clamp is 4.0 * CMP = 40.0
	if res.EnsembleFairPrice > 40.0+1e-4 {
		t.Errorf("expected clamped price <= 40.0, got %f", res.EnsembleFairPrice)
	}
	if !res.Clamped {
		t.Errorf("expected Clamped flag to be true")
	}
}
