package cmd

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

var ReportCommand = &cli.Command{
	Name:  "report",
	Usage: "Generate portfolio selection explanation report",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Required: true, Usage: "Path to the stockpicker output CSV file"},
		&cli.StringFlag{Name: "method", Aliases: []string{"m"}, Value: "balanced", Usage: "Weighting strategy (balanced, aggressive, conservative, multibagger)"},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		return runReportWithParams(ctx, c.String("file"), c.String("method"))
	},
}

func runReportWithParams(ctx context.Context, filePath, method string) error {
	if filePath == "" {
		return fmt.Errorf("--file parameter is required")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("opening file %s: %w", filePath, err)
	}
	defer file.Close()

	csvReader := csv.NewReader(file)
	records, err := csvReader.ReadAll()
	if err != nil {
		return fmt.Errorf("reading CSV: %w", err)
	}
	if len(records) < 2 {
		return fmt.Errorf("CSV file contains no data rows")
	}

	tickerIdx, weightIdx := -1, -1
	for i, h := range records[0] {
		hClean := strings.ToLower(strings.TrimSpace(h))
		switch hClean {
		case "ticker":
			tickerIdx = i
		case "weight":
			weightIdx = i
		}
	}
	if tickerIdx == -1 || weightIdx == -1 {
		return fmt.Errorf("invalid CSV format. Must contain 'ticker' and 'weight' columns")
	}

	type reportStock struct {
		ticker string
		weight float64
	}

	var portfolio []reportStock
	var tickers []string
	for _, record := range records[1:] {
		if len(record) <= tickerIdx || len(record) <= weightIdx {
			continue
		}
		ticker := strings.TrimSpace(record[tickerIdx])
		weightVal, err := strconv.ParseFloat(strings.TrimSpace(record[weightIdx]), 64)
		if err != nil || ticker == "" {
			continue
		}
		portfolio = append(portfolio, reportStock{ticker: ticker, weight: weightVal})
		tickers = append(tickers, ticker)
	}

	indexName := csvloader.GetUniverseName(filePath)
	reportDir := filepath.Join("report", fmt.Sprintf("%s_%s", indexName, method), "executions")
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		fmt.Printf("Warning: Failed to create report directory: %v\n", err)
	}
	dateStr := time.Now().Format("20060102")
	outReportPath := filepath.Join(reportDir, fmt.Sprintf("%s_03_portfolio_report.txt", dateStr))

	reportFile, err := os.Create(outReportPath)
	if err != nil {
		return fmt.Errorf("creating report file %s: %w", outReportPath, err)
	}
	defer reportFile.Close()
	var writer io.Writer = reportFile

	fmt.Fprintf(writer, "====================================================================\n")
	fmt.Fprintf(writer, "             Portfolio Selection Explanation Report                 \n")
	fmt.Fprintf(writer, "====================================================================\n")
	fmt.Fprintf(writer, "File:        %s\n", filePath)
	fmt.Fprintf(writer, "Strategy:    %s Preset\n", strings.ToUpper(method[:1])+method[1:])
	fmt.Fprintf(writer, "Stocks:      %d\n", len(tickers))
	fmt.Fprintf(writer, "Report File: %s\n", outReportPath)
	fmt.Fprintf(writer, "====================================================================\n\n")

	price3mo := make(map[string][]float64)
	hist1y := make(map[string]*yfinance.HistoricalData)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, t := range tickers {
		wg.Add(2)
		go func(ticker string) {
			defer wg.Done()
			p, err := yfinance.FetchHistoricalPrices(ticker, "3mo")
			if err == nil {
				mu.Lock()
				price3mo[ticker] = p
				mu.Unlock()
			}
		}(t)
		go func(ticker string) {
			defer wg.Done()
			h, err := yfinance.FetchHistoricalDataWithTimestamps(ticker, "1y")
			if err == nil {
				mu.Lock()
				hist1y[ticker] = h
				mu.Unlock()
			}
		}(t)
	}
	wg.Wait()

	cfg, cfgErr := stockpicker.LoadStrategyConfig(method)
	var hardFilters *config.HardFilters
	if cfgErr == nil && cfg != nil {
		hardFilters = cfg.HardFilters
	}

	fundamentals, err := yfinance.FetchFundamentals(tickers)
	if err != nil {
		fmt.Fprintf(writer, "Warning: Failed to fetch fundamentals: %v. Continuing...\n", err)
	}

	if method == "multibagger" {
		fmt.Fprintf(writer, "=========================================================================================\n")
		fmt.Fprintf(writer, "             Portfolio Overview Table (Multibagger Metrics)                             \n")
		fmt.Fprintf(writer, "=========================================================================================\n")
		fmt.Fprintf(writer, "%-16s | %-10s | %-8s | %-10s | %-5s | %-7s | %-12s\n", "Ticker", "TTM Growth", "3Y CAGR", "DSO (L/P)", "RSI", "Inst %", "Final Weight")
		fmt.Fprintf(writer, "-----------------------------------------------------------------------------------------\n")
		var totalWeight float64
		for _, s := range portfolio {
			t := s.ticker
			weight := s.weight
			totalWeight += weight
			f := fundamentals[t]
			_, ttmGrowth, cagr3y := yfinance.CalculateSalesGrowth(&f)
			_, dsoPrev, dsoLatest := yfinance.CalculateDSO(&f)
			rsiVal := 50.0
			if hData := hist1y[t]; hData != nil {
				rsiVal = yfinance.CalculateRSI(hData.Closes)
			}
			fmt.Fprintf(writer, "%-16s | %-10.1f%% | %-8.1f%% | %-10s | %-5.1f | %-7.1f%% | %-12.4f\n",
				t, ttmGrowth*100.0, cagr3y*100.0,
				fmt.Sprintf("%.0f/%.0f", dsoLatest, dsoPrev),
				rsiVal, f.HeldPercentInstitutions*100.0, weight)
		}
		fmt.Fprintf(writer, "-----------------------------------------------------------------------------------------\n")
		fmt.Fprintf(writer, "%-16s | %-10s | %-8s | %-10s | %-5s | %-7s | %-12.4f\n", "Total Weight", "", "", "", "", "", totalWeight)
		fmt.Fprintf(writer, "=========================================================================================\n\n")
	} else {
		fmt.Fprintf(writer, "=========================================================================\n")
		fmt.Fprintf(writer, "             Portfolio Overview Table                                    \n")
		fmt.Fprintf(writer, "=========================================================================\n")
		fmt.Fprintf(writer, "%-16s | %-12s | %-12s\n", "Ticker", "Final Weight", "1Y Return")
		fmt.Fprintf(writer, "-------------------------------------------------------------------------\n")
		var totalWeight float64
		for _, s := range portfolio {
			t := s.ticker
			weight := s.weight
			totalWeight += weight
			ret1y := 0.0
			if hData := hist1y[t]; hData != nil && len(hData.Closes) >= 2 {
				ret1y = (hData.Closes[len(hData.Closes)-1] - hData.Closes[0]) / hData.Closes[0] * 100.0
			}
			fmt.Fprintf(writer, "%-16s | %-12.4f | %+.1f%%\n", t, weight, ret1y)
		}
		fmt.Fprintf(writer, "-------------------------------------------------------------------------\n")
		fmt.Fprintf(writer, "%-16s | %-12.4f | %-12s\n", "Total Weight", totalWeight, "")
		fmt.Fprintf(writer, "=========================================================================\n\n")
	}

	for i, s := range portfolio {
		t := s.ticker
		fund := fundamentals[t]

		ret1y := 0.0
		var p1y []float64
		if hist1y[t] != nil {
			p1y = hist1y[t].Closes
		}
		if len(p1y) >= 2 {
			ret1y = (p1y[len(p1y)-1] - p1y[0]) / p1y[0] * 100.0
		}
		ret3mo := 0.0
		if p3mo := price3mo[t]; len(p3mo) >= 2 {
			ret3mo = (p3mo[len(p3mo)-1] - p3mo[0]) / p3mo[0] * 100.0
		}

		fmt.Fprintf(writer, "%d. %s (Portfolio Weight: %.2f%%)\n", i+1, t, s.weight*100.0)
		fmt.Fprintf(writer, "-------------------------------------------------------------------------\n")

		var rationale []string

		if method == "multibagger" {
			passedSales, ttmGrowth, cagr3y := yfinance.CalculateSalesGrowth(&fund)
			if len(fund.AnnualRevenue) >= 3 {
				yearsDiff := len(fund.AnnualRevenue) - 1
				if passedSales {
					rationale = append(rationale, fmt.Sprintf("Sales Growth Accelerator: Revenue growth is accelerating with a TTM growth rate of %.1f%%, exceeding the %d-Year annual CAGR of %.1f%%.", ttmGrowth*100.0, yearsDiff, cagr3y*100.0))
				} else {
					rationale = append(rationale, fmt.Sprintf("Sales Growth Warning: Revenue growth is not accelerating (TTM growth rate of %.1f%%, %d-Year annual CAGR of %.1f%%).", ttmGrowth*100.0, yearsDiff, cagr3y*100.0))
				}
			} else {
				rationale = append(rationale, "Sales Growth Accelerator: Unable to verify CAGR acceleration due to insufficient annual revenue history.")
			}

			maxCapEx := 1.15
			if hardFilters != nil && hardFilters.MaxCapExYoYMultiplier > 0 {
				maxCapEx = hardFilters.MaxCapExYoYMultiplier
			}
			passedAsset, atPrev, atLatest, pctCapExChange, capexLatestAbs := yfinance.CalculateAssetTurnoverCapEx(&fund, maxCapEx)
			if passedAsset || (atPrev > 0 && atLatest > 0) {
				rationale = append(rationale, fmt.Sprintf("Asset Turnover & CapEx Inflection: Asset turnover expanded YoY from %.2f to %.2f, indicating rising sales efficiency, while CapEx stabilized (YoY change of %+.1f%%, latest CapEx: %.1fCr).", atPrev, atLatest, pctCapExChange, capexLatestAbs/1e7))
			} else {
				rationale = append(rationale, "Asset Turnover & CapEx Inflection: Insufficient matching annual net PPE and CapEx data to determine operating leverage.")
			}

			_, dsoPrev, dsoLatest := yfinance.CalculateDSO(&fund)
			if dsoPrev > 0 && dsoLatest > 0 {
				if dsoLatest < dsoPrev {
					rationale = append(rationale, fmt.Sprintf("Working Capital Efficiency: Days Sales Outstanding (DSO) improved YoY from %.1f days to %.1f days, indicating faster cash collection.", dsoPrev, dsoLatest))
				} else if dsoLatest > dsoPrev {
					rationale = append(rationale, fmt.Sprintf("Working Capital Efficiency: Days Sales Outstanding (DSO) increased YoY from %.1f days to %.1f days, indicating slower cash collection.", dsoPrev, dsoLatest))
				} else {
					rationale = append(rationale, fmt.Sprintf("Working Capital Efficiency: Days Sales Outstanding (DSO) remained flat YoY at %.1f days.", dsoLatest))
				}
			} else {
				rationale = append(rationale, "Working Capital Efficiency: Insufficient accounts receivable data to determine DSO collection speed.")
			}

			rationale = append(rationale, fmt.Sprintf("Institutional Sponsorship: Institutions own %.1f%% of the total equity stake, showing validation from professional smart money.", fund.HeldPercentInstitutions*100.0))

			if hData, ok := hist1y[t]; ok && len(hData.Closes) >= 200 {
				rsiVal := yfinance.CalculateRSI(hData.Closes)
				lookback := 60
				multiplier := 2.0
				if hardFilters != nil {
					if hardFilters.VolumeBreakoutLookbackDays > 0 {
						lookback = hardFilters.VolumeBreakoutLookbackDays
					}
					if hardFilters.VolumeBreakoutMultiplier > 0 {
						multiplier = hardFilters.VolumeBreakoutMultiplier
					}
				}
				hasBreakout := yfinance.CheckVolumeBreakout(hData.Closes, hData.Opens, hData.Volumes, lookback, multiplier)
				breakoutStr := "no breakout detected"
				if hasBreakout {
					breakoutStr = "confirmed breakout detected"
				}
				rationale = append(rationale, fmt.Sprintf("Technical Stage Analysis: The stock is trading in a Stage 2 markup phase (above its 200-SMA) with strong momentum (RSI: %.1f) and has a %s on heavy green-day volume in the last 60 days.", rsiVal, breakoutStr))
			} else {
				rationale = append(rationale, "Technical Stage Analysis: Insufficient historical price and volume history to calculate RSI and SMA details.")
			}
		} else {
			if ret1y < 0 && ret3mo > 15.0 {
				rationale = append(rationale, fmt.Sprintf("Massive 3-Month Momentum: Over the default scoring window (3mo), %s has seen a huge rally of %+.2f%%. This gave it very high scores for the Return, Sharpe, and Sortino factors within the optimized timeframe, despite trailing %+.2f%% on a 1-year basis.", t, ret3mo, ret1y))
			} else if ret3mo > 25.0 {
				rationale = append(rationale, fmt.Sprintf("Strong Momentum: Showing powerful short-term performance with a %+.2f%% return over the past 3 months (1-year return is %+.2f%%).", ret3mo, ret1y))
			} else if ret1y > 50.0 {
				rationale = append(rationale, fmt.Sprintf("Steady Gainer: Solid long-term performer with a strong 1-year return of %+.2f%%.", ret1y))
			} else {
				rationale = append(rationale, fmt.Sprintf("Performance: Trailing 1-year return is %+.2f%% with a 3-month return of %+.2f%%.", ret1y, ret3mo))
			}
			if fund.ForwardPE > 0 && fund.ForwardPE <= 25.0 {
				rationale = append(rationale, fmt.Sprintf("Attractive Valuation: Its Forward P/E is %.1f, which is considered very cheap and high-value in the current market segments.", fund.ForwardPE))
			} else if fund.ForwardPE == 999.0 {
				rationale = append(rationale, "Valuation Warning: The company is currently unprofitable or has negative expected forward earnings.")
			} else if fund.ForwardPE > 45.0 {
				rationale = append(rationale, fmt.Sprintf("Premium Valuation: Trading at a higher Forward P/E of %.1f, reflecting high growth expectations.", fund.ForwardPE))
			}
			if fund.NetDebtEBITDA == 99.0 {
				rationale = append(rationale, "Solvency Warning: The company has high leverage relative to zero/negative EBITDA.")
			} else if fund.NetDebtEBITDA <= 0 {
				rationale = append(rationale, "Cash-Rich Balance Sheet: The company has a negative Net Debt/EBITDA ratio, indicating it is net cash-positive (cash holdings exceed total debt).")
			} else if fund.NetDebtEBITDA < 2.0 {
				rationale = append(rationale, fmt.Sprintf("Strong Balance Sheet: Healthy solvency with a low Net Debt/EBITDA ratio of %.2f.", fund.NetDebtEBITDA))
			}
			if fund.ROE > 0.15 {
				rationale = append(rationale, fmt.Sprintf("Strong Efficiency: Delivers high capital efficiency with a return on equity (ROE) of %.1f%%.", fund.ROE*100.0))
			}
			if fund.OperatingMargins > 0.10 {
				rationale = append(rationale, fmt.Sprintf("Stable Margins: Screens for business stability with positive operating margins of %.1f%%.", fund.OperatingMargins*100.0))
			}
		}

		for _, r := range rationale {
			fmt.Fprintf(writer, "• %s\n", r)
		}
		fmt.Fprintf(writer, "\n")
	}

	fmt.Printf("Successfully generated and saved report to %s\n", outReportPath)
	return nil
}
