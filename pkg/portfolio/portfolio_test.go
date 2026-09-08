package portfolio

import "testing"

func TestStripSeriesSuffix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"E2E-BE", "E2E"},
		{"e2e-be", "E2E"}, // Case-insensitive
		{"E2E-EQ", "E2E"},
		{"E2E-BZ", "E2E"},
		{"E2E-SM", "E2E"},
		{"E2E-ST", "E2E"},
		{"E2E-IL", "E2E"},
		{"E2E-BL", "E2E"},
		{"E2E-BT", "E2E"},
		{"E2E", "E2E"},
		{"BAJAJ-AUTO", "BAJAJ-AUTO"},         // Preserves valid hyphenated symbol
		{"BAJAJ-AUTO-BE", "BAJAJ-AUTO"},      // Hyphenated symbol with BE series
		{"TATAMOTORS-DVR", "TATAMOTORS-DVR"}, // Preserves distinct DVR security
		{"M&M", "M&M"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := StripSeriesSuffix(tt.input)
			if got != tt.expected {
				t.Errorf("StripSeriesSuffix(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCleanTicker(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"NSE:E2E-BE", "E2E"},
		{"NSE:E2E", "E2E"},
		{"BSE:E2E", "E2E"},
		{"E2E-BE", "E2E"},
		{"NSE:BAJAJ-AUTO", "BAJAJ-AUTO"},
		{"NSE:BAJAJ-AUTO-BE", "BAJAJ-AUTO"},
		{"  NSE:E2E-BE  ", "E2E"},
		{"nse:e2e-be", "E2E"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := CleanTicker(tt.input)
			if got != tt.expected {
				t.Errorf("CleanTicker(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFormatTickerWithExchange(t *testing.T) {
	if got := FormatTickerWithExchange("NSE", "E2E-BE"); got != "NSE:E2E" {
		t.Errorf("FormatTickerWithExchange(\"NSE\", \"E2E-BE\") = %q; want \"NSE:E2E\"", got)
	}
	if got := FormatTickerWithExchange("", "E2E"); got != "NSE:E2E" {
		t.Errorf("FormatTickerWithExchange(\"\", \"E2E\") = %q; want \"NSE:E2E\"", got)
	}
}
