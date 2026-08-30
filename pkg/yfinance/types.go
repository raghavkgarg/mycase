package yfinance

import "time"

// ChartResponse matches the Yahoo Finance chart endpoint response structure
type ChartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				RegularMarketPrice float64 `json:"regularMarketPrice"`
			} `json:"meta"`
		} `json:"result"`
		Error any `json:"error"`
	} `json:"chart"`
}

// HistoricalChartResponse matches Yahoo Finance chart JSON structure for multi-day range queries
type HistoricalChartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol string `json:"symbol"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Close  []float64 `json:"close"`
					Volume []float64 `json:"volume"`
					Open   []float64 `json:"open"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error any `json:"error"`
	} `json:"chart"`
}

// IntradayChartResponse matches the Yahoo Finance chart JSON structure for intraday queries
type IntradayChartResponse struct {
	Chart struct {
		Result []struct {
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open  []*float64 `json:"open"`
					Close []*float64 `json:"close"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error any `json:"error"`
	} `json:"chart"`
}

// QuoteSummaryResponse matches Yahoo Finance quoteSummary JSON structure with fundamental modules
type QuoteSummaryResponse struct {
	QuoteSummary struct {
		Result []struct {
			FinancialData struct {
				ReturnOnEquity struct {
					Raw float64 `json:"raw"`
				} `json:"returnOnEquity"`
				OperatingMargins struct {
					Raw float64 `json:"raw"`
				} `json:"operatingMargins"`
				TotalDebt struct {
					Raw float64 `json:"raw"`
				} `json:"totalDebt"`
				TotalCash struct {
					Raw float64 `json:"raw"`
				} `json:"totalCash"`
				Ebitda struct {
					Raw float64 `json:"raw"`
				} `json:"ebitda"`
				OperatingCashflow struct {
					Raw float64 `json:"raw"`
				} `json:"operatingCashflow"`
				FreeCashflow struct {
					Raw float64 `json:"raw"`
				} `json:"freeCashflow"`
				TotalRevenue struct {
					Raw float64 `json:"raw"`
				} `json:"totalRevenue"`
				DebtToEquity struct {
					Raw float64 `json:"raw"`
				} `json:"debtToEquity"`
			} `json:"financialData"`
			DefaultKeyStatistics struct {
				PegRatio struct {
					Raw float64 `json:"raw"`
				} `json:"pegRatio"`
				PriceToBook struct {
					Raw float64 `json:"raw"`
				} `json:"priceToBook"`
				ForwardPE struct {
					Raw float64 `json:"raw"`
				} `json:"forwardPE"`
				HeldPercentInsiders struct {
					Raw float64 `json:"raw"`
				} `json:"heldPercentInsiders"`
				HeldPercentInstitutions struct {
					Raw float64 `json:"raw"`
				} `json:"heldPercentInstitutions"`
				NetIncomeToCommon struct {
					Raw float64 `json:"raw"`
				} `json:"netIncomeToCommon"`
			} `json:"defaultKeyStatistics"`
			SummaryDetail struct {
				ForwardPE struct {
					Raw float64 `json:"raw"`
				} `json:"forwardPE"`
				MarketCap struct {
					Raw float64 `json:"raw"`
				} `json:"marketCap"`
				AverageVolume struct {
					Raw float64 `json:"raw"`
				} `json:"averageVolume"`
				RegularMarketPrice struct {
					Raw float64 `json:"raw"`
				} `json:"regularMarketPrice"`
			} `json:"summaryDetail"`
			AssetProfile struct {
				Sector   string `json:"sector"`
				Industry string `json:"industry"`
			} `json:"assetProfile"`
			CalendarEvents struct {
				Earnings struct {
					EarningsDate []struct {
						Fmt string `json:"fmt"`
						Raw int64  `json:"raw"`
					} `json:"earningsDate"`
				} `json:"earnings"`
			} `json:"calendarEvents"`
			Earnings struct {
				FinancialsChart struct {
					Yearly []struct {
						Date    int `json:"date"`
						Revenue struct {
							Raw float64 `json:"raw"`
						} `json:"revenue"`
						Earnings struct {
							Raw float64 `json:"raw"`
						} `json:"earnings"`
					} `json:"yearly"`
				} `json:"financialsChart"`
				EarningsChart struct {
					Quarterly []struct {
						Date         string `json:"date"`
						ReportedDate struct {
							Fmt string `json:"fmt"`
							Raw int64  `json:"raw"`
						} `json:"reportedDate"`
					} `json:"quarterly"`
				} `json:"earningsChart"`
			} `json:"earnings"`
		} `json:"result"`
		Error any `json:"error"`
	} `json:"quoteSummary"`
}

// AnnualFinancial holds annual revenue and earnings
type AnnualFinancial struct {
	Year     int
	Revenue  float64
	Earnings float64
}

// Fundamentals represents key fundamental metrics retrieved from Yahoo Finance
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
	DeliveryPct              float64
	DeliveryDate             string
	DeliverableQty           float64
}

// AnnualMetric holds historical value with its date
type AnnualMetric struct {
	Date  string
	Value float64
}

// IntradayData holds timestamp, open, and close prices for a stock
type IntradayData struct {
	Timestamps []int64
	Opens      []float64
	Closes     []float64
}

// HistoricalData holds daily timestamp, close, open, and volume data
type HistoricalData struct {
	Timestamps []int64
	Closes     []float64
	Opens      []float64
	Volumes    []float64
}

// TimeseriesResponse matches Yahoo Finance ws/fundamentals-timeseries JSON structure
type TimeseriesResponse struct {
	Timeseries struct {
		Result []struct {
			Meta struct {
				Symbol []string `json:"symbol"`
				Type   []string `json:"type"`
			} `json:"meta"`
			Timestamp                []int64 `json:"timestamp"`
			AnnualCapitalExpenditure []struct {
				AsOfDate      string `json:"asOfDate"`
				ReportedValue struct {
					Raw float64 `json:"raw"`
				} `json:"reportedValue"`
			} `json:"annualCapitalExpenditure"`
			AnnualTotalRevenue []struct {
				AsOfDate      string `json:"asOfDate"`
				ReportedValue struct {
					Raw float64 `json:"raw"`
				} `json:"reportedValue"`
			} `json:"annualTotalRevenue"`
			AnnualGrossProfit []struct {
				AsOfDate      string `json:"asOfDate"`
				ReportedValue struct {
					Raw float64 `json:"raw"`
				} `json:"reportedValue"`
			} `json:"annualGrossProfit"`
			AnnualNetPPE []struct {
				AsOfDate      string `json:"asOfDate"`
				ReportedValue struct {
					Raw float64 `json:"raw"`
				} `json:"reportedValue"`
			} `json:"annualNetPPE"`
			AnnualAccountsReceivable []struct {
				AsOfDate      string `json:"asOfDate"`
				ReportedValue struct {
					Raw float64 `json:"raw"`
				} `json:"reportedValue"`
			} `json:"annualAccountsReceivable"`
			AnnualOperatingIncome []struct {
				AsOfDate      string `json:"asOfDate"`
				ReportedValue struct {
					Raw float64 `json:"raw"`
				} `json:"reportedValue"`
			} `json:"annualOperatingIncome"`
			AnnualTotalAssets []struct {
				AsOfDate      string `json:"asOfDate"`
				ReportedValue struct {
					Raw float64 `json:"raw"`
				} `json:"reportedValue"`
			} `json:"annualTotalAssets"`
			AnnualCurrentLiabilities []struct {
				AsOfDate      string `json:"asOfDate"`
				ReportedValue struct {
					Raw float64 `json:"raw"`
				} `json:"reportedValue"`
			} `json:"annualCurrentLiabilities"`
			AnnualInterestExpense []struct {
				AsOfDate      string `json:"asOfDate"`
				ReportedValue struct {
					Raw float64 `json:"raw"`
				} `json:"reportedValue"`
			} `json:"annualInterestExpense"`
		} `json:"result"`
		Error any `json:"error"`
	} `json:"timeseries"`
}

// CleanIntradayNoiseAsOf discards in-progress or unconfirmed bars relative to asOf date and market close (15:45 IST).
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
		if lastTs.Truncate(24*time.Hour).After(asOfIST.Truncate(24*time.Hour)) {
			h.truncateLast()
			continue
		}
		break
	}

	// 2. If the last bar is on the SAME day as asOfIST:
	if len(h.Timestamps) > 0 {
		lastTs := time.Unix(h.Timestamps[len(h.Timestamps)-1], 0).In(istLoc)
		if lastTs.Year() == asOfIST.Year() && lastTs.YearDay() == asOfIST.YearDay() {
			// Market close is 15:30 IST + 15 min post-close settlement buffer = 15:45 IST
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

// CleanIntradayNoise discards today's live bar if executed before 15:45 IST.
func (h *HistoricalData) CleanIntradayNoise() {
	h.CleanIntradayNoiseAsOf(time.Now())
}
