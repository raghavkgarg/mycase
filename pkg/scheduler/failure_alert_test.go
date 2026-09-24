package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/broker/schwab"
)

// isolateState points the scheduler state file at a temp dir so runCadence's
// SaveState never touches the real data dir.
func isolateState(t *testing.T) {
	t.Helper()
	t.Setenv("MYCASE_DATA_DIR", t.TempDir())
}

// A transient (non-auth) failure increments the streak but does NOT alert until
// it reaches the configured threshold; on threshold it alerts once and marks the
// cadence alerted so it won't re-alert on the next failure.
func TestFailure_TransientAlertsAtThreshold(t *testing.T) {
	isolateState(t)
	cfg := baseConfig()
	cfg.FailureAlertAfter = 3
	s := newTestScheduler(cfg, &fakeRunner{})

	genericErr := fmt.Errorf("yahoo 503 service unavailable")

	// 1st and 2nd failures: below threshold → no alert.
	s.runCadence(context.Background(), CadenceEOD, "2026-09-16", func(context.Context) (StageResult, error) {
		return StageResult{}, genericErr
	}, newRunReport(time.Now(), "", false, ""))
	if s.state.alreadyAlerted(CadenceEOD) {
		t.Fatal("alerted after 1 failure; threshold is 3")
	}
	s.runCadence(context.Background(), CadenceEOD, "2026-09-16", func(context.Context) (StageResult, error) {
		return StageResult{}, genericErr
	}, newRunReport(time.Now(), "", false, ""))
	if s.state.alreadyAlerted(CadenceEOD) {
		t.Fatal("alerted after 2 failures; threshold is 3")
	}
	if got := s.state.failCount(CadenceEOD); got != 2 {
		t.Fatalf("failCount = %d, want 2", got)
	}

	// 3rd failure: hits threshold → alerted (log-only since no channels configured).
	s.runCadence(context.Background(), CadenceEOD, "2026-09-16", func(context.Context) (StageResult, error) {
		return StageResult{}, genericErr
	}, newRunReport(time.Now(), "", false, ""))
	if !s.state.alreadyAlerted(CadenceEOD) {
		t.Fatal("expected alert at 3rd consecutive failure")
	}
	if got := s.state.failCount(CadenceEOD); got != 3 {
		t.Fatalf("failCount = %d, want 3", got)
	}
}

// An auth error (schwab.ErrReauthRequired) alerts on the FIRST failure, since it
// never self-heals — waiting for the threshold would be pointless.
func TestFailure_AuthAlertsImmediately(t *testing.T) {
	isolateState(t)
	cfg := baseConfig()
	cfg.FailureAlertAfter = 3 // high threshold; auth must bypass it
	s := newTestScheduler(cfg, &fakeRunner{})

	authErr := fmt.Errorf("refresh failed: %w", schwab.ErrReauthRequired)
	s.runCadence(context.Background(), CadenceDrift, "2026-09-16", func(context.Context) (StageResult, error) {
		return StageResult{}, authErr
	}, newRunReport(time.Now(), "", false, ""))

	if !s.state.alreadyAlerted(CadenceDrift) {
		t.Fatal("auth error should alert on the first failure")
	}
	if got := s.state.failCount(CadenceDrift); got != 1 {
		t.Fatalf("failCount = %d, want 1", got)
	}
}

// A success after failures clears the streak and the alerted flag, so a later
// failure starts a fresh count (and can alert again).
func TestFailure_SuccessResetsStreak(t *testing.T) {
	isolateState(t)
	cfg := baseConfig()
	cfg.FailureAlertAfter = 2
	s := newTestScheduler(cfg, &fakeRunner{})

	genericErr := fmt.Errorf("network down")
	fail := func(context.Context) (StageResult, error) { return StageResult{}, genericErr }
	ok := func(context.Context) (StageResult, error) { return StageResult{}, nil }

	// Two failures → alerted.
	s.runCadence(context.Background(), CadenceEOD, "2026-09-16", fail, newRunReport(time.Now(), "", false, ""))
	s.runCadence(context.Background(), CadenceEOD, "2026-09-16", fail, newRunReport(time.Now(), "", false, ""))
	if !s.state.alreadyAlerted(CadenceEOD) {
		t.Fatal("expected alerted after 2 failures (threshold 2)")
	}

	// Success clears everything.
	s.runCadence(context.Background(), CadenceEOD, "2026-09-17", ok, newRunReport(time.Now(), "", false, ""))
	if s.state.failCount(CadenceEOD) != 0 {
		t.Fatalf("failCount after success = %d, want 0", s.state.failCount(CadenceEOD))
	}
	if s.state.alreadyAlerted(CadenceEOD) {
		t.Fatal("alerted flag should clear on success")
	}
}
