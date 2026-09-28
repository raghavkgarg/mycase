package stockpicker

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// LoadNSEHolidays loads official exchange holidays for NSE from DuckDB.
func LoadNSEHolidays(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	if db == nil {
		return nil, fmt.Errorf("nil db connection")
	}
	rows, err := db.QueryContext(ctx, "SELECT strftime(date, '%Y-%m-%d') FROM holidays WHERE exchange = 'NSE';")
	if err != nil {
		return nil, fmt.Errorf("query nse holidays: %w", err)
	}
	defer rows.Close()

	holidays := make(map[string]bool)
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err == nil && d != "" {
			holidays[d] = true
		}
	}
	return holidays, nil
}

// ExpectedNSETradingSession walks backward from asOfDate to find the latest valid NSE session.
// It is completely independent of price data and immune to feed outages.
func ExpectedNSETradingSession(asOfDate time.Time, holidays map[string]bool) time.Time {
	curr := asOfDate
	for {
		weekday := curr.Weekday()
		dateStr := curr.Format("2006-01-02")

		// Skip weekends (Saturday = 6, Sunday = 0)
		if weekday == time.Saturday || weekday == time.Sunday {
			curr = curr.AddDate(0, 0, -1)
			continue
		}

		// Skip official exchange holidays
		if holidays != nil && holidays[dateStr] {
			curr = curr.AddDate(0, 0, -1)
			continue
		}

		// Found the expected active trading session
		return curr
	}
}

// FreshnessCheckResult contains the diagnosis of upstream market data recency.
type FreshnessCheckResult struct {
	AsOfDate        string
	ExpectedSession string
	BenchLastBar    string
	IsBenchStale    bool
	BreadthLastBar  string
	IsBreadthStale  bool
	Degraded        bool
	Reason          string
}

// CheckBenchmarkFreshness evaluates if the benchmark or breadth feeds lag the expected calendar.
func CheckBenchmarkFreshness(
	asOfDate time.Time,
	benchLastBar time.Time,
	breadthLastBar time.Time,
	holidays map[string]bool,
) FreshnessCheckResult {
	expected := ExpectedNSETradingSession(asOfDate, holidays)
	expectedStr := expected.Format("2006-01-02")
	benchStr := benchLastBar.Format("2006-01-02")

	isBenchStale := benchLastBar.Before(expected)
	var isBreadthStale bool
	var breadthStr string
	if !breadthLastBar.IsZero() {
		breadthStr = breadthLastBar.Format("2006-01-02")
		isBreadthStale = breadthLastBar.Before(expected)
	}

	degraded := isBenchStale || isBreadthStale
	var reason string
	if isBenchStale && isBreadthStale {
		reason = fmt.Sprintf("Both benchmark (%s) and breadth (%s) lag expected session (%s)", benchStr, breadthStr, expectedStr)
	} else if isBenchStale {
		reason = fmt.Sprintf("Benchmark feed (%s) lags expected session (%s)", benchStr, expectedStr)
	} else if isBreadthStale {
		reason = fmt.Sprintf("Breadth feed (%s) lags expected session (%s)", breadthStr, expectedStr)
	}

	return FreshnessCheckResult{
		AsOfDate:        asOfDate.Format("2006-01-02"),
		ExpectedSession: expectedStr,
		BenchLastBar:    benchStr,
		IsBenchStale:    isBenchStale,
		BreadthLastBar:  breadthStr,
		IsBreadthStale:  isBreadthStale,
		Degraded:        degraded,
		Reason:          reason,
	}
}
