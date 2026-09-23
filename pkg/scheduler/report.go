package scheduler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/config"
)

// reportFileName is the human-readable maintenance log the scheduler appends a
// dated block to per run. It lives beside the JSON slog files under the data log
// dir, but is a distinct, operator-facing artifact (not machine JSON): one block
// per run-now pass, with indented per-stage lines and a SUCCESS/FAILED summary.
// It intentionally mirrors the jira-task-manager maintenance.log convention so an
// operator can eyeball "did the nightly run work?" at a glance.
const reportFileName = "scheduler-runs.log"

// StageResult is the reporting payload a cadence returns alongside its error. It
// carries the human-readable detail lines and non-fatal warnings the maintenance
// log should show for that stage. It is purely informational — a stage still
// signals hard failure via its error return; a failing stage may still return a
// partially-populated StageResult (e.g. the lines gathered before it failed).
type StageResult struct {
	// Lines are the "[stage] detail" body lines for this cadence (counts,
	// durations, paths). Rendered indented under the stage in the block.
	Lines []string
	// Warnings are non-fatal notices (⚠) surfaced beneath the stage lines.
	Warnings []string
}

// line appends a formatted detail line to the stage result.
func (r *StageResult) line(format string, args ...any) {
	r.Lines = append(r.Lines, fmt.Sprintf(format, args...))
}

// warn appends a non-fatal warning to the stage result.
func (r *StageResult) warn(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// stageEntry is one cadence's contribution to a run report: its label, outcome,
// timing, detail lines, and warnings.
type stageEntry struct {
	cadence  Cadence
	result   StageResult
	err      error
	duration time.Duration
}

// RunReport accumulates the per-stage outcomes of a single scheduler pass and
// renders them as one dated maintenance-log block. It is opened at the start of a
// pass, fed by each cadence via record, and flushed once at the end with Flush,
// which appends the block to the maintenance log (best-effort — a reporting
// failure never fails the run).
type RunReport struct {
	started time.Time
	tradday string
	live    bool
	stages  []stageEntry
	// path is the maintenance-log file to append to. Empty → default under the
	// data log dir. Disabled reporting is signalled by a nil *RunReport.
	path string
}

// newRunReport starts a report for a pass beginning at start for the given
// settled trading day. path "" resolves to the default maintenance-log path.
func newRunReport(start time.Time, tradingDay string, live bool, path string) *RunReport {
	return &RunReport{started: start, tradday: tradingDay, live: live, path: path}
}

// record adds a completed (run) cadence with its timing, result, and error.
func (r *RunReport) record(c Cadence, res StageResult, dur time.Duration, err error) {
	if r == nil {
		return
	}
	r.stages = append(r.stages, stageEntry{cadence: c, result: res, err: err, duration: dur})
}

// reportPath resolves the maintenance-log path (explicit, else default).
func (r *RunReport) reportPath() string {
	if r.path != "" {
		return r.path
	}
	return config.DataPath("logs", reportFileName)
}

// ok reports whether every recorded stage succeeded.
func (r *RunReport) ok() bool {
	for _, s := range r.stages {
		if s.err != nil {
			return false
		}
	}
	return true
}

// firstError returns the first stage error (for the FAILED summary line), or nil.
func (r *RunReport) firstError() (Cadence, error) {
	for _, s := range r.stages {
		if s.err != nil {
			return s.cadence, s.err
		}
	}
	return "", nil
}

// Render formats the dated maintenance-log block. Layout (mirrors the jtm
// maintenance.log convention):
//
//	──── Wed 2026-09-23 20:15 ────
//	  [eod]   3 methods, 2/500 integrity flags, 4 themes synced in 5m12s
//	  [drift] index 0.083, portfolio $512,340 in 1.4s
//	  ⚠ drift: alert dispatch failed — telegram 429
//	  ✓ SUCCESS in 5m14s
//
// A run with no stages (nothing was due) still emits a header + a note so the log
// records that the scheduler fired and found nothing to do.
func (r *RunReport) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n──── %s ────\n", r.started.Format("Mon 2006-01-02 15:04"))

	if len(r.stages) == 0 {
		fmt.Fprintf(&b, "  (nothing due for %s — no cadences ran)\n", r.tradday)
		fmt.Fprintf(&b, "  ✓ SUCCESS in %s\n", humanizeDuration(time.Since(r.started)))
		return b.String()
	}

	for _, s := range r.stages {
		tag := "[" + string(s.cadence) + "]"
		detail := strings.Join(s.result.Lines, "; ")
		if detail == "" {
			if s.err != nil {
				detail = "failed"
			} else {
				detail = "completed"
			}
		}
		fmt.Fprintf(&b, "  %-9s %s in %s\n", tag, detail, humanizeDuration(s.duration))
		for _, w := range s.result.Warnings {
			fmt.Fprintf(&b, "  ⚠ %s: %s\n", s.cadence, w)
		}
		if s.err != nil {
			fmt.Fprintf(&b, "  ⚠ %s: %v\n", s.cadence, s.err)
		}
	}

	total := humanizeDuration(time.Since(r.started))
	if r.ok() {
		fmt.Fprintf(&b, "  ✓ SUCCESS in %s\n", total)
	} else {
		cad, err := r.firstError()
		fmt.Fprintf(&b, "  ✗ FAILED — %s: %v (%s)\n", cad, err, total)
	}
	return b.String()
}

// Flush appends the rendered block to the maintenance log. It is best-effort:
// the data log dir is created if missing, and any write error is returned for the
// caller to log-and-ignore (reporting must never fail the run). A nil report is a
// no-op.
func (r *RunReport) Flush() error {
	if r == nil {
		return nil
	}
	path := r.reportPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating report dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening report file: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(r.Render()); err != nil {
		return fmt.Errorf("writing report block: %w", err)
	}
	return nil
}

// humanizeDuration renders a duration the way the maintenance log expects: whole
// seconds under a minute (e.g. "5s"), and "Xm Ys" style above (matching Go's
// default truncated to seconds), so blocks stay scannable.
func humanizeDuration(d time.Duration) string {
	if d < time.Second {
		// Sub-second: show milliseconds so a fast stage isn't "0s".
		return d.Round(time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}
