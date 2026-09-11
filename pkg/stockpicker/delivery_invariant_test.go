package stockpicker

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/marketdata"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

func TestDeliveryDelta_CrossCallSiteConsistency(t *testing.T) {
	// Construct a synthetic 26-day delivery series
	// Days 1..20 (baseline): 25.0% delivery
	// Days 21..25 (recent):  40.0% delivery
	// Day 26: today (must be excluded by T+1 PIT lag)
	now := time.Now()
	var records []marketdata.DeliveryRecord
	for i := 25; i >= 0; i-- {
		dStr := now.AddDate(0, 0, -i).Format("2006-01-02")
		pct := 25.0
		if i >= 1 && i <= 5 {
			pct = 40.0 // Recent 5 settled days
		} else if i == 0 {
			pct = 99.0 // Unsettled today
		}
		records = append(records, marketdata.DeliveryRecord{
			Date:        dStr,
			DeliveryPct: pct,
		})
	}

	ticker := "NSE:TESTCOIL"
	fund := yfinance.Fundamentals{
		Sector:          "Industrials",
		DeliveryHistory: records,
		DeliveryPct:     40.0,
	}

	// 1. Canonical Calculation from metrics_delivery.go
	expectedDelta, expectedAvg5, expectedAvg20, err := yfinance.GetDeliveryDelta(fund.DeliveryHistory, now, 1)
	if err != nil {
		t.Fatalf("unexpected GetDeliveryDelta error: %v", err)
	}

	expectedVal := (40.0 - 25.0) / 100.0 // +0.1500
	if math.Abs(expectedDelta-expectedVal) > 1e-6 {
		t.Errorf("canonical delta mismatch: expected %f, got %f", expectedVal, expectedDelta)
	}
	if math.Abs(expectedAvg5-0.40) > 1e-6 || math.Abs(expectedAvg20-0.25) > 1e-6 {
		t.Errorf("window averages mismatch: avg5=%f, avg20=%f", expectedAvg5, expectedAvg20)
	}

	// 2. Pillar 4 NormScore check in scoring.go
	p4 := NormScore(expectedDelta, DeliveryDeltaBounds, 25.0, false)
	// Bounds [-0.10, +0.30]: (0.15 - (-0.10)) / (0.30 - (-0.10)) = 0.25 / 0.40 = 0.625 -> 25 * 0.625 = 15.625 pts
	if math.Abs(p4-15.625) > 1e-4 {
		t.Errorf("Pillar 4 score mismatch: expected 15.625 pts, got %f", p4)
	}

	// 3. Driver String Formatting Consistency
	driverStr := fmt.Sprintf("Deliv Delta %+.1f%% (5D: %.1f%%, 20D Base: %.1f%%)",
		expectedDelta*100.0, expectedAvg5*100.0, expectedAvg20*100.0)
	expectedDriver := "Deliv Delta +15.0% (5D: 40.0%, 20D Base: 25.0%)"
	if driverStr != expectedDriver {
		t.Errorf("driver string mismatch: expected '%s', got '%s'", expectedDriver, driverStr)
	}

	_ = ticker
}
