package stockpicker

import (
	"math"
	"strings"
	"testing"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/selectiontracker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

func TestCheckSentryTrendRupture(t *testing.T) {
	// Case 1: Healthy stock above SMA200 and low drawdown
	closes1 := make([]float64, 200)
	opens1 := make([]float64, 200)
	for i := range closes1 {
		closes1[i] = 100.0 + float64(i)*0.1 // uptrend from 100 to 120
		opens1[i] = closes1[i] + 1.0
	}
	hist1 := &yfinance.HistoricalData{
		Closes: closes1,
		Opens:  opens1,
	}
	isRupture, reason := CheckSentryTrendRupture(hist1)
	if isRupture {
		t.Errorf("expected healthy stock to pass, got rupture with reason: %s", reason)
	}

	// Case 2: Stock breaking down below 200-SMA (< 0.95 * SMA200)
	closes2 := make([]float64, 200)
	opens2 := make([]float64, 200)
	for i := 0; i < 190; i++ {
		closes2[i] = 100.0
		opens2[i] = 105.0
	}
	for i := 190; i < 200; i++ {
		closes2[i] = 90.0 // drops to 90, SMA200 ~ 99.5, 90 < 0.95*99.5 (94.5)
		opens2[i] = 92.0
	}
	hist2 := &yfinance.HistoricalData{
		Closes: closes2,
		Opens:  opens2,
	}
	isRupture2, reason2 := CheckSentryTrendRupture(hist2)
	if !isRupture2 {
		t.Errorf("expected SMA200 breakdown to trigger rupture")
	}
	if !strings.Contains(reason2, "200-SMA") {
		t.Errorf("expected reason to mention 200-SMA, got: %s", reason2)
	}

	// Case 3: Peak drawdown > 20% from 52W high
	closes3 := make([]float64, 200)
	opens3 := make([]float64, 200)
	for i := 0; i < 150; i++ {
		closes3[i] = 100.0
		opens3[i] = 100.0
	}
	opens3[50] = 150.0 // 52W high at 150.0
	// Last close is 115.0 -> DD is (150 - 115)/150 = 23.3% > 20%
	closes3[199] = 115.0
	hist3 := &yfinance.HistoricalData{
		Closes: closes3,
		Opens:  opens3,
	}
	isRupture3, reason3 := CheckSentryTrendRupture(hist3)
	if !isRupture3 {
		t.Errorf("expected 23.3%% peak drawdown to trigger rupture")
	}
	if !strings.Contains(reason3, "exceeds -20% limit") {
		t.Errorf("expected reason to mention -20%% limit, got: %s", reason3)
	}

	// Case 4: Nil or empty data
	isRupture4, _ := CheckSentryTrendRupture(nil)
	if isRupture4 {
		t.Errorf("nil data should not trigger rupture")
	}
	isRupture5, _ := CheckSentryTrendRupture(&yfinance.HistoricalData{Closes: []float64{math.NaN()}})
	if isRupture5 {
		t.Errorf("NaN-only data should not trigger rupture")
	}
}

func TestSentryGateSelectionWaterfall(t *testing.T) {
	// Candidate pool
	activeKeys := []string{"TICK_A", "TICK_B", "TICK_C", "TICK_D", "TICK_E"}
	scores := map[string]float64{
		"TICK_A": 50.0, // incumbent, but breaking down below SMA200
		"TICK_B": 45.0, // incumbent, healthy
		"TICK_C": 40.0, // newcomer, but breaking down below SMA200
		"TICK_D": 35.0, // newcomer, healthy
		"TICK_E": 30.0, // newcomer, healthy
	}
	existingHoldings := map[string]float64{
		"TICK_A": 0.50,
		"TICK_B": 0.50,
	}

	fundamentals := map[string]yfinance.Fundamentals{
		"TICK_A": {Sector: "Sector1"},
		"TICK_B": {Sector: "Sector2"},
		"TICK_C": {Sector: "Sector3"},
		"TICK_D": {Sector: "Sector4"},
		"TICK_E": {Sector: "Sector5"},
	}

	// Healthy price history (200 days of 100 -> 120)
	makeHealthyHist := func() *yfinance.HistoricalData {
		c := make([]float64, 200)
		for i := range c {
			c[i] = 100.0 + float64(i)*0.1
		}
		return &yfinance.HistoricalData{Closes: c, Opens: c}
	}

	// Ruptured price history (dropped to 80 below SMA200 ~ 100)
	makeRupturedHist := func() *yfinance.HistoricalData {
		c := make([]float64, 200)
		for i := 0; i < 190; i++ {
			c[i] = 100.0
		}
		for i := 190; i < 200; i++ {
			c[i] = 80.0
		}
		return &yfinance.HistoricalData{Closes: c, Opens: c}
	}

	fullHistory := map[string]*yfinance.HistoricalData{
		"TICK_A": makeRupturedHist(), // fails Sentry
		"TICK_B": makeHealthyHist(),  // passes Sentry
		"TICK_C": makeRupturedHist(), // fails Sentry
		"TICK_D": makeHealthyHist(),  // passes Sentry
		"TICK_E": makeHealthyHist(),  // passes Sentry
	}

	tracker := selectiontracker.New()
	hardFilters := &config.HardFilters{MaxStocksPerSector: 3}
	smartCfg := SmartHysteresisConfig{
		Scores:           scores,
		Fundamentals:     fundamentals,
		FullHistory:      fullHistory,
		EnableSentryGate: true,
	}

	// Request top 2 stocks
	selected := SelectTopNMultibaggerWithCooldown(
		activeKeys,
		scores,
		fundamentals,
		hardFilters,
		2, // topN
		existingHoldings,
		2, // hysteresisBuffer
		tracker,
		nil,
		0,
		0,
		smartCfg,
	)

	// TICK_A must be evicted despite having highest score and being incumbent!
	// TICK_B is retained (Rank 2, healthy).
	// TICK_C is rejected by Sentry Gate (Rank 3).
	// TICK_D is selected (Rank 4, healthy)!
	if len(selected) != 2 {
		t.Fatalf("expected 2 selected stocks, got %d: %v", len(selected), selected)
	}

	hasA := false
	hasB := false
	hasC := false
	hasD := false
	for _, s := range selected {
		if s == "TICK_A" {
			hasA = true
		}
		if s == "TICK_B" {
			hasB = true
		}
		if s == "TICK_C" {
			hasC = true
		}
		if s == "TICK_D" {
			hasD = true
		}
	}

	if hasA {
		t.Errorf("TICK_A should have been evicted by Sentry Gate")
	}
	if !hasB {
		t.Errorf("TICK_B should have been retained as healthy incumbent")
	}
	if hasC {
		t.Errorf("TICK_C should have been rejected by Sentry Gate")
	}
	if !hasD {
		t.Errorf("TICK_D should have been promoted as next healthy candidate")
	}

	// Verify tracker recorded drop reasons
	dropA, okA := tracker.HysteresisDrops["TICK_A"]
	if !okA || !strings.Contains(dropA, "Sentry Gate") {
		t.Errorf("expected Sentry Gate drop for TICK_A, got: %s", dropA)
	}
	dropC, okC := tracker.HysteresisDrops["TICK_C"]
	if !okC || !strings.Contains(dropC, "Sentry Gate") {
		t.Errorf("expected Sentry Gate drop for TICK_C, got: %s", dropC)
	}
}
