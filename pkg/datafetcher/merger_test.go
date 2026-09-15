package datafetcher

import (
	"testing"

	"github.com/raghavkgarg/mycase/pkg/marketdata"
)

func TestMergeFundamentals_Precedence(t *testing.T) {
	schwabBase := marketdata.Fundamentals{
		ROE:               0.25,
		OperatingMargins:  0.30,
		MarketCap:         1e12,
		TTMRevenue:        4e11,
		FreeCashflow:      100, // Schwab TTM FCF
		OperatingCashflow: 0,   // Schwab has none
		NetIncome:         90,  // Schwab-derived floor
	}
	edgarPartial := marketdata.Fundamentals{
		OperatingCashflow: 500, // EDGAR authoritative
		NetIncome:         600, // EDGAR authoritative
		FreeCashflow:      450, // EDGAR OCF−capex
		AnnualRevenue: []marketdata.AnnualMetric{
			{Date: "2023-12-31", Value: 3.9e11},
		},
	}

	merged, prov := mergeFundamentals(schwabBase, edgarPartial, true, true)

	if prov != "schwab+edgar" {
		t.Errorf("provenance: want schwab+edgar, got %q", prov)
	}
	// Schwab ratios preserved.
	if merged.ROE != 0.25 || merged.OperatingMargins != 0.30 || merged.MarketCap != 1e12 {
		t.Errorf("schwab ratios not preserved: %+v", merged)
	}
	// EDGAR statement facts win.
	if merged.OperatingCashflow != 500 {
		t.Errorf("OperatingCashflow: want 500 (EDGAR), got %v", merged.OperatingCashflow)
	}
	if merged.NetIncome != 600 {
		t.Errorf("NetIncome: want 600 (EDGAR), got %v", merged.NetIncome)
	}
	if merged.FreeCashflow != 450 {
		t.Errorf("FreeCashflow: want 450 (EDGAR), got %v", merged.FreeCashflow)
	}
	if len(merged.AnnualRevenue) != 1 || merged.AnnualRevenue[0].Value != 3.9e11 {
		t.Errorf("AnnualRevenue: want EDGAR series, got %+v", merged.AnnualRevenue)
	}
}

func TestMergeFundamentals_NonDestructive(t *testing.T) {
	// EDGAR partial is sparse (only net income); it must NOT zero Schwab's FCF
	// or clobber Schwab's annual revenue with an empty series.
	schwabBase := marketdata.Fundamentals{
		FreeCashflow: 100,
		AnnualRevenue: []marketdata.AnnualMetric{
			{Date: "2023-12-31", Value: 5000},
		},
	}
	edgarPartial := marketdata.Fundamentals{NetIncome: 42}

	merged, _ := mergeFundamentals(schwabBase, edgarPartial, true, true)

	if merged.FreeCashflow != 100 {
		t.Errorf("FreeCashflow: want 100 preserved (EDGAR had none), got %v", merged.FreeCashflow)
	}
	if len(merged.AnnualRevenue) != 1 || merged.AnnualRevenue[0].Value != 5000 {
		t.Errorf("AnnualRevenue: want Schwab series preserved, got %+v", merged.AnnualRevenue)
	}
	if merged.NetIncome != 42 {
		t.Errorf("NetIncome: want 42 (EDGAR overlay), got %v", merged.NetIncome)
	}
}

func TestMergeFundamentals_SchwabOnly(t *testing.T) {
	// No EDGAR match for this ticker → base returned unchanged, provenance schwab.
	schwabBase := marketdata.Fundamentals{FreeCashflow: 100, ROE: 0.2}
	merged, prov := mergeFundamentals(schwabBase, marketdata.Fundamentals{}, true, false)

	if prov != "schwab" {
		t.Errorf("provenance: want schwab, got %q", prov)
	}
	if merged.FreeCashflow != 100 || merged.ROE != 0.2 {
		t.Errorf("schwab-only base altered: %+v", merged)
	}
}
