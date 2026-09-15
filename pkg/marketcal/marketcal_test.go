package marketcal

import (
	"testing"
	"time"
)

// istZone mirrors how the legacy marketdata tests construct IST inputs, so these
// assertions prove marketcal.NSE preserves the exact India behavior.
func istZone() *time.Location { return time.FixedZone("IST", 5*3600+30*60) }

func TestNSE_SettlementDate(t *testing.T) {
	ist := istZone()
	tests := []struct {
		name     string
		input    time.Time
		wantDate string
	}{
		{"Sept 21 21:05 (after cutoff) -> Sept 21", time.Date(2026, 9, 21, 21, 5, 0, 0, ist), "2026-09-21"},
		{"Sept 22 10:00 (before cutoff) -> Sept 21", time.Date(2026, 9, 22, 10, 0, 0, 0, ist), "2026-09-21"},
		{"Sept 22 20:59 (just before cutoff) -> Sept 21", time.Date(2026, 9, 22, 20, 59, 59, 0, ist), "2026-09-21"},
		{"Sept 22 21:00 (exactly at cutoff) -> Sept 22", time.Date(2026, 9, 22, 21, 0, 0, 0, ist), "2026-09-22"},
		{"Saturday Sept 12 10:00 -> Friday Sept 11", time.Date(2026, 9, 12, 10, 0, 0, 0, ist), "2026-09-11"},
		{"Saturday Sept 12 22:00 -> Friday Sept 11", time.Date(2026, 9, 12, 22, 0, 0, 0, ist), "2026-09-11"},
		{"Sunday Sept 13 14:00 -> Friday Sept 11", time.Date(2026, 9, 13, 14, 0, 0, 0, ist), "2026-09-11"},
		{"Monday Sept 14 10:00 (holiday before cutoff) -> Friday Sept 11", time.Date(2026, 9, 14, 10, 0, 0, 0, ist), "2026-09-11"},
		{"Monday Sept 14 21:05 (holiday after cutoff) -> Friday Sept 11", time.Date(2026, 9, 14, 21, 5, 0, 0, ist), "2026-09-11"},
		{"Tuesday Sept 15 14:00 (day after holiday before cutoff) -> Friday Sept 11", time.Date(2026, 9, 15, 14, 0, 0, 0, ist), "2026-09-11"},
		{"Tuesday Sept 15 21:05 (day after holiday after cutoff) -> Tuesday Sept 15", time.Date(2026, 9, 15, 21, 5, 0, 0, ist), "2026-09-15"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NSE.SettlementDate(tc.input).Format("2006-01-02")
			if got != tc.wantDate {
				t.Errorf("NSE.SettlementDate(%v) = %s, want %s", tc.input, got, tc.wantDate)
			}
		})
	}
}

func TestNSE_NextEODAvailable(t *testing.T) {
	ist := istZone()
	tests := []struct {
		name  string
		input time.Time
		want  string
	}{
		{"Sept 22 10:00 -> same day 21:00", time.Date(2026, 9, 22, 10, 0, 0, 0, ist), "2026-09-22 21:00"},
		{"Sept 22 21:15 -> next day 21:00", time.Date(2026, 9, 22, 21, 15, 0, 0, ist), "2026-09-23 21:00"},
		{"Friday 21:15 before holiday Monday -> Tuesday 21:00", time.Date(2026, 9, 11, 21, 15, 0, 0, ist), "2026-09-15 21:00"},
		{"Saturday 10:00 before holiday Monday -> Tuesday 21:00", time.Date(2026, 9, 12, 10, 0, 0, 0, ist), "2026-09-15 21:00"},
		{"Sunday 14:00 before holiday Monday -> Tuesday 21:00", time.Date(2026, 9, 13, 14, 0, 0, 0, ist), "2026-09-15 21:00"},
		{"Monday holiday 10:00 -> Tuesday 21:00", time.Date(2026, 9, 14, 10, 0, 0, 0, ist), "2026-09-15 21:00"},
		{"Normal Saturday 10:00 -> Monday 21:00", time.Date(2026, 9, 19, 10, 0, 0, 0, ist), "2026-09-21 21:00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NSE.NextEODAvailable(tc.input).Format("2006-01-02 15:04")
			if got != tc.want {
				t.Errorf("NSE.NextEODAvailable(%v) = %s, want %s", tc.input, got, tc.want)
			}
		})
	}
}

func TestNSE_IsFreshEOD(t *testing.T) {
	ist := istZone()
	nowSat := time.Date(2026, 9, 12, 10, 0, 0, 0, ist)
	friSettled := time.Date(2026, 9, 11, 21, 15, 0, 0, ist)
	friPreSettled := time.Date(2026, 9, 11, 15, 0, 0, 0, ist)

	if !NSE.IsFreshEOD(friSettled, nowSat) {
		t.Error("Friday 21:15 fetch should be fresh on Saturday morning")
	}
	if NSE.IsFreshEOD(friPreSettled, nowSat) {
		t.Error("Friday 15:00 fetch should be stale on Saturday (before 21:00 settlement)")
	}

	// On Monday morning (Holiday), Friday fetch remains fresh
	nowMonPre := time.Date(2026, 9, 14, 10, 0, 0, 0, ist)
	if !NSE.IsFreshEOD(friSettled, nowMonPre) {
		t.Error("Friday 21:15 fetch should remain fresh Monday morning")
	}

	// On Monday evening (Holiday), market was closed so Friday fetch STILL remains fresh
	nowMonPost := time.Date(2026, 9, 14, 21, 5, 0, 0, ist)
	if !NSE.IsFreshEOD(friSettled, nowMonPost) {
		t.Error("Friday fetch should remain fresh Monday evening because Monday was an NSE holiday")
	}

	// On Tuesday evening after 21:00 settlement, Tuesday settled, so Friday fetch is now stale
	nowTuePost := time.Date(2026, 9, 15, 21, 5, 0, 0, ist)
	if NSE.IsFreshEOD(friSettled, nowTuePost) {
		t.Error("Friday fetch should be stale Tuesday after 21:00 settlement")
	}
	tueSettled := time.Date(2026, 9, 15, 21, 2, 0, 0, ist)
	if !NSE.IsFreshEOD(tueSettled, nowTuePost) {
		t.Error("Tuesday 21:02 fetch should be fresh Tuesday after 21:00 settlement")
	}
}

func TestNYSE_SettlementDate(t *testing.T) {
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}
	tests := []struct {
		name     string
		input    time.Time
		wantDate string
	}{
		// Tuesday Sept 15 2026 is a normal weekday.
		{"Tue 16:05 ET (after close) -> same day", time.Date(2026, 9, 15, 16, 5, 0, 0, et), "2026-09-15"},
		{"Tue 10:00 ET (before close) -> Monday", time.Date(2026, 9, 15, 10, 0, 0, 0, et), "2026-09-14"},
		{"Tue 16:00 ET (exactly at close) -> same day", time.Date(2026, 9, 15, 16, 0, 0, 0, et), "2026-09-15"},
		// Weekend rollback to Friday.
		{"Saturday Sept 12 -> Friday Sept 11", time.Date(2026, 9, 12, 10, 0, 0, 0, et), "2026-09-11"},
		{"Sunday Sept 13 -> Friday Sept 11", time.Date(2026, 9, 13, 10, 0, 0, 0, et), "2026-09-11"},
		{"Monday Sept 14 09:00 (before close) -> Friday Sept 11", time.Date(2026, 9, 14, 9, 0, 0, 0, et), "2026-09-11"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NYSE.SettlementDate(tc.input).Format("2006-01-02")
			if got != tc.wantDate {
				t.Errorf("NYSE.SettlementDate(%v) = %s, want %s", tc.input, got, tc.wantDate)
			}
		})
	}
}

// TestNYSE_DSTCorrectness proves the US clock is DST-aware: the 16:00 ET cutoff
// is a real wall-clock hour in New York regardless of EST/EDT. A fixed offset
// would drift by an hour across the DST boundary; America/New_York does not.
func TestNYSE_DSTCorrectness(t *testing.T) {
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}

	// During EDT (summer): July 15 2026, a Wednesday, 17:00 ET is after close.
	summer := time.Date(2026, 7, 15, 17, 0, 0, 0, et)
	sCut := NYSE.LastSettledEOD(summer)
	if h, _, _ := sCut.Clock(); h != 16 {
		t.Errorf("summer cutoff hour = %d, want 16 (wall-clock ET)", h)
	}
	if name, off := sCut.Zone(); off != -4*3600 {
		t.Errorf("summer zone = %s offset %d, want EDT offset -14400", name, off)
	}

	// During EST (winter): Jan 15 2026, a Thursday, 17:00 ET is after close.
	winter := time.Date(2026, 1, 15, 17, 0, 0, 0, et)
	wCut := NYSE.LastSettledEOD(winter)
	if h, _, _ := wCut.Clock(); h != 16 {
		t.Errorf("winter cutoff hour = %d, want 16 (wall-clock ET)", h)
	}
	if name, off := wCut.Zone(); off != -5*3600 {
		t.Errorf("winter zone = %s offset %d, want EST offset -18000", name, off)
	}
}

func TestClockForTicker(t *testing.T) {
	tests := []struct {
		ticker string
		want   int // CutoffHour distinguishes NYSE(16) from NSE(21)
	}{
		{"US:AAPL", 16},
		{"NYSE:IBM", 16},
		{"NASDAQ:MSFT", 16},
		{"NSE:RELIANCE", 21},
		{"RELIANCE", 21},
		{"", 21},
	}
	for _, tc := range tests {
		if got := ClockForTicker(tc.ticker).CutoffHour; got != tc.want {
			t.Errorf("ClockForTicker(%q).CutoffHour = %d, want %d", tc.ticker, got, tc.want)
		}
	}
}
