package stockpicker

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestCheck200DaySMATrend(t *testing.T) {
	// 200 prices at 100.0 (SMA200 = 100)
	base200 := make([]float64, 200)
	for i := range base200 {
		base200[i] = 100.0
	}

	// 1. Dips 3% below 200-SMA (97.0), minRatio 0.95 -> Passes
	p1 := make([]float64, 220)
	for i := range p1 {
		p1[i] = 100.0
	}
	p1[219] = 97.0 // 3% dip below 100.0
	ok, _ := check200DaySMATrend(p1, 0.95)
	if !ok {
		t.Errorf("expected pass for 3%% dip with 0.95 ratio floor, got fail")
	}

	// 2. Dips 8% below 200-SMA (92.0), minRatio 0.95 -> Fails ratio floor
	p2 := make([]float64, 220)
	for i := range p2 {
		p2[i] = 100.0
	}
	p2[219] = 92.0
	ok, reason := check200DaySMATrend(p2, 0.95)
	if ok {
		t.Errorf("expected fail for 8%% dip with 0.95 ratio floor, got pass")
	} else if !testing.Verbose() && reason == "" {
		t.Errorf("expected reason for failure")
	}

	// 3. Price below 200-SMA (98.0) AND 200-SMA is sloping DOWN -> Fails slope check
	p3 := make([]float64, 220)
	for i := 0; i < 20; i++ {
		p3[i] = 150.0 // Past 200-SMA was high (~150)
	}
	for i := 20; i < 219; i++ {
		p3[i] = 100.0 // Today 200-SMA is lower (~100) -> downward slope
	}
	p3[219] = 98.0 // 2% dip below current 200-SMA
	ok, _ = check200DaySMATrend(p3, 0.95)
	if ok {
		t.Errorf("expected fail for downward SMA slope, got pass")
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

func TestIsUSIndex(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"sp500 keyword", "sp500", true},
		{"S&P 500 formatted", "s&p 500", true},
		{"nasdaq keyword", "nasdaq100", true},
		{"qtum filename", "data/qtum.csv", true},
		{"qtum index", "qtum", true},
		{"us prefix file", "us_tech.csv", true},
		{"Indian index", "nifty50", false},
		{"Indian file", "data/nifty50.csv", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsUSIndex(tt.input)
			if result != tt.expected {
				t.Errorf("IsUSIndex(%q) = %v; want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestScoreEarlyMultibagger(t *testing.T) {
	ctx := t.Context()
	activeKeys := []string{"STOCK_A", "STOCK_B"}

	fundamentals := map[string]yfinance.Fundamentals{
		"STOCK_A": {
			Sector:           "Auto",
			MarketCap:        1e10,
			OperatingMargins: 0.18,
			AnnualOperatingIncome: []yfinance.AnnualMetric{
				{Date: "2025-03-31", Value: 180e7},
			},
			AnnualTotalAssets: []yfinance.AnnualMetric{
				{Date: "2025-03-31", Value: 1000e7},
			},
			AnnualCurrentLiabilities: []yfinance.AnnualMetric{
				{Date: "2025-03-31", Value: 200e7},
			},
			DeliveryPct: 65.0,
		},
		"STOCK_B": {
			Sector:           "IT",
			MarketCap:        2e10,
			OperatingMargins: 0.10,
			AnnualOperatingIncome: []yfinance.AnnualMetric{
				{Date: "2025-03-31", Value: 80e7},
			},
			AnnualTotalAssets: []yfinance.AnnualMetric{
				{Date: "2025-03-31", Value: 1000e7},
			},
			AnnualCurrentLiabilities: []yfinance.AnnualMetric{
				{Date: "2025-03-31", Value: 200e7},
			},
			DeliveryPct: 20.0,
		},
	}

	// Stock A: tight base (VCP), near 52W high, positive momentum
	closesA := make([]float64, 100)
	opensA := make([]float64, 100)
	volsA := make([]float64, 100)
	for i := range closesA {
		closesA[i] = 100.0 + float64(i)*0.1
		opensA[i] = 100.0
		volsA[i] = 1000.0
	}
	closesA[99] = 110.0 // at 52W high

	// Stock B: loose wide fluctuations, far from 52W high
	closesB := make([]float64, 100)
	opensB := make([]float64, 100)
	volsB := make([]float64, 100)
	for i := range closesB {
		if i%2 == 0 {
			closesB[i] = 150.0
			opensB[i] = 80.0
		} else {
			closesB[i] = 80.0
			opensB[i] = 150.0
		}
		volsB[i] = 1000.0
	}
	closesB[99] = 85.0 // far from 150 peak

	fullHistory := map[string]*yfinance.HistoricalData{
		"STOCK_A": {Closes: closesA, Opens: opensA, Volumes: volsA},
		"STOCK_B": {Closes: closesB, Opens: opensB, Volumes: volsB},
	}

	hardFilters := &config.HardFilters{
		ScoreWeightIdiosyncraticRS: 25.0,
		ScoreWeightBaseVCP:         25.0,
		ScoreWeightVolumeFootprint: 25.0,
		ScoreWeightDeliveryDelta:   25.0,
	}

	scores := ScoreEarlyMultibagger(ctx, activeKeys, fundamentals, fullHistory, hardFilters)
	if scores["STOCK_A"] <= scores["STOCK_B"] {
		t.Errorf("Expected STOCK_A (tight base, 52W high prox, higher margin/delivery) to outscore STOCK_B: A=%.2f, B=%.2f",
			scores["STOCK_A"], scores["STOCK_B"])
	}
}

func TestNormalizeAndCapWeights_SectorCapViolations(t *testing.T) {
	// Synthetic scenario: 4 high-scoring stocks in Industrials, 1 in Consumer
	tickers := []string{"IND1", "IND2", "IND3", "IND4", "CNS1"}
	scores := map[string]float64{
		"IND1": 90.0,
		"IND2": 85.0,
		"IND3": 80.0,
		"IND4": 75.0,
		"CNS1": 40.0,
	}
	fundamentals := map[string]yfinance.Fundamentals{
		"IND1": {Sector: "Industrials"},
		"IND2": {Sector: "Industrials"},
		"IND3": {Sector: "Industrials"},
		"IND4": {Sector: "Industrials"},
		"CNS1": {Sector: "Consumer Cyclical"},
	}

	stockCap := 0.20
	sectorCap := 0.25

	weights := make(map[string]float64)
	var sumScore float64
	for _, s := range scores {
		sumScore += s
	}
	for _, t := range tickers {
		weights[t] = scores[t] / sumScore
	}

	NormalizeAndCapWeights(tickers, weights, fundamentals, stockCap, sectorCap, true)

	// Check 1: Individual stock cap
	for ticker, w := range weights {
		if w > stockCap+1e-6 {
			t.Errorf("stock %s exceeded individual stock cap %.2f: got %.4f", ticker, stockCap, w)
		}
	}

	// Check 2: Sector weight cap
	sectorSums := make(map[string]float64)
	for ticker, w := range weights {
		sec := fundamentals[ticker].Sector
		sectorSums[sec] += w
	}

	for sec, sum := range sectorSums {
		if sum > sectorCap+1e-6 {
			t.Errorf("sector %s exceeded sector cap %.2f: got %.4f", sec, sectorCap, sum)
		}
	}

	if sectorSums["Industrials"] > 0.250001 {
		t.Errorf("Industrials sector weight should be <= 0.25, got %.6f", sectorSums["Industrials"])
	}
}

func TestNormalizeAndCapWeights_AllowCashFalse(t *testing.T) {
	// 1 stock in 1 sector with allowCash = false should dynamically floor sectorCap to 1.0 (100%)
	tickers := []string{"STK1"}
	weights := map[string]float64{"STK1": 1.0}
	fundamentals := map[string]yfinance.Fundamentals{
		"STK1": {Sector: "Healthcare"},
	}

	NormalizeAndCapWeights(tickers, weights, fundamentals, 0.20, 0.25, false)
	if math.Abs(weights["STK1"]-1.0) > 1e-4 {
		t.Errorf("expected STK1 weight 1.0000 when allowCash=false, got %.4f", weights["STK1"])
	}
}

func TestCheckROCE_PITLagFiltering(t *testing.T) {
	// Synthetic fundamentals:
	// 2025-03-31 has high ROCE (EBIT 100 on CE 200 = 50%), but was reported on 2025-03-31
	// 2024-03-31 has low ROCE (EBIT 10 on CE 200 = 5%)
	f := &yfinance.Fundamentals{
		AnnualOperatingIncome: []yfinance.AnnualMetric{
			{Date: "2024-03-31", Value: 10.0},
			{Date: "2025-03-31", Value: 100.0},
		},
		AnnualTotalAssets: []yfinance.AnnualMetric{
			{Date: "2024-03-31", Value: 300.0},
			{Date: "2025-03-31", Value: 300.0},
		},
		AnnualCurrentLiabilities: []yfinance.AnnualMetric{
			{Date: "2024-03-31", Value: 100.0},
			{Date: "2025-03-31", Value: 100.0},
		},
	}

	minROCE := 0.12 // 12%

	// Query as of 2025-04-15 with 45-day lag:
	// 2025-04-15 - 45 days = 2025-03-01 -> 2025-03-31 is NOT yet known!
	asOfBeforeLag := time.Date(2025, 4, 15, 0, 0, 0, 0, time.UTC)
	passedBeforeLag := checkROCE(f, minROCE, asOfBeforeLag, 45)
	if passedBeforeLag {
		t.Errorf("expected ROCE check to fail before 45-day lag expiration, but passed")
	}

	// Query as of 2025-06-01 with 45-day lag:
	// 2025-06-01 - 45 days = 2025-04-17 -> 2025-03-31 is now known!
	asOfAfterLag := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	passedAfterLag := checkROCE(f, minROCE, asOfAfterLag, 45)
	if !passedAfterLag {
		t.Errorf("expected ROCE check to pass after 45-day lag expiration, but failed")
	}
}

func TestPickDeterminism(t *testing.T) {
	istLoc, _ := time.LoadLocation("Asia/Kolkata")
	ctx := context.Background()

	// Today's date (simulated session date)
	todayDate := time.Date(2026, 8, 26, 0, 0, 0, 0, istLoc)
	wallClock10AM := time.Date(2026, 8, 26, 10, 0, 0, 0, istLoc)
	wallClock14PM := time.Date(2026, 8, 26, 14, 30, 0, 0, istLoc)
	wallClock16PM := time.Date(2026, 8, 26, 16, 30, 0, 0, istLoc)

	// Historical timestamps
	tsTminus2 := time.Date(2026, 8, 24, 15, 30, 0, 0, istLoc).Unix()
	tsTminus1 := time.Date(2026, 8, 25, 15, 30, 0, 0, istLoc).Unix()
	tsTodayLive10AM := time.Date(2026, 8, 26, 10, 0, 0, 0, istLoc).Unix()
	tsTodayLive14PM := time.Date(2026, 8, 26, 14, 30, 0, 0, istLoc).Unix()
	tsTodayConfirmed := time.Date(2026, 8, 26, 15, 30, 0, 0, istLoc).Unix()

	// Candidate A (High traction)
	rawHistA_Morning := &yfinance.HistoricalData{
		Timestamps: []int64{tsTminus2, tsTminus1, tsTodayLive10AM},
		Closes:     []float64{100.0, 105.0, 108.5}, // 108.5 is partial live bar
		Opens:      []float64{99.0, 101.0, 105.0},
		Volumes:    []float64{1000.0, 2000.0, 600.0},
	}
	rawHistA_Afternoon := &yfinance.HistoricalData{
		Timestamps: []int64{tsTminus2, tsTminus1, tsTodayLive14PM},
		Closes:     []float64{100.0, 105.0, 107.0}, // 107.0 is partial live bar
		Opens:      []float64{99.0, 101.0, 105.0},
		Volumes:    []float64{1000.0, 2000.0, 1500.0},
	}
	rawHistA_EOD := &yfinance.HistoricalData{
		Timestamps: []int64{tsTminus2, tsTminus1, tsTodayConfirmed},
		Closes:     []float64{100.0, 105.0, 109.0}, // 109.0 is confirmed close
		Opens:      []float64{99.0, 101.0, 105.0},
		Volumes:    []float64{1000.0, 2000.0, 2500.0},
	}

	// Candidate B
	rawHistB_Morning := &yfinance.HistoricalData{
		Timestamps: []int64{tsTminus2, tsTminus1, tsTodayLive10AM},
		Closes:     []float64{50.0, 52.0, 53.5},
		Opens:      []float64{49.0, 50.0, 52.0},
		Volumes:    []float64{500.0, 1000.0, 300.0},
	}
	rawHistB_Afternoon := &yfinance.HistoricalData{
		Timestamps: []int64{tsTminus2, tsTminus1, tsTodayLive14PM},
		Closes:     []float64{50.0, 52.0, 54.0},
		Opens:      []float64{49.0, 50.0, 52.0},
		Volumes:    []float64{500.0, 1000.0, 800.0},
	}

	fundamentals := map[string]yfinance.Fundamentals{
		"STOCK_A": {Sector: "Industrials", MarketCap: 1000e7, InsidersPercent: 0.50, DeliveryPct: 50.0},
		"STOCK_B": {Sector: "Consumer Cyclical", MarketCap: 500e7, InsidersPercent: 0.40, DeliveryPct: 45.0},
	}
	hardFilters := &config.HardFilters{
		MinEffectiveScoreThreshold: 10.0,
		MaxStocksPerSector:         5,
		MaxSectorWeightCap:         0.50,
		MaxStockWeightCap:          0.60,
	}

	// 1. Simulation at 10:00 AM (Target date = Today)
	hist10AM := map[string]*yfinance.HistoricalData{
		"STOCK_A": {
			Timestamps: append([]int64{}, rawHistA_Morning.Timestamps...),
			Closes:     append([]float64{}, rawHistA_Morning.Closes...),
			Opens:      append([]float64{}, rawHistA_Morning.Opens...),
			Volumes:    append([]float64{}, rawHistA_Morning.Volumes...),
		},
		"STOCK_B": {
			Timestamps: append([]int64{}, rawHistB_Morning.Timestamps...),
			Closes:     append([]float64{}, rawHistB_Morning.Closes...),
			Opens:      append([]float64{}, rawHistB_Morning.Opens...),
			Volumes:    append([]float64{}, rawHistB_Morning.Volumes...),
		},
	}
	for _, h := range hist10AM {
		h.CleanIntradayNoiseAsOf(wallClock10AM)
	}

	// 2. Simulation at 14:30 PM (Target date = Today)
	hist14PM := map[string]*yfinance.HistoricalData{
		"STOCK_A": {
			Timestamps: append([]int64{}, rawHistA_Afternoon.Timestamps...),
			Closes:     append([]float64{}, rawHistA_Afternoon.Closes...),
			Opens:      append([]float64{}, rawHistA_Afternoon.Opens...),
			Volumes:    append([]float64{}, rawHistA_Afternoon.Volumes...),
		},
		"STOCK_B": {
			Timestamps: append([]int64{}, rawHistB_Afternoon.Timestamps...),
			Closes:     append([]float64{}, rawHistB_Afternoon.Closes...),
			Opens:      append([]float64{}, rawHistB_Afternoon.Opens...),
			Volumes:    append([]float64{}, rawHistB_Afternoon.Volumes...),
		},
	}
	for _, h := range hist14PM {
		h.CleanIntradayNoiseAsOf(wallClock14PM)
	}

	// Assert that BOTH morning and afternoon runs truncated the partial bar for Today
	if len(hist10AM["STOCK_A"].Closes) != 2 || len(hist14PM["STOCK_A"].Closes) != 2 {
		t.Fatalf("both intraday queries before 15:45 IST must truncate today's partial bar: got 10AM len=%d, 14PM len=%d",
			len(hist10AM["STOCK_A"].Closes), len(hist14PM["STOCK_A"].Closes))
	}

	// Run full end-to-end scoring pipeline on both intraday runs
	activeKeys := []string{"STOCK_A", "STOCK_B"}
	scores10AM := ScoreEarlyMultibagger(ctx, activeKeys, fundamentals, hist10AM, hardFilters)
	scores14PM := ScoreEarlyMultibagger(ctx, activeKeys, fundamentals, hist14PM, hardFilters)

	// Assert byte-identical scores between 10:00 AM and 14:30 PM
	for _, sym := range activeKeys {
		if scores10AM[sym] != scores14PM[sym] {
			t.Errorf("intraday score mismatch for %s: 10AM=%.6f, 14PM=%.6f", sym, scores10AM[sym], scores14PM[sym])
		}
	}

	tracker10AM := selectiontracker.New()
	tracker14PM := selectiontracker.New()
	sel10AM := SelectTopNEarlyMultibagger(activeKeys, scores10AM, fundamentals, hist10AM, hardFilters, 2, nil, 0, tracker10AM)
	sel14PM := SelectTopNEarlyMultibagger(activeKeys, scores14PM, fundamentals, hist14PM, hardFilters, 2, nil, 0, tracker14PM)

	if len(sel10AM) != len(sel14PM) || len(sel10AM) != 2 {
		t.Fatalf("selection count mismatch: 10AM=%d, 14PM=%d, expected 2", len(sel10AM), len(sel14PM))
	}
	for i := range sel10AM {
		if sel10AM[i] != sel14PM[i] {
			t.Errorf("selection rank %d mismatch: 10AM=%s, 14PM=%s", i, sel10AM[i], sel14PM[i])
		}
	}

	weights10AM := NormalizeEarlyMultibaggerWeights(sel10AM, scores10AM, fundamentals, hardFilters, nil, 0.10)
	weights14PM := NormalizeEarlyMultibaggerWeights(sel14PM, scores14PM, fundamentals, hardFilters, nil, 0.10)

	for _, sym := range sel10AM {
		if weights10AM[sym] != weights14PM[sym] {
			t.Errorf("weight mismatch for %s: 10AM=%.6f, 14PM=%.6f", sym, weights10AM[sym], weights14PM[sym])
		}
	}

	// 3. Simulation at 16:30 PM (Post-market close, bar finalized)
	hist16PM := map[string]*yfinance.HistoricalData{
		"STOCK_A": {
			Timestamps: append([]int64{}, rawHistA_EOD.Timestamps...),
			Closes:     append([]float64{}, rawHistA_EOD.Closes...),
			Opens:      append([]float64{}, rawHistA_EOD.Opens...),
			Volumes:    append([]float64{}, rawHistA_EOD.Volumes...),
		},
	}
	for _, h := range hist16PM {
		h.CleanIntradayNoiseAsOf(wallClock16PM)
	}
	if len(hist16PM["STOCK_A"].Closes) != 3 {
		t.Fatalf("after market close (16:30 PM), confirmed bar must be retained; expected len 3, got %d", len(hist16PM["STOCK_A"].Closes))
	}
	_ = todayDate
}
