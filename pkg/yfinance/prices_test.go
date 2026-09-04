package yfinance

import (
	"testing"
	"time"
)

func TestMapTickerToYahoo(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"NSE:TCS", "TCS.NS"},
		{"NSE:RELIANCE", "RELIANCE.NS"},
		{"BSE:500112", "500112.BO"},
		{"BSE:LT", "LT.BO"},
		{"^NSEI", "^NSEI"},
		{"^CNXSC", "^CNXSC"},
		{"RELIANCE", "RELIANCE.NS"},
		{"TCS", "TCS.NS"},
		{" NSE:INFY ", "INFY.NS"},
	}

	for _, tc := range tests {
		got := MapTickerToYahoo(tc.input)
		if got != tc.want {
			t.Errorf("MapTickerToYahoo(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

func TestCleanIntradayNoise_Nil(t *testing.T) {
	var h *HistoricalData
	h.CleanIntradayNoise() // must not panic
}

func TestCleanIntradayNoise_Empty(t *testing.T) {
	h := &HistoricalData{}
	h.CleanIntradayNoise() // must not panic
}

func TestCleanIntradayNoise_OldTimestamp(t *testing.T) {
	// Last timestamp is yesterday → nothing should be trimmed
	yesterday := time.Now().AddDate(0, 0, -1).Unix()
	h := &HistoricalData{
		Timestamps: []int64{yesterday - 86400, yesterday},
		Closes:     []float64{100.0, 101.0},
		Opens:      []float64{99.0, 100.5},
		Volumes:    []float64{1000.0, 1100.0},
	}
	before := len(h.Closes)
	h.CleanIntradayNoise()
	if len(h.Closes) != before {
		t.Errorf("old timestamp should not be trimmed: before=%d after=%d", before, len(h.Closes))
	}
}

func TestCleanIntradayNoise_TodayAfterClose(t *testing.T) {
	istLoc, _ := time.LoadLocation("Asia/Kolkata")
	// Target date at 16:00 IST (after 15:45 settlement)
	targetDate := time.Date(2026, 8, 26, 16, 0, 0, 0, istLoc)
	barToday := time.Date(2026, 8, 26, 15, 30, 0, 0, istLoc).Unix()
	barYesterday := time.Date(2026, 8, 25, 15, 30, 0, 0, istLoc).Unix()

	h := &HistoricalData{
		Timestamps: []int64{barYesterday, barToday},
		Closes:     []float64{100.0, 105.0},
		Opens:      []float64{99.0, 101.0},
		Volumes:    []float64{1000.0, 2000.0},
	}
	h.CleanIntradayNoiseAsOf(targetDate)
	if len(h.Closes) != 2 {
		t.Fatalf("after market close (16:00 IST), confirmed EOD bar should be retained; got len %d", len(h.Closes))
	}
}

func TestCleanIntradayNoise_IntradayBeforeSettlement(t *testing.T) {
	istLoc, _ := time.LoadLocation("Asia/Kolkata")
	// Target date at 14:00 IST (intraday, before 15:45 settlement)
	targetDate := time.Date(2026, 8, 26, 14, 0, 0, 0, istLoc)
	barToday := time.Date(2026, 8, 26, 13, 59, 0, 0, istLoc).Unix()
	barYesterday := time.Date(2026, 8, 25, 15, 30, 0, 0, istLoc).Unix()

	h := &HistoricalData{
		Timestamps: []int64{barYesterday, barToday},
		Closes:     []float64{100.0, 105.0},
		Opens:      []float64{99.0, 101.0},
		Volumes:    []float64{1000.0, 2000.0},
	}
	h.CleanIntradayNoiseAsOf(targetDate)
	if len(h.Closes) != 1 {
		t.Fatalf("intraday bar before settlement (14:00 IST) must be truncated; got len %d", len(h.Closes))
	}
	if h.Closes[0] != 100.0 {
		t.Errorf("expected remaining close 100.0, got %.1f", h.Closes[0])
	}
}

func TestCleanIntradayNoise_HistoricalAsOfDate(t *testing.T) {
	istLoc, _ := time.LoadLocation("Asia/Kolkata")
	// AsOfDate set to 2026-08-20, while historical data contains bars up to 2026-08-26
	asOf := time.Date(2026, 8, 20, 16, 0, 0, 0, istLoc)
	ts1 := time.Date(2026, 8, 19, 15, 30, 0, 0, istLoc).Unix()
	ts2 := time.Date(2026, 8, 20, 15, 30, 0, 0, istLoc).Unix()
	ts3 := time.Date(2026, 8, 21, 15, 30, 0, 0, istLoc).Unix()
	ts4 := time.Date(2026, 8, 26, 15, 30, 0, 0, istLoc).Unix()

	h := &HistoricalData{
		Timestamps: []int64{ts1, ts2, ts3, ts4},
		Closes:     []float64{10.0, 20.0, 30.0, 40.0},
		Opens:      []float64{9.0, 19.0, 29.0, 39.0},
		Volumes:    []float64{100.0, 200.0, 300.0, 400.0},
	}
	h.CleanIntradayNoiseAsOf(asOf)
	if len(h.Closes) != 2 {
		t.Fatalf("historical bars after asOf date must be truncated; expected len 2, got %d", len(h.Closes))
	}
	if h.Closes[1] != 20.0 {
		t.Errorf("expected latest bar close 20.0, got %.1f", h.Closes[1])
	}
}
