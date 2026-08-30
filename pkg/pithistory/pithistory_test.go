package pithistory

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/raghavkgarg/mycase/pkg/stockpicker"
)

func TestDuckDB_SaveAndQuerySnapshot(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_pit.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test pit db: %v", err)
	}
	defer db.Close()

	// Construct a synthetic snapshot with 5 Stage-1 survivors and 1 rejected stock
	snap := &stockpicker.PITRunSnapshot{
		AsOfDate:          "2026-08-26",
		IndexName:         "microcap250_smallcap250",
		Method:            "earlymb",
		RegimeMultiplier:  1.0,
		TotalConstituents: 6,
		Stage1Count:       5,
		SelectedCount:     2,
		Candidates: map[string]stockpicker.CandidateScoreDetail{
			"STOCK1": {
				Ticker:          "STOCK1",
				Sector:          "Industrials",
				PassedStage1:    true,
				RawScore:        40.0,
				EffectiveScore:  40.0,
				CompositeRS:     0.25,
				VCPRatio:        0.35,
				RVOLZScore:      1.5,
				DecayedPP:       2.0,
				DeliveryDelta:   0.15,
				Selected:        true,
				FinalWeight:     0.60,
			},
			"STOCK2": {
				Ticker:          "STOCK2",
				Sector:          "Healthcare",
				PassedStage1:    true,
				RawScore:        35.0,
				EffectiveScore:  35.0,
				CompositeRS:     0.20,
				VCPRatio:        0.45,
				RVOLZScore:      1.0,
				DecayedPP:       1.0,
				DeliveryDelta:   0.10,
				Selected:        true,
				FinalWeight:     0.40,
			},
			"STOCK3": {
				Ticker:          "STOCK3",
				Sector:          "Basic Materials",
				PassedStage1:    true,
				RawScore:        30.0,
				EffectiveScore:  30.0,
				CompositeRS:     0.15,
				VCPRatio:        0.55,
				RVOLZScore:      0.5,
				DecayedPP:       0.0,
				DeliveryDelta:   0.05,
				Selected:        false,
				FinalWeight:     0.0,
			},
			"STOCK4": {
				Ticker:          "STOCK4",
				Sector:          "Consumer Cyclical",
				PassedStage1:    true,
				RawScore:        25.0,
				EffectiveScore:  25.0,
				CompositeRS:     0.10,
				VCPRatio:        0.65,
				RVOLZScore:      0.0,
				DecayedPP:       0.0,
				DeliveryDelta:   0.00,
				Selected:        false,
				FinalWeight:     0.0,
			},
			"STOCK5": {
				Ticker:          "STOCK5",
				Sector:          "Technology",
				PassedStage1:    true,
				RawScore:        20.0,
				EffectiveScore:  20.0,
				CompositeRS:     0.05,
				VCPRatio:        0.75,
				RVOLZScore:      -0.5,
				DecayedPP:       0.0,
				DeliveryDelta:   -0.05,
				Selected:        false,
				FinalWeight:     0.0,
			},
			"STOCK_FAIL": {
				Ticker:          "STOCK_FAIL",
				Sector:          "Financial Services",
				PassedStage1:    false,
				RejectionReason: "Low ROCE < 12.0%",
				RawScore:        0.0,
				EffectiveScore:  0.0,
				Selected:        false,
				FinalWeight:     0.0,
			},
		},
	}

	// 1. Save Run Snapshot
	if err := db.SaveRunSnapshot(ctx, snap); err != nil {
		t.Fatalf("failed to save run snapshot: %v", err)
	}

	// 2. Query Empirical Quantiles for raw scores [20, 25, 30, 35, 40]
	q, err := db.GetEmpiricalQuantiles(ctx, "microcap250_smallcap250", "earlymb", 0)
	if err != nil {
		t.Fatalf("failed to get empirical quantiles: %v", err)
	}

	if q["samples"] != 5 {
		t.Errorf("expected 5 samples, got %f", q["samples"])
	}
	// P50 should be 30.0
	if q["p50"] != 30.0 {
		t.Errorf("expected P50=30.0, got %f", q["p50"])
	}
	// P40 should be 28.0 (quantile_cont on [20, 25, 30, 35, 40] at 0.40 = 20 + 0.40*(40-20) / index interpolated)
	if q["p40"] <= 20.0 || q["p40"] >= 35.0 {
		t.Errorf("expected P40 in (20, 35), got %f", q["p40"])
	}

	// 3. Query Candidate History for STOCK1
	hist, err := db.GetCandidateHistory(ctx, "STOCK1", 10)
	if err != nil {
		t.Fatalf("failed to get candidate history: %v", err)
	}
	if len(hist) != 1 {
		t.Fatalf("expected 1 history record, got %d", len(hist))
	}
	if hist[0].RawScore != 40.0 || !hist[0].Selected {
		t.Errorf("candidate history mismatch: got %+v", hist[0])
	}

	// 4. Update Forward Return for STOCK1
	if err := db.UpdateForwardReturns(ctx, "2026-08-26", "microcap250_smallcap250", "earlymb", "STOCK1", 0.125); err != nil {
		t.Fatalf("failed to update forward return: %v", err)
	}

	// 5. Query Run History
	runs, err := db.GetRunHistory(ctx, "microcap250_smallcap250", "earlymb", 10)
	if err != nil {
		t.Fatalf("failed to get run history: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Stage1Survivors != 5 || runs[0].SelectedCount != 2 {
		t.Errorf("run history summary mismatch: got %+v", runs[0])
	}
}

func TestDuckDB_RegimeMultiplierConsistency(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "regime_test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test pit db: %v", err)
	}
	defer db.Close()

	regime := 0.6558
	snap := &stockpicker.PITRunSnapshot{
		AsOfDate:          "2026-08-27",
		IndexName:         "microcap250_smallcap250",
		Method:            "earlymb",
		RegimeMultiplier:  regime,
		TotalConstituents: 3,
		Stage1Count:       2,
		SelectedCount:     1,
		Candidates: map[string]stockpicker.CandidateScoreDetail{
			"SELECTED_STK": {
				Ticker:         "SELECTED_STK",
				PassedStage1:   true,
				RawScore:       49.2,
				EffectiveScore: 49.2 * regime,
				Selected:       true,
				FinalWeight:    0.25,
			},
			"REJECTED_STK": {
				Ticker:         "REJECTED_STK",
				PassedStage1:   true,
				RawScore:       45.6,
				EffectiveScore: 45.6 * regime,
				Selected:       false,
				FinalWeight:    0.0,
			},
			"SAFETY_DROP": {
				Ticker:          "SAFETY_DROP",
				PassedStage1:    false,
				RejectionReason: "Low ROCE",
				RawScore:        0.0,
				EffectiveScore:  0.0,
				Selected:        false,
			},
		},
	}

	if err := db.SaveRunSnapshot(ctx, snap); err != nil {
		t.Fatalf("failed to save snapshot: %v", err)
	}

	// Query candidate history and assert invariant
	histSelected, err := db.GetCandidateHistory(ctx, "SELECTED_STK", 10)
	if err != nil || len(histSelected) != 1 {
		t.Fatalf("failed to get history for SELECTED_STK: %v", err)
	}
	expectedEffSel := 49.2 * regime
	if math.Abs(histSelected[0].EffectiveScore-expectedEffSel) > 1e-4 {
		t.Errorf("SELECTED_STK effective score mismatch: expected %.4f, got %.4f", expectedEffSel, histSelected[0].EffectiveScore)
	}

	histRejected, err := db.GetCandidateHistory(ctx, "REJECTED_STK", 10)
	if err != nil || len(histRejected) != 1 {
		t.Fatalf("failed to get history for REJECTED_STK: %v", err)
	}
	expectedEffRej := 45.6 * regime
	if math.Abs(histRejected[0].EffectiveScore-expectedEffRej) > 1e-4 {
		t.Errorf("REJECTED_STK effective score mismatch: expected %.4f, got %.4f", expectedEffRej, histRejected[0].EffectiveScore)
	}
}
