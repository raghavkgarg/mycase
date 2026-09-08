package stockpicker

import (
	"testing"
)

func TestDiffSnapshots_DataUnavailableDistinction(t *testing.T) {
	prevSnap := &PITRunSnapshot{
		AsOfDate: "2026-08-31",
		Candidates: map[string]CandidateScoreDetail{
			"NSE:LAURUSLABS": {
				Ticker:       "NSE:LAURUSLABS",
				PassedStage1: true,
				Selected:     true,
				RawScore:     55.2,
			},
			"NSE:GENUINEEXIT": {
				Ticker:       "NSE:GENUINEEXIT",
				PassedStage1: true,
				Selected:     true,
				RawScore:     50.0,
			},
		},
	}

	currSnap := &PITRunSnapshot{
		AsOfDate: "2026-09-01",
		Candidates: map[string]CandidateScoreDetail{
			"NSE:LAURUSLABS": {
				Ticker:          "NSE:LAURUSLABS",
				PassedStage1:    false,
				DataFetchFailed: true,
				RejectionReason: "DATA_FETCH_FAILED: historical price bars unavailable",
				Selected:        false,
				RawScore:        0.0,
			},
			"NSE:GENUINEEXIT": {
				Ticker:          "NSE:GENUINEEXIT",
				PassedStage1:    false,
				DataFetchFailed: false,
				RejectionReason: "Below 200-Day SMA",
				Selected:        false,
				RawScore:        0.0,
			},
		},
	}

	diff := DiffSnapshots(prevSnap, currSnap)
	if diff == nil {
		t.Fatalf("expected non-nil diff report")
	}

	// LAURUSLABS must land in DataDroppedSelections, NOT in RemovedSelections
	if len(diff.DataDroppedSelections) != 1 || diff.DataDroppedSelections[0] != "NSE:LAURUSLABS" {
		t.Errorf("expected LAURUSLABS in DataDroppedSelections, got: %v", diff.DataDroppedSelections)
	}
	if len(diff.DataFailedStage1) != 1 || diff.DataFailedStage1[0] != "NSE:LAURUSLABS" {
		t.Errorf("expected LAURUSLABS in DataFailedStage1, got: %v", diff.DataFailedStage1)
	}

	// GENUINEEXIT must land in RemovedSelections and RemovedStage1
	if len(diff.RemovedSelections) != 1 || diff.RemovedSelections[0] != "NSE:GENUINEEXIT" {
		t.Errorf("expected GENUINEEXIT in RemovedSelections, got: %v", diff.RemovedSelections)
	}
	if len(diff.RemovedStage1) != 1 || diff.RemovedStage1[0] != "NSE:GENUINEEXIT" {
		t.Errorf("expected GENUINEEXIT in RemovedStage1, got: %v", diff.RemovedStage1)
	}
}
