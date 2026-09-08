package printer

import (
	"strings"
	"testing"

	"github.com/raghavkgarg/mycase/pkg/broker"
	brokertypes "github.com/raghavkgarg/mycase/pkg/broker/types"
)

func TestRenderThemeAllocationSummary(t *testing.T) {
	rawHoldings := []brokertypes.Holding{
		{TradingSymbol: "STOCK1", Exchange: "NSE", Quantity: 10, AveragePrice: 100.0, LastPrice: 150.0, PnL: 500.0, PnLPct: 50.0},
		{TradingSymbol: "STOCK2", Exchange: "NSE", Quantity: 5, AveragePrice: 200.0, LastPrice: 250.0, PnL: 250.0, PnLPct: 25.0},
	}

	groups := []ThemeGroup{
		{
			Name:         "Theme KK Advise",
			Prefix:       "My KK",
			TargetWeight: 0.60,
			Holdings:     []brokertypes.Holding{rawHoldings[0]},
		},
		{
			Name:         "Theme AI Advice",
			Prefix:       "My AI",
			TargetWeight: 0.40,
			Holdings:     []brokertypes.Holding{rawHoldings[1]},
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

	if !strings.Contains(output, "PORTFOLIO SNAPSHOT") {
		t.Errorf("expected output to contain 'PORTFOLIO SNAPSHOT', got:\n%s", output)
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

func TestFindMissingTickers_SeriesSuffix(t *testing.T) {
	tickers := map[string]bool{
		"NSE:E2E":        true,
		"NSE:SCHNEIDER":  true,
		"NSE:BAJAJ-AUTO": true,
	}
	holdings := []brokertypes.Holding{
		{TradingSymbol: "E2E-BE", Exchange: "NSE", Quantity: 10},
		{TradingSymbol: "SCHNEIDER", Exchange: "NSE", Quantity: 5},
		{TradingSymbol: "BAJAJ-AUTO", Exchange: "NSE", Quantity: 2},
	}

	missing := findMissingTickers(tickers, holdings)
	if len(missing) != 0 {
		t.Errorf("expected 0 missing tickers for E2E-BE satisfying NSE:E2E, got: %v", missing)
	}
}

func TestRenderHoldingsSnapshot_SeriesSuffixCategorization(t *testing.T) {
	rawHoldings := []brokertypes.Holding{
		{
			TradingSymbol: "E2E-BE",
			Exchange:      "NSE",
			Quantity:      5,
			AveragePrice:  2500.0,
			LastPrice:     3000.0,
			PnL:           2500.0,
			PnLPct:        20.0,
		},
	}

	groups := []ThemeGroup{
		{
			Name:         "Theme AI Advice",
			Prefix:       "My AI",
			CSVPath:      "data/aitheme.csv",
			TargetWeight: 1.0,
			Tickers: map[string]bool{
				"NSE:E2E": true,
			},
			Holdings: rawHoldings,
		},
	}

	output := RenderHoldingsSnapshot(rawHoldings, groups, nil)

	if strings.Contains(output, "Holdings not categorized in any group") {
		t.Errorf("expected no uncategorized holdings warning, got:\n%s", output)
	}
	if strings.Contains(output, "Tickers in data/aitheme.csv not present in holdings") {
		t.Errorf("expected no missing tickers warning, got:\n%s", output)
	}
	if !strings.Contains(output, "✓ All holdings are correctly categorized, and all group tickers are present in holdings.") {
		t.Errorf("expected success verification message, got:\n%s", output)
	}
	if !strings.Contains(output, "E2E-BE") {
		t.Errorf("expected E2E-BE row in output table, got:\n%s", output)
	}
}
