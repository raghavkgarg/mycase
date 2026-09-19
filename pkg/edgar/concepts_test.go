package edgar

import (
	"os"
	"path/filepath"
	"testing"
)

func loadTestFacts(t *testing.T, name string) *companyFacts {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	cf, err := parseFacts(body)
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return cf
}

func TestMapFacts_TESTCO(t *testing.T) {
	cf := loadTestFacts(t, "companyfacts_TESTCO.json")
	f := mapFacts(cf)

	// NetIncome: latest reported fact overall is the Q1-2024 10-Q (filed
	// 2024-05-01), value 120 — latestValue picks by most-recent filed/end.
	if f.NetIncome != 120 {
		t.Errorf("NetIncome: want 120 (latest reported), got %v", f.NetIncome)
	}

	// OperatingCashflow: latest reported is Q1-2024 (250).
	if f.OperatingCashflow != 250 {
		t.Errorf("OperatingCashflow: want 250 (latest reported), got %v", f.OperatingCashflow)
	}

	// FreeCashflow = latest ANNUAL OCF (FY2023=1000) − latest ANNUAL capex
	// (FY2023=300) = 700. Note it must use annual (not the Q1 OCF).
	if f.FreeCashflow != 700 {
		t.Errorf("FreeCashflow: want 700 (annual OCF 1000 − capex 300), got %v", f.FreeCashflow)
	}

	// AnnualRevenue: two distinct period-ends (2022, 2023). The 2023 end appears
	// twice (10-K val 9000 filed 2024-02, 10-K/A val 9100 filed 2024-06); the
	// later-filed restatement (9100) must win. Sorted ascending by date.
	if len(f.AnnualRevenue) != 2 {
		t.Fatalf("AnnualRevenue: want 2 entries, got %d (%+v)", len(f.AnnualRevenue), f.AnnualRevenue)
	}
	if f.AnnualRevenue[0].Date != "2022-12-31" || f.AnnualRevenue[0].Value != 8000 {
		t.Errorf("AnnualRevenue[0]: want 2022-12-31=8000, got %+v", f.AnnualRevenue[0])
	}
	if f.AnnualRevenue[1].Date != "2023-12-31" || f.AnnualRevenue[1].Value != 9100 {
		t.Errorf("AnnualRevenue[1]: want 2023-12-31=9100 (restated), got %+v", f.AnnualRevenue[1])
	}

	// AnnualTotalAssets: one instant fact.
	if len(f.AnnualTotalAssets) != 1 || f.AnnualTotalAssets[0].Value != 20000 {
		t.Errorf("AnnualTotalAssets: want [20000], got %+v", f.AnnualTotalAssets)
	}

	// AnnualOperatingIncome absent from fixture → nil series.
	if f.AnnualOperatingIncome != nil {
		t.Errorf("AnnualOperatingIncome: want nil (absent concept), got %+v", f.AnnualOperatingIncome)
	}
}

func TestAnnualSeries_OnlyFullYear(t *testing.T) {
	// A concept with only quarterly facts yields no annual series.
	g := map[string]conceptData{
		"NetIncomeLoss": {Units: map[string][]factValue{"USD": {
			{End: "2024-03-31", Val: 100, FP: "Q1", Form: "10-Q", Filed: "2024-05-01"},
			{End: "2024-06-30", Val: 110, FP: "Q2", Form: "10-Q", Filed: "2024-08-01"},
		}}},
	}
	if got := annualSeries(g, tagsNetIncome); got != nil {
		t.Errorf("want nil series for quarterly-only facts, got %+v", got)
	}
}

func TestFirstPresentTag_Order(t *testing.T) {
	// Revenue candidate order prefers the contract-revenue tag; when only the
	// legacy Revenues tag is present, it should still be found.
	g := map[string]conceptData{
		"Revenues": {Units: map[string][]factValue{"USD": {
			{End: "2023-12-31", Val: 5000, FP: "FY", Form: "10-K", Filed: "2024-02-01"},
		}}},
	}
	got := annualSeries(g, tagsRevenue)
	if len(got) != 1 || got[0].Value != 5000 {
		t.Errorf("want [5000] via legacy Revenues tag, got %+v", got)
	}
}

func TestFreeCashflow_AlternateCapexTag(t *testing.T) {
	// Visa/Qualcomm pattern: capex reported under PaymentsToAcquireProductiveAssets
	// rather than the classic PaymentsToAcquirePropertyPlantAndEquipment. FCF must
	// still bind = OCF − capex.
	g := map[string]conceptData{
		"NetCashProvidedByUsedInOperatingActivities": {Units: map[string][]factValue{"USD": {
			{End: "2025-09-30", Val: 23000, FP: "FY", Form: "10-K", Filed: "2025-11-01"},
		}}},
		"PaymentsToAcquireProductiveAssets": {Units: map[string][]factValue{"USD": {
			{End: "2025-09-30", Val: 1500, FP: "FY", Form: "10-K", Filed: "2025-11-01"},
		}}},
	}
	cf := &companyFacts{}
	cf.Facts.USGAAP = g
	f := mapFacts(cf)
	if f.FreeCashflow != 23000-1500 {
		t.Errorf("FreeCashflow: want %v via alternate capex tag, got %v", 23000-1500, f.FreeCashflow)
	}
}

func TestFreeCashflow_EmptyClassicTagDoesNotShadow(t *testing.T) {
	// FTNT/PANW/ISRG/SRE/HPQ pattern: the classic capex tag is *present* but carries
	// no FY facts, while real capex lives under PaymentsToAcquireProductiveAssets.
	// The empty classic tag must not shadow the populated alternate.
	g := map[string]conceptData{
		"NetCashProvidedByUsedInOperatingActivities": {Units: map[string][]factValue{"USD": {
			{End: "2025-12-31", Val: 6000, FP: "FY", Form: "10-K", Filed: "2026-02-01"},
		}}},
		// Present but only quarterly / non-annual → no usable FY fact.
		"PaymentsToAcquirePropertyPlantAndEquipment": {Units: map[string][]factValue{"USD": {
			{End: "2025-03-31", Val: 200, FP: "Q1", Form: "10-Q", Filed: "2025-04-20"},
		}}},
		"PaymentsToAcquireProductiveAssets": {Units: map[string][]factValue{"USD": {
			{End: "2025-12-31", Val: 900, FP: "FY", Form: "10-K", Filed: "2026-02-01"},
		}}},
	}
	cf := &companyFacts{}
	cf.Facts.USGAAP = g
	f := mapFacts(cf)
	if f.FreeCashflow != 6000-900 {
		t.Errorf("FreeCashflow: want %v (empty classic tag should not shadow), got %v", 6000-900, f.FreeCashflow)
	}
}

func TestFreeCashflow_RequiresBothConcepts(t *testing.T) {
	// OCF present but capex absent → FCF left zero (merger falls back to Schwab).
	g := map[string]conceptData{
		"NetCashProvidedByUsedInOperatingActivities": {Units: map[string][]factValue{"USD": {
			{End: "2023-12-31", Val: 1000, FP: "FY", Form: "10-K", Filed: "2024-02-01"},
		}}},
	}
	cf := &companyFacts{}
	cf.Facts.USGAAP = g
	f := mapFacts(cf)
	if f.FreeCashflow != 0 {
		t.Errorf("FreeCashflow: want 0 when capex concept absent, got %v", f.FreeCashflow)
	}
	if f.OperatingCashflow != 1000 {
		t.Errorf("OperatingCashflow: want 1000, got %v", f.OperatingCashflow)
	}
}
