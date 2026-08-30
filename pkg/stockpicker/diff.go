package stockpicker

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type RunDiffReport struct {
	PreviousDate      string
	CurrentDate       string
	AddedStage1       []string
	RemovedStage1     []string
	AddedSelections   []string
	RemovedSelections []string
	ScoreDeltas       map[string]float64 // ticker -> delta
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
		if exists && pDet.PassedStage1 && cDet.PassedStage1 {
			delta := cDet.EffectiveScore - pDet.EffectiveScore
			if math.Abs(delta) >= 3.0 { // report meaningful score shifts (>= 3 pts)
				report.ScoreDeltas[t] = delta
			}
		}
	}
	for t, pDet := range prev.Candidates {
		cDet, exists := curr.Candidates[t]
		if pDet.PassedStage1 && (!exists || !cDet.PassedStage1) {
			report.RemovedStage1 = append(report.RemovedStage1, t)
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
			report.RemovedSelections = append(report.RemovedSelections, t)
		}
	}

	sort.Strings(report.AddedStage1)
	sort.Strings(report.RemovedStage1)
	sort.Strings(report.AddedSelections)
	sort.Strings(report.RemovedSelections)

	return report
}

func PrintDiffReport(diff *RunDiffReport) {
	if diff == nil {
		return
	}
	if len(diff.AddedStage1) == 0 && len(diff.RemovedStage1) == 0 && len(diff.AddedSelections) == 0 && len(diff.RemovedSelections) == 0 && len(diff.ScoreDeltas) == 0 {
		return
	}

	fmt.Printf("\n--- HISTORICAL RUN DIFF (vs. Previous Run %s) ---\n", diff.PreviousDate)
	if len(diff.AddedSelections) > 0 {
		fmt.Printf("  * NEW Portfolio Entrants (%d): %s\n", len(diff.AddedSelections), strings.Join(diff.AddedSelections, ", "))
	}
	if len(diff.RemovedSelections) > 0 {
		fmt.Printf("  * EXITED Portfolio Candidates (%d): %s\n", len(diff.RemovedSelections), strings.Join(diff.RemovedSelections, ", "))
	}
	if len(diff.AddedStage1) > 0 {
		fmt.Printf("  * Added to Stage 1 (%d): %s\n", len(diff.AddedStage1), strings.Join(diff.AddedStage1, ", "))
	}
	if len(diff.RemovedStage1) > 0 {
		fmt.Printf("  * Dropped from Stage 1 (%d): %s\n", len(diff.RemovedStage1), strings.Join(diff.RemovedStage1, ", "))
	}
	if len(diff.ScoreDeltas) > 0 {
		fmt.Printf("  * Significant Score Shifts (|Δ| >= 3 pts):\n")
		for t, delta := range diff.ScoreDeltas {
			fmt.Printf("      - %-15s: %+.1f pts\n", t, delta)
		}
	}
	fmt.Printf("--------------------------------------------------------------------\n\n")
}
