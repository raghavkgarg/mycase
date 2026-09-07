package stockpicker

import (
	"testing"

	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

func TestApplyPITVelocityBoost(t *testing.T) {
	activeKeys := []string{"STOCK_A", "STOCK_B", "STOCK_C"}
	scores := map[string]float64{
		"STOCK_A": 35.0,
		"STOCK_B": 40.0,
		"STOCK_C": 30.0,
	}
	fundamentals := map[string]yfinance.Fundamentals{
		"STOCK_A": {DeliveryPct: 55.0}, // +20% delta
		"STOCK_B": {DeliveryPct: 35.0}, // 0% delta
		"STOCK_C": {DeliveryPct: 20.0}, // -15% delta
	}

	velocities := map[string]TemporalVelocity{
		// STOCK_A: 20 -> 28 -> 35 (surging: 3-session surge (+3.0) + 2 passes (+1.0) = +4.0)
		"STOCK_A": {
			Ticker:          "STOCK_A",
			RunsEvaluated:   2,
			ConsecutivePass: 2,
			ScoreTrajectory: []float64{20.0, 28.0},
		},
		// STOCK_B: 40 -> 40 (stable: 2 passes (+1.0) = +1.0)
		"STOCK_B": {
			Ticker:          "STOCK_B",
			RunsEvaluated:   2,
			ConsecutivePass: 2,
			ScoreTrajectory: []float64{40.0, 40.0},
		},
		// STOCK_C: has no velocity record
	}

	boosts := ApplyPITVelocityBoost(activeKeys, scores, fundamentals, velocities)

	if boosts["STOCK_A"] != 4.0 {
		t.Errorf("expected 4.0 boost for STOCK_A, got %.1f", boosts["STOCK_A"])
	}
	if scores["STOCK_A"] != 39.0 {
		t.Errorf("expected 39.0 raw score for STOCK_A, got %.1f", scores["STOCK_A"])
	}

	if boosts["STOCK_B"] != 1.0 {
		t.Errorf("expected 1.0 boost for STOCK_B, got %.1f", boosts["STOCK_B"])
	}
	if scores["STOCK_B"] != 41.0 {
		t.Errorf("expected 41.0 raw score for STOCK_B, got %.1f", scores["STOCK_B"])
	}

	if _, exists := boosts["STOCK_C"]; exists {
		t.Errorf("expected no boost for STOCK_C")
	}

	// Active keys should be sorted: STOCK_B (41.0) > STOCK_A (39.0) > STOCK_C (30.0)
	if activeKeys[0] != "STOCK_B" || activeKeys[1] != "STOCK_A" || activeKeys[2] != "STOCK_C" {
		t.Errorf("unexpected sorted activeKeys: %v", activeKeys)
	}
}
