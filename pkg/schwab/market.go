package schwab

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// FetchHistoricalDataWithTimestamps retrieves daily price history for a US ticker
// and returns data in the same format as yfinance.HistoricalData.
// rangeStr supports: "1mo", "3mo", "6mo", "1y", "2y", "5y", "10y", "max"
func (c *Client) FetchHistoricalDataWithTimestamps(ctx context.Context, symbol string, rangeStr string) (*yfinance.HistoricalData, error) {
	periodType, period := mapRangeToPeriod(rangeStr)

	params := url.Values{
		"symbol":        {symbol},
		"periodType":    {periodType},
		"period":        {period},
		"frequencyType": {"daily"},
		"frequency":     {"1"},
	}

	resp, err := c.GetMarketData(ctx, "/pricehistory?"+params.Encode())
	if err != nil {
		return nil, fmt.Errorf("schwab price history for %s: %w", symbol, err)
	}

	var candles CandleList
	if err := DecodeJSON(resp, &candles); err != nil {
		return nil, err
	}

	if candles.Empty || len(candles.Candles) == 0 {
		return nil, fmt.Errorf("schwab returned no price history for %s", symbol)
	}

	hist := &yfinance.HistoricalData{
		Timestamps: make([]int64, len(candles.Candles)),
		Closes:     make([]float64, len(candles.Candles)),
		Opens:      make([]float64, len(candles.Candles)),
		Volumes:    make([]float64, len(candles.Candles)),
	}

	for i, c := range candles.Candles {
		// Schwab timestamps are Unix milliseconds; convert to seconds
		hist.Timestamps[i] = c.Datetime / 1000
		hist.Closes[i] = c.Close
		hist.Opens[i] = c.Open
		hist.Volumes[i] = c.Volume
	}

	return hist, nil
}

// FetchHistoricalByDateRange retrieves daily price history between two dates.
func (c *Client) FetchHistoricalByDateRange(ctx context.Context, symbol string, from, to time.Time) (*yfinance.HistoricalData, error) {
	params := url.Values{
		"symbol":        {symbol},
		"periodType":    {"month"},
		"frequencyType": {"daily"},
		"frequency":     {"1"},
		"startDate":     {fmt.Sprintf("%d", from.UnixMilli())},
		"endDate":       {fmt.Sprintf("%d", to.UnixMilli())},
	}

	resp, err := c.GetMarketData(ctx, "/pricehistory?"+params.Encode())
	if err != nil {
		return nil, fmt.Errorf("schwab price history %s [%s – %s]: %w", symbol, from.Format("2006-01-02"), to.Format("2006-01-02"), err)
	}

	var candles CandleList
	if err := DecodeJSON(resp, &candles); err != nil {
		return nil, err
	}

	if candles.Empty || len(candles.Candles) == 0 {
		return nil, fmt.Errorf("schwab returned no data for %s [%s – %s]", symbol, from.Format("2006-01-02"), to.Format("2006-01-02"))
	}

	hist := &yfinance.HistoricalData{
		Timestamps: make([]int64, len(candles.Candles)),
		Closes:     make([]float64, len(candles.Candles)),
		Opens:      make([]float64, len(candles.Candles)),
		Volumes:    make([]float64, len(candles.Candles)),
	}

	for i, candle := range candles.Candles {
		hist.Timestamps[i] = candle.Datetime / 1000
		hist.Closes[i] = candle.Close
		hist.Opens[i] = candle.Open
		hist.Volumes[i] = candle.Volume
	}

	return hist, nil
}

// FetchQuotes retrieves real-time quotes for multiple US symbols.
// Returns a map of "US:{symbol}" → last price.
func (c *Client) FetchQuotes(ctx context.Context, symbols []string) (map[string]float64, error) {
	if len(symbols) == 0 {
		return make(map[string]float64), nil
	}

	// Strip the US: prefix for the API call, keep it for the result map
	rawSymbols := make([]string, len(symbols))
	for i, s := range symbols {
		rawSymbols[i] = StripUSPrefix(s)
	}

	params := url.Values{
		"symbols": {strings.Join(rawSymbols, ",")},
		"fields":  {"quote"},
	}

	resp, err := c.GetMarketData(ctx, "/quotes?"+params.Encode())
	if err != nil {
		return nil, fmt.Errorf("schwab quotes: %w", err)
	}

	var quoteResp QuoteResponse
	if err := DecodeJSON(resp, &quoteResp); err != nil {
		return nil, err
	}

	prices := make(map[string]float64, len(symbols))
	for _, fullSymbol := range symbols {
		raw := StripUSPrefix(fullSymbol)
		if qd, ok := quoteResp[raw]; ok {
			price := qd.Quote.LastPrice
			if price == 0 {
				price = qd.Quote.RegularMarketLastPrice
			}
			if price == 0 {
				price = qd.Quote.Mark
			}
			prices[fullSymbol] = price
		}
	}

	return prices, nil
}

// FetchFundamentals retrieves fundamental data for US symbols and maps them
// to the existing yfinance.Fundamentals struct used by downstream code.
func (c *Client) FetchFundamentals(ctx context.Context, symbols []string) (map[string]yfinance.Fundamentals, error) {
	result := make(map[string]yfinance.Fundamentals, len(symbols))

	for _, fullSymbol := range symbols {
		raw := StripUSPrefix(fullSymbol)

		params := url.Values{
			"symbol":     {raw},
			"projection": {"fundamental"},
		}

		resp, err := c.GetMarketData(ctx, "/instruments?"+params.Encode())
		if err != nil {
			// Non-fatal: skip this ticker, continue with others
			continue
		}

		var instrResp InstrumentResponse
		if err := DecodeJSON(resp, &instrResp); err != nil {
			continue
		}

		if len(instrResp.Instruments) == 0 || instrResp.Instruments[0].Fundamental == nil {
			continue
		}

		fund := instrResp.Instruments[0].Fundamental
		result[fullSymbol] = mapSchwabFundamentals(fund)
	}

	return result, nil
}

// mapSchwabFundamentals converts Schwab's Fundamental struct to the existing
// yfinance.Fundamentals that downstream scoring/filtering code expects.
func mapSchwabFundamentals(f *Fundamental) yfinance.Fundamentals {
	return yfinance.Fundamentals{
		PEGRatio:         f.PegRatio,
		ROE:              f.ReturnOnEquity / 100.0, // Schwab returns %; yfinance uses decimal
		ForwardPE:        f.PeRatio,                // closest available (trailing PE)
		OperatingMargins: f.OperatingMarginTTM / 100.0,
		PBRatio:          f.PbRatio,
		MarketCap:        f.MarketCap * 1_000_000, // Schwab reports in millions
		AverageVolume:    f.Vol3MonthAvg,
		FreeCashflow:     f.FreeCashFlowPerShare * f.SharesOutstanding,
		DebtToEquity:     f.TotalDebtToEquity,
		TTMRevenue:       f.RevenueTTM,
		Sector:           "", // Not available from instruments endpoint
		DividendYield:    f.DivYield / 100.0,
		ReturnOnAssets:   f.ReturnOnAssets / 100.0,
		Beta:             f.Beta,
		NetProfitMargin:  f.NetProfitMarginTTM / 100.0,
		GrossMarginTTM:   f.GrossMarginTTM / 100.0,
	}
}

// StripUSPrefix removes the "US:", "NYSE:", or "NASDAQ:" prefix from a ticker.
func StripUSPrefix(ticker string) string {
	for _, prefix := range []string{"US:", "NYSE:", "NASDAQ:"} {
		if strings.HasPrefix(ticker, prefix) {
			return ticker[len(prefix):]
		}
	}
	return ticker
}

// IsUSTicker reports whether a ticker has a US market prefix.
func IsUSTicker(ticker string) bool {
	return strings.HasPrefix(ticker, "US:") ||
		strings.HasPrefix(ticker, "NASDAQ:") ||
		strings.HasPrefix(ticker, "NYSE:")
}

// mapRangeToPeriod converts a Yahoo-style range string to Schwab periodType + period.
func mapRangeToPeriod(rangeStr string) (periodType, period string) {
	switch rangeStr {
	case "1mo":
		return "month", "1"
	case "3mo":
		return "month", "3"
	case "6mo":
		return "month", "6"
	case "1y":
		return "year", "1"
	case "2y":
		return "year", "2"
	case "5y":
		return "year", "5"
	case "10y":
		return "year", "10"
	case "max", "20y":
		return "year", "20"
	default:
		// Default to 1 year
		return "year", "1"
	}
}
