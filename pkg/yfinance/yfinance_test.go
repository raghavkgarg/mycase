package yfinance

import (
	"testing"
	"time"
)

func TestFormatPrevComingResultDates(t *testing.T) {
	now := time.Now()
	prev := now.AddDate(0, -1, 0)
	coming := now.AddDate(0, 2, 0)

	allDates := []time.Time{prev, coming}
	got := FormatPrevComingResultDates(allDates)

	expected := prev.Format("02-01-06") + " -> " + coming.Format("02-01-06")
	if got != expected {
		t.Errorf("FormatPrevComingResultDates() = %q; want %q", got, expected)
	}
}

func TestFormatPrevComingResultDates_Empty(t *testing.T) {
	got := FormatPrevComingResultDates(nil)
	if got != "N/A -> N/A" {
		t.Errorf("FormatPrevComingResultDates(nil) = %q; want %q", got, "N/A -> N/A")
	}
}
