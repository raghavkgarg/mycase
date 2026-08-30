package printer

import (
	"strings"
	"testing"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/portfolio"
)

func TestRenderThemeAllocationSummary(t *testing.T) {
	rawHoldings := []portfolio.Holding{
		{TradingSymbol: "STOCK1", Exchange: "NSE", Quantity: 10, AveragePrice: 100.0, LastPrice: 150.0, PnL: 500.0, PnLPct: 50.0},
		{TradingSymbol: "STOCK2", Exchange: "NSE", Quantity: 5, AveragePrice: 200.0, LastPrice: 250.0, PnL: 250.0, PnLPct: 25.0},
	}

	groups := []ThemeGroup{
		{
			Name:         "Theme KK Advise",
			Prefix:       "My KK",
			TargetWeight: 0.60,
			Holdings:     []portfolio.Holding{rawHoldings[0]},
		},
		{
			Name:         "Theme AI Advice",
			Prefix:       "My AI",
			TargetWeight: 0.40,
			Holdings:     []portfolio.Holding{rawHoldings[1]},
		},
	}

	output := RenderHoldingsSnapshot(rawHoldings, groups, nil)

	// Verify headers and content in output
	if !strings.Contains(output, "THEME TARGET VS ACTUAL WEIGHT ALLOCATION SUMMARY") {
		t.Errorf("expected output to contain summary title, got:\n%s", output)
	}

	if !strings.Contains(output, "KK Advise") {
		t.Errorf("expected output to contain 'KK Advise', got:\n%s", output)
	}

	if !strings.Contains(output, "AI Advice") {
		t.Errorf("expected output to contain 'AI Advice', got:\n%s", output)
	}

	if !strings.Contains(output, "PnL") || !strings.Contains(output, "PnL %") || !strings.Contains(output, "Actual Wt") || !strings.Contains(output, "Target Wt") || !strings.Contains(output, "Drift") {
		t.Errorf("expected output to contain table headers (PnL, PnL %%, Actual Wt, Target Wt, Drift), got:\n%s", output)
	}
}

func TestPrintPreviewTable_WithSellReturns(t *testing.T) {
	basketKeys := []string{"NSE:MINDACORP", "NSE:CUPID"}
	basket := map[string]float64{
		"NSE:MINDACORP": 0.0,
		"NSE:CUPID":     1.0,
	}
	quoteData := map[string]float64{
		"NSE:MINDACORP": 740.0,
		"NSE:CUPID":     280.0,
	}
	currentHoldings := map[string]int{
		"MINDACORP": 12,
		"CUPID":     0,
	}
	finalQuantities := []int{0, 30}
	holdingDetails := map[string]broker.Holding{
		"MINDACORP": {
			TradingSymbol: "MINDACORP",
			Quantity:      12,
			AveragePrice:  620.0,
			LastPrice:     740.0,
		},
	}

	output := PrintPreviewTable(basketKeys, basket, quoteData, currentHoldings, finalQuantities, holdingDetails)

	if !strings.Contains(output, "PORTFOLIO SNAPSHOT:") {
		t.Errorf("expected output to contain 'PORTFOLIO SNAPSHOT:', got:\n%s", output)
	}

	if !strings.Contains(output, "EXITS & SELL ORDERS RETURN BREAKDOWN:") {
		t.Errorf("expected output to contain 'EXITS & SELL ORDERS RETURN BREAKDOWN:', got:\n%s", output)
	}

	if !strings.Contains(output, "MINDACORP") || !strings.Contains(output, "EXIT") {
		t.Errorf("expected output to contain MINDACORP EXIT line, got:\n%s", output)
	}

	if !strings.Contains(output, "Total Realized Gain/Loss (Net of DP):") {
		t.Errorf("expected output to contain Total Realized Gain/Loss (Net of DP), got:\n%s", output)
	}

	if !strings.Contains(output, "Total DP Charges") {
		t.Errorf("expected output to contain Total DP Charges, got:\n%s", output)
	}

	if !strings.Contains(output, "Estimated Realized PnL on Sells (Net):") {
		t.Errorf("expected output to contain Estimated Realized PnL on Sells (Net) summary line, got:\n%s", output)
	}
}
