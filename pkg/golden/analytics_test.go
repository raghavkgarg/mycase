package golden

import (
	"testing"
)

func TestComputeValuationScore(t *testing.T) {
	tests := []struct {
		name     string
		upside   *float64
		expected float64
	}{
		{"nil upside", nil, 20.0},
		{"huge positive upside +100%", ptr(100.0), 100.0},
		{"positive upside +30%", ptr(30.0), 80.0},
		{"zero upside 0%", ptr(0.0), 50.0},
		{"mild negative -10%", ptr(-10.0), 40.0},
		{"moderate negative -35%", ptr(-35.0), 21.0},
		{"severe negative -60%", ptr(-60.0), 9.0},
		{"extreme negative -100%", ptr(-100.0), 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeValuationScore(tt.upside)
			if got != tt.expected {
				t.Errorf("ComputeValuationScore(%v) = %f, expected %f", tt.upside, got, tt.expected)
			}
		})
	}
}

func TestClassifyRegime(t *testing.T) {
	// 1. Golden Core
	c1 := GoldenConstituent{
		Ticker:          "NSE:CHENNPETRO",
		EMBPassedStage1: true,
		MBPassedStage1:  true,
		MBScore:         ptr(65.0),
		UpsidePct:       ptr(45.0),
	}
	ClassifyRegime(&c1)
	if c1.Regime != RegimeGoldenCore {
		t.Errorf("Expected RegimeGoldenCore, got %s", c1.Regime)
	}

	// 2. High Velocity
	c2 := GoldenConstituent{
		Ticker:          "NSE:DIVISLAB",
		EMBPassedStage1: true,
		MBPassedStage1:  true,
		MBScore:         ptr(70.0),
		UpsidePct:       ptr(-64.2),
	}
	ClassifyRegime(&c2)
	if c2.Regime != RegimeHighVelocity {
		t.Errorf("Expected RegimeHighVelocity, got %s", c2.Regime)
	}

	// 3. Value Breakout
	c3 := GoldenConstituent{
		Ticker:          "NSE:TURNAROUND",
		EMBPassedStage1: true,
		MBPassedStage1:  false,
		MBScore:         ptr(30.0),
		UpsidePct:       ptr(50.0),
	}
	ClassifyRegime(&c3)
	if c3.Regime != RegimeValueBreakout {
		t.Errorf("Expected RegimeValueBreakout, got %s", c3.Regime)
	}
}

func ptr(v float64) *float64 {
	return &v
}
