package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/marketcal"
)

// fakeRunner records which cadences fired and in what order.
type fakeRunner struct {
	calls []Cadence
}

func (f *fakeRunner) RunEOD(context.Context) error { f.calls = append(f.calls, CadenceEOD); return nil }
func (f *fakeRunner) RunDrift(context.Context) error {
	f.calls = append(f.calls, CadenceDrift)
	return nil
}
func (f *fakeRunner) RunRebalance(context.Context) error {
	f.calls = append(f.calls, CadenceRebalance)
	return nil
}

func newTestScheduler(cfg Config, r Runner) *Scheduler {
	// Bypass LoadState (which touches the data dir) with a clean in-memory state.
	return &Scheduler{cfg: cfg, runner: r, state: State{LastRun: map[string]string{}}}
}

func baseConfig() Config {
	return Config{
		Clock:           marketcal.NYSE, // weekday logic, no holidays
		EnableEOD:       true,
		EnableDrift:     true,
		EnableRebalance: false,
		EODIndex:        "sp500",
		EODMethod:       "us_quality_momentum",
		EODTopN:         20,
	}
}

func TestTick_EODThenDrift_TradingDay(t *testing.T) {
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tz unavailable: %v", err)
	}
	// Wed Sep 16 2026, after close.
	now := time.Date(2026, 9, 16, 16, 30, 0, 0, et)
	f := &fakeRunner{}
	s := newTestScheduler(baseConfig(), f)

	s.tick(context.Background(), now)

	if len(f.calls) != 2 || f.calls[0] != CadenceEOD || f.calls[1] != CadenceDrift {
		t.Fatalf("expected [eod drift], got %v", f.calls)
	}
}

func TestTick_SkipsNonTradingDay(t *testing.T) {
	et, _ := time.LoadLocation("America/New_York")
	// Saturday Sep 12 2026.
	now := time.Date(2026, 9, 12, 16, 30, 0, 0, et)
	f := &fakeRunner{}
	s := newTestScheduler(baseConfig(), f)

	s.tick(context.Background(), now)

	if len(f.calls) != 0 {
		t.Fatalf("expected no cadences on Saturday, got %v", f.calls)
	}
}

func TestTick_SkipsHoliday(t *testing.T) {
	et, _ := time.LoadLocation("America/New_York")
	cfg := baseConfig()
	// Christmas 2026 (Fri) as a holiday on the clock.
	cfg.Clock = marketcal.NYSE.WithHolidays("2026-12-25")
	now := time.Date(2026, 12, 25, 16, 30, 0, 0, et)
	f := &fakeRunner{}
	s := newTestScheduler(cfg, f)

	s.tick(context.Background(), now)

	if len(f.calls) != 0 {
		t.Fatalf("expected no cadences on a holiday, got %v", f.calls)
	}
}

// A rebalance day forces an EOD first even when the EOD cadence is disabled, so
// the proposal is built on fresh data; order is EOD → drift → rebalance.
func TestTick_RebalanceForcesEOD(t *testing.T) {
	et, _ := time.LoadLocation("America/New_York")
	cfg := baseConfig()
	cfg.EnableEOD = false
	cfg.EnableRebalance = true
	cfg.Pipeline.Schedule = config.ScheduleConfig{Frequency: "quarterly", Day: "first_trading_day"}

	// Quarterly runs land on the 2nd of Jan/Apr/Jul/Oct. Apr 2 2026 is a Thursday.
	now := time.Date(2026, 4, 2, 16, 30, 0, 0, et)
	f := &fakeRunner{}
	s := newTestScheduler(cfg, f)

	s.tick(context.Background(), now)

	if len(f.calls) != 3 || f.calls[0] != CadenceEOD || f.calls[2] != CadenceRebalance {
		t.Fatalf("expected [eod drift rebalance] with EOD forced, got %v", f.calls)
	}
}

// nextTick lands on the post-close offset and rolls forward over the weekend.
func TestNextTick_RollsOverWeekend(t *testing.T) {
	et, _ := time.LoadLocation("America/New_York")
	s := newTestScheduler(baseConfig(), &fakeRunner{})
	// Friday Sep 11 2026 after the tick — next should be Monday Sep 14.
	from := time.Date(2026, 9, 11, 17, 0, 0, 0, et)
	next := s.nextTick(from)
	if next.Weekday() != time.Monday || next.Day() != 14 {
		t.Errorf("nextTick from Fri evening = %v, want Mon Sep 14", next)
	}
	// Default offset is 15m after the 16:00 cutoff → 16:15.
	if next.Hour() != 16 || next.Minute() != 15 {
		t.Errorf("nextTick time = %02d:%02d, want 16:15", next.Hour(), next.Minute())
	}
}

func TestRebalanceDue_NotDoubleFired(t *testing.T) {
	et, _ := time.LoadLocation("America/New_York")
	cfg := baseConfig()
	cfg.EnableRebalance = true
	cfg.Pipeline.Schedule = config.ScheduleConfig{Frequency: "quarterly", Day: "first_trading_day"}
	s := newTestScheduler(cfg, &fakeRunner{})

	now := time.Date(2026, 4, 2, 16, 30, 0, 0, et)
	if !s.rebalanceDue(now) {
		t.Fatal("expected rebalance due on Apr 2 (quarterly)")
	}
	// Mark it run today; must not fire again.
	s.state.markRun(CadenceRebalance, s.cfg.Clock.SettlementDate(now).Format("2006-01-02"))
	if s.rebalanceDue(now) {
		t.Error("rebalance should not fire twice on the same day")
	}
}

// countRunner records how many times each cadence fired (not just order), so we
// can assert the one-shot RunOnce path does not double-run EOD when catch-up and
// the same-day tick both want it.
type countRunner struct {
	eod, drift, rebalance int
}

func (c *countRunner) RunEOD(context.Context) error       { c.eod++; return nil }
func (c *countRunner) RunDrift(context.Context) error     { c.drift++; return nil }
func (c *countRunner) RunRebalance(context.Context) error { c.rebalance++; return nil }

// RunOnce on a trading day runs catch-up EOD then the day's tick, but EOD must
// fire exactly once (the tick's same-day guard suppresses the second), and drift
// still runs once after it.
func TestRunOnce_NoDoubleEOD(t *testing.T) {
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tz unavailable: %v", err)
	}
	// Freeze "now" is not injectable, but catch-up uses SettlementDate(now) and the
	// tick uses the same; on any trading day the two agree, exercising the guard.
	cfg := baseConfig()
	r := &countRunner{}
	s := newTestScheduler(cfg, r)

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// On a weekend/holiday, catch-up may still fire once for the last settled day
	// while the tick is skipped — EOD count is 1 either way; never 2.
	if r.eod > 1 {
		t.Errorf("EOD fired %d times, want at most 1 (no double-run)", r.eod)
	}
	_ = et
}

// A second RunOnce in the same process must not re-run a cadence already recorded
// for today (state guards persist within the Scheduler across calls).
func TestRunOnce_Idempotent_SamePass(t *testing.T) {
	cfg := baseConfig()
	r := &countRunner{}
	s := newTestScheduler(cfg, r)

	_ = s.RunOnce(context.Background())
	firstEOD, firstDrift := r.eod, r.drift
	_ = s.RunOnce(context.Background())

	if r.eod != firstEOD {
		t.Errorf("EOD re-ran on second RunOnce: %d -> %d", firstEOD, r.eod)
	}
	if r.drift != firstDrift {
		t.Errorf("drift re-ran on second RunOnce: %d -> %d", firstDrift, r.drift)
	}
}
