package market

import (
	"math"
	"testing"
	"time"
)

func TestIsMarketOpen(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", int(5.5*3600))
	}

	tests := []struct {
		name     string
		t        time.Time
		expected bool
	}{
		{
			name:     "Weekday within trading hours (Wednesday 11:30 AM IST)",
			t:        time.Date(2026, time.September, 23, 11, 30, 0, 0, loc),
			expected: true,
		},
		{
			name:     "Weekday at market open exact (Wednesday 09:15 AM IST)",
			t:        time.Date(2026, time.September, 23, 9, 15, 0, 0, loc),
			expected: true,
		},
		{
			name:     "Weekday at market close exact (Wednesday 03:30 PM IST)",
			t:        time.Date(2026, time.September, 23, 15, 30, 0, 0, loc),
			expected: true,
		},
		{
			name:     "Weekday before market open (Wednesday 09:14 AM IST)",
			t:        time.Date(2026, time.September, 23, 9, 14, 0, 0, loc),
			expected: false,
		},
		{
			name:     "Weekday after market close (Wednesday 03:31 PM IST)",
			t:        time.Date(2026, time.September, 23, 15, 31, 0, 0, loc),
			expected: false,
		},
		{
			name:     "Weekend Saturday at 11:00 AM IST",
			t:        time.Date(2026, time.September, 26, 11, 0, 0, 0, loc),
			expected: false,
		},
		{
			name:     "Weekend Sunday at 02:00 PM IST",
			t:        time.Date(2026, time.September, 27, 14, 0, 0, 0, loc),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsMarketOpen(tc.t)
			if got != tc.expected {
				t.Errorf("IsMarketOpen(%v) = %v; want %v", tc.t, got, tc.expected)
			}
		})
	}
}

func TestCheckMarketHours(t *testing.T) {
	// CheckMarketHours prints status and returns IsMarketOpen(time.Now())
	res := CheckMarketHours()
	expected := IsMarketOpen(time.Now())
	if res != expected {
		t.Errorf("CheckMarketHours() = %v; want %v", res, expected)
	}
}

func TestCalculateGTTParams(t *testing.T) {
	ltp := 1000.0

	// BUY order: trigger = 1000 * 1.003 = 1003.0, limit = 1000 + 2.0 = 1002.0
	triggerBuy, limitBuy := CalculateGTTParams(ltp, "BUY")
	if math.Abs(triggerBuy-1003.0) > 0.01 {
		t.Errorf("expected triggerBuy 1003.0, got %.2f", triggerBuy)
	}
	if math.Abs(limitBuy-1002.0) > 0.01 {
		t.Errorf("expected limitBuy 1002.0, got %.2f", limitBuy)
	}

	// SELL order: trigger = 1000 * 0.997 = 997.0, limit = 1000 - 2.0 = 998.0
	triggerSell, limitSell := CalculateGTTParams(ltp, "SELL")
	if math.Abs(triggerSell-997.0) > 0.01 {
		t.Errorf("expected triggerSell 997.0, got %.2f", triggerSell)
	}
	if math.Abs(limitSell-998.0) > 0.01 {
		t.Errorf("expected limitSell 998.0, got %.2f", limitSell)
	}
}

func TestCalculateBufferedLimitPrice(t *testing.T) {
	ltp := 500.0
	// bufferPrice = 500 * 1.03 = 515.0
	limitPrice := CalculateBufferedLimitPrice(ltp)
	if math.Abs(limitPrice-515.0) > 0.01 {
		t.Errorf("expected 515.0, got %.2f", limitPrice)
	}

	// Rounding check: 123.45 * 1.03 = 127.1535 -> 127.2
	ltp2 := 123.45
	limitPrice2 := CalculateBufferedLimitPrice(ltp2)
	expected2 := math.Round(123.45*1.03*10.0) / 10.0
	if math.Abs(limitPrice2-expected2) > 0.01 {
		t.Errorf("expected %.2f, got %.2f", expected2, limitPrice2)
	}
}
