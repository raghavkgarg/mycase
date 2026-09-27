package stockpicker

import (
	"context"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/selectiontracker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

func TestScoreFairPrice_Monotonicity(t *testing.T) {
	ctx := context.Background()

	// Stock A: huge upside (+60%), low CV, high ROCE
	// Stock B: modest upside (+10%), high CV, low ROCE
	activeKeys := []string{"STK_A", "STK_B"}
	fundamentals := map[string]yfinance.Fundamentals{
		"STK_A": {
			RegularPrice:      100.0,
			MarketCap:         1000000000,
			NetIncome:         100000000,
			OperatingCashflow: 120000000,
			DebtToEquity:      0.10,
			ROE:               0.25,
			OperatingMargins:  0.22,
			Sector:            "Technology",
		},
		"STK_B": {
			RegularPrice:      100.0,
			MarketCap:         1000000000,
			NetIncome:         50000000,
			OperatingCashflow: 30000000,
			DebtToEquity:      1.50,
			ROE:               0.10,
			OperatingMargins:  0.08,
			Sector:            "Energy",
		},
	}

	p160 := 160.0
	p110 := 110.0
	fairPrices := map[string]*yfinance.FairPriceResult{
		"STK_A": {
			CMP:                  100.0,
			EnsembleFairPrice:    160.0,
			PessimisticFairPrice: 128.0,
			OptimisticFairPrice:  184.0,
			UpsidePct:            60.0,
			ValidModelCount:      5,
			ModelCV:              0.10,
			DCFFairPrice:         &p160,
			EPVFairPrice:         &p160,
			GrahamNumber:         &p160,
			RelativeFairPrice:    &p160,
			PEGFairPrice:         &p160,
		},
		"STK_B": {
			CMP:                  100.0,
			EnsembleFairPrice:    110.0,
			PessimisticFairPrice: 88.0,
			OptimisticFairPrice:  126.5,
			UpsidePct:            10.0,
			ValidModelCount:      3,
			ModelCV:              0.35,
			DCFFairPrice:         &p110,
		},
	}

	scores := ScoreFairPrice(ctx, activeKeys, fundamentals, fairPrices, &config.HardFilters{})

	if scores["STK_A"] <= scores["STK_B"] {
		t.Errorf("expected STK_A score (%f) > STK_B score (%f)", scores["STK_A"], scores["STK_B"])
	}
}

func TestNormalizeFairPriceWeights_Caps(t *testing.T) {
	selected := []string{"STK_1", "STK_2", "STK_3", "STK_4"}
	scores := map[string]float64{
		"STK_1": 90.0,
		"STK_2": 80.0,
		"STK_3": 70.0,
		"STK_4": 60.0,
	}
	fundamentals := map[string]yfinance.Fundamentals{
		"STK_1": {Sector: "Technology"},
		"STK_2": {Sector: "Technology"},
		"STK_3": {Sector: "Technology"},
		"STK_4": {Sector: "Healthcare"},
	}
	hf := &config.HardFilters{
		MaxStockWeightCap:  0.08, // 8% single stock cap
		MaxSectorWeightCap: 0.25, // 25% sector cap
	}

	weights := NormalizeFairPriceWeights(selected, scores, fundamentals, hf, nil, 0.0)

	// Check single stock cap
	for _, tName := range selected {
		if weights[tName] > 0.08+1e-4 {
			t.Errorf("stock %s weight %f exceeds cap 0.08", tName, weights[tName])
		}
	}

	// Check Technology sector cap
	techWeight := weights["STK_1"] + weights["STK_2"] + weights["STK_3"]
	if techWeight > 0.25+1e-4 {
		t.Errorf("Technology aggregate weight %f exceeds cap 0.25", techWeight)
	}
}

func TestSelectTopNFairPriceWithCooldown(t *testing.T) {
	activeKeys := []string{"T1", "T2", "T3", "T4"}
	scores := map[string]float64{
		"T1": 85.0,
		"T2": 80.0,
		"T3": 75.0,
		"T4": 70.0,
	}
	fundamentals := map[string]yfinance.Fundamentals{
		"T1": {Sector: "Technology"},
		"T2": {Sector: "Technology"},
		"T3": {Sector: "Technology"},
		"T4": {Sector: "Technology"},
	}
	fairPrices := map[string]*yfinance.FairPriceResult{
		"T1": {EnsembleFairPrice: 120.0, UpsidePct: 20.0, Verdict: "UNDERVALUED"},
		"T2": {EnsembleFairPrice: 130.0, UpsidePct: 30.0, Verdict: "DEEPLY_UNDERVALUED"},
		"T3": {EnsembleFairPrice: 115.0, UpsidePct: 15.0, Verdict: "UNDERVALUED"},
		"T4": {EnsembleFairPrice: 110.0, UpsidePct: 10.0, Verdict: "FAIRLY_VALUED"},
	}

	hf := &config.HardFilters{
		MaxStocksPerSector: 2, // Only max 2 allowed from Technology!
	}
	tracker := selectiontracker.New()

	selected := SelectTopNFairPriceWithCooldown(
		activeKeys, scores, fairPrices, fundamentals, hf, 4, nil, 0, tracker,
		make(map[string]time.Time), 0, 0,
	)

	if len(selected) > 2 {
		t.Errorf("expected max 2 stocks selected due to sector cap, got %d", len(selected))
	}
}
