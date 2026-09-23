package scheduler

import (
	"fmt"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/marketcal"
)

// RunPlan is the dry-run preview of a single run-now pass: what the scheduler
// would do for a given moment, without fetching, writing, or proposing anything.
type RunPlan struct {
	Now            time.Time
	SettlementDay  string   // most recent settled trading day ("2006-01-02")
	IsTradingDay   bool     // whether Now is itself a trading day
	CatchUpDay     string   // non-empty if a missed EOD would be caught up first
	Cadences       []string // cadences that would run, in order
	Skipped        []string // cadences configured off / not due, with reason
	TradingDaysBhd int      // trading days the EOD is behind (0 = current)
	Live           bool
}

// Plan computes the RunPlan for `now` without executing anything. It mirrors the
// gating logic in catchUp + tick so the preview matches what RunOnce would do.
func (s *Scheduler) Plan(now time.Time) RunPlan {
	clk := s.cfg.Clock
	settled := clk.SettlementDate(now).Format("2006-01-02")
	p := RunPlan{
		Now:            now,
		SettlementDay:  settled,
		IsTradingDay:   clk.IsTradingDay(now),
		TradingDaysBhd: TradingDaysBehind(s.state, clk, now),
		Live:           s.cfg.Live,
	}

	// Catch-up: a missed EOD for the latest settled day would run first.
	if s.cfg.EnableEOD && s.state.lastRun(CadenceEOD) != settled {
		p.CatchUpDay = settled
	}

	if !p.IsTradingDay {
		p.Skipped = append(p.Skipped,
			fmt.Sprintf("all cadences (today %s is not a trading day)", now.In(clkLoc(clk)).Format("2006-01-02")))
		// Catch-up may still run even on a non-trading day if EOD is stale.
		if p.CatchUpDay != "" {
			p.Cadences = append(p.Cadences, "eod (catch-up for "+p.CatchUpDay+")")
		}
		return p
	}

	rebalanceDue := s.cfg.EnableRebalance && s.rebalanceDue(now)

	// EOD.
	switch {
	case s.state.lastRun(CadenceEOD) == settled && p.CatchUpDay == "":
		p.Skipped = append(p.Skipped, "eod (already ran for "+settled+")")
	case s.cfg.EnableEOD || rebalanceDue:
		p.Cadences = append(p.Cadences, "eod")
	default:
		p.Skipped = append(p.Skipped, "eod (disabled)")
	}

	// Drift.
	switch {
	case !s.cfg.EnableDrift:
		p.Skipped = append(p.Skipped, "drift (disabled)")
	case s.state.lastRun(CadenceDrift) == settled:
		p.Skipped = append(p.Skipped, "drift (already ran for "+settled+")")
	default:
		p.Cadences = append(p.Cadences, "drift")
	}

	// Rebalance.
	switch {
	case !s.cfg.EnableRebalance:
		p.Skipped = append(p.Skipped, "rebalance (disabled)")
	case rebalanceDue:
		p.Cadences = append(p.Cadences, "rebalance")
	default:
		p.Skipped = append(p.Skipped, "rebalance (not a scheduled rebalance day)")
	}

	return p
}

// Render formats the plan for the CLI dry-run output.
func (p RunPlan) Render() string {
	var b strings.Builder
	mode := "LIVE (real broker + data)"
	if !p.Live {
		mode = "MOCK broker"
	}
	fmt.Fprintf(&b, "Dry run — nothing fetched, written, or proposed.\n")
	fmt.Fprintf(&b, "  As of:          %s\n", p.Now.Format("2006-01-02 15:04 MST"))
	fmt.Fprintf(&b, "  Trading day:    %t (latest settled: %s)\n", p.IsTradingDay, p.SettlementDay)
	fmt.Fprintf(&b, "  Broker mode:    %s\n", mode)
	if p.TradingDaysBhd > 0 {
		fmt.Fprintf(&b, "  EOD behind:     %d trading day(s)\n", p.TradingDaysBhd)
	} else {
		fmt.Fprintf(&b, "  EOD behind:     up to date\n")
	}
	if len(p.Cadences) == 0 {
		fmt.Fprintf(&b, "\nWould run: (nothing)\n")
	} else {
		fmt.Fprintf(&b, "\nWould run, in order:\n")
		for i, c := range p.Cadences {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, c)
		}
	}
	if len(p.Skipped) > 0 {
		fmt.Fprintf(&b, "\nSkipped:\n")
		for _, s := range p.Skipped {
			fmt.Fprintf(&b, "  - %s\n", s)
		}
	}
	return b.String()
}

// TradingDaysBehind reports how many trading days the EOD cadence is behind the
// most recent settled trading day. 0 means current (or no prior run to compare on
// a fresh install where lastRun is empty and today is not yet settled). It counts
// trading days (weekends/holidays excluded) strictly after the last recorded EOD
// day, up to and including the latest settled day.
func TradingDaysBehind(state State, clk marketcal.Clock, now time.Time) int {
	last := state.lastRun(CadenceEOD)
	settled := clk.SettlementDate(now)
	settledStr := settled.Format("2006-01-02")
	if last == "" {
		// Never run: "behind" only matters once there's a settled day to have run
		// for. Report 1 so a fresh install nudges the first run, but never negative.
		return 1
	}
	if last >= settledStr {
		return 0
	}
	lastT, err := time.ParseInLocation("2006-01-02", last, clkLoc(clk))
	if err != nil {
		return 0
	}
	count := 0
	for d := lastT.AddDate(0, 0, 1); !d.After(settled); d = d.AddDate(0, 0, 1) {
		if clk.IsTradingDay(d) {
			count++
		}
	}
	return count
}

func clkLoc(clk marketcal.Clock) *time.Location {
	if clk.Loc == nil {
		return time.UTC
	}
	return clk.Loc
}
