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

	// When run at 10 AM on Sept 22, today's file will be available at 21:00 on Sept 22
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
