package yfinance

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/marketdata"
)

func TestCalculateDeliveryDelta_ExactWorkedExample(t *testing.T) {
	// Construct 25 daily records:
	// Days 1..20 (baseline): 30.0% delivery
	// Days 21..25 (recent):  45.0% delivery
	var records []marketdata.DeliveryRecord

	for i := 0; i < 20; i++ {
		dStr := fmt.Sprintf("2026-08-%02d", i+1)
		records = append(records, marketdata.DeliveryRecord{
			Date:        dStr,
			DeliveryPct: 30.0,
		})
	}

	for i := 20; i < 25; i++ {
		dStr := fmt.Sprintf("2026-08-%02d", i+1)
		records = append(records, marketdata.DeliveryRecord{
			Date:        dStr,
			DeliveryPct: 45.0,
		})
	}

	asOf := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	res, err := CalculateDeliveryDelta(records, asOf, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if math.Abs(res.Avg5D-0.45) > 1e-6 {
		t.Errorf("expected Avg5D=0.45, got %f", res.Avg5D)
	}
	if math.Abs(res.Avg20D-0.30) > 1e-6 {
		t.Errorf("expected Avg20D=0.30, got %f", res.Avg20D)
	}
	if math.Abs(res.Delta5D20D-0.15) > 1e-6 {
		t.Errorf("expected Delta5D20D=+0.15, got %f", res.Delta5D20D)
	}
	if res.SettledDays != 25 {
		t.Errorf("expected SettledDays=25, got %d", res.SettledDays)
	}
}

func TestCalculateDeliveryDelta_DisjointIsolation(t *testing.T) {
	// Verify that a massive spike in recent 5 days does NOT pollute the 20-day baseline
	var records []marketdata.DeliveryRecord
	for i := 1; i <= 20; i++ {
		records = append(records, marketdata.DeliveryRecord{
			Date:        fmt.Sprintf("2026-08-%02d", i),
			DeliveryPct: 25.0,
		})
	}
	for i := 21; i <= 25; i++ {
		records = append(records, marketdata.DeliveryRecord{
			Date:        fmt.Sprintf("2026-08-%02d", i),
			DeliveryPct: 80.0, // Massive institutional block spike
		})
	}

	asOf := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	res, err := CalculateDeliveryDelta(records, asOf, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 20D baseline must strictly remain 25.0%
	if math.Abs(res.Avg20D-0.25) > 1e-6 {
		t.Errorf("disjoint violation: Avg20D contaminated by recent spike, expected 0.25, got %f", res.Avg20D)
	}
	if math.Abs(res.Avg5D-0.80) > 1e-6 {
		t.Errorf("expected Avg5D=0.80, got %f", res.Avg5D)
	}
	if math.Abs(res.Delta5D20D-0.55) > 1e-6 {
		t.Errorf("expected Delta5D20D=+0.55, got %f", res.Delta5D20D)
	}
}

func TestCalculateDeliveryDelta_InsufficientHistory(t *testing.T) {
	var records []marketdata.DeliveryRecord
	for i := 1; i <= 24; i++ {
		records = append(records, marketdata.DeliveryRecord{
			Date:        fmt.Sprintf("2026-08-%02d", i),
			DeliveryPct: 35.0,
		})
	}

	asOf := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	_, err := CalculateDeliveryDelta(records, asOf, 1)
	if err == nil {
		t.Fatalf("expected ErrInsufficientDeliveryHistory for 24 records, got nil")
	}

	delta, _, _, gErr := GetDeliveryDelta(records, asOf, 1)
	if gErr == nil {
		t.Fatalf("expected GetDeliveryDelta to return error")
	}
	if delta != 0.0 {
		t.Errorf("expected neutral fallback delta=0.0 on error, got %f", delta)
	}
}

func TestCalculateDeliveryDelta_PITLagEnforced(t *testing.T) {
	// 26 records up to 2026-08-26
	var records []marketdata.DeliveryRecord
	for i := 1; i <= 26; i++ {
		records = append(records, marketdata.DeliveryRecord{
			Date:        fmt.Sprintf("2026-08-%02d", i),
			DeliveryPct: 30.0,
		})
	}

	// As of 2026-08-26 with lagDays=1, cutoff is 2026-08-25
	// Record 26 (2026-08-26) must be excluded!
	asOf := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	res, err := CalculateDeliveryDelta(records, asOf, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SettledDays != 25 {
		t.Errorf("PIT lag failed: expected 25 settled days (excluding day 26), got %d", res.SettledDays)
	}
}

func TestDeliveryRecord_NullJSONUnmarshalsToZero(t *testing.T) {
	// Pin the Go JSON behavior that the <= 0 filter on line 40 of metrics_delivery.go
	// relies on: a JSON null for a float64 field deserializes to 0.0 (the zero value),
	// not an error or NaN. If someone refactors DeliveryRecord.DeliveryPct to *float64,
	// this test will fail, signaling that the filter needs updating.
	var r marketdata.DeliveryRecord
	raw := []byte(`{"date":"2026-09-10","delivery_pct":null}`)
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if r.DeliveryPct != 0.0 {
		t.Errorf("expected null to unmarshal to 0.0, got %f", r.DeliveryPct)
	}
	if r.Date != "2026-09-10" {
		t.Errorf("expected Date='2026-09-10', got '%s'", r.Date)
	}
}

func TestCalculateDeliveryDelta_ZeroDeliveryRecordsExcluded(t *testing.T) {
	// Simulate the RUBICON failure mode: an established stock has 30 days of valid history,
	// but 5 recent days had fetch failures that produced DeliveryPct=0.0 (from JSON null
	// or genuine fetch failure). These zero-value records must be excluded from the window,
	// not averaged in as "0% delivery."
	var records []marketdata.DeliveryRecord

	// 30 valid days of 40% delivery
	for i := 1; i <= 30; i++ {
		records = append(records, marketdata.DeliveryRecord{
			Date:        fmt.Sprintf("2026-08-%02d", i),
			DeliveryPct: 40.0,
		})
	}

	// 5 fetch-failure days with DeliveryPct=0.0 (the RUBICON pattern)
	for i := 1; i <= 5; i++ {
		records = append(records, marketdata.DeliveryRecord{
			Date:        fmt.Sprintf("2026-09-%02d", i),
			DeliveryPct: 0.0, // Simulates null → 0.0 or genuine fetch failure
		})
	}

	asOf := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	res, err := CalculateDeliveryDelta(records, asOf, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The 5 zero-value records must be stripped entirely. The window should be
	// built exclusively from the 30 valid 40% records, yielding Avg5D=0.40, Avg20D=0.40, Delta=0.0.
	if math.Abs(res.Avg5D-0.40) > 1e-6 {
		t.Errorf("zero-delivery contamination: expected Avg5D=0.40 (from valid records only), got %f", res.Avg5D)
	}
	if math.Abs(res.Avg20D-0.40) > 1e-6 {
		t.Errorf("zero-delivery contamination: expected Avg20D=0.40 (from valid records only), got %f", res.Avg20D)
	}
	if math.Abs(res.Delta5D20D) > 1e-6 {
		t.Errorf("expected Delta=0.0 (identical windows from valid data), got %f", res.Delta5D20D)
	}
	if res.SettledDays != 30 {
		t.Errorf("expected 30 settled days (5 zero-value excluded), got %d", res.SettledDays)
	}
}
