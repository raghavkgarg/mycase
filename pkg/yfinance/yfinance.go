package yfinance

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"sync"
	"time"
)

// FetchCookieAndCrumb requests fc.yahoo.com for cookie and query2 getcrumb for crumb
func FetchCookieAndCrumb(client *http.Client) (string, error) {
	req1, err := http.NewRequest("GET", "https://fc.yahoo.com", nil)
	if err != nil {
		return "", err
	}
	req1.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36")

	resp1, err := client.Do(req1)
	if err != nil {
		return "", err
	}
	resp1.Body.Close()

	req2, err := http.NewRequest("GET", "https://query2.finance.yahoo.com/v1/test/getcrumb", nil)
	if err != nil {
		return "", err
	}
	req2.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36")

	resp2, err := client.Do(req2)
	if err != nil {
		return "", err
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get crumb, status: %d", resp2.StatusCode)
	}

	crumbBytes, err := io.ReadAll(resp2.Body)
	if err != nil {
		return "", err
	}
	return string(crumbBytes), nil
}

// FetchFundamentals fetches fundamental metrics for a list of tickers in parallel
func FetchFundamentals(tickers []string) (map[string]Fundamentals, error) {
	fundamentals := make(map[string]Fundamentals)
	if len(tickers) == 0 {
		return fundamentals, nil
	}

	var uncachedTickers []string
	for _, t := range tickers {
		var cached Fundamentals
		if loadFromCache("fundamentals", t, &cached) {
			fundamentals[t] = cached
		} else {
			uncachedTickers = append(uncachedTickers, t)
		}
	}

	if len(uncachedTickers) == 0 {
		return fundamentals, nil
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	client := &http.Client{
		Timeout: 8 * time.Second,
		Jar:     jar,
	}

	// Fetch Cookie and Crumb once to reuse
	crumb, err := FetchCookieAndCrumb(client)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve cookie and crumb: %w", err)
	}

	type fetchJob struct {
		ticker string
	}
	type fetchResult struct {
		ticker string
		fund   Fundamentals
		err    error
	}

	jobs := make(chan fetchJob, len(uncachedTickers))
	results := make(chan fetchResult, len(uncachedTickers))
	var wg sync.WaitGroup

	// Spawning 15 concurrent workers
	workerCount := min(len(uncachedTickers), 15)

	for range workerCount {
		wg.Go(func() {
			for job := range jobs {
				ySym := MapTickerToYahoo(job.ticker)
				url := fmt.Sprintf("https://query2.finance.yahoo.com/v10/finance/quoteSummary/%s?modules=financialData,defaultKeyStatistics,summaryDetail,assetProfile,earnings&crumb=%s", ySym, crumb)

				req, err := http.NewRequest("GET", url, nil)
				if err != nil {
					results <- fetchResult{ticker: job.ticker, err: err}
					continue
				}
				req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36")

				resp, err := client.Do(req)
				if err != nil {
					results <- fetchResult{ticker: job.ticker, err: err}
					continue
				}

				if resp.StatusCode != http.StatusOK {
					resp.Body.Close()
					results <- fetchResult{ticker: job.ticker, err: fmt.Errorf("HTTP status %d", resp.StatusCode)}
					continue
				}

				var qRes QuoteSummaryResponse
				decodeErr := json.NewDecoder(resp.Body).Decode(&qRes)
				resp.Body.Close()
				if decodeErr != nil {
					results <- fetchResult{ticker: job.ticker, err: decodeErr}
					continue
				}

				if len(qRes.QuoteSummary.Result) == 0 {
					results <- fetchResult{ticker: job.ticker, err: fmt.Errorf("no results in quoteSummary")}
					continue
				}

				res := qRes.QuoteSummary.Result[0]
				fd := res.FinancialData
				ds := res.DefaultKeyStatistics
				sd := res.SummaryDetail

				// Forward PE fallback logic
				forwardPE := sd.ForwardPE.Raw
				if forwardPE == 0 {
					forwardPE = ds.ForwardPE.Raw
				}
				// If Forward PE is negative (unprofitable), penalize it with a high value
				if forwardPE < 0 {
					forwardPE = 999.0
				}

				// PEG Ratio logic: negative PEG is unprofitable / risky, penalize it
				pegRatio := ds.PegRatio.Raw
				if pegRatio < 0 {
					pegRatio = 99.0
				}

				// Price to Book ratio logic: negative P/B means negative equity, penalize it
				pbRatio := ds.PriceToBook.Raw
				if pbRatio < 0 {
					pbRatio = 99.0
				}

				// Net Debt / EBITDA calculation
				netDebt := fd.TotalDebt.Raw - fd.TotalCash.Raw
				netDebtEbitda := 0.0
				if fd.Ebitda.Raw > 0 {
					netDebtEbitda = netDebt / fd.Ebitda.Raw
				} else {
					// Negative/Zero EBITDA is high risk, penalize it
					netDebtEbitda = 99.0
				}

				// Map yearly earnings chart
				var earningsHistory []AnnualFinancial
				for _, yf := range res.Earnings.FinancialsChart.Yearly {
					earningsHistory = append(earningsHistory, AnnualFinancial{
						Year:     yf.Date,
						Revenue:  yf.Revenue.Raw,
						Earnings: yf.Earnings.Raw,
					})
				}

				// Fetch timeseries fundamentals
				typesStr := "annualTotalRevenue,annualNetPPE,annualAccountsReceivable,annualCapitalExpenditure,annualOperatingIncome,annualTotalAssets,annualCurrentLiabilities,annualInterestExpense,annualGrossProfit"
				tsURL := fmt.Sprintf("https://query2.finance.yahoo.com/ws/fundamentals-timeseries/v1/finance/timeseries/%s?symbol=%s&type=%s&period1=0&period2=%d&crumb=%s", ySym, ySym, typesStr, time.Now().Unix(), crumb)

				var annualRevenue []AnnualMetric
				var annualGrossProfit []AnnualMetric
				var annualNetPPE []AnnualMetric
				var annualAccountsReceivable []AnnualMetric
				var annualCapEx []AnnualMetric
				var annualOperatingIncome []AnnualMetric
				var annualTotalAssets []AnnualMetric
				var annualCurrentLiabilities []AnnualMetric
				var annualInterestExpense []AnnualMetric

				tsReq, tsErr := http.NewRequest("GET", tsURL, nil)
				if tsErr == nil {
					tsReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36")
					tsResp, tsRespErr := client.Do(tsReq)
					if tsRespErr == nil {
						if tsResp.StatusCode == http.StatusOK {
							var tsRes TimeseriesResponse
							if json.NewDecoder(tsResp.Body).Decode(&tsRes) == nil {
								for _, item := range tsRes.Timeseries.Result {
									if len(item.Meta.Type) > 0 {
										tType := item.Meta.Type[0]
										switch tType {
										case "annualTotalRevenue":
											for _, r := range item.AnnualTotalRevenue {
												if r.AsOfDate != "" {
													annualRevenue = append(annualRevenue, AnnualMetric{
														Date:  r.AsOfDate,
														Value: r.ReportedValue.Raw,
													})
												}
											}
										case "annualGrossProfit":
											for _, r := range item.AnnualGrossProfit {
												if r.AsOfDate != "" {
													annualGrossProfit = append(annualGrossProfit, AnnualMetric{
														Date:  r.AsOfDate,
														Value: r.ReportedValue.Raw,
													})
												}
											}
										case "annualNetPPE":
											for _, r := range item.AnnualNetPPE {
												if r.AsOfDate != "" {
													annualNetPPE = append(annualNetPPE, AnnualMetric{
														Date:  r.AsOfDate,
														Value: r.ReportedValue.Raw,
													})
												}
											}
										case "annualAccountsReceivable":
											for _, r := range item.AnnualAccountsReceivable {
												if r.AsOfDate != "" {
													annualAccountsReceivable = append(annualAccountsReceivable, AnnualMetric{
														Date:  r.AsOfDate,
														Value: r.ReportedValue.Raw,
													})
												}
											}
										case "annualCapitalExpenditure":
											for _, r := range item.AnnualCapitalExpenditure {
												if r.AsOfDate != "" {
													annualCapEx = append(annualCapEx, AnnualMetric{
														Date:  r.AsOfDate,
														Value: r.ReportedValue.Raw,
													})
												}
											}
										case "annualOperatingIncome":
											for _, r := range item.AnnualOperatingIncome {
												if r.AsOfDate != "" {
													annualOperatingIncome = append(annualOperatingIncome, AnnualMetric{
														Date:  r.AsOfDate,
														Value: r.ReportedValue.Raw,
													})
												}
											}
										case "annualTotalAssets":
											for _, r := range item.AnnualTotalAssets {
												if r.AsOfDate != "" {
													annualTotalAssets = append(annualTotalAssets, AnnualMetric{
														Date:  r.AsOfDate,
														Value: r.ReportedValue.Raw,
													})
												}
											}
										case "annualCurrentLiabilities":
											for _, r := range item.AnnualCurrentLiabilities {
												if r.AsOfDate != "" {
													annualCurrentLiabilities = append(annualCurrentLiabilities, AnnualMetric{
														Date:  r.AsOfDate,
														Value: r.ReportedValue.Raw,
													})
												}
											}
										case "annualInterestExpense":
											for _, r := range item.AnnualInterestExpense {
												if r.AsOfDate != "" {
													annualInterestExpense = append(annualInterestExpense, AnnualMetric{
														Date:  r.AsOfDate,
														Value: r.ReportedValue.Raw,
													})
												}
											}
										}
									}
								}
							}
						}
						tsResp.Body.Close()
					}
				}

				fund := Fundamentals{
					PEGRatio:                 pegRatio,
					ROE:                      fd.ReturnOnEquity.Raw,
					ForwardPE:                forwardPE,
					OperatingMargins:         fd.OperatingMargins.Raw,
					PBRatio:                  pbRatio,
					NetDebtEBITDA:            netDebtEbitda,
					MarketCap:                sd.MarketCap.Raw,
					InsidersPercent:          ds.HeldPercentInsiders.Raw,
					HeldPercentInstitutions:  ds.HeldPercentInstitutions.Raw,
					TTMRevenue:               fd.TotalRevenue.Raw,
					OperatingCashflow:        fd.OperatingCashflow.Raw,
					FreeCashflow:             fd.FreeCashflow.Raw,
					AverageVolume:            sd.AverageVolume.Raw,
					RegularPrice:             sd.RegularMarketPrice.Raw,
					NetIncome:                ds.NetIncomeToCommon.Raw,
					Sector:                   res.AssetProfile.Sector,
					EarningsHistory:          earningsHistory,
					AnnualRevenue:            annualRevenue,
					AnnualGrossProfit:        annualGrossProfit,
					AnnualNetPPE:             annualNetPPE,
					AnnualAccountsReceivable: annualAccountsReceivable,
					AnnualCapEx:              annualCapEx,
					DebtToEquity:             fd.DebtToEquity.Raw,
					TotalDebt:                fd.TotalDebt.Raw,
					AnnualOperatingIncome:    annualOperatingIncome,
					AnnualTotalAssets:        annualTotalAssets,
					AnnualCurrentLiabilities: annualCurrentLiabilities,
					AnnualInterestExpense:    annualInterestExpense,
				}

				results <- fetchResult{ticker: job.ticker, fund: fund}
			}
		})
	}

	for _, t := range uncachedTickers {
		jobs <- fetchJob{ticker: t}
	}
	close(jobs)

	wg.Wait()
	close(results)

	for res := range results {
		if res.err == nil {
			fundamentals[res.ticker] = res.fund
			saveToCache("fundamentals", res.ticker, res.fund)
		} else {
			// Print warning but don't fail the whole execution
			fmt.Printf("Warning: Failed to fetch fundamentals for %s: %v\n", res.ticker, res.err)
		}
	}

	return fundamentals, nil
}
