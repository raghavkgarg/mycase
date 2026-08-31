package datafetcher

import (
	"context"
	"maps"
	"time"

	"github.com/raghavkgarg/mycase/pkg/broker/schwab"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// Router dispatches market data requests to the appropriate provider
// based on ticker prefix. US-prefixed tickers go to Schwab (if client
// is available); everything else goes through Yahoo Finance.
type Router struct {
	schwabClient *schwab.Client // nil if Schwab is not configured
}

// NewRouter creates a Router. Pass nil for schwabClient if Schwab is not configured
// (US tickers will fall back to Yahoo Finance).
func NewRouter(schwabClient *schwab.Client) *Router {
	return &Router{schwabClient: schwabClient}
}

// FetchHistoricalDataWithTimestamps fetches daily OHLCV for a ticker over a range.
// Routes US tickers to Schwab, others to Yahoo Finance.
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

	// Fetch US tickers from Schwab (or Yahoo fallback)
	if len(usTickers) > 0 {
		if r.schwabClient != nil {
			schwabPrices, err := r.schwabClient.FetchQuotes(ctx, usTickers)
			if err != nil {
				// Fallback to Yahoo for US tickers
				yfPrices, yfErr := yfinance.FetchQuotes(ctx, usTickers)
				if yfErr != nil {
					return nil, err // return original Schwab error
				}
				maps.Copy(prices, yfPrices)
			} else {
				maps.Copy(prices, schwabPrices)
			}
		} else {
			// No Schwab client — use Yahoo for US tickers too
			yfPrices, err := yfinance.FetchQuotes(ctx, usTickers)
			if err != nil {
				return nil, err
			}
			maps.Copy(prices, yfPrices)
		}
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

	// US: Schwab (or Yahoo fallback)
	if len(usTickers) > 0 {
		if r.schwabClient != nil {
			schwabFund, err := r.schwabClient.FetchFundamentals(ctx, usTickers)
			if err != nil {
				// Fallback to Yahoo
				yfFund, yfErr := yfinance.FetchFundamentals(ctx, usTickers)
				if yfErr != nil {
					return nil, err
				}
				maps.Copy(result, yfFund)
			} else {
				maps.Copy(result, schwabFund)
			}
		} else {
			yfFund, err := yfinance.FetchFundamentals(ctx, usTickers)
			if err != nil {
				return nil, err
			}
			maps.Copy(result, yfFund)
		}
	}

	return result, nil
}

// GetBenchmarkSymbol returns the appropriate benchmark for a set of tickers.
// Mixed portfolios get S&P 500 if any US tickers are present.
func (r *Router) GetBenchmarkSymbol(tickers []string) string {
	return yfinance.GetBenchmarkSymbol(tickers)
}

// Note: *Router structurally satisfies stockpicker.DataFetcher. The interface is
// defined by its consumer (stockpicker), and satisfaction is compile-checked
// where a *Router is assigned to stockpicker.Options.DataFetcher (in pkg/autopilot
// and cmd). No compile-time assert lives here so this low-level data-routing
// package does not import the high-level strategy package (R16 problem P2).
