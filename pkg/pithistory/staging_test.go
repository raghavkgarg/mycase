package pithistory

import (
	"math"
	"testing"
)

func TestCalculateSetupQuality(t *testing.T) {
	// Example 1: Tips Music: VCP 0.31, Comp RS +8.4% -> (1 + 0.084) / (0.31 + 0.10) = 1.084 / 0.41 = 2.64
	sq1 := CalculateSetupQuality(0.084, 0.31)
	if math.Abs(sq1-2.6439) > 0.01 {
		t.Errorf("expected Setup Quality ~2.64, got %.4f", sq1)
	}

	// Example 2: Park Hospitals: VCP 0.41, Comp RS +32.6% -> (1 + 0.326) / (0.41 + 0.10) = 1.326 / 0.51 = 2.60
	sq2 := CalculateSetupQuality(0.326, 0.41)
	if math.Abs(sq2-2.600) > 0.01 {
		t.Errorf("expected Setup Quality ~2.60, got %.4f", sq2)
	}

	// Example 3: Avalon: VCP 1.23, Comp RS +70.1% -> (1 + 0.701) / (1.23 + 0.10) = 1.701 / 1.33 = 1.28
	sq3 := CalculateSetupQuality(0.701, 1.23)
	if math.Abs(sq3-1.2789) > 0.01 {
		t.Errorf("expected Setup Quality ~1.28, got %.4f", sq3)
	}

	// Safety guardrail test: Deep negative RS (-120%) should be clamped to 0.10 numerator
	sqCrash := CalculateSetupQuality(-1.20, 0.40)
	if sqCrash <= 0 {
		t.Errorf("expected positive Setup Quality under market crash, got %.4f", sqCrash)
	}
	expectedCrash := 0.10 / (0.40 + 0.10)
	if math.Abs(sqCrash-expectedCrash) > 1e-5 {
		t.Errorf("expected %.4f under crash guardrail, got %.4f", expectedCrash, sqCrash)
	}
}

func TestGetIgnitionStatus(t *testing.T) {
	code1, _ := GetIgnitionStatus(0.08)
	if code1 != "ACTIVE" {
		t.Errorf("expected ACTIVE for +8%% deliv, got %s", code1)
	}

	code2, _ := GetIgnitionStatus(0.02)
	if code2 != "NEUTRAL" {
		t.Errorf("expected NEUTRAL for +2%% deliv, got %s", code2)
	}

	code3, _ := GetIgnitionStatus(-0.06)
	if code3 != "COOLING" {
		t.Errorf("expected COOLING for -6%% deliv, got %s", code3)
	}
}

func TestCalculateScoreCV(t *testing.T) {
	// Stable series: 30, 31, 30, 31, 30 -> very low CV
	scoresStable := []float64{30, 31, 30, 31, 30}
	cvStable := CalculateScoreCV(scoresStable)
	if cvStable > 0.10 {
		t.Errorf("expected CV < 0.10 for stable scores, got %.4f", cvStable)
	}

	// Volatile series: 10, 40, 15, 45, 20 -> high CV > 0.30
	scoresVolatile := []float64{10, 40, 15, 45, 20}
	cvVolatile := CalculateScoreCV(scoresVolatile)
	if cvVolatile < 0.30 {
		t.Errorf("expected CV > 0.30 for volatile scores, got %.4f", cvVolatile)
	}
}

func TestCalculateRiskParityWeights(t *testing.T) {
	candidates := []StagedCandidate{
		{Ticker: "NSE:A1", Sector: "Technology", SetupQuality: 3.0, CurrentPrice: 1000, ATR20: 20}, // 2% ATR -> high weight
		{Ticker: "NSE:A2", Sector: "Technology", SetupQuality: 2.8, CurrentPrice: 500, ATR20: 10},  // 2% ATR
		{Ticker: "NSE:A3", Sector: "Technology", SetupQuality: 2.5, CurrentPrice: 200, ATR20: 4},   // 2% ATR
		{Ticker: "NSE:A4", Sector: "Technology", SetupQuality: 2.0, CurrentPrice: 100, ATR20: 2},   // Exceeds 3 stocks per sector -> should be dropped
		{Ticker: "NSE:B1", Sector: "Healthcare", SetupQuality: 2.7, CurrentPrice: 1500, ATR20: 45}, // 3% ATR
		{Ticker: "NSE:B2", Sector: "Healthcare", SetupQuality: 2.4, CurrentPrice: 800, ATR20: 24},  // 3% ATR
		{Ticker: "NSE:C1", Sector: "Financials", SetupQuality: 2.6, CurrentPrice: 400, ATR20: 16},  // 4% ATR
		{Ticker: "NSE:C2", Sector: "Financials", SetupQuality: 2.2, CurrentPrice: 600, ATR20: 24},  // 4% ATR
		{Ticker: "NSE:D1", Sector: "Energy", SetupQuality: 2.3, CurrentPrice: 250, ATR20: 12.5},    // 5% ATR
		{Ticker: "NSE:D2", Sector: "Energy", SetupQuality: 2.1, CurrentPrice: 300, ATR20: 15},      // 5% ATR
		{Ticker: "NSE:E1", Sector: "Consumer", SetupQuality: 2.2, CurrentPrice: 1200, ATR20: 36},   // 3% ATR
		{Ticker: "NSE:E2", Sector: "Consumer", SetupQuality: 2.0, CurrentPrice: 900, ATR20: 27},    // 3% ATR
		{Ticker: "NSE:F1", Sector: "Industrials", SetupQuality: 1.9, CurrentPrice: 750, ATR20: 30}, // 4% ATR
		{Ticker: "NSE:F2", Sector: "Industrials", SetupQuality: 1.8, CurrentPrice: 450, ATR20: 18}, // 4% ATR
		{Ticker: "NSE:G1", Sector: "Materials", SetupQuality: 1.7, CurrentPrice: 1100, ATR20: 44},  // 4% ATR
	}

	sized := CalculateRiskParityWeights(candidates)
	if len(sized) == 0 {
		t.Fatalf("expected sized candidates, got empty slice")
	}

	var totalWeight float64
	techCount := 0
	sectorWeights := make(map[string]float64)
	for _, c := range sized {
		totalWeight += c.Weight
		sectorWeights[c.Sector] += c.Weight
		if c.Sector == "Technology" {
			techCount++
		}
		// Check single stock cap (8% = 0.08)
		if c.Weight > 0.085 { // Allow slight rounding margin
			t.Errorf("candidate %s weight %.4f exceeds 8%% cap", c.Ticker, c.Weight)
		}
	}

	// Max 3 stocks per sector
	if techCount > 3 {
		t.Errorf("expected max 3 stocks for Technology, got %d", techCount)
	}

	// Check sector cap (25% = 0.25)
	for sec, sw := range sectorWeights {
		if sw > 0.255 {
			t.Errorf("sector %s weight %.4f exceeds 25%% cap", sec, sw)
		}
	}

	// Sum should be close to 1.0 (100%)
	if math.Abs(totalWeight-1.0) > 0.02 {
		t.Errorf("expected total weight ~1.0, got %.4f", totalWeight)
	}
}
