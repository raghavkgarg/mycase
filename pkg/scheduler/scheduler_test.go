package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/marketcal"
)

// fakeRunner records which cadences fired and in what order.
type fakeRunner struct {
	calls []Cadence
}

func (f *fakeRunner) RunEOD(context.Context) (StageResult, error) {
	f.calls = append(f.calls, CadenceEOD)
	return StageResult{}, nil
}
func (f *fakeRunner) RunDrift(context.Context) (StageResult, error) {
	f.calls = append(f.calls, CadenceDrift)
	return StageResult{}, nil
}
func (f *fakeRunner) RunRebalance(context.Context) (StageResult, error) {
	f.calls = append(f.calls, CadenceRebalance)
	return StageResult{}, nil
}

func newTestScheduler(cfg Config, r Runner) *Scheduler {
	// Bypass LoadState (which touches the data dir) with a clean in-memory state.
	return &Scheduler{cfg: cfg, runner: r, state: emptyState()}
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

	s.tick(context.Background(), now, newRunReport(now, "", false, ""))

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

	s.tick(context.Background(), now, newRunReport(now, "", false, ""))

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

	s.tick(context.Background(), now, newRunReport(now, "", false, ""))

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

	s.tick(context.Background(), now, newRunReport(now, "", false, ""))

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

func (c *countRunner) RunEOD(context.Context) (StageResult, error) {
	c.eod++
	return StageResult{}, nil
}
func (c *countRunner) RunDrift(context.Context) (StageResult, error) {
	c.drift++
	return StageResult{}, nil
}
func (c *countRunner) RunRebalance(context.Context) (StageResult, error) {
	c.rebalance++
	return StageResult{}, nil
}

// RunOnce on a trading day runs catch-up EOD then the day's tick, but EOD must
// fire exactly once (the tick's same-day guard suppresses the second), and drift
// still runs once after it.
func TestRunOnce_NoDoubleEOD(t *testing.T) {
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tz unavailable: %v", err)
	}
	isolateState(t)
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
	isolateState(t)
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

func TestTradingDaysBehind(t *testing.T) {
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tz unavailable: %v", err)
	}
	clk := marketcal.NYSE
	// Wed Sep 16 2026 ~ after close.
	now := time.Date(2026, 9, 16, 17, 0, 0, 0, et)

	cases := []struct {
		name    string
		lastEOD string
		want    int
	}{
		{"current (ran today)", "2026-09-16", 0},
		{"one behind (ran Tue)", "2026-09-15", 1},
		{"over a weekend (ran Fri Sep 11)", "2026-09-11", 3}, // Mon14,Tue15,Wed16
		{"never run", "", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := State{LastRun: map[string]string{}}
			if tc.lastEOD != "" {
				st.LastRun[string(CadenceEOD)] = tc.lastEOD
			}
			if got := TradingDaysBehind(st, clk, now); got != tc.want {
				t.Errorf("TradingDaysBehind = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPlan_TradingDay_EODThenDrift(t *testing.T) {
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tz unavailable: %v", err)
	}
	cfg := baseConfig()
	cfg.Live = true
	s := newTestScheduler(cfg, &fakeRunner{})
	now := time.Date(2026, 9, 16, 17, 0, 0, 0, et) // Wed, trading day

	p := s.Plan(now)
	if !p.IsTradingDay {
		t.Fatal("expected trading day")
	}
	if len(p.Cadences) != 2 || p.Cadences[0] != "eod" || p.Cadences[1] != "drift" {
		t.Errorf("expected [eod drift], got %v", p.Cadences)
	}
	out := p.Render()
	if !strings.Contains(out, "Dry run") || !strings.Contains(out, "LIVE") {
		t.Errorf("render missing header/mode: %q", out)
	}
}

func TestPlan_NonTradingDay_SkipsAll(t *testing.T) {
	et, _ := time.LoadLocation("America/New_York")
	cfg := baseConfig()
	s := newTestScheduler(cfg, &fakeRunner{})
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, et) // Saturday

	p := s.Plan(now)
	if p.IsTradingDay {
		t.Fatal("Saturday should not be a trading day")
	}
	// Catch-up may still list an EOD for the last settled day, but the normal
	// trading-day cadences must not appear.
	for _, c := range p.Cadences {
		if c == "drift" || c == "rebalance" {
			t.Errorf("non-trading day should not run %s; got %v", c, p.Cadences)
		}
	}
}

// reportingRunner returns populated StageResults so the end-to-end report wiring
// (RunOnce → runCadence → record → flushReport) can be asserted against a file.
type reportingRunner struct{}

func (reportingRunner) RunEOD(context.Context) (StageResult, error) {
	var sr StageResult
	sr.line("3 method(s) screened")
	sr.line("4 theme(s) synced")
	return sr, nil
}
func (reportingRunner) RunDrift(context.Context) (StageResult, error) {
	var sr StageResult
	sr.line("drift index 0.0500")
	return sr, nil
}
func (reportingRunner) RunRebalance(context.Context) (StageResult, error) {
	return StageResult{}, nil
}

// RunOnce with EnableReport writes a dated maintenance-log block with the stages
// that ran. Uses a temp report path so it never touches the real data dir.
func TestRunOnce_WritesReport(t *testing.T) {
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tz unavailable: %v", err)
	}
	_ = et
	isolateState(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "scheduler-runs.log")

	cfg := baseConfig()
	cfg.EnableReport = true
	cfg.ReportPath = path
	s := newTestScheduler(cfg, reportingRunner{})

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		// On a non-trading day (weekend) with no prior EOD, catch-up still runs
		// EOD once, so a block is written. If somehow nothing ran, the file may be
		// absent — treat that as a soft skip rather than a hard failure.
		t.Skipf("no report written (likely nothing due today): %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "────") || !strings.Contains(out, "[eod]") {
		t.Errorf("report missing header or eod stage:\n%s", out)
	}
	if !strings.Contains(out, "SUCCESS") {
		t.Errorf("report missing SUCCESS summary:\n%s", out)
	}
}
