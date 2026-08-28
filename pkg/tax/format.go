package tax

import "strconv"

// fmtUSD formats a dollar amount with two decimals and no currency symbol
// (callers prepend "$"). Negative values retain their sign.
func fmtUSD(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}
