package yfinance

import (
	"errors"
	"sort"
	"time"

	"github.com/raghavkgarg/mycase/pkg/marketdata"
)

var ErrInsufficientDeliveryHistory = errors.New("insufficient delivery history (< 25 settled sessions)")

// DeliveryDeltaResult holds the calculated metrics for Pillar 4 institutional accumulation.
type DeliveryDeltaResult struct {
	Delta5D20D  float64 // Recent 5D Avg - Baseline 20D Avg (as decimal, e.g. +0.125)
	Avg5D       float64 // Recent 5-day arithmetic mean delivery as decimal (e.g. 0.45 = 45%)
	Avg20D      float64 // Historical 20-day disjoint baseline arithmetic mean delivery as decimal (e.g. 0.30 = 30%)
	SettledDays int     // Total number of settled sessions evaluated
}

// CalculateDeliveryDelta computes the orthogonal institutional accumulation delta:
// Delta = Avg(Last 5 Days) - Avg(Historical Days 6-25 Baseline).
// It strictly enforces PIT lag (excluding records after asOf - lagDays) and requires at least 25 settled sessions.
func CalculateDeliveryDelta(records []marketdata.DeliveryRecord, asOf time.Time, lagDays int) (DeliveryDeltaResult, error) {
	if len(records) == 0 {
		return DeliveryDeltaResult{}, ErrInsufficientDeliveryHistory
	}

	if lagDays <= 0 {
		lagDays = 1 // Default T+1 settlement lag
	}

	// Cutoff date string in YYYY-MM-DD
	// E.g. If asOf is 2026-09-10 and lagDays is 1, max allowed settled date is 2026-09-09.
	cutoffStr := asOf.AddDate(0, 0, -lagDays).Format("2006-01-02")

	// Filter confirmed records up to cutoff and deduplicate by date.
	// The DeliveryPct <= 0 guard catches two failure modes:
	//   1. JSON null from Python (NaN/missing) → Go float64 zero-value 0.0
	//   2. Genuine fetch failures that produce 0% (the RUBICON -0.35 pattern from pre-fix data)
	// Known accepted limitation: a corrupt but plausible non-zero value (e.g., NSE returning
	// 0.5% for a day where data is actually incomplete) will pass this filter and enter the
	// rolling average. Per-stock distributional anomaly detection would catch this but is
	// disproportionate to the risk — the magnitude of such errors under 5D averaging is small.
	byDate := make(map[string]marketdata.DeliveryRecord)
	for _, r := range records {
		if r.Date == "" || r.DeliveryPct <= 0 {
			continue
		}
		if r.Date <= cutoffStr {
			byDate[r.Date] = r
		}
	}

	if len(byDate) < 25 {
		return DeliveryDeltaResult{}, ErrInsufficientDeliveryHistory
	}

	var confirmed []marketdata.DeliveryRecord
	for _, r := range byDate {
		confirmed = append(confirmed, r)
	}

	// Sort confirmed records chronologically ascending (oldest first, newest last)
	sort.Slice(confirmed, func(i, j int) bool {
		return confirmed[i].Date < confirmed[j].Date
	})

	n := len(confirmed)
	// Disjoint Window Architecture:
	// Recent 5 days: indices [n-5 : n] (days t-4 to t)
	// Baseline 20 days: indices [n-25 : n-5] (days t-24 to t-5)
	recent5 := confirmed[n-5 : n]
	baseline20 := confirmed[n-25 : n-5]

	sum5 := 0.0
	for _, r := range recent5 {
		sum5 += r.DeliveryPct
	}
	avg5 := (sum5 / 5.0) / 100.0 // Convert to decimal (e.g. 45% -> 0.45)

	sum20 := 0.0
	for _, r := range baseline20 {
		sum20 += r.DeliveryPct
	}
	avg20 := (sum20 / 20.0) / 100.0 // Convert to decimal (e.g. 30% -> 0.30)

	delta := avg5 - avg20

	return DeliveryDeltaResult{
		Delta5D20D:  delta,
		Avg5D:       avg5,
		Avg20D:      avg20,
		SettledDays: n,
	}, nil
}

// GetDeliveryDelta extracts Delta5D20D from DeliveryHistory enforcing PIT lag, or returns 0.0 (neutral delta) if history is insufficient.
func GetDeliveryDelta(records []marketdata.DeliveryRecord, asOf time.Time, lagDays int) (delta float64, avg5 float64, avg20 float64, err error) {
	dd, err := CalculateDeliveryDelta(records, asOf, lagDays)
	if err != nil {
		return 0.0, 0.0, 0.0, err
	}
	return dd.Delta5D20D, dd.Avg5D, dd.Avg20D, nil
}
