// Package scheduler is the autonomous orchestrator (Phase 12): one long-lived
// process that owns all three operating cadences instead of three separate OS
// units. It runs a single in-process tick loop and dispatches, in dependency
// order:
//
//   - EOD update (daily, after market close) — refresh the DuckDB cache/snapshot.
//   - Drift check (daily, after EOD) — alert if the live portfolio has drifted.
//   - Rebalance (quarterly/monthly) — produce an autopilot proposal.
//
// Coordination the three former standalone units could not achieve is trivial
// here: the drift check runs only after the same day's EOD completes (so it reads
// a fresh cache), and a rebalance day forces an EOD first. Every cadence is gated
// on a single holiday-aware trading-day authority (marketcal via the injected
// Clock), so weekends and exchange holidays are skipped uniformly.
//
// Market pluggability (option A): one active market per install. The Clock is
// injected (broker.TradingClock() at the composition root), and all cadence
// timing derives from it, so switching markets is a config change, not a rewrite.
//
// Investor-in-the-loop: the scheduler may screen, propose, and alert, but it
// NEVER places orders unless AutoExecute is set AND the run is live. Default off.
//
// Layer: L6 (beside server). It composes eod (L5), autopilot (L5), daemon (L4),
// broker (L1), config, and marketcal, so it sits at the top of pkg/.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"log/slog"

	"github.com/raghavkgarg/mycase/pkg/alert"
	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/broker/schwab"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/daemon"
	"github.com/raghavkgarg/mycase/pkg/marketcal"
)

// Cadence identifies one of the scheduler's three operating rhythms.
type Cadence string

const (
	CadenceEOD       Cadence = "eod"
	CadenceDrift     Cadence = "drift"
	CadenceRebalance Cadence = "rebalance"
)

// Config parameterizes the scheduler. It is assembled by the composition root
// (cmd/scheduler.go) from pipeline.yaml + defaults.json + the active broker, so
// this package needs no file parsing and stays unit-testable.
type Config struct {
	// Clock is the active market's holiday-aware calendar (broker.TradingClock()).
	// All cadence gating and EOD timing derive from it.
	Clock marketcal.Clock

	// Broker is the live broker used for the drift check and rebalance proposal.
	Broker broker.Broker

	// Alert carries drift-alert channels/threshold (from pipeline.yaml alerts:).
	Alert config.AlertConfig

	// Pipeline is the resolved pipeline config for the rebalance cadence.
	Pipeline config.PipelineConfig

	// PortfolioFile is the golden-copy CSV the drift check compares against.
	PortfolioFile string

	// ConfigPath is the pipeline.yaml path (passed to autopilot for report context).
	ConfigPath string

	// EODIndex/EODMethod/EODTopN parameterize the daily EOD screening run.
	EODIndex  string
	EODMethod string
	EODTopN   int

	// DBPath is the DuckDB path for the EOD update ("" → default).
	DBPath string

	// CloseOffsetMin is how long after the market close cutoff the daily cadences
	// fire (gives the EOD data time to settle). Default 15.
	CloseOffsetMin int

	// MaxRunMin bounds a single RunOnce pass. When it elapses the run's context is
	// cancelled, which propagates through the broker rate-limiter (limiter.Wait)
	// and every ctx-aware HTTP call, unwinding the pass and releasing the DuckDB
	// writer lock rather than sitting on it. Default 20 (see maxRun).
	MaxRunMin int

	// FailureAlertAfter is the number of consecutive failed attempts of a cadence
	// that triggers a persistent-failure alert. An auth error (re-auth required)
	// always alerts on the first occurrence regardless of this. Default 3.
	FailureAlertAfter int

	// Enable toggles per cadence. A disabled cadence is never dispatched.
	EnableEOD       bool
	EnableDrift     bool
	EnableRebalance bool

	// AutoExecute permits the rebalance cadence to place orders (requires Live too).
	// Default false — the investor confirms via the dashboard.
	AutoExecute bool

	// Live indicates a real (non-dry) run; combined with AutoExecute it is the
	// only path that would place orders. The proposal/alert flow runs regardless.
	Live bool

	// EnableReport turns on the human-readable maintenance-log block appended per
	// pass (data/logs/scheduler-runs.log by default). Default off — an explicit
	// opt-in like the cadence toggles. Empty ReportPath uses the default location.
	EnableReport bool

	// ReportPath overrides the maintenance-log file path. Empty → the default
	// under the data log dir (data/logs/scheduler-runs.log).
	ReportPath string
}

// closeOffset returns the configured post-close offset, defaulting to 15 minutes.
func (c Config) closeOffset() time.Duration {
	if c.CloseOffsetMin <= 0 {
		return 15 * time.Minute
	}
	return time.Duration(c.CloseOffsetMin) * time.Minute
}

// maxRun returns the overall deadline for a single RunOnce pass, defaulting to
// 20 minutes. It is deliberately generous: a cold-cache catch-up on the US path
// (~500 tickers) can take several minutes, but a run that exceeds this is stuck
// (hung socket, dead broker) and must release the DuckDB lock.
func (c Config) maxRun() time.Duration {
	if c.MaxRunMin <= 0 {
		return 20 * time.Minute
	}
	return time.Duration(c.MaxRunMin) * time.Minute
}

// failureAlertAfter returns the consecutive-failure count that triggers an alert,
// defaulting to 3.
func (c Config) failureAlertAfter() int {
	if c.FailureAlertAfter <= 0 {
		return 3
	}
	return c.FailureAlertAfter
}

// Runner executes a single cadence. Split out so the tick loop is testable with
// fakes and the real dispatch (eod.Run / daemon.RunCheck / autopilot.Run) lives
// in dispatch.go. Each method returns a StageResult carrying the human-readable
// detail lines + warnings for the maintenance-log report, alongside the error
// that gates success.
type Runner interface {
	RunEOD(ctx context.Context) (StageResult, error)
	RunDrift(ctx context.Context) (StageResult, error)
	RunRebalance(ctx context.Context) (StageResult, error)
}

// Scheduler owns the tick loop and per-cadence last-run state (for catch-up).
type Scheduler struct {
	cfg    Config
	runner Runner
	state  State
}

// New builds a Scheduler with the default (production) runner that dispatches to
// eod/daemon/autopilot. Pass a custom Runner via NewWithRunner for tests.
func New(cfg Config) *Scheduler {
	return NewWithRunner(cfg, &defaultRunner{cfg: cfg})
}

// NewWithRunner builds a Scheduler with an explicit Runner (used by tests).
func NewWithRunner(cfg Config, r Runner) *Scheduler {
	st, _ := LoadState()
	return &Scheduler{cfg: cfg, runner: r, state: st}
}

// Run blocks, dispatching cadences at each daily tick until ctx is cancelled.
// On start it performs a catch-up pass (running a missed EOD if the machine was
// asleep at the last close), then sleeps until the next post-close tick.
func (s *Scheduler) Run(ctx context.Context) error {
	slog.InfoContext(ctx, "scheduler.started",
		"market_cutoff_hour", s.cfg.Clock.CutoffHour,
		"eod", s.cfg.EnableEOD, "drift", s.cfg.EnableDrift, "rebalance", s.cfg.EnableRebalance,
		"auto_execute", s.cfg.AutoExecute, "live", s.cfg.Live)

	startNow := time.Now()
	startRep := newRunReport(startNow, s.cfg.Clock.SettlementDate(startNow).Format("2006-01-02"), s.cfg.Live, s.cfg.ReportPath)
	s.catchUp(ctx, startRep)
	s.flushReport(ctx, startRep)

	for {
		next := s.nextTick(time.Now())
		slog.InfoContext(ctx, "scheduler.next_tick",
			"at", next.Local().Format("2006-01-02 15:04:05 MST"))

		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "scheduler.stopped")
			return nil
		case <-time.After(time.Until(next)):
		}

		now := time.Now()
		rep := newRunReport(now, s.cfg.Clock.SettlementDate(now).Format("2006-01-02"), s.cfg.Live, s.cfg.ReportPath)
		s.warnIfEmptyCalendar(ctx, rep)
		s.tick(ctx, now, rep)
		s.flushReport(ctx, rep)
	}
}

// RunOnce performs a single sequenced dispatch pass and returns, instead of
// blocking in a tick loop. It is the entry point for the launchd/systemd
// one-shot model (`scheduler tick`): the OS scheduler owns *when* to fire (a
// StartCalendarInterval / OnCalendar timer at market-close+offset, which handles
// sleep/wake correctly), and this owns *what* runs and in what order.
//
// It first runs the same catch-up check as the keep-alive loop (so a close missed
// while the machine slept is still processed on the next fire), then, if today is
// a trading day, runs the ordered EOD → drift → rebalance pass for the current
// settled day. All ordering and cross-cadence coordination is preserved because
// it is still a single process running sequential calls — the loop was never what
// provided the ordering. State (scheduler_state.json) prevents double-runs across
// invocations exactly as it does across loop ticks.
func (s *Scheduler) RunOnce(ctx context.Context) error {
	// Single-instance guard: refuse up front if another run holds the lock, so a
	// stray manual run-now overlapping the scheduled fire never collides on the
	// DuckDB writer lock mid-pass. Stale locks (from a killed run) self-heal.
	lock, err := acquireLock()
	if err != nil {
		if locked := (ErrLocked{}); errors.As(err, &locked) {
			slog.WarnContext(ctx, "scheduler.run_refused_locked", "holder_pid", locked.PID)
		}
		return err
	}
	defer lock.release()

	// Bound the whole pass. Without this, a hung outbound call (dead broker,
	// stalled socket) would sit indefinitely holding the single-writer DuckDB
	// lock — the failure mode that blocked a concurrent command in the field.
	// The deadline propagates through the broker's limiter.Wait(ctx) and every
	// ctx-aware HTTP call, so the pass unwinds and releases the lock.
	ctx, cancel := context.WithTimeout(ctx, s.cfg.maxRun())
	defer cancel()

	slog.InfoContext(ctx, "scheduler.tick_once_started",
		"eod", s.cfg.EnableEOD, "drift", s.cfg.EnableDrift, "rebalance", s.cfg.EnableRebalance,
		"auto_execute", s.cfg.AutoExecute, "live", s.cfg.Live, "max_run_min", int(s.cfg.maxRun().Minutes()))

	now := time.Now()
	rep := newRunReport(now, s.cfg.Clock.SettlementDate(now).Format("2006-01-02"), s.cfg.Live, s.cfg.ReportPath)
	s.warnIfEmptyCalendar(ctx, rep)
	s.catchUp(ctx, rep)
	s.tick(ctx, now, rep)
	s.flushReport(ctx, rep)

	slog.InfoContext(ctx, "scheduler.tick_once_completed")
	return nil
}

// warnIfEmptyCalendar flags a pass whose active clock carries no holidays — an
// almost-certain sign the holidays table is unseeded (a real exchange always has
// holidays), which silently degrades gating to weekend-only. It records a
// pass-level report warning and logs at WARN so the miss is observable at the run
// itself rather than months later when a cadence fires on a holiday. It never
// blocks the run (the empty calendar still yields a usable weekend-only clock).
func (s *Scheduler) warnIfEmptyCalendar(ctx context.Context, rep *RunReport) {
	if len(s.cfg.Clock.Holidays) > 0 {
		return
	}
	slog.WarnContext(ctx, "scheduler.empty_holiday_calendar",
		"impact", "trading-day gating is WEEKEND-ONLY (holidays not skipped)",
		"fix", "seed the holidays table (duckdb data/mycase.db < holiday.sql) — see docs/18-runbook.md")
	rep.warn("holiday calendar is EMPTY — gating is weekend-only; seed the holidays table (see docs/18-runbook.md)")
}

// flushReport appends the maintenance-log block for a completed pass, unless
// reporting is disabled (Config.EnableReport false) or the report is empty (a
// keep-alive catch-up pass that ran nothing — no need to log "nothing due" on
// every daemon tick). A write failure is logged and swallowed: reporting must
// never fail the run.
func (s *Scheduler) flushReport(ctx context.Context, rep *RunReport) {
	if rep == nil || !s.cfg.EnableReport || len(rep.stages) == 0 {
		return
	}
	if err := rep.Flush(); err != nil {
		slog.WarnContext(ctx, "scheduler.report_write_failed", "err", err, "path", rep.reportPath())
		return
	}
	slog.InfoContext(ctx, "scheduler.report_written", "path", rep.reportPath(), "stages", len(rep.stages))
}

// nextTick returns the next daily dispatch time: the market close cutoff plus the
// configured offset, rolled forward to the next trading day.
func (s *Scheduler) nextTick(from time.Time) time.Time {
	clk := s.cfg.Clock
	loc := clk.Loc
	if loc == nil {
		loc = time.UTC
	}
	local := from.In(loc)
	target := time.Date(local.Year(), local.Month(), local.Day(), clk.CutoffHour, 0, 0, 0, loc).
		Add(s.cfg.closeOffset())
	if !local.Before(target) {
		target = target.AddDate(0, 0, 1)
	}
	// Roll forward over non-trading days.
	for !clk.IsTradingDay(target) {
		target = target.AddDate(0, 0, 1)
	}
	return target
}

// tick runs one daily dispatch: EOD → drift, plus rebalance when due. It is
// skipped entirely on a non-trading day (defensive — nextTick already lands on a
// trading day, but a catch-up or clock skew could call this off-day).
func (s *Scheduler) tick(ctx context.Context, now time.Time, rep *RunReport) {
	if !s.cfg.Clock.IsTradingDay(now) {
		slog.InfoContext(ctx, "scheduler.tick_skipped_non_trading_day",
			"date", now.In(s.cfg.Clock.Loc).Format("2006-01-02"))
		return
	}
	day := s.cfg.Clock.SettlementDate(now).Format("2006-01-02")
	slog.InfoContext(ctx, "scheduler.tick", "trading_day", day)

	rebalanceDue := s.cfg.EnableRebalance && s.rebalanceDue(now)

	// EOD first. A rebalance day forces an EOD even if the EOD cadence is off, so
	// the proposal is built on fresh data. Guard against a same-day double-run so
	// catch-up (which may have just run today's EOD) and this tick don't run it
	// twice within one RunOnce invocation.
	if (s.cfg.EnableEOD || rebalanceDue) && s.state.lastRun(CadenceEOD) != day {
		s.runCadence(ctx, CadenceEOD, day, s.runner.RunEOD, rep)
	}
	// Drift after EOD (fresh cache). Depends on today's EOD being done, not on it
	// running in this pass — so it still runs when EOD was completed by catch-up.
	if s.cfg.EnableDrift && s.state.lastRun(CadenceDrift) != day {
		s.runCadence(ctx, CadenceDrift, day, s.runner.RunDrift, rep)
	}
	// Rebalance last.
	if rebalanceDue {
		s.runCadence(ctx, CadenceRebalance, day, s.runner.RunRebalance, rep)
	}
}

// runCadence executes one cadence, times it, records its last-run day, logs the
// outcome, and appends its result to the run report. A cadence failure is logged
// and swallowed so one failing cadence never stops the loop or blocks the others;
// the failure is still recorded in the report (which drives the FAILED summary)
// and tracked in state so a persistent (or auth) failure raises an alert rather
// than failing silently forever.
func (s *Scheduler) runCadence(ctx context.Context, c Cadence, day string, fn func(context.Context) (StageResult, error), rep *RunReport) {
	slog.InfoContext(ctx, "scheduler.cadence_started", "cadence", string(c), "trading_day", day)
	start := time.Now()
	res, err := fn(ctx)
	dur := time.Since(start)
	rep.record(c, res, dur, err)
	if err != nil {
		count := s.state.recordFailure(c)
		slog.ErrorContext(ctx, "scheduler.cadence_failed",
			"cadence", string(c), "err", err, "ms", dur.Milliseconds(), "consecutive_failures", count)
		s.maybeAlertFailure(ctx, c, err, count)
		_ = SaveState(s.state)
		return
	}
	s.state.markRun(c, day)
	s.state.recordSuccess(c)
	_ = SaveState(s.state)
	slog.InfoContext(ctx, "scheduler.cadence_completed", "cadence", string(c), "trading_day", day, "ms", dur.Milliseconds())
}

// maybeAlertFailure dispatches a persistent-failure / re-auth alert when warranted
// and not already sent for the current streak. An auth error (ErrReauthRequired)
// alerts on its first occurrence — it never self-heals, so waiting is pointless.
// Any other failure alerts once its consecutive-failure count reaches the
// configured threshold. Alerting is best-effort: a send failure is logged and
// swallowed so it never fails the run.
func (s *Scheduler) maybeAlertFailure(ctx context.Context, c Cadence, cadErr error, count int) {
	if s.state.alreadyAlerted(c) {
		return // one alert per streak; already notified
	}
	authIssue := errors.Is(cadErr, schwab.ErrReauthRequired)
	if !authIssue && count < s.cfg.failureAlertAfter() {
		return // transient failure, below threshold — keep retrying quietly
	}

	title, body, level := s.failureAlertMessage(c, cadErr, count, authIssue)
	msg := alert.Alert{Title: title, Body: body, Level: level}

	alerters := daemon.BuildAlerters(s.cfg.Alert)
	if len(alerters) == 0 {
		slog.WarnContext(ctx, "scheduler.failure_alert_unconfigured",
			"cadence", string(c), "auth", authIssue, "consecutive_failures", count,
			"hint", "no alert channels configured (pipeline.yaml alerts.channels); failure is log-only")
		// Still mark alerted so we don't spin on this every day; the log carries it.
		s.state.markAlerted(c)
		return
	}
	for _, a := range alerters {
		if err := a.Send(msg); err != nil {
			slog.WarnContext(ctx, "scheduler.failure_alert_send_failed", "cadence", string(c), "err", err)
		}
	}
	slog.InfoContext(ctx, "scheduler.failure_alert_sent",
		"cadence", string(c), "auth", authIssue, "consecutive_failures", count)
	s.state.markAlerted(c)
}

// failureAlertMessage builds the alert payload for a cadence failure, giving the
// auth case an explicit, actionable title (re-run `mycase auth`).
func (s *Scheduler) failureAlertMessage(c Cadence, cadErr error, count int, authIssue bool) (title, body, level string) {
	if authIssue {
		return "mycase: re-authentication required",
			fmt.Sprintf("The %s cadence failed because the broker session expired.\n"+
				"Automated runs are stalled until you re-authenticate.\n\n"+
				"Fix: run `mycase auth --broker schwab` on the host.\n\nError: %v", c, cadErr),
			"critical"
	}
	return fmt.Sprintf("mycase: %s cadence failing (%d consecutive)", c, count),
		fmt.Sprintf("The %s cadence has failed %d times in a row.\n"+
			"It will keep retrying, but check the host — the data/scheduler.log has details.\n\nLast error: %v",
			c, count, cadErr),
		"warn"
}

// catchUp runs a missed EOD if the machine was asleep at the last close: if the
// most recent settled trading day has no recorded EOD run, dispatch one now.
func (s *Scheduler) catchUp(ctx context.Context, rep *RunReport) {
	if !s.cfg.EnableEOD {
		return
	}
	now := time.Now()
	lastSettled := s.cfg.Clock.SettlementDate(now).Format("2006-01-02")
	if s.state.lastRun(CadenceEOD) == lastSettled {
		return // already have the latest EOD
	}
	if !s.cfg.Clock.IsTradingDay(now) && s.state.lastRun(CadenceEOD) != "" {
		// Off-day with some prior run — nothing new to catch up.
		return
	}
	slog.InfoContext(ctx, "scheduler.catchup_eod",
		"missed_for", lastSettled, "last_run", s.state.lastRun(CadenceEOD))
	s.runCadence(ctx, CadenceEOD, lastSettled, s.runner.RunEOD, rep)
}

// rebalanceDue reports whether today matches the configured rebalance schedule
// and a rebalance has not already run today.
func (s *Scheduler) rebalanceDue(now time.Time) bool {
	day := s.cfg.Clock.SettlementDate(now).Format("2006-01-02")
	if s.state.lastRun(CadenceRebalance) == day {
		return false
	}
	return isRebalanceDay(now, s.cfg.Pipeline.Schedule, s.cfg.Clock)
}
