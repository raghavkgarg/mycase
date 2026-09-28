package datafetcher

import (
	"context"
	"log/slog"
	"maps"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/broker/schwab"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// fundamentalsSource is the narrow capability the Router needs from an EDGAR
// client: statement-level fundamentals for a batch of US tickers, returned as
// partial marketdata.Fundamentals keyed by the input ticker. Defined here (the
// consumer) so *edgar.Client satisfies it structurally and the Router can be
// tested with a fake — consistent with the layering rule that consumers define
// their interfaces.
type fundamentalsSource interface {
	FetchFundamentals(ctx context.Context, tickers []string) (map[string]yfinance.Fundamentals, error)
}

// usPrimaryWithYahooFallback runs the standard US data path as one explicit
// ordered chain: try the primary (Schwab) first, and on any error log a single
// structured fallback event and retry the same tickers via Yahoo. If Yahoo also
// fails, the original primary error is returned (it is the more actionable one —
// Yahoo is the backstop). When no Schwab client is configured, Yahoo is the
// primary. This is the one place the "Schwab → Yahoo" degradation is expressed,
// so every batch capability (quotes, and the Schwab leg of fundamentals) reports
// it identically. `op` names the capability for the log event
// (e.g. "quotes", "fundamentals"); `primary` is nil when Schwab is unconfigured.
func usPrimaryWithYahooFallback[V any](
	ctx context.Context,
	op string,
	tickers []string,
	primary func(context.Context, []string) (map[string]V, error),
	yahoo func(context.Context, []string) (map[string]V, error),
) (map[string]V, error) {
	if primary == nil {
		slog.DebugContext(ctx, "datafetcher."+op+"_served",
			"source", "yahoo", "reason", "no_schwab_client", "count", len(tickers))
		return yahoo(ctx, tickers)
	}
	got, err := primary(ctx, tickers)
	if err != nil {
		slog.WarnContext(ctx, "datafetcher."+op+"_schwab_fallback",
			"source", "yahoo", "reason", "schwab_error", "count", len(tickers), "err", err)
		yfGot, yfErr := yahoo(ctx, tickers)
		if yfErr != nil {
			return nil, err // original Schwab error — the backstop also failed
		}
		return yfGot, nil
	}
	slog.DebugContext(ctx, "datafetcher."+op+"_served", "source", "schwab", "count", len(tickers))
	return got, nil
}

// Router dispatches market data requests to the appropriate provider
// based on ticker prefix. US-prefixed tickers go to Schwab (if client
// is available); everything else goes through Yahoo Finance.
//
// When an EDGAR source is configured (Phase 10c, opt-in), US fundamentals are
// composed from Schwab TTM ratios + EDGAR statement facts via the merger; a nil
// edgarSource leaves behavior exactly as it was before EDGAR existed.
type Router struct {
	schwabClient *schwab.Client     // nil if Schwab is not configured
	edgarSource  fundamentalsSource // nil if EDGAR is not configured/enabled
}

// NewRouter creates a Router. Pass nil for schwabClient if Schwab is not configured
// (US tickers will fall back to Yahoo Finance). EDGAR is off by default; use
// WithEDGAR to enable the composite US-fundamentals path.
func NewRouter(schwabClient *schwab.Client) *Router {
	return &Router{schwabClient: schwabClient}
}

// WithEDGAR attaches an EDGAR fundamentals source, enabling the Schwab+EDGAR
// composite for US fundamentals. Passing nil is a no-op (behavior unchanged).
// Returns the Router for chaining at construction.
func (r *Router) WithEDGAR(src fundamentalsSource) *Router {
	// Guard against a typed-nil interface wrapping a nil *edgar.Client.
	if src == nil {
		return r
	}
	r.edgarSource = src
	return r
}

// FetchHistoricalDataWithTimestamps fetches daily OHLCV for a ticker over a range.
// Routes US tickers to Schwab, others to Yahoo Finance.
//
// The single-ticker historical methods intentionally do NOT fall back to Yahoo on
// a Schwab error (unlike the batch quotes/fundamentals paths): a historical price
// series silently sourced from a different provider mid-run would splice two
// price histories with different adjustment conventions, which is worse than a
// clean failure the caller can retry. The batch paths fall back because a missing
// quote/fundamental degrades gracefully; a spliced price series does not.
func (r *Router) FetchHistoricalDataWithTimestamps(ctx context.Context, ticker string, rangeStr string) (*yfinance.HistoricalData, error) {
	if schwab.IsUSTicker(ticker) && r.schwabClient != nil {
		symbol := schwab.StripUSPrefix(ticker)
		return r.schwabClient.FetchHistoricalDataWithTimestamps(ctx, symbol, rangeStr)
	}
	return yfinance.FetchHistoricalDataWithTimestamps(ctx, ticker, rangeStr)
}

// FetchHistoricalByDateRange fetches daily OHLCV between two dates.
// Routes US tickers to Schwab, others to Yahoo Finance.
func (r *Router) FetchHistoricalByDateRange(ctx context.Context, ticker string, from, to time.Time) (*yfinance.HistoricalData, error) {
	if schwab.IsUSTicker(ticker) && r.schwabClient != nil {
		symbol := schwab.StripUSPrefix(ticker)
		return r.schwabClient.FetchHistoricalByDateRange(ctx, symbol, from, to)
	}
	return yfinance.FetchHistoricalByDateRange(ctx, ticker, from, to)
}

// FetchHistoricalPrices fetches daily close prices (no timestamps) for a ticker.
// Routes US tickers to Schwab, others to Yahoo Finance.
func (r *Router) FetchHistoricalPrices(ctx context.Context, ticker string, rangeStr string) ([]float64, error) {
	if schwab.IsUSTicker(ticker) && r.schwabClient != nil {
		symbol := schwab.StripUSPrefix(ticker)
		hist, err := r.schwabClient.FetchHistoricalDataWithTimestamps(ctx, symbol, rangeStr)
		if err != nil {
			return nil, err
		}
		return hist.Closes, nil
	}
	return yfinance.FetchHistoricalPrices(ctx, ticker, rangeStr)
}

// FetchQuotes fetches latest prices for a list of tickers.
// Splits the list by market and queries each provider.
func (r *Router) FetchQuotes(ctx context.Context, tickers []string) (map[string]float64, error) {
	var usTickers, otherTickers []string
	for _, t := range tickers {
		if schwab.IsUSTicker(t) {
			usTickers = append(usTickers, t)
		} else {
			otherTickers = append(otherTickers, t)
		}
	}

	prices := make(map[string]float64, len(tickers))

	// Fetch non-US tickers from Yahoo
	if len(otherTickers) > 0 {
		yfPrices, err := yfinance.FetchQuotes(ctx, otherTickers)
		if err != nil {
			return nil, err
		}
		maps.Copy(prices, yfPrices)
	}

	// Fetch US tickers from Schwab (or Yahoo fallback) via the shared ordered chain.
	if len(usTickers) > 0 {
		var primary func(context.Context, []string) (map[string]float64, error)
		if r.schwabClient != nil {
			primary = r.schwabClient.FetchQuotes
		}
		usPrices, err := usPrimaryWithYahooFallback(ctx, "quotes", usTickers, primary, yfinance.FetchQuotes)
		if err != nil {
			return nil, err
		}
		maps.Copy(prices, usPrices)
	}

	return prices, nil
}

// FetchFundamentals fetches fundamental data for a list of tickers.
// Routes US tickers to Schwab, others to Yahoo Finance.
func (r *Router) FetchFundamentals(ctx context.Context, tickers []string) (map[string]yfinance.Fundamentals, error) {
	var usTickers, otherTickers []string
	for _, t := range tickers {
		if schwab.IsUSTicker(t) {
			usTickers = append(usTickers, t)
		} else {
			otherTickers = append(otherTickers, t)
		}
	}

	result := make(map[string]yfinance.Fundamentals, len(tickers))

	// Non-US: Yahoo Finance
	if len(otherTickers) > 0 {
		yfFund, err := yfinance.FetchFundamentals(ctx, otherTickers)
		if err != nil {
			return nil, err
		}
		maps.Copy(result, yfFund)
	}

	// US: Schwab (or Yahoo fallback), optionally overlaid with EDGAR statements.
	// A DuckDB cache short-circuit sits in front: any US ticker with a fresh
	// cached (already-merged) blob skips BOTH the Schwab and EDGAR calls, so a
	// re-run of the same universe on the same day costs zero live US calls.
	if len(usTickers) > 0 {
		var toFetch []string
		for _, t := range usTickers {
			if f, ok := checkFundamentalsCache(ctx, t); ok {
				result[t] = f
			} else {
				toFetch = append(toFetch, t)
			}
		}
		if len(usTickers) > len(toFetch) {
			slog.DebugContext(ctx, "datafetcher.fundamentals_cache_hits",
				"cached", len(usTickers)-len(toFetch), "to_fetch", len(toFetch))
		}

		if len(toFetch) > 0 {
			// One ordered chain (Schwab → Yahoo) shared with quotes. On the Schwab
			// success path we additionally overlay EDGAR statement facts and cache
			// the merged blob; that enrichment is folded into the primary closure so
			// the fallback logic stays a single expression. EDGAR failure is
			// non-fatal inside overlayEDGARAndCache (keeps Schwab values).
			var primary func(context.Context, []string) (map[string]yfinance.Fundamentals, error)
			if r.schwabClient != nil {
				primary = func(ctx context.Context, ts []string) (map[string]yfinance.Fundamentals, error) {
					schwabFund, err := r.schwabClient.FetchFundamentals(ctx, ts)
					if err != nil {
						return nil, err
					}
					r.overlayEDGARAndCache(ctx, ts, schwabFund)
					return schwabFund, nil
				}
			}
			usFund, err := usPrimaryWithYahooFallback(ctx, "fundamentals", toFetch, primary, yfinance.FetchFundamentals)
			if err != nil {
				return nil, err
			}
			maps.Copy(result, usFund)
		}
	}

	return result, nil
}

// overlayEDGARAndCache enriches Schwab-sourced US fundamentals in place with
// EDGAR statement facts (operating cash flow, net income, annual series,
// authoritative FCF) via the field-level, non-destructive merger, then persists
// each ticker's final blob to the DuckDB cache so a re-run serves it warm and
// skips both the Schwab and EDGAR calls. A nil EDGAR source or an EDGAR fetch
// error leaves the Schwab fundamentals untouched — EDGAR is a strict
// enrichment, never a regression (fail-gracefully per the API rules) — but the
// Schwab-only values are still cached (source "schwab").
func (r *Router) overlayEDGARAndCache(ctx context.Context, usTickers []string, schwabFund map[string]yfinance.Fundamentals) {
	var edgarFund map[string]yfinance.Fundamentals
	if r.edgarSource != nil {
		ef, err := r.edgarSource.FetchFundamentals(ctx, usTickers)
		if err != nil {
			slog.WarnContext(ctx, "datafetcher.edgar_overlay_failed",
				"count", len(usTickers), "err", err, "action", "keeping_schwab")
		} else {
			edgarFund = ef
		}
	}

	merged := 0
	for ticker, base := range schwabFund {
		partial, ok := edgarFund[ticker]
		m, prov := mergeFundamentals(base, partial, true, ok)
		schwabFund[ticker] = m
		storeFundamentalsCache(ctx, ticker, m, prov)
		if ok {
			merged++
		}
	}
	slog.DebugContext(ctx, "datafetcher.fundamentals_merged_cached",
		"us_tickers", len(usTickers), "edgar_matched", merged)
}

// FetchIntradayData fetches 1-minute intraday OHLC for a ticker over a range.
// Schwab exposes no intraday endpoint in this client, so intraday always uses
// Yahoo Finance regardless of ticker prefix.
func (r *Router) FetchIntradayData(ctx context.Context, ticker string, rangeStr string) (*yfinance.IntradayData, error) {
	if schwab.IsUSTicker(ticker) {
		slog.DebugContext(ctx, "datafetcher.intraday_served",
			"source", "yahoo", "reason", "schwab_no_intraday", "ticker", ticker)
	}
	return yfinance.FetchIntradayData(ctx, ticker, rangeStr)
}

// GetBenchmarkSymbol returns the appropriate benchmark ticker for a set of
// tickers. When a Schwab client is configured and the portfolio contains US
// tickers, it returns the Schwab-fetchable "US:SPY" (SPY ETF, the honest
// "you could have bought this" baseline) so the benchmark routes through
// Schwab. Without a Schwab client it falls back to Yahoo's index symbols
// (^GSPC for US, ^NSEI otherwise).
func (r *Router) GetBenchmarkSymbol(tickers []string) string {
	for _, t := range tickers {
		if schwab.IsUSTicker(t) {
			if r.schwabClient != nil {
				return "US:SPY"
			}
			return "^GSPC"
		}
	}
	return yfinance.GetBenchmarkSymbol(tickers)
}

// NormalizeBenchmarkSymbol maps a configured benchmark symbol to the form this
// Router can fetch. A Yahoo US index symbol (^GSPC/^SPX) is upgraded to the
// Schwab-fetchable "US:SPY" when a Schwab client is present, so callers that
// read the benchmark from MarketConfig still route through Schwab. Non-US and
// already-prefixed symbols pass through unchanged.
func (r *Router) NormalizeBenchmarkSymbol(symbol string) string {
	if r.schwabClient == nil {
		return symbol
	}
	switch strings.TrimSpace(symbol) {
	case "^GSPC", "^SPX", "SPX", "$SPX":
		return "US:SPY"
	default:
		return symbol
	}
}

// Note: *Router structurally satisfies stockpicker.DataFetcher. The interface is
// defined by its consumer (stockpicker), and satisfaction is compile-checked
// where a *Router is assigned to stockpicker.Options.DataFetcher (in pkg/autopilot
// and cmd). No compile-time assert lives here so this low-level data-routing
// package does not import the high-level strategy package (R16 problem P2).
