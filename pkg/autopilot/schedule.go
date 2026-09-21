package autopilot

import (
	"context"
	"fmt"
	"time"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/marketdata"
)

// benchmarkFetcher is the minimal price surface IsTradingDay needs. Defined
// consumer-side (autopilot) so this package depends only on the leaf
// marketdata DTOs; *datafetcher.Router satisfies it structurally.
type benchmarkFetcher interface {
	FetchHistoricalDataWithTimestamps(ctx context.Context, ticker, rangeStr string) (*marketdata.HistoricalData, error)
	NormalizeBenchmarkSymbol(symbol string) string
}

// IsTradingDay reports whether today is a trading day for the active market.
//
// The authority is the holiday-aware market calendar (broker.TradingClock →
// marketcal): weekends and the exchange holidays in config/holidays.json are
// non-trading days. Before 09:00 local the market has not opened, so it returns
// false (the same early guard as before). The fetcher is an optional secondary
// probe: if the calendar says it's a trading day but a benchmark candle is
// available and its latest date is not today (e.g. an unlisted holiday the
// hand-maintained calendar missed), it defers to that live signal. A nil fetcher
// or a fetch error simply trusts the calendar.
func IsTradingDay(ctx context.Context, fetcher benchmarkFetcher) bool {
	clock := broker.TradingClock()
	now := time.Now().In(clock.Loc)

	// Before the open, the market hasn't traded yet today.
	if now.Hour() < 9 {
		return false
	}

	// Calendar is the primary authority — weekends + configured holidays.
	if !clock.IsTradingDay(now) {
		return false
	}

	// Optional live cross-check: catch a holiday the calendar didn't list.
	if fetcher == nil {
		return true
	}
	benchmark := fetcher.NormalizeBenchmarkSymbol(broker.LoadMarketConfig().Benchmark)
	data, err := fetcher.FetchHistoricalDataWithTimestamps(ctx, benchmark, "5d")
	if err != nil || data == nil || len(data.Timestamps) == 0 {
		// Can't confirm — trust the calendar.
		return true
	}
	lastTS := data.Timestamps[len(data.Timestamps)-1]
	lastDay := time.Unix(lastTS, 0).In(clock.Loc).Truncate(24 * time.Hour)
	return lastDay.Equal(now.Truncate(24 * time.Hour))
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
func ShouldRetryTomorrow(ctx context.Context, fetcher benchmarkFetcher) bool {
	return !IsTradingDay(ctx, fetcher)
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
