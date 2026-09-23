package scheduler

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunReport_RenderSuccess(t *testing.T) {
	start := time.Date(2026, 9, 23, 20, 15, 0, 0, time.UTC)
	r := newRunReport(start, "2026-09-23", true, "")

	var eod StageResult
	eod.line("3 method(s) screened")
	eod.line("2/500 integrity flags")
	eod.line("4 theme(s) synced")
	r.record(CadenceEOD, eod, 5*time.Minute+12*time.Second, nil)

	var drift StageResult
	drift.line("drift index 0.0830")
	r.record(CadenceDrift, drift, 1400*time.Millisecond, nil)

	out := r.Render()
	if !strings.Contains(out, "──── Wed 2026-09-23 20:15 ────") {
		t.Errorf("missing dated header: %q", out)
	}
	if !strings.Contains(out, "[eod]") || !strings.Contains(out, "3 method(s) screened; 2/500 integrity flags; 4 theme(s) synced in 5m12s") {
		t.Errorf("missing/incorrect eod line: %q", out)
	}
	if !strings.Contains(out, "[drift]") || !strings.Contains(out, "in 1s") {
		t.Errorf("missing/incorrect drift line: %q", out)
	}
	if !strings.Contains(out, "✓ SUCCESS in") {
		t.Errorf("missing SUCCESS summary: %q", out)
	}
}

func TestRunReport_RenderFailure(t *testing.T) {
	start := time.Now()
	r := newRunReport(start, "2026-09-23", false, "")

	var eod StageResult
	eod.line("3 method(s) screened")
	r.record(CadenceEOD, eod, 2*time.Second, nil)

	var drift StageResult
	r.record(CadenceDrift, drift, 500*time.Millisecond, errors.New("fetching quotes: 401 unauthorized"))

	out := r.Render()
	if !strings.Contains(out, "✗ FAILED — drift: fetching quotes: 401 unauthorized") {
		t.Errorf("missing FAILED summary with cause: %q", out)
	}
	if strings.Contains(out, "✓ SUCCESS") {
		t.Errorf("failed run should not report SUCCESS: %q", out)
	}
}

func TestRunReport_Warnings(t *testing.T) {
	r := newRunReport(time.Now(), "2026-09-23", true, "")
	var eod StageResult
	eod.line("3 method(s) screened")
	eod.warn("sp500 data integrity: 40/500 candidates flagged (8.0%%)")
	r.record(CadenceEOD, eod, time.Second, nil)

	out := r.Render()
	if !strings.Contains(out, "⚠ eod: sp500 data integrity") {
		t.Errorf("missing warning line: %q", out)
	}
}

func TestRunReport_Flush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "scheduler-runs.log")
	r := newRunReport(time.Now(), "2026-09-23", true, path)
	var eod StageResult
	eod.line("ok")
	r.record(CadenceEOD, eod, time.Second, nil)

	// Two flushes append two blocks (maintenance-log semantics).
	if err := r.Flush(); err != nil {
		t.Fatalf("first flush: %v", err)
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("second flush: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if n := strings.Count(string(data), "✓ SUCCESS"); n != 2 {
		t.Errorf("expected 2 appended blocks, got %d\n%s", n, data)
	}
}

func TestRunReport_EmptyPass(t *testing.T) {
	r := newRunReport(time.Now(), "2026-09-23", true, "")
	out := r.Render()
	if !strings.Contains(out, "nothing due for 2026-09-23") {
		t.Errorf("empty pass should note nothing due: %q", out)
	}
	if !strings.Contains(out, "✓ SUCCESS") {
		t.Errorf("empty pass is still a success: %q", out)
	}
}

func TestRunReport_NilSafe(t *testing.T) {
	var r *RunReport
	r.record(CadenceEOD, StageResult{}, time.Second, nil) // no panic
	if err := r.Flush(); err != nil {
		t.Errorf("nil report Flush should be no-op, got %v", err)
	}
}

func TestRunReport_PassLevelWarning(t *testing.T) {
	r := newRunReport(time.Now(), "2026-09-23", true, "")
	r.warn("holiday calendar is EMPTY — gating is weekend-only; seed the holidays table")
	var eod StageResult
	eod.line("3 method(s) screened")
	r.record(CadenceEOD, eod, time.Second, nil)

	out := r.Render()
	// Pass-level warning appears before the stage lines.
	wIdx := strings.Index(out, "holiday calendar is EMPTY")
	sIdx := strings.Index(out, "[eod]")
	if wIdx < 0 || sIdx < 0 {
		t.Fatalf("missing warning or stage line:\n%s", out)
	}
	if wIdx > sIdx {
		t.Errorf("pass-level warning should render before stages:\n%s", out)
	}
}
