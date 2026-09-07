package stockpicker

import (
	"fmt"
	"sort"

	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// TemporalVelocity tracks multi-session Point-in-Time score trajectories and survival consistency.
type TemporalVelocity struct {
	Ticker           string    `json:"ticker"`
	RunsEvaluated    int       `json:"runs_evaluated"`
	ConsecutivePass  int       `json:"consecutive_pass"`  // Consecutive sessions passing Stage 1 ending at latest prior run
	ScoreTrajectory  []float64 `json:"score_trajectory"`  // Chronological raw scores across past runs [T-2, T-1, ...]
	Dates            []string  `json:"dates"`             // Corresponding dates [T-2, T-1, ...]
	VelocityDelta    float64   `json:"velocity_delta"`    // Difference between most recent two past runs (T-1 - T-2)
	AvgScore         float64   `json:"avg_score"`         // Average of past valid scores
	LatestDelivDelta float64   `json:"latest_deliv_delta"`
}

// ComputeBoost returns the additive temporal velocity boost in points [0.0, 5.0] and pattern name.
func (tv *TemporalVelocity) ComputeBoost(currentRawScore, currentDelivDelta float64) (float64, string) {
	if currentRawScore <= 0 || tv.RunsEvaluated == 0 {
		return 0.0, ""
	}

	boost := 0.0
	var patterns []string
	n := len(tv.ScoreTrajectory)

	// 1. 3-Session Consecutive Surge (+3.0 pt): S_T > S_{T-1} > S_{T-2}
	if n >= 2 && tv.ScoreTrajectory[n-1] > 0 && tv.ScoreTrajectory[n-2] > 0 {
		sT1 := tv.ScoreTrajectory[n-1]
		sT2 := tv.ScoreTrajectory[n-2]
		if currentRawScore > sT1 && sT1 > sT2 {
			boost += 3.0
			patterns = append(patterns, "3-Session Surge (+3.0pt)")
		} else if currentRawScore-sT1 >= 5.0 && currentDelivDelta > 0 {
			// 2. Velocity Breakout (+2.0 pt): >= 5pt jump with positive institutional delivery
			boost += 2.0
			patterns = append(patterns, "Velocity Breakout (+2.0pt)")
		}
	} else if n == 1 && tv.ScoreTrajectory[0] > 0 {
		sT1 := tv.ScoreTrajectory[0]
		if currentRawScore-sT1 >= 5.0 && currentDelivDelta > 0 {
			boost += 2.0
			patterns = append(patterns, "Velocity Breakout (+2.0pt)")
		}
	}

	// 3. Stage-1 Survival Consistency (+1.0 pt for >= 2 prior consecutive passes + today)
	if tv.ConsecutivePass >= 2 {
		boost += 1.0
		patterns = append(patterns, "Survival Streak (+1.0pt)")
	}

	// Cap total temporal boost at 5.0 points (preserves 100-point orthogonal foundation)
	if boost > 5.0 {
		boost = 5.0
	}
	patternStr := ""
	for i, p := range patterns {
		if i > 0 {
			patternStr += " | "
		}
		patternStr += p
	}
	return boost, patternStr
}

// ApplyPITVelocityBoost applies temporal accumulation velocity bonuses to raw candidate scores.
// Returns a map of ticker -> velocity boost points applied.
func ApplyPITVelocityBoost(
	activeKeys []string,
	scores map[string]float64,
	fundamentals map[string]yfinance.Fundamentals,
	velocities map[string]TemporalVelocity,
) map[string]float64 {
	boosts := make(map[string]float64)
	if len(velocities) == 0 {
		return boosts
	}

	appliedCount := 0
	for _, t := range activeKeys {
		raw := scores[t]
		if raw <= 0 {
			continue
		}
		delivDelta := (fundamentals[t].DeliveryPct / 100.0) - 0.35
		tv, exists := velocities[t]
		if !exists {
			continue
		}
		boost, _ := tv.ComputeBoost(raw, delivDelta)
		if boost > 0 {
			scores[t] = raw + boost
			boosts[t] = boost
			appliedCount++
		}
	}

	if appliedCount > 0 {
		fmt.Printf("Applied DuckDB PIT Temporal Velocity Boosts to %d candidates (capped at +5.0 pt max)\n", appliedCount)
		// Re-sort activeKeys based on boosted scores
		sort.Slice(activeKeys, func(i, j int) bool {
			return scores[activeKeys[i]] > scores[activeKeys[j]]
		})
	}
	return boosts
}
