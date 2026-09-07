package stockpicker

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type RunDiffReport struct {
	PreviousDate          string
	CurrentDate           string
	AddedStage1           []string
	RemovedStage1         []string
	DataFailedStage1      []string // dropped from Stage 1 specifically due to DataFetchFailed
	AddedSelections       []string
	RemovedSelections     []string // genuine exits (gate failures or score drops)
	DataDroppedSelections []string // previously selected holdings dropped due to DataFetchFailed
	ScoreDeltas           map[string]float64 // ticker -> delta
}

func DiffSnapshots(prev, curr *PITRunSnapshot) *RunDiffReport {
	if prev == nil || curr == nil {
		return nil
	}

	report := &RunDiffReport{
		PreviousDate: prev.AsOfDate,
		CurrentDate:  curr.AsOfDate,
		ScoreDeltas:  make(map[string]float64),
	}

	// 1. Stage 1 changes
	for t, cDet := range curr.Candidates {
		pDet, exists := prev.Candidates[t]
		if !exists || !pDet.PassedStage1 {
			if cDet.PassedStage1 {
				report.AddedStage1 = append(report.AddedStage1, t)
			}
		}
		if exists && pDet.PassedStage1 && cDet.PassedStage1 && !pDet.DataFetchFailed && !cDet.DataFetchFailed && pDet.RawScore > 0 && cDet.RawScore > 0 {
			delta := cDet.EffectiveScore - pDet.EffectiveScore
			if math.Abs(delta) >= 3.0 { // report meaningful score shifts (>= 3 pts)
				report.ScoreDeltas[t] = delta
			}
		}
	}
	for t, pDet := range prev.Candidates {
		cDet, exists := curr.Candidates[t]
		if pDet.PassedStage1 && (!exists || !cDet.PassedStage1) {
			if exists && (cDet.DataFetchFailed || strings.HasPrefix(cDet.RejectionReason, "DATA_FETCH_FAILED")) {
				report.DataFailedStage1 = append(report.DataFailedStage1, t)
			} else {
				report.RemovedStage1 = append(report.RemovedStage1, t)
			}
		}
	}

	// 2. Selection changes
	for t, cDet := range curr.Candidates {
		pDet, exists := prev.Candidates[t]
		if cDet.Selected && (!exists || !pDet.Selected) {
			report.AddedSelections = append(report.AddedSelections, t)
		}
	}
	for t, pDet := range prev.Candidates {
		cDet, exists := curr.Candidates[t]
		if pDet.Selected && (!exists || !cDet.Selected) {
			if exists && (cDet.DataFetchFailed || strings.HasPrefix(cDet.RejectionReason, "DATA_FETCH_FAILED")) {
				report.DataDroppedSelections = append(report.DataDroppedSelections, t)
			} else {
				report.RemovedSelections = append(report.RemovedSelections, t)
			}
		}
	}

	sort.Strings(report.AddedStage1)
	sort.Strings(report.RemovedStage1)
	sort.Strings(report.DataFailedStage1)
	sort.Strings(report.AddedSelections)
	sort.Strings(report.RemovedSelections)
	sort.Strings(report.DataDroppedSelections)

	return report
}

func PrintDiffReport(diff *RunDiffReport) {
	if diff == nil {
		return
	}
	if len(diff.AddedStage1) == 0 && len(diff.RemovedStage1) == 0 && len(diff.DataFailedStage1) == 0 &&
		len(diff.AddedSelections) == 0 && len(diff.RemovedSelections) == 0 && len(diff.DataDroppedSelections) == 0 &&
		len(diff.ScoreDeltas) == 0 {
		return
	}

	fmt.Printf("\n--- HISTORICAL RUN DIFF (vs. Previous Run %s) ---\n", diff.PreviousDate)
	if len(diff.DataDroppedSelections) > 0 {
		fmt.Printf("  🚨 [ACTION REQUIRED: DATA FETCH FAILURE ON ACTIVE HOLDINGS] (%d): %s\n",
			len(diff.DataDroppedSelections), strings.Join(diff.DataDroppedSelections, ", "))
		fmt.Printf("      ⚠️  These active holdings were NOT dropped by technical or fundamental gates; upstream data fetch failed!\n")
	}
	if len(diff.AddedSelections) > 0 {
		fmt.Printf("  * NEW Portfolio Entrants (%d): %s\n", len(diff.AddedSelections), strings.Join(diff.AddedSelections, ", "))
	}
	if len(diff.RemovedSelections) > 0 {
		fmt.Printf("  * EXITED Portfolio Candidates (Genuine Gate/Score Exits) (%d): %s\n", len(diff.RemovedSelections), strings.Join(diff.RemovedSelections, ", "))
	}
	if len(diff.AddedStage1) > 0 {
		fmt.Printf("  * Added to Stage 1 (%d): %s\n", len(diff.AddedStage1), strings.Join(diff.AddedStage1, ", "))
	}
	if len(diff.RemovedStage1) > 0 {
		fmt.Printf("  * Dropped from Stage 1 (Gate Eliminations) (%d): %s\n", len(diff.RemovedStage1), strings.Join(diff.RemovedStage1, ", "))
	}
	if len(diff.DataFailedStage1) > 0 {
		fmt.Printf("  * Dropped from Stage 1 (Data Fetch Failed) (%d): %s\n", len(diff.DataFailedStage1), strings.Join(diff.DataFailedStage1, ", "))
	}
	if len(diff.ScoreDeltas) > 0 {
		fmt.Printf("  * Significant Score Shifts (|Δ| >= 3 pts):\n")
		for t, delta := range diff.ScoreDeltas {
			fmt.Printf("      - %-15s: %+.1f pts\n", t, delta)
		}
	}
	fmt.Printf("--------------------------------------------------------------------\n\n")
}
