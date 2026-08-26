package render

import (
	"fmt"
	"math"
	"strings"
)

// Pct formats a float as a percentage with sign and 2 decimal places.
// Examples: 0.1234 → "+12.34%", -0.041 → "-4.10%"
// The input is expected as a ratio (0.12 = 12%), not already multiplied.
func Pct(v float64) string {
	pct := v * 100
	if pct >= 0 {
		return fmt.Sprintf("+%.2f%%", pct)
	}
	return fmt.Sprintf("%.2f%%", pct)
}

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

// Change formats a value as a colored percentage — green for positive, red for negative.
// When output is not a TTY, returns the same as Pct().
func Change(v float64) string {
	s := Pct(v)
	if v >= 0 {
		return Green(s)
	}
	return Red(s)
}

// ChangeRaw is like Change but for already-multiplied percentages.
func ChangeRaw(v float64) string {
	s := PctRaw(v)
	if v >= 0 {
		return Green(s)
	}
	return Red(s)
}

// Sparkline renders a slice of floats as a Unicode sparkline.
// Uses 8 levels of block characters: ▁▂▃▄▅▆▇█
// Returns empty string for empty/nil input.
func Sparkline(values []float64) string {
	if len(values) == 0 {
		return ""
	}

	blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	minVal, maxVal := values[0], values[0]
	for _, v := range values[1:] {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	spread := maxVal - minVal
	if spread == 0 {
		// All values identical — flat line at middle height.
		return strings.Repeat(string(blocks[3]), len(values))
	}

	var sb strings.Builder
	sb.Grow(len(values) * 3) // UTF-8 block chars are 3 bytes
	for _, v := range values {
		// Normalize to [0, 1] then map to block index [0, 7].
		norm := (v - minVal) / spread
		idx := min(int(norm*7), 7)
		sb.WriteRune(blocks[idx])
	}
	return sb.String()
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
