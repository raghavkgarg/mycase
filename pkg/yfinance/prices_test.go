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
	// Last timestamp is today but after 15:30 IST → nothing trimmed
	// We use a fixed past date to avoid flakiness — pick a known past weekday afternoon
	istLoc, _ := time.LoadLocation("Asia/Kolkata")
	// 2026-01-15 16:00 IST (safely after market close)
	ts := time.Date(2026, 1, 15, 16, 0, 0, 0, istLoc).Unix()
	h := &HistoricalData{
		Timestamps: []int64{ts - 86400, ts},
		Closes:     []float64{100.0, 101.0},
		Opens:      []float64{99.0, 100.0},
		Volumes:    []float64{1000.0, 1100.0},
	}
	before := len(h.Closes)
	h.CleanIntradayNoise()
	if len(h.Closes) != before {
		t.Errorf("after-close timestamp should not be trimmed: before=%d after=%d", before, len(h.Closes))
	}
}
