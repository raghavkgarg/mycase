// Package marketdata holds the broker/provider-agnostic market data types shared
// across the codebase: HistoricalData (daily OHLCV series), Fundamentals, and the
// small annual-metric helpers they nest.
//
// It is a low-level leaf package (L0). Its only internal dependency is the pure
// algorithmic pkg/marketcal floor (L-1), used to implement the EOD-settlement
// time helpers below; it imports nothing else internal. Extracting these types
// out of pkg/yfinance lets type-only consumers — broker/schwab, attribution,
// optimizer, datafetcher — reference the shared shapes without importing yfinance
// (and thus transitively the DuckDB cache). This removes the inverted "a broker
// client imports the Yahoo Finance package" edge (R16 problem P1).
//
// pkg/yfinance re-exports these via type aliases (yfinance.HistoricalData =
// marketdata.HistoricalData, etc.) so existing yfinance.* call sites are unchanged.
package marketdata

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/marketcal"
)

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

// DeliveryRecord represents a single day's deliverable position record from NSE.
type DeliveryRecord struct {
	Date           string  `json:"date"`
	ClosePrice     float64 `json:"close_price"`
	DeliverableQty float64 `json:"deliverable_qty"`
	DeliveryPct    float64 `json:"delivery_pct"`
	Series         string  `json:"series,omitempty"`
}

// UnmarshalJSON provides resilient unmarshaling for DeliveryRecord, handling
// numeric values, nulls, and dirty NSE strings (such as "-", " - ", or "N/A") safely.
func (d *DeliveryRecord) UnmarshalJSON(data []byte) error {
	var aux struct {
		Date           string `json:"date"`
		ClosePrice     any    `json:"close_price"`
		DeliverableQty any    `json:"deliverable_qty"`
		DeliveryPct    any    `json:"delivery_pct"`
		Series         string `json:"series"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	d.Date = aux.Date
	d.ClosePrice = parseFlexibleFloat(aux.ClosePrice)
	d.DeliverableQty = parseFlexibleFloat(aux.DeliverableQty)
	d.DeliveryPct = parseFlexibleFloat(aux.DeliveryPct)
	d.Series = aux.Series
	return nil
}

func parseFlexibleFloat(v any) float64 {
	if v == nil {
		return 0.0
	}
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		clean := strings.TrimSpace(strings.ReplaceAll(val, ",", ""))
		if clean == "" || clean == "-" || clean == "--" || clean == "N/A" || clean == "None" || clean == "null" {
			return 0.0
		}
		if f, err := strconv.ParseFloat(clean, 64); err == nil {
			return f
		}
		return 0.0
	default:
		return 0.0
	}
}

// Fundamentals represents key fundamental metrics for a security. Populated from
// Yahoo Finance (India + US fallback) and Schwab (US-specific fields).
type Fundamentals struct {
	Sector           string
	ResultPrevComing string

	DeliveryDate             string
	EarningsHistory          []AnnualFinancial
	AnnualRevenue            []AnnualMetric
	AnnualGrossProfit        []AnnualMetric
	AnnualNetPPE             []AnnualMetric
	AnnualAccountsReceivable []AnnualMetric
	AnnualCapEx              []AnnualMetric
	AnnualOperatingIncome    []AnnualMetric
	AnnualTotalAssets        []AnnualMetric
	AnnualCurrentLiabilities []AnnualMetric
	AnnualInterestExpense    []AnnualMetric
	AnnualOperatingCashFlow  []AnnualMetric
	AnnualFreeCashFlow       []AnnualMetric
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
	DebtToEquity             float64
	TotalDebt                float64
	PledgedPercent           float64

	// NSE delivery data (EBM strategy — populated from Yahoo/NSE)
	DeliveryPct     float64
	DeliverableQty  float64
	DeliveryHistory []DeliveryRecord

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
// today's date during market hours (before 15:45 IST).
func (h *HistoricalData) CleanIntradayNoise() {
	h.CleanIntradayNoiseAsOf(time.Now())
}

// CleanIntradayNoiseAsOf discards in-progress or unconfirmed bars relative to the
// asOf date and market close (15:45 IST — market close 15:30 + 15 min settlement buffer).
func (h *HistoricalData) CleanIntradayNoiseAsOf(asOf time.Time) {
	if h == nil || len(h.Closes) == 0 || len(h.Timestamps) == 0 {
		return
	}
	istLoc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		istLoc = time.FixedZone("IST", 5*3600+30*60)
	}
	asOfIST := asOf.In(istLoc)
	if asOf.IsZero() {
		asOfIST = time.Now().In(istLoc)
	}

	// 1. Truncate any bars that are strictly AFTER the asOf date
	for len(h.Timestamps) > 0 {
		lastTs := time.Unix(h.Timestamps[len(h.Timestamps)-1], 0).In(istLoc)
		if lastTs.Truncate(24 * time.Hour).After(asOfIST.Truncate(24 * time.Hour)) {
			h.truncateLast()
			continue
		}
		break
	}

	// 2. If the last bar is on the SAME day as asOfIST and before the settlement cutoff, drop it.
	if len(h.Timestamps) > 0 {
		lastTs := time.Unix(h.Timestamps[len(h.Timestamps)-1], 0).In(istLoc)
		if lastTs.Year() == asOfIST.Year() && lastTs.YearDay() == asOfIST.YearDay() {
			settlementCutoff := time.Date(asOfIST.Year(), asOfIST.Month(), asOfIST.Day(), 15, 45, 0, 0, istLoc)
			if asOfIST.Before(settlementCutoff) {
				h.truncateLast()
			}
		}
	}
}

func (h *HistoricalData) truncateLast() {
	if len(h.Closes) > 0 {
		h.Closes = h.Closes[:len(h.Closes)-1]
	}
	if len(h.Opens) > 0 {
		h.Opens = h.Opens[:len(h.Opens)-1]
	}
	if len(h.Volumes) > 0 {
		h.Volumes = h.Volumes[:len(h.Volumes)-1]
	}
	if len(h.Timestamps) > 0 {
		h.Timestamps = h.Timestamps[:len(h.Timestamps)-1]
	}
}

// The EOD-settlement time helpers below preserve the historical India (NSE)
// behavior — a 21:00 IST daily cutoff with weekend and holiday rollback — by delegating to
// pkg/marketcal.NSE. They keep their original signatures so existing call sites
// (cmd/pick, cmd/db, cmd/pit) and tests are unchanged. New, market-aware call
// sites should prefer the *ForTicker variants (or use marketcal directly), which
// pick the NSE or NYSE clock from the ticker's market prefix.

// LastSettledEODTime returns the most recent completed NSE (India) EOD settlement
// cutoff (21:00 IST) at or before t. See marketcal.Clock.LastSettledEOD.
func LastSettledEODTime(t time.Time) time.Time {
	return marketcal.NSE.LastSettledEOD(t)
}

// LastSettledEODTimeForTicker is the market-aware variant: it uses the NSE or
// NYSE settlement clock selected from the ticker's market prefix.
func LastSettledEODTimeForTicker(ticker string, t time.Time) time.Time {
	return marketcal.ClockForTicker(ticker).LastSettledEOD(t)
}

// IsFreshEOD reports whether fetchedAt is at or after the last NSE (India) settled
// EOD cutoff relative to now. See marketcal.Clock.IsFreshEOD.
func IsFreshEOD(fetchedAt, now time.Time) bool {
	return marketcal.NSE.IsFreshEOD(fetchedAt, now)
}

// IsFreshEODForTicker is the market-aware variant: freshness is judged against
// the ticker's own market clock (NSE 21:00 IST vs NYSE 16:00 ET).
func IsFreshEODForTicker(ticker string, fetchedAt, now time.Time) bool {
	return marketcal.ClockForTicker(ticker).IsFreshEOD(fetchedAt, now)
}

// EODSettlementDate returns the NSE (India) settled trading date (midnight IST)
// for t. See marketcal.Clock.SettlementDate.
func EODSettlementDate(t time.Time) time.Time {
	return marketcal.NSE.SettlementDate(t)
}

// EODSettlementDateForTicker is the market-aware variant.
func EODSettlementDateForTicker(ticker string, t time.Time) time.Time {
	return marketcal.ClockForTicker(ticker).SettlementDate(t)
}

// NextEODAvailableDate returns when the next NSE (India) EOD file will be
// available (next 21:00 IST cutoff, skipping weekends and holidays). See
// marketcal.Clock.NextEODAvailable.
func NextEODAvailableDate(t time.Time) time.Time {
	return marketcal.NSE.NextEODAvailable(t)
}

// NextEODAvailableDateForTicker is the market-aware variant.
func NextEODAvailableDateForTicker(ticker string, t time.Time) time.Time {
	return marketcal.ClockForTicker(ticker).NextEODAvailable(t)
}

// IsNSEHoliday reports whether the given date (in IST) is an NSE trading holiday.
func IsNSEHoliday(t time.Time) bool {
	return marketcal.IsNSEHoliday(t)
}

// FormatOrdinalDay returns e.g. "21st", "22nd", "23rd", "24th" for a day number.
func FormatOrdinalDay(d int) string {
	switch d {
	case 1, 21, 31:
		return fmt.Sprintf("%dst", d)
	case 2, 22:
		return fmt.Sprintf("%dnd", d)
	case 3, 23:
		return fmt.Sprintf("%drd", d)
	default:
		return fmt.Sprintf("%dth", d)
	}
}
