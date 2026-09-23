package marketdata

import (
	"testing"
	"time"
)

func TestEODSettlementDate(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+30*60)

	tests := []struct {
		name     string
		input    time.Time
		wantDate string
	}{
		{
			name:     "Sept 21 21:05 (After 21:00 cutoff) -> Sept 21",
			input:    time.Date(2026, 9, 21, 21, 5, 0, 0, ist),
			wantDate: "2026-09-21",
		},
		{
			name:     "Sept 22 10:00 (Before 21:00 cutoff) -> Sept 21",
			input:    time.Date(2026, 9, 22, 10, 0, 0, 0, ist),
			wantDate: "2026-09-21",
		},
		{
			name:     "Sept 22 20:59 (Just before 21:00 cutoff) -> Sept 21",
			input:    time.Date(2026, 9, 22, 20, 59, 59, 0, ist),
			wantDate: "2026-09-21",
		},
		{
			name:     "Sept 22 21:00 (Exactly at 21:00 cutoff) -> Sept 22",
			input:    time.Date(2026, 9, 22, 21, 0, 0, 0, ist),
			wantDate: "2026-09-22",
		},
		{
			name:     "Saturday Sept 12 10:00 (Weekend) -> Friday Sept 11",
			input:    time.Date(2026, 9, 12, 10, 0, 0, 0, ist),
			wantDate: "2026-09-11",
		},
		{
			name:     "Saturday Sept 12 22:00 (Weekend evening) -> Friday Sept 11",
			input:    time.Date(2026, 9, 12, 22, 0, 0, 0, ist),
			wantDate: "2026-09-11",
		},
		{
			name:     "Sunday Sept 13 14:00 (Weekend) -> Friday Sept 11",
			input:    time.Date(2026, 9, 13, 14, 0, 0, 0, ist),
			wantDate: "2026-09-11",
		},
		{
			// NOTE: package-level EODSettlementDate uses the bare (holiday-unaware)
			// marketcal.NSE by design — this L0 leaf cannot read config/holidays.json.
			// Holiday-aware settlement is provided at a higher layer via
			// broker.TradingClock() (injected holidays). So Sept 14, though an NSE
			// holiday, settles to itself here after the 21:00 cutoff. See the
			// HolidayProvider item in the roadmap for making this injectable.
			name:     "Monday Sept 14 10:00 (before 21:00 cutoff) -> Friday Sept 11",
			input:    time.Date(2026, 9, 14, 10, 0, 0, 0, ist),
			wantDate: "2026-09-11",
		},
		{
			name:     "Monday Sept 14 21:05 (after 21:00 cutoff; bare clock, not holiday-aware) -> Monday Sept 14",
			input:    time.Date(2026, 9, 14, 21, 5, 0, 0, ist),
			wantDate: "2026-09-14",
		},
		{
			name:     "Tuesday Sept 15 14:00 (before 21:00 cutoff) -> Monday Sept 14",
			input:    time.Date(2026, 9, 15, 14, 0, 0, 0, ist),
			wantDate: "2026-09-14",
		},
		{
			name:     "Tuesday Sept 15 21:05 (after 21:00 cutoff) -> Tuesday Sept 15",
			input:    time.Date(2026, 9, 15, 21, 5, 0, 0, ist),
			wantDate: "2026-09-15",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EODSettlementDate(tc.input)
			if got.Format("2006-01-02") != tc.wantDate {
				t.Errorf("EODSettlementDate(%v) = %s, want %s", tc.input, got.Format("2006-01-02"), tc.wantDate)
			}
		})
	}
}

func TestNextEODAvailableDate(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+30*60)

	// When run at 10 AM on Sept 22 (trading day), today's file will be available at 21:00 on Sept 22
	t1 := time.Date(2026, 9, 22, 10, 0, 0, 0, ist)
	got1 := NextEODAvailableDate(t1)
	if got1.Format("2006-01-02 15:04") != "2026-09-22 21:00" {
		t.Errorf("NextEODAvailableDate(Sept 22 10AM) = %s, want 2026-09-22 21:00", got1.Format("2006-01-02 15:04"))
	}

	// When run at 21:15 on Sept 22, next file will be available at 21:00 on Sept 23
	t2 := time.Date(2026, 9, 22, 21, 15, 0, 0, ist)
	got2 := NextEODAvailableDate(t2)
	if got2.Format("2006-01-02 15:04") != "2026-09-23 21:00" {
		t.Errorf("NextEODAvailableDate(Sept 22 21:15) = %s, want 2026-09-23 21:00", got2.Format("2006-01-02 15:04"))
	}

	// Holiday-aware cases: with the bare (holiday-unaware) marketcal.NSE these land
	// on Monday Sept 14; once holidays are injectable (see HolidayProvider in the
	// roadmap) they would skip the Sept 14 NSE holiday to Tuesday Sept 15.
	tFriPost := time.Date(2026, 9, 11, 21, 15, 0, 0, ist)
	gotFriPost := NextEODAvailableDate(tFriPost)
	if gotFriPost.Format("2006-01-02 15:04") != "2026-09-14 21:00" {
		t.Errorf("NextEODAvailableDate(Friday 21:15) = %s, want 2026-09-14 21:00 (bare clock)", gotFriPost.Format("2006-01-02 15:04"))
	}

	tSatHol := time.Date(2026, 9, 12, 10, 0, 0, 0, ist)
	gotSatHol := NextEODAvailableDate(tSatHol)
	if gotSatHol.Format("2006-01-02 15:04") != "2026-09-14 21:00" {
		t.Errorf("NextEODAvailableDate(Saturday Sept 12 10AM) = %s, want 2026-09-14 21:00 (bare clock)", gotSatHol.Format("2006-01-02 15:04"))
	}

	// Normal weekend test: Saturday Sept 19 -> Monday Sept 21 21:00
	tSatNormal := time.Date(2026, 9, 19, 10, 0, 0, 0, ist)
	gotSatNormal := NextEODAvailableDate(tSatNormal)
	if gotSatNormal.Format("2006-01-02 15:04") != "2026-09-21 21:00" {
		t.Errorf("NextEODAvailableDate(Saturday Sept 19 10AM) = %s, want 2026-09-21 21:00", gotSatNormal.Format("2006-01-02 15:04"))
	}

	// Normal weekend test: Sunday Sept 20 -> Monday Sept 21 21:00
	tSunNormal := time.Date(2026, 9, 20, 14, 0, 0, 0, ist)
	gotSunNormal := NextEODAvailableDate(tSunNormal)
	if gotSunNormal.Format("2006-01-02 15:04") != "2026-09-21 21:00" {
		t.Errorf("NextEODAvailableDate(Sunday Sept 20 2PM) = %s, want 2026-09-21 21:00", gotSunNormal.Format("2006-01-02 15:04"))
	}
}

func TestIsFreshEOD(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+30*60)

	nowSat := time.Date(2026, 9, 12, 10, 0, 0, 0, ist)
	friSettled := time.Date(2026, 9, 11, 21, 15, 0, 0, ist)
	friPreSettled := time.Date(2026, 9, 11, 15, 0, 0, 0, ist)

	if !IsFreshEOD(friSettled, nowSat) {
		t.Errorf("expected Friday 21:15 fetch to be fresh on Saturday morning")
	}
	if IsFreshEOD(friPreSettled, nowSat) {
		t.Errorf("expected Friday 15:00 fetch to be stale on Saturday (prior to 21:00 EOD settlement)")
	}

	// On Monday morning (Holiday), Friday fetch remains fresh
	nowMonPre := time.Date(2026, 9, 14, 10, 0, 0, 0, ist)
	if !IsFreshEOD(friSettled, nowMonPre) {
		t.Errorf("expected Friday 21:15 fetch to remain fresh on Monday morning")
	}

	// On Monday evening: with the bare (holiday-unaware) clock, Monday Sept 14
	// 21:05 is treated as settled, so the Friday fetch is now stale. Once holidays
	// are injectable (HolidayProvider — see roadmap), Sept 14 would be recognized
	// as an NSE holiday and the Friday fetch would remain fresh.
	nowMonPost := time.Date(2026, 9, 14, 21, 5, 0, 0, ist)
	if IsFreshEOD(friSettled, nowMonPost) {
		t.Errorf("expected Friday fetch to be stale on Monday 21:05 (bare clock treats it as settled)")
	}

	// On Tuesday evening after 21:00 settlement, Tuesday settled, so Friday fetch is now stale
	nowTuePost := time.Date(2026, 9, 15, 21, 5, 0, 0, ist)
	if IsFreshEOD(friSettled, nowTuePost) {
		t.Errorf("expected Friday fetch to be stale on Tuesday after 21:00 settlement")
	}
	tueSettled := time.Date(2026, 9, 15, 21, 2, 0, 0, ist)
	if !IsFreshEOD(tueSettled, nowTuePost) {
		t.Errorf("expected Tuesday 21:02 fetch to be fresh on Tuesday after 21:00 settlement")
	}
}

func TestFormatOrdinalDay(t *testing.T) {
	tests := []struct {
		day  int
		want string
	}{
		{1, "1st"},
		{2, "2nd"},
		{3, "3rd"},
		{4, "4th"},
		{21, "21st"},
		{22, "22nd"},
		{23, "23rd"},
		{30, "30th"},
		{31, "31st"},
	}

	for _, tc := range tests {
		if got := FormatOrdinalDay(tc.day); got != tc.want {
			t.Errorf("FormatOrdinalDay(%d) = %s, want %s", tc.day, got, tc.want)
		}
	}
}
