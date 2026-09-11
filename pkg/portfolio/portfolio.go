package portfolio

import (
	"strings"

	brokertypes "github.com/raghavkgarg/mycase/pkg/broker/types"
)

// Holding is a type alias kept for backward compatibility with printer and cmd packages.
// It aliases the leaf broker/types.Holding directly (not pkg/broker) so that portfolio
// depends only on an L0 leaf, keeping it at L1 below its L2 consumers (optimizer,
// broker/zerodha). See docs/reconcile-main.md + .kiro/steering/architecture.md.
type Holding = brokertypes.Holding

// KnownSeriesSuffixes lists standard Indian exchange series suffixes (NSE/BSE)
// appended to tradingsymbols, such as Trade-to-Trade (-BE), Z-group (-BZ), SME (-SM, -ST), etc.
var KnownSeriesSuffixes = []string{
	"-BE", "-BZ", "-EQ", "-SM", "-ST", "-BL", "-BT", "-GC", "-IL",
}

// StripSeriesSuffix removes exchange series suffixes like "-BE", "-BZ", "-EQ", "-SM"
// from a trading symbol, leaving the base equity symbol in uppercase (e.g. "E2E-BE" -> "E2E").
// It preserves hyphenated company names like "BAJAJ-AUTO".
func StripSeriesSuffix(symbol string) string {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	for _, suffix := range KnownSeriesSuffixes {
		if strings.HasSuffix(sym, suffix) {
			return sym[:len(sym)-len(suffix)]
		}
	}
	return sym
}

// CleanTicker normalizes any ticker (with or without exchange prefix like "NSE:" or "BSE:")
// and strips any series suffix, returning the uppercase base symbol (e.g. "NSE:E2E-BE" -> "E2E").
func CleanTicker(ticker string) string {
	parts := strings.Split(ticker, ":")
	sym := ticker
	if len(parts) > 1 {
		sym = parts[len(parts)-1]
	}
	return strings.ToUpper(StripSeriesSuffix(sym))
}

// FormatTickerWithExchange formats a symbol with exchange prefix, ensuring series suffix is cleaned.
func FormatTickerWithExchange(exchange, symbol string) string {
	base := CleanTicker(symbol)
	ex := strings.ToUpper(strings.TrimSpace(exchange))
	if ex == "" {
		ex = "NSE"
	}
	return ex + ":" + base
}
