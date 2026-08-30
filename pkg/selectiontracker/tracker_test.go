package selectiontracker

import (
	"math"
	"testing"
)

func TestSelectionFunnel_NonZeroStagesValidation(t *testing.T) {
	tracker := New()
	tracker.InitialCount = 10

	// 2 Fail Stage 1
	tracker.RecordSafetyDrop("STOCK_FAIL1", "Low ROCE")
	tracker.RecordSafetyDrop("STOCK_FAIL2", "High Debt")

	// 8 Pass Stage 1
	tracker.RecordRawScore("STOCK1", 40.0, 1)
	tracker.RecordRawScore("STOCK2", 38.0, 2)
	tracker.RecordRawScore("STOCK3", 35.0, 3)
	tracker.RecordRawScore("STOCK4", 33.0, 4)
	tracker.RecordRawScore("STOCK5", 31.0, 5)
	tracker.RecordRawScore("STOCK6", 29.0, 6)
	tracker.RecordRawScore("STOCK7", 25.0, 7)
	tracker.RecordRawScore("STOCK8", 20.0, 8)

	// Stage 2 Outcomes:
	// 2 Regime Cutoff Drops (< 30.0)
	tracker.RecordScoreThresholdDrop("STOCK7", "Below cutoff")
	tracker.RecordScoreThresholdDrop("STOCK8", "Below cutoff")

	// 1 Sector Cap Drop
	tracker.RecordSectorCapDrop("STOCK5", "Technology", []string{"STOCK1", "STOCK2", "STOCK3"})

	// 1 Rank Limit / Hysteresis Drop
	tracker.RecordHysteresisDrop("STOCK6", 6, 4, 5, false)

	// 4 Final Selected
	tracker.RecordSelected("STOCK1", 1, 4, false)
	tracker.RecordSelected("STOCK2", 2, 4, false)
	tracker.RecordSelected("STOCK3", 3, 4, false)
	tracker.RecordSelected("STOCK4", 4, 4, false)

	funnel, err := tracker.BuildFunnel()
	if err != nil {
		t.Fatalf("expected funnel validation to succeed, got error: %v", err)
	}

	if len(funnel.Stage1Survivors) != 8 {
		t.Errorf("expected 8 Stage 1 survivors, got %d", len(funnel.Stage1Survivors))
	}
	if len(funnel.RegimeRejected) != 2 {
		t.Errorf("expected 2 Regime Rejected, got %d", len(funnel.RegimeRejected))
	}
	if len(funnel.SectorCapped) != 1 {
		t.Errorf("expected 1 Sector Capped, got %d", len(funnel.SectorCapped))
	}
	if len(funnel.RankLimited) != 1 {
		t.Errorf("expected 1 Rank Limited, got %d", len(funnel.RankLimited))
	}
	if len(funnel.FinalSelected) != 4 {
		t.Errorf("expected 4 Final Selected, got %d", len(funnel.FinalSelected))
	}

	// Deliberate corruption test: drop one candidate from accounted outcomes
	corruptFunnel := funnel
	corruptFunnel.FinalSelected = corruptFunnel.FinalSelected[:3] // 3 instead of 4
	if cErr := corruptFunnel.Validate(); cErr == nil {
		t.Errorf("expected Validate() to fail on mismatched count, but it passed")
	}
}

func TestTracker_RawAndEffectiveScoreConsistency(t *testing.T) {
	tracker := New()
	tracker.RegimeMultiplier = 0.6558

	tracker.RecordScore("TEST1", 45.6, 29.9, 1)
	if tracker.RawScores["TEST1"] != 45.6 {
		t.Errorf("expected RawScore 45.6, got %.2f", tracker.RawScores["TEST1"])
	}
	if tracker.EffectiveScores["TEST1"] != 29.9 {
		t.Errorf("expected EffectiveScore 29.9, got %.2f", tracker.EffectiveScores["TEST1"])
	}

	// Automatic computation test with RecordRawScore
	tracker.RecordRawScore("TEST2", 50.0, 2)
	expectedEff := 50.0 * 0.6558
	if tracker.RawScores["TEST2"] != 50.0 {
		t.Errorf("expected RawScore 50.0, got %.2f", tracker.RawScores["TEST2"])
	}
	if math.Abs(tracker.EffectiveScores["TEST2"]-expectedEff) > 1e-4 {
		t.Errorf("expected EffectiveScore %.4f, got %.4f", expectedEff, tracker.EffectiveScores["TEST2"])
	}
}

