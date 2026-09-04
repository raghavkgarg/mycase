package yfinance

import (
	"context"
	"sort"
	"testing"
	"time"
)

func TestFetchNselibDeliveryData(t *testing.T) {
	ctx := context.Background()
	tickers := []string{"NSE:NETWEB", "NSE:CHENNPETRO", "NSE:RELIANCE"}

	delMap, err := FetchNselibDeliveryData(ctx, tickers)
	if err != nil {
		t.Fatalf("FetchNselibDeliveryData failed: %v", err)
	}

	for _, sym := range tickers {
		pct, ok := delMap[sym]
		if !ok {
			t.Errorf("Missing delivery percentage for %s", sym)
		} else {
			t.Logf("%-14s | Delivery %%: %.2f%%", sym, pct)
		}
	}
}

func TestNselibEarningsDates(t *testing.T) {
	ctx := context.Background()
	tickers := []string{"NSE:NETWEB", "NSE:TATAMOTORS", "NSE:RELIANCE"}

	for _, sym := range tickers {
		dates, err := FetchNselibEarningsDates(ctx, sym)
		if err != nil {
			t.Logf("Nselib warning for %s: %v", sym, err)
			continue
		}
		t.Logf("%-14s | Nselib Earnings Dates Count: %d | Dates: %v", sym, len(dates), dates)
	}
}

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

func TestFetchCustomerConcentrationData(t *testing.T) {
	ctx := context.Background()
	tickers := []string{"NSE:ARVIND", "NSE:PARKHOSPS", "NSE:NETWEB"}
	res, err := FetchCustomerConcentrationData(ctx, tickers)
	if err != nil {
		t.Fatalf("FetchCustomerConcentrationData failed: %v", err)
	}
	for tk, val := range res {
		t.Logf("%s -> %s", tk, val)
	}
}
