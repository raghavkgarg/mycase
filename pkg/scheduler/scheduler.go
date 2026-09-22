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
	"time"

	"log/slog"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/config"
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
}

// closeOffset returns the configured post-close offset, defaulting to 15 minutes.
func (c Config) closeOffset() time.Duration {
	if c.CloseOffsetMin <= 0 {
		return 15 * time.Minute
	}
	return time.Duration(c.CloseOffsetMin) * time.Minute
}

// Runner executes a single cadence. Split out so the tick loop is testable with
// fakes and the real dispatch (eod.Run / daemon.RunCheck / autopilot.Run) lives
// in dispatch.go.
type Runner interface {
	RunEOD(ctx context.Context) error
	RunDrift(ctx context.Context) error
	RunRebalance(ctx context.Context) error
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

	s.catchUp(ctx)

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

		s.tick(ctx, time.Now())
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
	slog.InfoContext(ctx, "scheduler.tick_once_started",
		"eod", s.cfg.EnableEOD, "drift", s.cfg.EnableDrift, "rebalance", s.cfg.EnableRebalance,
		"auto_execute", s.cfg.AutoExecute, "live", s.cfg.Live)

	s.catchUp(ctx)
	s.tick(ctx, time.Now())

	slog.InfoContext(ctx, "scheduler.tick_once_completed")
	return nil
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
func (s *Scheduler) tick(ctx context.Context, now time.Time) {
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
		s.runCadence(ctx, CadenceEOD, day, s.runner.RunEOD)
	}
	// Drift after EOD (fresh cache). Depends on today's EOD being done, not on it
	// running in this pass — so it still runs when EOD was completed by catch-up.
	if s.cfg.EnableDrift && s.state.lastRun(CadenceDrift) != day {
		s.runCadence(ctx, CadenceDrift, day, s.runner.RunDrift)
	}
	// Rebalance last.
	if rebalanceDue {
		s.runCadence(ctx, CadenceRebalance, day, s.runner.RunRebalance)
	}
}

// runCadence executes one cadence, records its last-run day, and logs the outcome.
// A cadence failure is logged and swallowed so one failing cadence never stops the
// loop or blocks the others.
func (s *Scheduler) runCadence(ctx context.Context, c Cadence, day string, fn func(context.Context) error) {
	slog.InfoContext(ctx, "scheduler.cadence_started", "cadence", string(c), "trading_day", day)
	if err := fn(ctx); err != nil {
		slog.ErrorContext(ctx, "scheduler.cadence_failed", "cadence", string(c), "err", err)
		return
	}
	s.state.markRun(c, day)
	_ = SaveState(s.state)
	slog.InfoContext(ctx, "scheduler.cadence_completed", "cadence", string(c), "trading_day", day)
}

// catchUp runs a missed EOD if the machine was asleep at the last close: if the
// most recent settled trading day has no recorded EOD run, dispatch one now.
func (s *Scheduler) catchUp(ctx context.Context) {
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
	s.runCadence(ctx, CadenceEOD, lastSettled, s.runner.RunEOD)
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
