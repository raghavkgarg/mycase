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
			p, err := yfinance.FetchHistoricalPrices(ctx, ticker, "3mo")
			if err == nil {
				mu.Lock()
				price3mo[ticker] = p
				mu.Unlock()
			}
		}(t)
		go func(ticker string) {
			defer wg.Done()
			h, err := yfinance.FetchHistoricalDataWithTimestamps(ctx, ticker, "1y")
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

	fundamentals, err := yfinance.FetchFundamentals(ctx, tickers)
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

		fmt.Fprintf(writer, "%d. %s (Portfolio Weight: %.2f%%)\n", i+1, t, s.weight*100.0)
		fmt.Fprintf(writer, "-------------------------------------------------------------------------\n")

		for _, r := range stockpicker.BuildRationale(t, method, fund, hist1y[t], price3mo[t], hardFilters) {
			fmt.Fprintf(writer, "• %s\n", r)
		}
		fmt.Fprintf(writer, "\n")
	}

	fmt.Printf("Successfully generated and saved report to %s\n", outReportPath)
	return nil
}
