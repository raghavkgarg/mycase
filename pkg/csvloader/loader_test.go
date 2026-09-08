package csvloader

import (
	"os"
	"strings"
	"testing"
	"testing/quick"
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

func TestGetUniverseName_AlwaysNonEmpty(t *testing.T) {
	f := func(s string) bool {
		return GetUniverseName(s) != ""
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

func TestParseBasket_Valid(t *testing.T) {
	csv := "ticker,weight\nRELIANCE,0.5\nTCS,0.5\n"
	weights, tickers, err := ParseBasket(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tickers) != 2 {
		t.Fatalf("want 2 tickers, got %d", len(tickers))
	}
	if weights["RELIANCE"] != 0.5 || weights["TCS"] != 0.5 {
		t.Errorf("unexpected weights: %v", weights)
	}
}

func TestParseBasket_CaseInsensitiveHeader(t *testing.T) {
	csv := "Ticker,Weight\nRELIANCE,0.40\n"
	weights, tickers, err := ParseBasket(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tickers) != 1 || weights["RELIANCE"] != 0.40 {
		t.Errorf("case-insensitive header failed: tickers=%v weights=%v", tickers, weights)
	}
}

func TestParseBasket_MissingHeader(t *testing.T) {
	csv := "name,value\nFoo,0.5\n"
	_, _, err := ParseBasket(strings.NewReader(csv))
	if err == nil {
		t.Error("expected error for missing ticker/weight columns")
	}
}

func TestParseBasket_EmptyBody(t *testing.T) {
	csv := "ticker,weight\n"
	weights, tickers, err := ParseBasket(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tickers) != 0 || len(weights) != 0 {
		t.Errorf("expected empty result, got tickers=%v", tickers)
	}
}

func TestParseBasket_DuplicateTicker(t *testing.T) {
	// Last occurrence wins for the weight, but ticker appears only once in the ordered slice.
	csv := "ticker,weight\nRELIANCE,0.30\nRELIANCE,0.70\n"
	weights, tickers, err := ParseBasket(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tickers) != 1 {
		t.Errorf("duplicate ticker should appear once in tickers slice, got %d", len(tickers))
	}
	if weights["RELIANCE"] != 0.70 {
		t.Errorf("last weight should win for duplicate ticker: want 0.70, got %f", weights["RELIANCE"])
	}
}

func TestParseBasket_InvalidWeight(t *testing.T) {
	csv := "ticker,weight\nRELIANCE,notanumber\n"
	_, _, err := ParseBasket(strings.NewReader(csv))
	if err == nil {
		t.Error("expected error for non-numeric weight")
	}
}

func TestCountActiveHoldings(t *testing.T) {
	// 1. Empty / header-only CSV
	f1, err := os.CreateTemp("", "holdings1_*.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f1.Name())
	f1.WriteString("ticker,weight\n")
	f1.Close()

	count1, err := CountActiveHoldings(f1.Name())
	if err != nil || count1 != 0 {
		t.Errorf("expected 0 active holdings, got %d (err: %v)", count1, err)
	}

	// 2. All zero-weight holdings (exits)
	f2, err := os.CreateTemp("", "holdings2_*.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f2.Name())
	f2.WriteString("ticker,weight\nTCS,0.0000\nINFY,0.0\n")
	f2.Close()

	count2, err := CountActiveHoldings(f2.Name())
	if err != nil || count2 != 0 {
		t.Errorf("expected 0 active holdings for exited portfolio, got %d (err: %v)", count2, err)
	}

	// 3. Mixed active and exited holdings
	f3, err := os.CreateTemp("", "holdings3_*.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f3.Name())
	f3.WriteString("ticker,weight\nTCS,0.15\nINFY,0.0\nWIPRO,0.25\n")
	f3.Close()

	count3, err := CountActiveHoldings(f3.Name())
	if err != nil || count3 != 2 {
		t.Errorf("expected 2 active holdings, got %d (err: %v)", count3, err)
	}
}
