package stockpicker

import (
	"testing"
	"time"
)

func TestExpectedNSETradingSession_CalendarHolidays(t *testing.T) {
	holidays := map[string]bool{
		"2026-09-14": true, // Ganesh Chaturthi
	}

	// 1. A normal Friday session (2026-09-11)
	fri, _ := time.Parse("2006-01-02", "2026-09-11")
	expFri := ExpectedNSETradingSession(fri, holidays)
	if expFri.Format("2006-01-02") != "2026-09-11" {
		t.Fatalf("expected Friday 2026-09-11, got %s", expFri.Format("2006-01-02"))
	}

	// 2. A weekend day: Sunday (2026-09-13) -> should roll back to Friday (2026-09-11)
	sun, _ := time.Parse("2006-01-02", "2026-09-13")
	expSun := ExpectedNSETradingSession(sun, holidays)
	if expSun.Format("2006-01-02") != "2026-09-11" {
		t.Fatalf("expected Friday 2026-09-11 for Sunday input, got %s", expSun.Format("2006-01-02"))
	}

	// 3. Holiday Monday (2026-09-14) -> should roll back to Friday (2026-09-11)
	monHoliday, _ := time.Parse("2006-01-02", "2026-09-14")
	expMon := ExpectedNSETradingSession(monHoliday, holidays)
	if expMon.Format("2006-01-02") != "2026-09-11" {
		t.Fatalf("expected Friday 2026-09-11 for Holiday Monday, got %s", expMon.Format("2006-01-02"))
	}

	// 4. Tuesday (2026-09-15) -> active trading session
	tue, _ := time.Parse("2006-01-02", "2026-09-15")
	expTue := ExpectedNSETradingSession(tue, holidays)
	if expTue.Format("2006-01-02") != "2026-09-15" {
		t.Fatalf("expected Tuesday 2026-09-15, got %s", expTue.Format("2006-01-02"))
	}
}

func TestCheckBenchmarkFreshness_StaleBenchmarkDetection(t *testing.T) {
	holidays := map[string]bool{
		"2026-09-14": true,
	}

	asOf, _ := time.Parse("2006-01-02", "2026-09-15") // Tuesday

	// Case A: Fresh benchmark (dated 2026-09-15)
	freshBench, _ := time.Parse("2006-01-02", "2026-09-15")
	resFresh := CheckBenchmarkFreshness(asOf, freshBench, time.Time{}, holidays)
	if resFresh.Degraded || resFresh.IsBenchStale {
		t.Fatalf("expected fresh benchmark on 2026-09-15, got degraded=%v", resFresh.Degraded)
	}

	// Case B: Stale benchmark (feed stopped on 2026-09-11)
	staleBench, _ := time.Parse("2006-01-02", "2026-09-11")
	resStale := CheckBenchmarkFreshness(asOf, staleBench, time.Time{}, holidays)
	if !resStale.Degraded || !resStale.IsBenchStale {
		t.Fatalf("expected stale benchmark detected on 2026-09-15 with bench bar 2026-09-11, got degraded=%v", resStale.Degraded)
	}
}
