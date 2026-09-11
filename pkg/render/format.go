package render

import (
	"fmt"
	"math"
	"strings"
)

// PctRaw formats an already-multiplied percentage (12.34 → "+12.34%").
func PctRaw(v float64) string {
	if v >= 0 {
		return fmt.Sprintf("+%.2f%%", v)
	}
	return fmt.Sprintf("%.2f%%", v)
}

// Currency formats a float as currency with thousands separators.
// Examples: Currency(1234.5, "$") → "$1,234.50"
//
//	Currency(-500, "₹") → "-₹500.00"
func Currency(v float64, sym string) string {
	negative := v < 0
	v = math.Abs(v)

	// Format with 2 decimal places.
	raw := fmt.Sprintf("%.2f", v)

	// Split into integer and decimal parts.
	parts := strings.SplitN(raw, ".", 2)
	intPart := parts[0]
	decPart := parts[1]

	// Add thousands separators to integer part.
	intPart = addThousandsSep(intPart)

	if negative {
		return "-" + sym + intPart + "." + decPart
	}
	return sym + intPart + "." + decPart
}

// PnL formats a signed currency amount with an explicit +/- sign, e.g.
// PnL(1234.5, "$") → "+$1,234.50", PnL(-500, "₹") → "-₹500.00", PnL(0, "$") → "$0.00".
// Unlike Currency, positive values carry a leading "+" (profit/loss convention).
func PnL(v float64, sym string) string {
	switch {
	case v > 0:
		return "+" + Currency(v, sym)
	case v < 0:
		return "-" + Currency(math.Abs(v), sym)
	default:
		return Currency(0, sym)
	}
}

// PnLPct formats an already-multiplied percentage as a signed P&L percentage:
// PnLPct(12.34) → "+12.34%", PnLPct(-4.1) → "-4.10%", PnLPct(0) → "0.00%".
// Zero carries no sign (unlike PctRaw, which emits "+0.00%").
func PnLPct(v float64) string {
	switch {
	case v > 0:
		return fmt.Sprintf("+%.2f%%", v)
	case v < 0:
		return fmt.Sprintf("-%.2f%%", math.Abs(v))
	default:
		return "0.00%"
	}
}

// addThousandsSep inserts commas into an integer string.
// "1234567" → "1,234,567"
func addThousandsSep(s string) string {
	n := len(s)
	if n <= 3 {
		return s
	}

	// Work from the right, inserting commas every 3 digits.
	var sb strings.Builder
	sb.Grow(n + (n-1)/3)

	lead := n % 3
	if lead == 0 {
		lead = 3
	}
	sb.WriteString(s[:lead])
	for i := lead; i < n; i += 3 {
		sb.WriteByte(',')
		sb.WriteString(s[i : i+3])
	}
	return sb.String()
}
