package yfinance

import (
	"context"
	"sort"
	"testing"
	"time"
)

func TestScreenerEarningsDates(t *testing.T) {
	ctx := context.Background()
	tickers := []string{"NSE:CHENNPETRO", "NSE:NETWEB", "NSE:CCL", "NSE:NAVINFLUOR", "NSE:RELIANCE"}

	for _, sym := range tickers {
		dates, err := FetchScreenerEarningsDates(ctx, sym)
		if err != nil {
			t.Errorf("Error fetching %s: %v", sym, err)
			continue
		}

		dateMap := make(map[string]time.Time)
		for _, d := range dates {
			dateMap[d.Format("2006-01-02")] = d
		}
		var sorted []time.Time
		for _, d := range dateMap {
			sorted = append(sorted, d)
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Before(sorted[j])
		})

		prevComing := FormatPrevComingResultDates(sorted)
		t.Logf("%-14s | Screener Result Prev -> Coming: %s", sym, prevComing)
	}
}
