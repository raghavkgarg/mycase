package autopilot

import (
	"strings"
	"testing"

	"github.com/raghavkgarg/mycase/pkg/attribution"
	"github.com/raghavkgarg/mycase/pkg/config"
)

func TestFormatAlphaNudgeAlert(t *testing.T) {
	assessment := attribution.NudgeAssessment{
		Nudge:       true,
		Alpha:       -0.045,
		Threshold:   -0.02,
		TradingDays: 250,
	}
	a := FormatAlphaNudgeAlert("us_sp500", "US:SPY", assessment)

	if a.Level != "warn" {
		t.Errorf("Level = %q, want warn", a.Level)
	}
	if !strings.Contains(a.Body, "us_sp500") {
		t.Error("body should mention the portfolio")
	}
	if !strings.Contains(a.Body, "US:SPY") {
		t.Error("body should mention the benchmark")
	}
	if !strings.Contains(a.Body, "-4.50%") {
		t.Errorf("body should render the alpha figure, got:\n%s", a.Body)
	}
	if a.Title == "" {
		t.Error("expected a non-empty title")
	}
}

func TestSendAlphaNudgeAlerts_NoNudgeIsNoOp(t *testing.T) {
	// Nudge=false → returns nil without attempting delivery (no channels needed).
	assessment := attribution.NudgeAssessment{Nudge: false}
	err := SendAlphaNudgeAlerts("p", "US:SPY", assessment,
		config.ScheduleConfig{Notify: []string{"telegram"}}, config.AlertConfig{})
	if err != nil {
		t.Errorf("expected nil for a non-nudge assessment, got %v", err)
	}
}
