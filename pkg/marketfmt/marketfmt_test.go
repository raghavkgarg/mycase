package marketfmt

import "testing"

func TestMarketFor(t *testing.T) {
	tests := []struct {
		in   string
		want Market
	}{
		{"us", US},
		{"US", US},
		{" Us ", US},
		{"india", India},
		{"", India},
		{"nse", India},
	}
	for _, tt := range tests {
		if got := MarketFor(tt.in); got != tt.want {
			t.Errorf("MarketFor(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestSymbol(t *testing.T) {
	if Symbol(US) != "$" {
		t.Errorf("Symbol(US) = %q, want $", Symbol(US))
	}
	if Symbol(India) != "₹" {
		t.Errorf("Symbol(India) = %q, want ₹", Symbol(India))
	}
}

func TestCompact(t *testing.T) {
	tests := []struct {
		name string
		v    float64
		m    Market
		want string
	}{
		// US magnitude tiers — fixed 1 decimal
		{"us trillions", 5e12, US, "$5.0T"},
		{"us billions", 5e9, US, "$5.0B"},
		{"us billions frac", 3.4e9, US, "$3.4B"},
		{"us millions", 12.3e6, US, "$12.3M"},
		{"us thousands", 950e3, US, "$950.0K"},
		{"us plain", 500, US, "$500.00"},
		{"us negative", -1.2e9, US, "-$1.2B"},
		{"us zero", 0, US, "$0.00"},

		// India magnitude tiers — fixed 2 decimals (crore = /1e7, lakh = /1e5)
		{"india big crore", 5e13, India, "₹5,000,000.00 Cr"}, // 5e13/1e7 = 5,000,000
		{"india crore", 5e7, India, "₹5.00 Cr"},              // 5e7/1e7 = 5
		{"india crore frac", 12.34e7, India, "₹12.34 Cr"},    // 12.34e7/1e7 = 12.34
		{"india band max", 5e12, India, "₹500,000.00 Cr"},    // 5e12/1e7 = 500,000
		{"india lakh", 5e5, India, "₹5.00 L"},                // 5e5/1e5 = 5
		{"india plain", 500, India, "₹500.00"},
		{"india negative", -3.4e7, India, "-₹3.40 Cr"},
	}
	for _, tt := range tests {
		if got := Compact(tt.v, tt.m); got != tt.want {
			t.Errorf("%s: Compact(%g, %v) = %q, want %q", tt.name, tt.v, tt.m, got, tt.want)
		}
	}
}

func TestCompactNum(t *testing.T) {
	if got := CompactNum(5e9, US); got != "5.0B" {
		t.Errorf("CompactNum(5e9, US) = %q, want 5.0B", got)
	}
	if got := CompactNum(5e9, India); got != "500.00 Cr" {
		t.Errorf("CompactNum(5e9, India) = %q, want 500.00 Cr", got)
	}
	// The mfs.json multibagger band: 5e9 .. 5e12
	if got := CompactNum(5e12, India); got != "500,000.00 Cr" {
		t.Errorf("CompactNum(5e12, India) = %q, want 500,000.00 Cr", got)
	}
}

func TestCurrency(t *testing.T) {
	tests := []struct {
		v    float64
		m    Market
		want string
	}{
		{1234.5, US, "$1,234.50"},
		{-500, US, "-$500.00"},
		{100000, US, "$100,000.00"},
		{1234.5, India, "₹1,234.50"},
	}
	for _, tt := range tests {
		if got := Currency(tt.v, tt.m); got != tt.want {
			t.Errorf("Currency(%g, %v) = %q, want %q", tt.v, tt.m, got, tt.want)
		}
	}
}

func TestGroupInt(t *testing.T) {
	tests := []struct{ in, want string }{
		{"1", "1"},
		{"12", "12"},
		{"123", "123"},
		{"1234", "1,234"},
		{"12345", "12,345"},
		{"1234567", "1,234,567"},
	}
	for _, tt := range tests {
		if got := groupInt(tt.in); got != tt.want {
			t.Errorf("groupInt(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
