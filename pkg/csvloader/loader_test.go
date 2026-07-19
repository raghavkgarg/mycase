package csvloader

import (
	"testing"
)

func TestGetUniverseName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"data/microsmall.csv", "microsmall"},
		{"data/candidates/proposals/20260719_microsmall_balanced_optim.csv", "microsmall"},
		{"data/candidates/index_picks/smallcap250_balanced.csv", "smallcap250"},
		{"combine_microsmall.csv", "microsmall"},
		{"stockpicker_smallcap250_balanced.csv", "smallcap250"},
		{"bk_microsmall_20260719_150405.csv", "microsmall"},
		{"data/backups/microsmall/bk_20260719_150405.csv", "portfolio"},
		{"portfolio.csv", "portfolio"},
		{"basket.csv", "portfolio"},
		{"/path/to/some-random-index.csv", "some_random_index"},
	}

	for _, tc := range tests {
		got := GetUniverseName(tc.input)
		if got != tc.expected {
			t.Errorf("GetUniverseName(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}
}
