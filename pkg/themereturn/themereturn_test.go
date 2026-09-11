package themereturn

import (
	"math"
	"testing"
	"time"
)

func TestCalculateXIRR_SimpleAnnual(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// -1000 invested, 1 year later value is 1100 => exactly +10% p.a.
	cfs := []DatedCashFlow{
		{Date: t0, Amount: -1000.0},
		{Date: t1, Amount: 1100.0},
	}

	rate, err := CalculateXIRR(cfs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := 0.10
	if math.Abs(rate-expected) > 0.001 {
		t.Errorf("expected ~%.4f, got %.4f", expected, rate)
	}
}

func TestCalculateXIRR_MultipleTranchesWithDividends(t *testing.T) {
	t0 := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	tDiv := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	tFinal := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)

	cfs := []DatedCashFlow{
		{Date: t0, Amount: -100000.0},
		{Date: t1, Amount: -50000.0},
		{Date: tDiv, Amount: 500.0},
		{Date: tFinal, Amount: 160000.0},
	}

	rate, err := CalculateXIRR(cfs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rate <= 0 {
		t.Errorf("expected positive compounding rate, got %.4f", rate)
	}
}

func TestModifiedDietz(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tMid := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	tEnd := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	// V0 = 100,000. Halfway through year, add 50,000. End of year, value is 180,000.
	cfs := []DatedCashFlow{
		{Date: t0, Amount: -100000.0},
		{Date: tMid, Amount: -50000.0},
	}

	cum, ann := computeModifiedDietz(cfs, 180000.0, t0, tEnd)
	if cum <= 0 || ann <= 0 {
		t.Errorf("expected positive return, got cum=%.4f, ann=%.4f", cum, ann)
	}
}

func TestEvaluateThemeReturn_RealMicroSmall(t *testing.T) {
	db, err := OpenDB("")
	if err != nil {
		t.Skipf("skipping integration test: portfolio.db not found: %v", err)
	}
	defer db.Close()

	theme, err := ResolveTheme("microsmall", "", "../../config/themes.json")
	if err != nil {
		theme, err = ResolveTheme("microsmall", "data/microsmall.csv", "")
	}
	if err != nil {
		t.Fatalf("failed to resolve theme: %v", err)
	}

	liveLTPs := map[string]float64{
		"JAMNAAUTO": 126.61, "CCL": 1094.50, "SANDUMA": 195.17, "SUMICHEM": 490.80,
		"TENNIND": 515.45, "THYROCARE": 573.15, "CUPID": 280.90, "CASTROLIND": 188.35,
		"VARROC": 827.05, "ACUTAAS": 3181.60, "HINDCOPPER": 509.10, "GOKULAGRO": 231.69,
		"CHENNPETRO": 1430.20, "NAVINFLUOR": 8620.00, "SMLMAH": 6393.50, "ATLANTAELE": 1938.10,
		"ENGINERSIN": 277.10, "MANORAMA": 2003.60,
	}

	opts := ThemeReturnOptions{
		AccountID:        "CBR420",
		IncludeLifecycle: true,
		Detail:           true,
		Benchmark:        "NIFTY50_TRI",
	}

	report, err := EvaluateThemeReturn(db, theme, liveLTPs, opts)
	if err != nil {
		t.Fatalf("EvaluateThemeReturn failed: %v", err)
	}

	if math.Abs(report.ActiveInvestedValue-207526.31) > 10.0 {
		t.Errorf("expected ActiveInvestedValue ~207526.31, got %.2f", report.ActiveInvestedValue)
	}

	if math.Abs(report.ActiveCurrentValue-216171.85) > 10.0 {
		t.Errorf("expected ActiveCurrentValue ~216171.85, got %.2f", report.ActiveCurrentValue)
	}

	if report.ActiveDividends < 0.0 {
		t.Errorf("expected non-negative ActiveDividends, got %.2f", report.ActiveDividends)
	}

	if report.ActiveMWR < 0.0 {
		t.Errorf("expected positive ActiveMWR, got %.2f%%", report.ActiveMWR)
	}

	if len(report.ExitedPositions) != 14 {
		t.Errorf("expected 14 exited positions, got %d", len(report.ExitedPositions))
	}
	if math.Abs(report.LifecycleGrossSells-114819.00) > 10.0 {
		t.Errorf("expected LifecycleGrossSells ~114819.00, got %.2f", report.LifecycleGrossSells)
	}
	if math.Abs(report.LifecycleTotalWealth-10935.31) > 10.0 {
		t.Errorf("expected LifecycleTotalWealth ~10935.31, got %.2f", report.LifecycleTotalWealth)
	}
}
