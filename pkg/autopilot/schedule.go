package autopilot

import (
	"context"
	"fmt"
	"time"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// IsTradingDay checks if today is a trading day by attempting to fetch
// a recent benchmark price and checking if the latest candle is from today.
func IsTradingDay(ctx context.Context) bool {
	mktCfg := broker.LoadMarketConfig()
	loc, err := time.LoadLocation(mktCfg.Timezone)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)

	// Only check during/after trading hours
	if now.Hour() < 9 {
		return false
	}

	data, err := yfinance.FetchHistoricalDataWithTimestamps(ctx, mktCfg.Benchmark, "5d")
	if err != nil || data == nil || len(data.Timestamps) == 0 {
		// If we can't check, assume it's a trading day and let the pipeline try
		return true
	}

	// Check if the latest timestamp is from today
	lastTS := data.Timestamps[len(data.Timestamps)-1]
	lastDate := time.Unix(lastTS, 0).In(loc)
	today := now.Truncate(24 * time.Hour)
	lastDay := lastDate.Truncate(24 * time.Hour)

	return lastDay.Equal(today)
}

// NextQuarterDates returns the scheduled run dates for a quarterly frequency.
// Quarter starts: Jan 2, Apr 2, Jul 2, Oct 2 (avoids Jan 1 / public holidays).
func NextQuarterDates(from time.Time) []time.Time {
	year := from.Year()
	loc := scheduleLocation()
	quarterMonths := []time.Month{time.January, time.April, time.July, time.October}

	var dates []time.Time
	for _, m := range quarterMonths {
		d := time.Date(year, m, 2, 10, 0, 0, 0, loc)
		dates = append(dates, d)
	}
	// Also include next year's Jan in case we're past Oct
	dates = append(dates, time.Date(year+1, time.January, 2, 10, 0, 0, 0, loc))

	return dates
}

// NextRunDate calculates the next scheduled autopilot run date based on config.
func NextRunDate(cfg config.ScheduleConfig) time.Time {
	loc := scheduleLocation()
	now := time.Now().In(loc)

	switch cfg.Frequency {
	case "monthly":
		return nextMonthlyDate(now, cfg.Day)
	case "quarterly":
		return nextQuarterlyDate(now, cfg.Day)
	default:
		// drift-triggered has no fixed schedule
		return time.Time{}
	}
}

func nextQuarterlyDate(now time.Time, daySpec string) time.Time {
	dates := NextQuarterDates(now)
	for _, d := range dates {
		adjusted := applyDaySpec(d, daySpec)
		if adjusted.After(now) {
			return adjusted
		}
	}
	// Shouldn't happen — NextQuarterDates includes next year
	return dates[len(dates)-1]
}

func nextMonthlyDate(now time.Time, daySpec string) time.Time {
	loc := scheduleLocation()
	// Try this month first, then next month
	for offset := 0; offset <= 1; offset++ {
		candidate := time.Date(now.Year(), now.Month()+time.Month(offset), 1, 10, 0, 0, 0, loc)
		adjusted := applyDaySpec(candidate, daySpec)
		if adjusted.After(now) {
			return adjusted
		}
	}
	// Fallback: 2 months from now
	return time.Date(now.Year(), now.Month()+2, 1, 10, 0, 0, 0, loc)
}

// applyDaySpec adjusts a date based on the day specification string.
func applyDaySpec(base time.Time, daySpec string) time.Time {
	loc := scheduleLocation()
	switch daySpec {
	case "first_trading_day":
		// Use the 2nd of the month (1st is often a holiday)
		return time.Date(base.Year(), base.Month(), 2, 10, 0, 0, 0, loc)
	case "last_trading_day":
		// Last weekday of the month
		lastDay := time.Date(base.Year(), base.Month()+1, 0, 10, 0, 0, 0, loc)
		for lastDay.Weekday() == time.Saturday || lastDay.Weekday() == time.Sunday {
			lastDay = lastDay.AddDate(0, 0, -1)
		}
		return lastDay
	default:
		// Assume it's a day number (e.g., "15")
		day := 2 // default fallback
		if _, err := fmt.Sscanf(daySpec, "%d", &day); err != nil {
			day = 2
		}
		if day < 1 || day > 28 {
			day = 2
		}
		return time.Date(base.Year(), base.Month(), day, 10, 0, 0, 0, loc)
	}
}

// scheduleLocation returns the timezone for scheduling based on market config.
func scheduleLocation() *time.Location {
	mktCfg := broker.LoadMarketConfig()
	loc, err := time.LoadLocation(mktCfg.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// ShouldRetryTomorrow determines if the autopilot should retry the next day
// (e.g., if today is not a trading day).
func ShouldRetryTomorrow(ctx context.Context) bool {
	return !IsTradingDay(ctx)
}

// LaunchdQuarterlyIntervals returns the StartCalendarInterval entries
// for quarterly autopilot scheduling (macOS launchd).
func LaunchdQuarterlyIntervals() string {
	return `<key>StartCalendarInterval</key>
<array>
	<dict>
		<key>Month</key><integer>1</integer>
		<key>Day</key><integer>2</integer>
		<key>Hour</key><integer>10</integer>
		<key>Minute</key><integer>0</integer>
	</dict>
	<dict>
		<key>Month</key><integer>4</integer>
		<key>Day</key><integer>2</integer>
		<key>Hour</key><integer>10</integer>
		<key>Minute</key><integer>0</integer>
	</dict>
	<dict>
		<key>Month</key><integer>7</integer>
		<key>Day</key><integer>2</integer>
		<key>Hour</key><integer>10</integer>
		<key>Minute</key><integer>0</integer>
	</dict>
	<dict>
		<key>Month</key><integer>10</integer>
		<key>Day</key><integer>2</integer>
		<key>Hour</key><integer>10</integer>
		<key>Minute</key><integer>0</integer>
	</dict>
</array>`
}

// LaunchdMonthlyInterval returns the StartCalendarInterval entry
// for monthly autopilot scheduling (macOS launchd).
func LaunchdMonthlyInterval(day int) string {
	if day < 1 || day > 28 {
		day = 2
	}
	return fmt.Sprintf(`<key>StartCalendarInterval</key>
<dict>
	<key>Day</key><integer>%d</integer>
	<key>Hour</key><integer>10</integer>
	<key>Minute</key><integer>0</integer>
</dict>`, day)
}
