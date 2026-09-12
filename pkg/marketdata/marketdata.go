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

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
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
}

// UnmarshalJSON provides resilient unmarshaling for DeliveryRecord, handling
// numeric values, nulls, and dirty NSE strings (such as "-", " - ", or "N/A") safely.
func (d *DeliveryRecord) UnmarshalJSON(data []byte) error {
	var aux struct {
		Date           string      `json:"date"`
		ClosePrice     interface{} `json:"close_price"`
		DeliverableQty interface{} `json:"deliverable_qty"`
		DeliveryPct    interface{} `json:"delivery_pct"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	d.Date = aux.Date
	d.ClosePrice = parseFlexibleFloat(aux.ClosePrice)
	d.DeliverableQty = parseFlexibleFloat(aux.DeliverableQty)
	d.DeliveryPct = parseFlexibleFloat(aux.DeliveryPct)
	return nil
}

func parseFlexibleFloat(v interface{}) float64 {
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

// LastSettledEODTime returns the timestamp of the most recent completed market EOD settlement cutoff (21:00 IST).
// - On Saturday, Sunday, or Monday before 21:00 IST: the last settled session is Friday at 21:00 IST.
// - On Tuesday through Friday before 21:00 IST: the last settled session is yesterday at 21:00 IST.
// - On Monday through Friday at or after 21:00 IST: the last settled session is today at 21:00 IST.
func LastSettledEODTime(t time.Time) time.Time {
	ist := time.FixedZone("IST", 5*3600+30*60)
	tIST := t.In(ist)
	weekday := tIST.Weekday()

	switch weekday {
	case time.Saturday:
		fri := tIST.AddDate(0, 0, -1)
		return time.Date(fri.Year(), fri.Month(), fri.Day(), 21, 0, 0, 0, ist)
	case time.Sunday:
		fri := tIST.AddDate(0, 0, -2)
		return time.Date(fri.Year(), fri.Month(), fri.Day(), 21, 0, 0, 0, ist)
	case time.Monday:
		if tIST.Hour() >= 21 {
			return time.Date(tIST.Year(), tIST.Month(), tIST.Day(), 21, 0, 0, 0, ist)
		}
		fri := tIST.AddDate(0, 0, -3)
		return time.Date(fri.Year(), fri.Month(), fri.Day(), 21, 0, 0, 0, ist)
	default: // Tuesday through Friday
		if tIST.Hour() >= 21 {
			return time.Date(tIST.Year(), tIST.Month(), tIST.Day(), 21, 0, 0, 0, ist)
		}
		yesterday := tIST.AddDate(0, 0, -1)
		return time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 21, 0, 0, 0, ist)
	}
}

// IsFreshEOD returns true if fetchedAt was recorded at or after the last settled EOD session cutoff.
func IsFreshEOD(fetchedAt, now time.Time) bool {
	cutoff := LastSettledEODTime(now)
	return !fetchedAt.Before(cutoff)
}

// EODSettlementDate returns the effective settled EOD market date for a given time t in IST.
// The sole daily cutoff is 21:00 IST (9:00 PM):
// - Any time before 21:00 IST belongs to the previous completed trading day's EOD cycle.
// - Any time at or after 21:00 IST belongs to today's completed EOD cycle.
// - Weekend awareness: Saturday, Sunday, and Monday before 21:00 IST map to Friday's settled EOD date.
func EODSettlementDate(t time.Time) time.Time {
	cutoff := LastSettledEODTime(t)
	ist := time.FixedZone("IST", 5*3600+30*60)
	return time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, ist)
}

// NextEODAvailableDate returns the date on which the next day's EOD file will be available (at 21:00 IST).
// Handles weekends gracefully: Friday post-21:00, Saturday, and Sunday point to Monday at 21:00 IST.
func NextEODAvailableDate(t time.Time) time.Time {
	ist := time.FixedZone("IST", 5*3600+30*60)
	tIST := t.In(ist)
	weekday := tIST.Weekday()

	switch weekday {
	case time.Friday:
		if tIST.Hour() < 21 {
			return time.Date(tIST.Year(), tIST.Month(), tIST.Day(), 21, 0, 0, 0, ist)
		}
		mon := tIST.AddDate(0, 0, 3)
		return time.Date(mon.Year(), mon.Month(), mon.Day(), 21, 0, 0, 0, ist)
	case time.Saturday:
		mon := tIST.AddDate(0, 0, 2)
		return time.Date(mon.Year(), mon.Month(), mon.Day(), 21, 0, 0, 0, ist)
	case time.Sunday:
		mon := tIST.AddDate(0, 0, 1)
		return time.Date(mon.Year(), mon.Month(), mon.Day(), 21, 0, 0, 0, ist)
	default:
		if tIST.Hour() < 21 {
			return time.Date(tIST.Year(), tIST.Month(), tIST.Day(), 21, 0, 0, 0, ist)
		}
		nextDay := tIST.AddDate(0, 0, 1)
		return time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), 21, 0, 0, 0, ist)
	}
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

