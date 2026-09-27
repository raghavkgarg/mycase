package eod

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/marketcal"
)

func TestConfig_Clock(t *testing.T) {
	// Default clock falls back to marketcal.NSE
	cfgDefault := Config{}
	clk := cfgDefault.clock()
	if clk.Loc != marketcal.NSE.Loc {
		t.Errorf("expected default clock to be NSE (%v), got %v", marketcal.NSE.Loc, clk.Loc)
	}

	// Explicit clock is respected
	locNY, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("loading America/New_York: %v", err)
	}
	customClock := marketcal.Clock{
		Loc:        locNY,
		CutoffHour: 16,
	}
	cfgCustom := Config{Clock: customClock}
	if cfgCustom.clock().CutoffHour != 16 {
		t.Errorf("expected cutoff hour 16, got %d", cfgCustom.clock().CutoffHour)
	}
}

func TestConfig_DryRunPlan(t *testing.T) {
	cfg := Config{
		IndexName: "niftytotalmarket",
		Method:    "earlymb,multibagger",
		TopN:      20,
	}

	plan := cfg.DryRunPlan()
	if len(plan) != 4 {
		t.Fatalf("expected 4 plan steps, got %d", len(plan))
	}

	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "niftytotalmarket") {
		t.Errorf("plan does not contain index name: %s", joined)
	}
	if !strings.Contains(joined, "earlymb, multibagger") {
		t.Errorf("plan does not format multiple methods: %s", joined)
	}
	if !strings.Contains(joined, "Top 20") {
		t.Errorf("plan does not contain Top 20: %s", joined)
	}
}

func TestWithCapturedStdout(t *testing.T) {
	called := false
	err := withCapturedStdout(context.Background(), func() error {
		called = true
		fmt.Println("This line should be captured into slog and not leak into stdout")
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error from withCapturedStdout: %v", err)
	}
	if !called {
		t.Error("expected fn to be executed")
	}
}

func TestResult_Fields(t *testing.T) {
	res := Result{
		AsOf:            "2026-09-25",
		Methods:         []string{"earlymb"},
		IntegrityTotal:  750,
		IntegrityFailed: 0,
		ThemesSynced:    5,
		Warnings:        []string{"test warning"},
	}

	if res.AsOf != "2026-09-25" || len(res.Methods) != 1 || res.IntegrityTotal != 750 {
		t.Errorf("Result struct mismatch: %+v", res)
	}
}
