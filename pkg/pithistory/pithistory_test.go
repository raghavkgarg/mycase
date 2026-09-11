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
				Ticker:         "STOCK1",
				Sector:         "Industrials",
				PassedStage1:   true,
				RawScore:       40.0,
				EffectiveScore: 40.0,
				CompositeRS:    0.25,
				VCPRatio:       0.35,
				RVOLZScore:     1.5,
				DecayedPP:      2.0,
				DeliveryDelta:  0.15,
				Selected:       true,
				FinalWeight:    0.60,
			},
			"STOCK2": {
				Ticker:         "STOCK2",
				Sector:         "Healthcare",
				PassedStage1:   true,
				RawScore:       35.0,
				EffectiveScore: 35.0,
				CompositeRS:    0.20,
				VCPRatio:       0.45,
				RVOLZScore:     1.0,
				DecayedPP:      1.0,
				DeliveryDelta:  0.10,
				Selected:       true,
				FinalWeight:    0.40,
			},
			"STOCK3": {
				Ticker:         "STOCK3",
				Sector:         "Basic Materials",
				PassedStage1:   true,
				RawScore:       30.0,
				EffectiveScore: 30.0,
				CompositeRS:    0.15,
				VCPRatio:       0.55,
				RVOLZScore:     0.5,
				DecayedPP:      0.0,
				DeliveryDelta:  0.05,
				Selected:       false,
				FinalWeight:    0.0,
			},
			"STOCK4": {
				Ticker:         "STOCK4",
				Sector:         "Consumer Cyclical",
				PassedStage1:   true,
				RawScore:       25.0,
				EffectiveScore: 25.0,
				CompositeRS:    0.10,
				VCPRatio:       0.65,
				RVOLZScore:     0.0,
				DecayedPP:      0.0,
				DeliveryDelta:  0.00,
				Selected:       false,
				FinalWeight:    0.0,
			},
			"STOCK5": {
				Ticker:         "STOCK5",
				Sector:         "Technology",
				PassedStage1:   true,
				RawScore:       20.0,
				EffectiveScore: 20.0,
				CompositeRS:    0.05,
				VCPRatio:       0.75,
				RVOLZScore:     -0.5,
				DecayedPP:      0.0,
				DeliveryDelta:  -0.05,
				Selected:       false,
				FinalWeight:    0.0,
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

func TestDuckDB_DataFetchFailedExcludedFromQuantiles(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_quantiles.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test pit db: %v", err)
	}
	defer db.Close()

	snap := &stockpicker.PITRunSnapshot{
		AsOfDate:          "2026-09-01",
		IndexName:         "test_index",
		Method:            "earlymb",
		RegimeMultiplier:  1.0,
		TotalConstituents: 3,
		Stage1Count:       2,
		SelectedCount:     1,
		Candidates: map[string]stockpicker.CandidateScoreDetail{
			"VALID_1": {
				Ticker:          "VALID_1",
				PassedStage1:    true,
				DataFetchFailed: false,
				RawScore:        50.0,
				EffectiveScore:  50.0,
			},
			"VALID_2": {
				Ticker:          "VALID_2",
				PassedStage1:    true,
				DataFetchFailed: false,
				RawScore:        30.0,
				EffectiveScore:  30.0,
			},
			"FETCH_FAILED": {
				Ticker:          "FETCH_FAILED",
				PassedStage1:    false,
				DataFetchFailed: true,
				RejectionReason: "DATA_FETCH_FAILED: historical price bars unavailable",
				RawScore:        0.0,
				EffectiveScore:  0.0,
			},
		},
	}

	if err := db.SaveRunSnapshot(ctx, snap); err != nil {
		t.Fatalf("failed to save snapshot: %v", err)
	}

	// Calculate empirical quantiles
	q, err := db.GetEmpiricalQuantiles(ctx, "test_index", "earlymb", 0)
	if err != nil {
		t.Fatalf("failed to compute empirical quantiles: %v", err)
	}

	// Samples must be exactly 2 (VALID_1 and VALID_2), FETCH_FAILED must be excluded
	if q["samples"] != 2.0 {
		t.Errorf("expected 2 samples in quantiles, got %.0f", q["samples"])
	}
	if q["p50"] < 30.0 || q["p50"] > 50.0 {
		t.Errorf("median score out of bounds: %.2f", q["p50"])
	}
}

func TestTemporalVelocityAndBoost(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_velocity.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test pit db: %v", err)
	}
	defer db.Close()

	// Create 2 historical snapshots: Day 1 (T-2) and Day 2 (T-1)
	snap1 := &stockpicker.PITRunSnapshot{
		AsOfDate:          "2026-08-28",
		IndexName:         "test_idx",
		Method:            "earlymb",
		RegimeMultiplier:  0.7,
		TotalConstituents: 2,
		Stage1Count:       2,
		Candidates: map[string]stockpicker.CandidateScoreDetail{
			"SURGER": {
				Ticker:       "SURGER",
				PassedStage1: true,
				RawScore:     25.0,
			},
			"STABLE": {
				Ticker:       "STABLE",
				PassedStage1: true,
				RawScore:     35.0,
			},
		},
	}
	snap2 := &stockpicker.PITRunSnapshot{
		AsOfDate:          "2026-08-31",
		IndexName:         "test_idx",
		Method:            "earlymb",
		RegimeMultiplier:  0.6,
		TotalConstituents: 2,
		Stage1Count:       2,
		Candidates: map[string]stockpicker.CandidateScoreDetail{
			"SURGER": {
				Ticker:        "SURGER",
				PassedStage1:  true,
				RawScore:      35.0,
				DeliveryDelta: 0.15,
			},
			"STABLE": {
				Ticker:        "STABLE",
				PassedStage1:  true,
				RawScore:      36.0,
				DeliveryDelta: 0.05,
			},
		},
	}

	_ = db.SaveRunSnapshot(ctx, snap1)
	_ = db.SaveRunSnapshot(ctx, snap2)

	velocities, err := db.GetCandidateTemporalVelocities(ctx, "test_idx", "earlymb", "2026-09-01", 5)
	if err != nil {
		t.Fatalf("failed to get velocities: %v", err)
	}

	surger, exists := velocities["SURGER"]
	if !exists {
		t.Fatalf("expected SURGER in velocities")
	}
	if surger.RunsEvaluated != 2 {
		t.Errorf("expected 2 runs, got %d", surger.RunsEvaluated)
	}
	if surger.ConsecutivePass != 2 {
		t.Errorf("expected 2 consecutive passes, got %d", surger.ConsecutivePass)
	}

	// Now simulate Day 3 (T) scoring:
	// SURGER score rose to 45.0 (25 -> 35 -> 45) => 3-Session Consecutive Surge (+3.0) + Consecutive pass (+1.0) = +4.0
	boostSurger, _ := surger.ComputeBoost(45.0, 0.20)
	if boostSurger != 4.0 {
		t.Errorf("expected 4.0 boost for SURGER, got %.1f", boostSurger)
	}

	// STABLE score is 37.0 (35 -> 36 -> 37) => Consecutive pass (+1.0) + Surge (+3.0) = +4.0
	stable := velocities["STABLE"]
	boostStable, _ := stable.ComputeBoost(37.0, 0.05)
	if boostStable != 4.0 {
		t.Errorf("expected 4.0 boost for STABLE, got %.1f", boostStable)
	}

	// Test a stock with no past history => 0.0 boost
	emptyTV := stockpicker.TemporalVelocity{}
	if b, _ := emptyTV.ComputeBoost(40.0, 0.10); b != 0.0 {
		t.Errorf("expected 0.0 boost for new stock, got %.1f", b)
	}
}
