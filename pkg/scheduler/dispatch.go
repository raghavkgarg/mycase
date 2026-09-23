package scheduler

import (
	"context"
	"time"

	"log/slog"

	"github.com/raghavkgarg/mycase/pkg/autopilot"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/daemon"
	"github.com/raghavkgarg/mycase/pkg/eod"
	"github.com/raghavkgarg/mycase/pkg/marketcal"
)

// defaultRunner is the production Runner: it dispatches each cadence to the real
// eod/daemon/autopilot entry points. Held by the scheduler unless a test injects
// its own Runner.
type defaultRunner struct {
	cfg Config
}

// RunEOD executes the daily EOD update via pkg/eod, using the injected market
// clock so screening/settlement timing is correct for the active market. It
// returns a StageResult summarizing the run (methods screened, integrity flags,
// themes synced) for the maintenance-log report.
func (r *defaultRunner) RunEOD(ctx context.Context) (StageResult, error) {
	var sr StageResult
	res, err := eod.Run(ctx, eod.Config{
		Clock:     r.cfg.Clock,
		IndexName: r.cfg.EODIndex,
		Method:    r.cfg.EODMethod,
		DBPath:    r.cfg.DBPath,
		TopN:      r.cfg.EODTopN,
		// Operational caller: keep the pick banner/tables out of stdout (the
		// scheduler's log channel); they go to slog at debug instead.
		QuietStdout: true,
		// Fetcher left nil: eod/stockpicker falls back to the direct path. The
		// composition root may set a router on the Config in a later refinement;
		// keeping it nil here avoids the scheduler importing datafetcher.
	})
	if res != nil {
		nMethods := len(res.Methods)
		sr.line("%d method(s) screened", nMethods)
		if res.IntegrityTotal > 0 {
			sr.line("%d/%d integrity flags", res.IntegrityFailed, res.IntegrityTotal)
		}
		sr.line("%d theme(s) synced", res.ThemesSynced)
		for _, w := range res.Warnings {
			sr.warn("%s", w)
		}
	}
	return sr, err
}

// RunDrift runs the portfolio drift check via pkg/daemon (alerts only). It
// surfaces the drift index and portfolio value into the report.
func (r *defaultRunner) RunDrift(ctx context.Context) (StageResult, error) {
	var sr StageResult
	res, err := daemon.RunCheck(ctx, r.cfg.Broker, r.cfg.Alert, r.cfg.PortfolioFile)
	if err == nil {
		sr.line("drift index %.4f", res.DriftIndex)
		sr.line("portfolio %.2f across %d holdings", res.TotalValue, len(res.BasketKeys))
	}
	return sr, err
}

// RunRebalance produces an autopilot proposal. It NEVER places orders here — the
// investor-in-the-loop rule holds: autopilot.Run builds and persists a proposal
// and the scheduler dispatches the proposal alert. Order execution stays a
// separate, explicit confirm step unless AutoExecute + Live are both set, which
// is gated and logged below. The proposal's change counts + report path are
// surfaced into the maintenance-log report.
func (r *defaultRunner) RunRebalance(ctx context.Context) (StageResult, error) {
	var sr StageResult
	res, err := autopilot.Run(ctx, autopilot.RunConfig{
		Broker:      r.cfg.Broker,
		ConfigPath:  r.cfg.ConfigPath,
		PipelineCfg: r.cfg.Pipeline,
	})
	if err != nil {
		return sr, err
	}
	if res != nil && res.Proposal != nil {
		p := res.Proposal
		sr.line("%d entries, %d exits, %d reweights",
			len(p.Entries), len(p.Exits), len(p.WeightChanges))
		sr.line("%d order(s) proposed", len(p.Orders))
		for _, tw := range p.TaxWarnings {
			sr.warn("tax: %s", tw)
		}
	}
	if res != nil {
		sr.line("report %s", res.ReportPath)
	}
	slog.InfoContext(ctx, "scheduler.rebalance_proposed",
		"report", res.ReportPath, "golden_copy", res.GoldenCopyPath)

	if r.cfg.AutoExecute && r.cfg.Live {
		// Auto-execution is the one path that would place orders. It is gated hard
		// and logged prominently. Execution itself is not wired here yet — this
		// branch exists so the (previously dead) auto_execute config has a single,
		// auditable consumer; wiring the executor call is a deliberate follow-up.
		slog.WarnContext(ctx, "scheduler.auto_execute_enabled",
			"note", "auto_execute+live set; order placement is a gated follow-up, not yet wired")
		sr.warn("auto_execute+live set; order placement is a gated follow-up, not yet wired")
	} else {
		slog.InfoContext(ctx, "scheduler.rebalance_awaiting_confirmation",
			"note", "proposal built; confirm via dashboard or autopilot confirm")
	}
	return sr, nil
}

// isRebalanceDay reports whether now falls on the configured rebalance schedule,
// delegating to autopilot.IsScheduledRunDate so the schedule semantics live in one
// place. Drift-triggered frequency has no fixed calendar day (handled by the drift
// cadence, not the calendar).
func isRebalanceDay(now time.Time, sched config.ScheduleConfig, clk marketcal.Clock) bool {
	if sched.Frequency == "" || sched.Frequency == "drift-triggered" {
		return false
	}
	loc := clk.Loc
	if loc == nil {
		loc = time.UTC
	}
	return autopilot.IsScheduledRunDate(now.In(loc), sched)
}
