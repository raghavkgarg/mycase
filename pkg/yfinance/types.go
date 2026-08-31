package yfinance

import "github.com/raghavkgarg/mycase/pkg/marketdata"

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

// AnnualFinancial, AnnualMetric, Fundamentals, IntradayData, and HistoricalData
// are the provider-agnostic market data types. They live in pkg/marketdata (a
// zero-import leaf) so type-only consumers need not import yfinance (and thus the
// cache) — see R16 P1. These aliases keep existing yfinance.* call sites working.
type (
	AnnualFinancial = marketdata.AnnualFinancial
	AnnualMetric    = marketdata.AnnualMetric
	Fundamentals    = marketdata.Fundamentals
	IntradayData    = marketdata.IntradayData
	HistoricalData  = marketdata.HistoricalData
)

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
