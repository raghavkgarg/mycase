// Package marketfmt provides market-aware formatting of monetary amounts and
// magnitudes for the two markets the system trades: the US (dollars, Western
// K/M/B/T abbreviation) and India (rupees, lakh/crore abbreviation).
//
// It is the presentation sibling of marketcal (market-time math): a pure,
// zero-import leaf (stdlib only). It deliberately does NOT depend on
// pkg/broker, so it defines its own Market type rather than taking a
// broker.MarketConfig. Callers map their market string via MarketFor.
//
// Design notes:
//   - Compact/CompactNum apply magnitude abbreviation (the common case in
//     filter diagnostics, reports, and bands): US -> $5.0B, India -> ₹5,000 Cr.
//   - Currency renders a plain amount with a symbol and Western thousands
//     grouping (no abbreviation). Indian 2,2,3 grouping is intentionally NOT
//     implemented yet — abbreviated output covers the large-value cases and the
//     project favours minimal complexity; add it only when a display needs it.
package marketfmt

import (
	"math"
	"strconv"
	"strings"
)

// Market identifies the market whose conventions to format for. The zero value
// is India, matching broker.MarketConfigForName's default.
type Market int

const (
	// India uses the rupee (₹) and the lakh/crore magnitude system.
	India Market = iota
	// US uses the dollar ($) and the K/M/B/T magnitude system.
	US
)

// MarketFor maps the app's market string ("us" / "india", case-insensitive) to
// a Market. Anything that is not "us" maps to India (the default market).
func MarketFor(s string) Market {
	if strings.EqualFold(strings.TrimSpace(s), "us") {
		return US
	}
	return India
}

// Symbol returns the currency symbol for the market.
func Symbol(m Market) string {
	if m == US {
		return "$"
	}
	return "₹"
}

// Compact formats a monetary amount with the market's currency symbol and a
// magnitude suffix. Abbreviated values carry fixed precision per market (US: 1
// decimal, India: 2), which keeps columns aligned and avoids ambiguity:
//
//	US:    $5.0B, $12.3M, $950.0K, $500.00, -$1.2B
//	India: ₹5.00 Cr, ₹12.34 Cr, ₹5.00 L, ₹500.00, -₹3.40 Cr
func Compact(v float64, m Market) string {
	if math.Signbit(v) && v != 0 {
		return "-" + Symbol(m) + compactMagnitude(math.Abs(v), m)
	}
	return Symbol(m) + compactMagnitude(math.Abs(v), m)
}

// CompactNum formats a magnitude WITHOUT a currency symbol — for counts, share
// volumes, and band labels where the unit is implied:
//
//	US: 5.0B, 950.0K / India: 5.00 Cr, 500,000.00 Cr
func CompactNum(v float64, m Market) string {
	if math.Signbit(v) && v != 0 {
		return "-" + compactMagnitude(math.Abs(v), m)
	}
	return compactMagnitude(math.Abs(v), m)
}

// Currency formats a plain amount with the market's symbol and Western
// thousands grouping, without magnitude abbreviation:
//
//	US: $1,234.50, -$500.00 / India: ₹1,234.50
func Currency(v float64, m Market) string {
	neg := math.Signbit(v)
	abs := math.Abs(v)
	s := groupWestern(abs, 2)
	if neg {
		return "-" + Symbol(m) + s
	}
	return Symbol(m) + s
}

// compactMagnitude renders a non-negative magnitude with the market's suffix,
// no currency symbol and no sign (callers add both). Abbreviated values use
// fixed precision per market (US: 1 decimal, India: 2); sub-threshold values
// use 2-decimal Western-grouped plain form.
func compactMagnitude(abs float64, m Market) string {
	var scale float64
	var suffix string
	decimals := 2
	if m == US {
		decimals = 1
		switch {
		case abs >= 1e12:
			scale, suffix = 1e12, "T"
		case abs >= 1e9:
			scale, suffix = 1e9, "B"
		case abs >= 1e6:
			scale, suffix = 1e6, "M"
		case abs >= 1e3:
			scale, suffix = 1e3, "K"
		}
	} else {
		switch {
		case abs >= 1e7:
			scale, suffix = 1e7, " Cr"
		case abs >= 1e5:
			scale, suffix = 1e5, " L"
		}
	}

	if scale == 0 {
		// Below the smallest abbreviation threshold: plain amount, 2 decimals,
		// Western grouping (e.g. "500.00", "1,234.50").
		return groupWestern(abs, 2)
	}
	return groupWestern(abs/scale, decimals) + suffix
}

// groupWestern formats a non-negative float with the given number of decimals
// and Western (3-digit) thousands grouping: 1234567.5 -> "1,234,567.50".
func groupWestern(v float64, decimals int) string {
	s := strconv.FormatFloat(v, 'f', decimals, 64)

	intPart := s
	fracPart := ""
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		intPart = s[:dot]
		fracPart = s[dot:] // includes the leading '.'
	}

	intPart = groupInt(intPart)
	return intPart + fracPart
}

// groupInt inserts commas every 3 digits from the right in a plain digit string.
func groupInt(digits string) string {
	n := len(digits)
	if n <= 3 {
		return digits
	}
	var b strings.Builder
	// Leading group may be 1-3 digits.
	lead := n % 3
	if lead == 0 {
		lead = 3
	}
	b.WriteString(digits[:lead])
	for i := lead; i < n; i += 3 {
		b.WriteByte(',')
		b.WriteString(digits[i : i+3])
	}
	return b.String()
}
