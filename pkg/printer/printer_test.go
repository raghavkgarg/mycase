package printer

import (
	"strings"
	"testing"

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
