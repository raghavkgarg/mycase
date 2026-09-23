package stockpicker

import (
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/marketcal"
)

// A zero Options.Clock defaults to the bare NSE calendar (preserves the
// historical India-only behavior for callers that don't inject a clock).
func TestOptions_Clock_DefaultsToNSE(t *testing.T) {
	var o Options
	clk := o.clock()
	if clk.Loc == nil {
		t.Fatal("default clock has nil Loc")
	}
	if clk.Loc.String() != marketcal.NSE.Loc.String() {
		t.Errorf("default clock Loc = %s, want %s", clk.Loc, marketcal.NSE.Loc)
	}
	if clk.CutoffHour != marketcal.NSE.CutoffHour {
		t.Errorf("default clock cutoff = %d, want %d (NSE)", clk.CutoffHour, marketcal.NSE.CutoffHour)
	}
}

// An injected holiday-aware clock is honored, and its holiday set drives the
// settlement decision — proving the aware clock threads through as a value with
// no global. On a US market holiday, SettlementDate rolls back to the prior
// trading day rather than landing on the holiday.
func TestOptions_Clock_InjectedHolidayAware(t *testing.T) {
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tz unavailable: %v", err)
	}
	// NYSE clock with Thanksgiving 2026 (Thu Nov 26) as a holiday.
	o := Options{Clock: marketcal.NYSE.WithHolidays("2026-11-26")}
	clk := o.clock()

	if clk.CutoffHour != marketcal.NYSE.CutoffHour {
		t.Errorf("injected clock cutoff = %d, want NYSE %d", clk.CutoffHour, marketcal.NYSE.CutoffHour)
	}
	if clk.IsTradingDay(time.Date(2026, 11, 26, 12, 0, 0, 0, et)) {
		t.Error("Thanksgiving 2026 should not be a trading day on the injected clock")
	}
	// After the holiday's close, settlement is the prior trading day (Wed Nov 25),
	// not the holiday itself.
	got := clk.SettlementDate(time.Date(2026, 11, 26, 17, 0, 0, 0, et)).Format("2006-01-02")
	if got != "2026-11-25" {
		t.Errorf("settlement on Thanksgiving = %s, want 2026-11-25 (rolled back)", got)
	}
}
