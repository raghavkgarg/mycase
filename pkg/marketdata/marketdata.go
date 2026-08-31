// Package marketdata holds the broker/provider-agnostic market data types shared
// across the codebase: HistoricalData (daily OHLCV series), Fundamentals, and the
// small annual-metric helpers they nest.
//
// It is a leaf package with zero internal imports. Extracting these types out of
// pkg/yfinance lets type-only consumers — broker/schwab, attribution, optimizer,
// datafetcher — reference the shared shapes without importing yfinance (and thus
// transitively the DuckDB cache). This removes the inverted "a broker client
// imports the Yahoo Finance package" edge (R16 problem P1).
//
// pkg/yfinance re-exports these via type aliases (yfinance.HistoricalData =
// marketdata.HistoricalData, etc.) so existing yfinance.* call sites are unchanged.
package marketdata

import "time"

// AnnualFinancial holds annual revenue and earnings.
type AnnualFinancial struct {
	Year     int
	Revenue  float64
	Earnings float64
}

// AnnualMetric holds a historical value with its date.
type AnnualMetric struct {
	Date  string
	Value float64
}

// Fundamentals represents key fundamental metrics for a security. Populated from
// Yahoo Finance (India + US fallback) and Schwab (US-specific fields).
type Fundamentals struct {
	PEGRatio                 float64
	ROE                      float64
	ForwardPE                float64
	OperatingMargins         float64
	PBRatio                  float64
	NetDebtEBITDA            float64
	MarketCap                float64
	InsidersPercent          float64
	HeldPercentInstitutions  float64
	TTMRevenue               float64
	OperatingCashflow        float64
	FreeCashflow             float64
	AverageVolume            float64
	RegularPrice             float64
	NetIncome                float64
	Sector                   string
	EarningsHistory          []AnnualFinancial
	AnnualRevenue            []AnnualMetric
	AnnualGrossProfit        []AnnualMetric
	AnnualNetPPE             []AnnualMetric
	AnnualAccountsReceivable []AnnualMetric
	AnnualCapEx              []AnnualMetric
	DebtToEquity             float64
	TotalDebt                float64
	PledgedPercent           float64
	AnnualOperatingIncome    []AnnualMetric
	AnnualTotalAssets        []AnnualMetric
	AnnualCurrentLiabilities []AnnualMetric
	AnnualInterestExpense    []AnnualMetric
	ResultPrevComing         string

	// US-specific fields (populated from Schwab)
	DividendYield   float64 // Annual dividend yield as decimal (e.g., 0.02 = 2%)
	ReturnOnAssets  float64 // ROA as decimal
	Beta            float64 // Stock beta vs market
	NetProfitMargin float64 // Net profit margin as decimal
	GrossMarginTTM  float64 // Gross margin TTM as decimal
}

// IntradayData holds timestamp, open, and close prices for a stock.
type IntradayData struct {
	Timestamps []int64
	Opens      []float64
	Closes     []float64
}

// HistoricalData holds daily timestamp, close, open, and volume data.
type HistoricalData struct {
	Timestamps []int64
	Closes     []float64
	Opens      []float64
	Volumes    []float64
}

// CleanIntradayNoise discards the last day's price/volume from the slice if it is
// today's date during market hours (before 15:30 IST).
func (h *HistoricalData) CleanIntradayNoise() {
	if h == nil || len(h.Closes) == 0 || len(h.Timestamps) == 0 {
		return
	}
	istLoc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		istLoc = time.UTC
	}
	now := time.Now().In(istLoc)
	lastTs := time.Unix(h.Timestamps[len(h.Timestamps)-1], 0).In(istLoc)

	// If the last timestamp is today, and it is before market close (15:30)
	if lastTs.Year() == now.Year() && lastTs.YearDay() == now.YearDay() {
		// Market is open from 9:15 to 15:30.
		// If current time is before 15:30, discard the live updating day
		cutoffTime := time.Date(now.Year(), now.Month(), now.Day(), 15, 30, 0, 0, istLoc)
		if now.Before(cutoffTime) {
			// Discard the last element
			h.Closes = h.Closes[:len(h.Closes)-1]
			if len(h.Opens) > 0 {
				h.Opens = h.Opens[:len(h.Opens)-1]
			}
			if len(h.Volumes) > 0 {
				h.Volumes = h.Volumes[:len(h.Volumes)-1]
			}
			h.Timestamps = h.Timestamps[:len(h.Timestamps)-1]
		}
	}
}
