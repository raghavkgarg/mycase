// Package eod runs the daily end-of-day database update: point-in-time research
// screening + factor scoring, a self-healing snapshot-completeness pass, and
// theme-lifecycle synchronization. It is the library form of the `mycase db
// update` command, extracted from cmd/ so the autonomous scheduler (pkg/scheduler)
// can invoke it directly without depending on the CLI layer.
//
// Layer: L5. It composes stockpicker (L3), pithistory (L4), themedb (L1),
// config/csvloader/marketcal, so it sits above pithistory. The scheduler (L6)
// orchestrates it alongside autopilot (L5) and the daemon (L4).
//
// Market pluggability: the settlement/trading calendar is injected as a
// marketcal.Clock (Config.Clock), so the same EOD logic runs for India (NSE) or
// the US (NYSE) — the caller supplies a holiday-aware clock built from
// config/holidays.json (e.g. broker.TradingClock()). A zero-value Config.Clock
// defaults to marketcal.NSE to preserve the historical India behavior.
//
// Two-channel output: all progress is diagnostic and goes to slog (stderr/file),
// keeping stdout clean. The dry-run *plan* is returned to the caller as strings
// so the CLI can render it as a user-facing preview.
package eod

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"log/slog"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/marketcal"
	"github.com/raghavkgarg/mycase/pkg/pithistory"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
	"github.com/raghavkgarg/mycase/pkg/themedb"
)

// Config parameterizes an EOD update run.
type Config struct {
	// Fetcher routes market-data fetches (US→Schwab, else→Yahoo). Injected by the
	// caller so this package needs no dependency on datafetcher or the cmd-local
	// router constructor. If nil, stockpicker falls back to direct yfinance calls.
	Fetcher stockpicker.DataFetcher

	// Clock is the market settlement/trading calendar. Zero value → marketcal.NSE.
	Clock marketcal.Clock

	IndexName string // constituent index to screen (e.g. "sp500", "microcap250")
	Method    string // scoring method (e.g. "us_quality_momentum", "multibagger")
	DBPath    string // DuckDB path; "" → pithistory/themedb defaults (data/mycase.db)
	TopN      int    // number of selections
	Force     bool   // recompute even if a snapshot for today already exists

	// QuietStdout redirects the user-facing pick banner/funnel tables that
	// stockpicker writes to os.Stdout into slog (Debug) instead, keeping stdout
	// clean. Set by operational callers (the scheduler, whose stdout is the
	// keep-alive/log channel); left false by the interactive `db update` command
	// where the investor is meant to see that output.
	QuietStdout bool
}

// clock returns the configured clock, defaulting to NSE when unset (preserves the
// historical India-only behavior of the original `db update`).
func (c Config) clock() marketcal.Clock {
	if c.Clock.Loc == nil {
		return marketcal.NSE
	}
	return c.Clock
}

// DryRunPlan returns the human-readable steps an EOD run would perform, for the
// CLI to print as a preview. It performs no work and touches no database.
func (c Config) DryRunPlan() []string {
	return []string{
		"1. Check database schema & connections (data/mycase.db)",
		fmt.Sprintf("2. Daily PIT Research Screening for %s (%s, Top %d)", c.IndexName, c.Method, c.TopN),
		"3. Automated constituent self-healing retry pass",
		"4. Synchronize theme lifecycle, exits, and return intelligence across all themes",
	}
}

// Run executes the unified EOD update: PIT screening + scoring, self-healing
// snapshot verification, and theme-lifecycle sync. Progress is logged via slog;
// stdout is left clean for command results. A per-ticker or per-theme failure is
// logged and skipped, never aborting the whole run (API discipline).
func Run(ctx context.Context, cfg Config) error {
	clock := cfg.clock()
	now := time.Now()
	targetEOD := clock.SettlementDate(now)
	targetDateStr := targetEOD.Format("2006-01-02")
	nextAvailable := clock.NextEODAvailable(now)

	slog.InfoContext(ctx, "eod.started",
		"index", cfg.IndexName, "method", cfg.Method, "top_n", cfg.TopN,
		"as_of", targetDateStr, "next_eod", nextAvailable.Format("2006-01-02 15:04"))

	db, err := themedb.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("opening mycase.db: %w", err)
	}
	defer db.Close()

	if err := runScreening(ctx, cfg, targetDateStr); err != nil {
		return err
	}
	syncThemes(ctx, db)

	slog.InfoContext(ctx, "eod.completed", "as_of", targetDateStr)
	return nil
}

// runScreening performs Stage 1 (PIT screening + scoring) and Stage 2 (self-heal
// + integrity check). It short-circuits when today's snapshot already exists and
// Force is not set.
func runScreening(ctx context.Context, cfg Config, targetDateStr string) error {
	slog.InfoContext(ctx, "eod.stage_started", "stage", "1/3", "name", "pit_screening",
		"index", cfg.IndexName, "method", cfg.Method)

	if !cfg.Force && snapshotExists(ctx, cfg.DBPath, targetDateStr, cfg.IndexName, cfg.Method) {
		slog.InfoContext(ctx, "eod.pit_skipped_already_run",
			"as_of", targetDateStr, "index", cfg.IndexName, "method", cfg.Method)
		return nil
	}

	opts := &stockpicker.Options{
		DataFetcher:        cfg.Fetcher,
		IndexName:          cfg.IndexName,
		Method:             cfg.Method,
		TopN:               cfg.TopN,
		RangeStr:           "1y",
		RebalanceTolerance: 0.10,
		AsOfDate:           targetDateStr,
	}
	// stockpicker.RunWithResult prints the pick banner + funnel tables to stdout
	// (user-facing `pick` output). For an operational caller (QuietStdout — e.g.
	// the scheduler, whose stdout is the log/keep-alive channel) redirect it into
	// slog (debug) per the two-channel rule; the interactive `db update` leaves it
	// on stdout for the investor.
	run := func() error { return runPick(ctx, cfg.DBPath, opts) }
	var err error
	if cfg.QuietStdout {
		err = withCapturedStdout(ctx, run)
	} else {
		err = run()
	}
	if err != nil {
		// Non-fatal: continue to the self-healing pass, which may recover dropouts.
		slog.WarnContext(ctx, "eod.pit_warning", "err", err, "recovery", "self_heal")
	}

	// Stage 2: self-healing retry for transient dropouts.
	slog.InfoContext(ctx, "eod.stage_started", "stage", "2/3", "name", "self_heal")
	if snap, sErr := stockpicker.RetryFailedSnapshotCandidates(ctx, cfg.IndexName, cfg.Method, targetDateStr); sErr == nil && snap != nil {
		if pitDB, pErr := pithistory.Open(cfg.DBPath); pErr == nil {
			if err := pitDB.SaveRunSnapshot(ctx, snap); err != nil {
				slog.WarnContext(ctx, "eod.snapshot_save_failed", "err", err)
			}
			pitDB.Close()
		}
	} else if sErr != nil {
		slog.WarnContext(ctx, "eod.self_heal_notice", "err", sErr)
	}

	checkIntegrity(ctx, cfg.DBPath, cfg.IndexName, cfg.Method)
	return nil
}

// snapshotExists reports whether a PIT snapshot already exists for the run key.
func snapshotExists(ctx context.Context, dbPath, asOf, index, method string) bool {
	pitDB, err := pithistory.Open(dbPath)
	if err != nil {
		return false
	}
	defer pitDB.Close()
	has, _ := pitDB.HasRun(ctx, asOf, index, method)
	return has
}

// checkIntegrity audits the freshly committed snapshot and logs a warning when a
// material fraction of candidates have unverified/missing fundamentals.
func checkIntegrity(ctx context.Context, dbPath, index, method string) {
	pitDB, err := pithistory.Open(dbPath)
	if err != nil {
		return
	}
	defer pitDB.Close()

	integrity, iErr := pitDB.CheckDataIntegrity(ctx, index, method)
	if iErr != nil || integrity == nil || integrity.TotalCandidates == 0 {
		return
	}
	if integrity.FailurePct >= 5.0 {
		slog.WarnContext(ctx, "eod.data_integrity_warning",
			"failed", integrity.FailedCandidates,
			"total", integrity.TotalCandidates,
			"failure_pct", integrity.FailurePct,
			"flagged_sample", strings.Join(integrity.FlaggedTickers, ","))
		return
	}
	slog.InfoContext(ctx, "eod.data_integrity_ok",
		"clean", integrity.TotalCandidates-integrity.FailedCandidates,
		"total", integrity.TotalCandidates)
}

// syncThemes runs Stage 3: synchronize each configured theme's lifecycle, exits,
// and return intelligence from the latest proposals. A theme with no local files
// yet is skipped, not fatal.
func syncThemes(ctx context.Context, db *themedb.DB) {
	slog.InfoContext(ctx, "eod.stage_started", "stage", "3/3", "name", "theme_sync")
	themes, err := config.LoadThemes(config.Path("themes.json"))
	if err != nil {
		slog.WarnContext(ctx, "eod.themes_load_failed", "err", err)
		return
	}
	for _, tc := range themes {
		uName := csvloader.GetUniverseName(tc.CSVPath)
		kw := strings.ToLower(tc.Prefix)
		if strings.Contains(strings.ToLower(tc.Name), "microsmall") {
			kw = "microsmall"
		}
		syncOpts := themedb.SyncThemeOptions{
			ThemeName:     uName,
			GoldenCSVPath: tc.CSVPath,
			ProposalsDir:  config.DataPath("candidates", "proposals"),
			Keyword:       kw,
		}
		if err := db.SyncThemeFromProposals(ctx, syncOpts); err != nil {
			// Non-fatal: a theme may have no local proposals yet.
			slog.DebugContext(ctx, "eod.theme_sync_skipped", "theme", tc.Name, "err", err)
			continue
		}
		v, _ := db.GetLatestVersion(ctx, uName)
		active, _ := db.GetActiveHoldings(ctx, uName)
		exited, _ := db.GetExitedHoldings(ctx, uName)
		slog.InfoContext(ctx, "eod.theme_synced",
			"theme", tc.Name, "universe", uName,
			"version", v, "active", len(active), "exited", len(exited))
	}
}

// runPick screens + scores a universe via stockpicker and persists the resulting
// point-in-time snapshot to DuckDB. The persistence lives here (not in
// stockpicker) because pithistory imports stockpicker — the reverse edge would be
// an import cycle. The DataFetcher must already be set on opts by the caller.
func runPick(ctx context.Context, dbPath string, opts *stockpicker.Options) error {
	result, err := stockpicker.RunWithResult(ctx, opts)
	if err != nil {
		return err
	}
	if result == nil || result.PITSnapshot == nil {
		return nil
	}
	pitDB, dbErr := pithistory.Open(dbPath)
	if dbErr != nil {
		slog.WarnContext(ctx, "eod.snapshot_store_unavailable", "err", dbErr)
		return nil
	}
	defer pitDB.Close()
	if sErr := pitDB.SaveRunSnapshot(ctx, result.PITSnapshot); sErr != nil {
		slog.WarnContext(ctx, "eod.snapshot_save_failed", "err", sErr)
	} else {
		slog.InfoContext(ctx, "eod.snapshot_persisted", "as_of", opts.AsOfDate)
	}
	return nil
}

// withCapturedStdout runs fn with os.Stdout redirected into a pipe whose lines are
// forwarded to slog at Debug level (event "eod.pick_output"), then restores
// os.Stdout. This keeps the user-facing `pick` banner/tables that
// stockpicker.RunWithResult writes to stdout out of the scheduler's stdout/log
// channel while preserving them (at debug) for troubleshooting. On any pipe setup
// failure it falls back to running fn with stdout untouched, so EOD never fails
// merely because capture could not be established.
func withCapturedStdout(ctx context.Context, fn func() error) error {
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		return fn()
	}
	orig := os.Stdout
	os.Stdout = w

	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), " \t")
			if line == "" {
				continue
			}
			slog.DebugContext(ctx, "eod.pick_output", "line", line)
		}
		_, _ = io.Copy(io.Discard, r) // drain any remainder after a scan error
	}()

	runErr := fn()

	os.Stdout = orig
	_ = w.Close()
	<-done
	_ = r.Close()
	return runErr
}
