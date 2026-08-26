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
	"gopkg.in/yaml.v3"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/monitoring"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

var MonitorCommand = &cli.Command{
	Name:  "monitor",
	Usage: "Run portfolio drift monitoring simulation",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Value: "data/microsmall.csv", Usage: "Path to the portfolio CSV file"},
		&cli.BoolFlag{Name: "interactive", Usage: "Run in interactive terminal mode"},
		&cli.StringFlag{Name: "style", Value: "moderate", Usage: "Monitoring style preset (hyper-aggressive, moderate, passive)"},
		&cli.FloatFlag{Name: "capital", Value: 100000.0, Usage: "Initial capital invested"},
		&cli.StringFlag{Name: "date", Usage: "Start date for simulation (YYYY-MM-DD)"},
		&cli.StringFlag{Name: "strategy", Aliases: []string{"method", "m", "s"}, Value: "balanced", Usage: "Strategy policy preset (value, multibagger, balanced, aggressive, conservative)"},
		&cli.StringFlag{Name: "timestamp", Usage: "Unified run timestamp in YYYYMMDD_HHMMSS format"},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		return runMonitorWithParams(ctx,
			c.String("file"),
			c.Bool("interactive"),
			c.String("style"),
			c.Float("capital"),
			c.String("date"),
			c.String("strategy"),
			c.String("timestamp"),
			c.IsSet("strategy"),
		)
	},
}

func runMonitorWithParams(ctx context.Context, filePath string, interactive bool, style string, capital float64, date, strategy, timestamp string, strategyExplicit bool) error {
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
		if hClean == "ticker" {
			tickerIdx = i
		} else if hClean == "weight" {
			weightIdx = i
		}
	}
	if tickerIdx == -1 || weightIdx == -1 {
		return fmt.Errorf("invalid CSV format. Must contain 'ticker' and 'weight' columns")
	}

	var portfolio []monitoring.StockInfo
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
		portfolio = append(portfolio, monitoring.StockInfo{Ticker: ticker, Weight: weightVal})
		tickers = append(tickers, ticker)
	}
	if len(portfolio) == 0 {
		return fmt.Errorf("no valid stocks found in CSV")
	}

	activeStrategy := strategy
	if !strategyExplicit {
		activeStrategy = monitorGetPipelineStrategy()
	}

	params := monitorPresetParams(style, activeStrategy)
	params.StartDate = date

	if interactive {
		dateStr := params.StartDate
		params = monitorInteractiveMenu(params)
		params.StartDate = dateStr
		if params.StartDate == "" && timestamp == "" {
			params.StartDate = monitorPromptTimeframeChoice()
		}
	}

	mfsCfg, err := config.LoadHardFilters("config/mfs.json", "multibagger")
	maxCapEx := 2.00
	if err == nil && mfsCfg != nil {
		maxCapEx = mfsCfg.MaxCapExYoYMultiplier
	}
	params.MaxCapExYoYMultiplier = maxCapEx

	fmt.Printf("\nRunning simulation with parameters:\n")
	fmt.Printf("- Strategy Policy: %s\n", strings.ToUpper(activeStrategy))
	fmt.Printf("- Consecutive Quarters Exit: %d\n", params.ConsecutiveQuartersExit)
	fmt.Printf("- DSO Deterioration Trigger: %.1f%%\n", params.DSODeteriorationThreshold*100.0)
	fmt.Printf("- CapEx YoY Reinvestment Cap: %.2f\n", params.MaxCapExYoYMultiplier)
	fmt.Printf("- SMA 200 Consecutive Days: %d\n", params.SMADays)
	fmt.Printf("- Rebalance Frequency: Every %d months\n", params.RebalanceMonths)
	fmt.Printf("- Max Weight Drift: %.1f%%\n\n", params.MaxWeightDrift*100.0)

	fmt.Println("Fetching financial data and price histories...")
	histData, benchData, fundamentals, mockedTickers, isMockUsed := monitorLoadAllData(ctx, tickers)
	if isMockUsed {
		fmt.Println("⚠️ Yahoo Finance API unavailable or returned incomplete data. Switched to high-fidelity mock fallback.")
	} else {
		fmt.Println("✅ Successfully fetched live data from Yahoo Finance.")
	}

	for i := range portfolio {
		if mockedTickers[portfolio[i].Ticker] {
			portfolio[i].IsMock = true
		}
	}

	simResult, err := monitoring.RunSimulation(portfolio, params, histData, benchData, fundamentals, capital)
	if err != nil {
		return fmt.Errorf("simulation error: %w", err)
	}

	generateMonitorReport(simResult, filePath, style, isMockUsed, params, activeStrategy, timestamp)
	return nil
}

func monitorPresetParams(style, strategy string) monitoring.PolicyParams {
	p := monitoring.PolicyParams{
		Strategy: strategy,
	}
	isVal := strings.ToLower(strategy) == "value"
	switch strings.ToLower(style) {
	case "hyper-aggressive":
		p.ConsecutiveQuartersExit = 1
		p.DSODeteriorationThreshold = 0.10
		p.SMADays = 5
		p.RebalanceMonths = 3
		p.MaxWeightDrift = 0.12
		if isVal {
			p.DSODeteriorationThreshold = 0.20
		}
	case "passive":
		p.ConsecutiveQuartersExit = 3
		p.DSODeteriorationThreshold = 0.25
		p.SMADays = 20
		p.RebalanceMonths = 12
		p.MaxWeightDrift = 0.20
		if isVal {
			p.DSODeteriorationThreshold = 0.35
		}
	default:
		p.ConsecutiveQuartersExit = 2
		p.DSODeteriorationThreshold = 0.15
		p.SMADays = 10
		p.RebalanceMonths = 6
		p.MaxWeightDrift = 0.15
		if isVal {
			p.DSODeteriorationThreshold = 0.30
		}
	}
	return p
}

func monitorInteractiveMenu(defaults monitoring.PolicyParams) monitoring.PolicyParams {
	fmt.Println("=====================================================")
	fmt.Println("        PORTFOLIO MONITORING POLICY SIMULATOR        ")
	fmt.Println("=====================================================")
	fmt.Println("Choose a monitoring style:")
	fmt.Println("1. Hyper-Aggressive (Strict triggers, frequent rebalancing)")
	fmt.Println("2. Moderate / Balanced (Standard guidelines, 6m rebalance) [Default]")
	fmt.Println("3. Passive (Loose triggers, annual rebalancing)")
	fmt.Println("4. Custom Parameters (Specify your own thresholds)")
	fmt.Println("-----------------------------------------------------")
	fmt.Print("Enter choice (1-4): ")

	var choice int
	fmt.Scanln(&choice)

	switch choice {
	case 1:
		return monitorPresetParams("hyper-aggressive", defaults.Strategy)
	case 3:
		return monitorPresetParams("passive", defaults.Strategy)
	case 4:
		var quarters, smaDays, rebalanceMonths int
		var dsoDeterioration, maxDrift float64

		fmt.Printf("Enter consecutive quarters of growth slowdown to trigger exit [current: %d]: ", defaults.ConsecutiveQuartersExit)
		fmt.Scanln(&quarters)
		if quarters <= 0 {
			quarters = defaults.ConsecutiveQuartersExit
		}
		fmt.Printf("Enter DSO YoY deterioration %% threshold (e.g. 15 for 15%%) [current: %.1f]: ", defaults.DSODeteriorationThreshold*100.0)
		fmt.Scanln(&dsoDeterioration)
		if dsoDeterioration <= 0 {
			dsoDeterioration = defaults.DSODeteriorationThreshold * 100.0
		}
		fmt.Printf("Enter consecutive days below 200 SMA to alert [current: %d]: ", defaults.SMADays)
		fmt.Scanln(&smaDays)
		if smaDays <= 0 {
			smaDays = defaults.SMADays
		}
		fmt.Printf("Enter rebalance frequency in months [current: %d]: ", defaults.RebalanceMonths)
		fmt.Scanln(&rebalanceMonths)
		if rebalanceMonths <= 0 {
			rebalanceMonths = defaults.RebalanceMonths
		}
		fmt.Printf("Enter dynamic weight drift threshold %% (e.g. 15 for 15%%) [current: %.1f]: ", defaults.MaxWeightDrift*100.0)
		fmt.Scanln(&maxDrift)
		if maxDrift <= 0 {
			maxDrift = defaults.MaxWeightDrift * 100.0
		}
		return monitoring.PolicyParams{
			Strategy:                  defaults.Strategy,
			ConsecutiveQuartersExit:   quarters,
			DSODeteriorationThreshold: dsoDeterioration / 100.0,
			SMADays:                   smaDays,
			RebalanceMonths:           rebalanceMonths,
			MaxWeightDrift:            maxDrift / 100.0,
		}
	default:
		return monitorPresetParams("moderate", defaults.Strategy)
	}
}

func monitorLoadAllData(ctx context.Context, tickers []string) (
	map[string]*yfinance.HistoricalData,
	*yfinance.HistoricalData,
	map[string]yfinance.Fundamentals,
	map[string]bool,
	bool,
) {
	var benchData *yfinance.HistoricalData
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Go(func() {
		benchTicker := broker.LoadMarketConfig().Benchmark
		b, err := yfinance.FetchHistoricalDataWithTimestamps(ctx, benchTicker, "2y")
		if err == nil && b != nil && len(b.Closes) > 200 {
			mu.Lock()
			benchData = b
			mu.Unlock()
		}
	})

	liveHist := make(map[string]*yfinance.HistoricalData)
	liveFunds := make(map[string]yfinance.Fundamentals)

	for _, t := range tickers {
		wg.Add(2)
		go func(ticker string) {
			defer wg.Done()
			h, err := yfinance.FetchHistoricalDataWithTimestamps(ctx, ticker, "2y")
			if err == nil && h != nil && len(h.Closes) > 200 {
				mu.Lock()
				liveHist[ticker] = h
				mu.Unlock()
			}
		}(t)
		go func(ticker string) {
			defer wg.Done()
			funds, err := yfinance.FetchFundamentals(ctx, []string{ticker})
			if err == nil && len(funds) > 0 {
				mu.Lock()
				if val, ok := funds[ticker]; ok {
					liveFunds[ticker] = val
				}
				mu.Unlock()
			}
		}(t)
	}
	wg.Wait()

	histData, benchData, fundamentals, mockedTickers, mockUsed := monitoring.FillWithMockData(tickers, liveHist, liveFunds, benchData)
	return histData, benchData, fundamentals, mockedTickers, mockUsed
}

func generateMonitorReport(res monitoring.SimulationResult, inputPath string, style string, isMockUsed bool, params monitoring.PolicyParams, strategy, timestamp string) {
	indexName := csvloader.GetUniverseName(inputPath)
	reportDir := filepath.Join("report", fmt.Sprintf("%s_%s", indexName, strategy), "simulations")
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		fmt.Printf("Warning: Failed to create report directory: %v\n", err)
	}

	timeStr := timestamp
	if timeStr == "" {
		timeStr = time.Now().Format("20060102_150405")
	}
	outReportPath := filepath.Join(reportDir, fmt.Sprintf("%s_monitoring.txt", timeStr))

	reportFile, err := os.Create(outReportPath)
	var writer io.Writer = os.Stdout
	if err != nil {
		fmt.Printf("Warning: Failed to create report file %s: %v\n", outReportPath, err)
	} else {
		defer reportFile.Close()
		writer = io.MultiWriter(os.Stdout, reportFile)
	}

	fmt.Fprintln(writer)
	fmt.Fprintf(writer, "=========================================================================\n")
	fmt.Fprintf(writer, "             Portfolio Monitoring Simulation Report                      \n")
	fmt.Fprintf(writer, "=========================================================================\n")
	fmt.Fprintf(writer, "File:             %s\n", inputPath)
	fmt.Fprintf(writer, "Policy Preset:    %s\n", strings.ToUpper(style[:1])+style[1:])
	if params.StartDate != "" {
		fmt.Fprintf(writer, "Simulated Scope:  From %s to Present\n", params.StartDate)
	} else {
		fmt.Fprintf(writer, "Simulated Scope:  1 Year Historical Backtest\n")
	}
	fmt.Fprintf(writer, "Output File:      %s\n", outReportPath)
	if isMockUsed {
		fmt.Fprintf(writer, "Data Mode:        ⚠️ High-Fidelity Mock Fallback\n")
	} else {
		fmt.Fprintf(writer, "Data Mode:        ✅ Live Yahoo Finance Data\n")
	}
	fmt.Fprintf(writer, "=========================================================================\n\n")

	fmt.Fprintf(writer, "%-15s | %-13s | %-12s | %-14s | %-13s | %-18s | %-11s | %-14s\n",
		"Ticker", "Sector", "3Y CAGR (%)", "TTM Growth (%)", "DSO Delta (%)", "Cap Stall Severity", "Data Source", "Policy Verdict")
	fmt.Fprintf(writer, "-------------------------------------------------------------------------------------------------------------------------------------\n")

	for _, v := range res.Verdicts {
		sect := v.Sector
		if len(sect) > 13 {
			sect = sect[:13]
		}
		fmt.Fprintf(writer, "%-15s | %-13s | %-12.1f | %-14.1f | %-13.1f | %-18s | %-11s | %-14s\n",
			v.Ticker, sect, v.CAGR3Y, v.TTMGrowth, v.DSODelta, v.CapStallSeverity, v.DataSource, v.Verdict)
	}

	fmt.Fprintf(writer, "-------------------------------------------------------------------------------------------------------------------------------------\n\n")
	fmt.Fprintf(writer, "SIMULATED CHURN RATE\n%.1f%%\n\n", res.ChurnRate)
	fmt.Fprintf(writer, "ALPHA EFFICIENCY\n%.2f\n\n", res.AlphaEfficiency)
	fmt.Fprintf(writer, "PORTFOLIO RETURN:   %+.2f%%\n", res.PortfolioReturn)
	fmt.Fprintf(writer, "BENCHMARK RETURN:   %+.2f%%\n", res.BenchmarkReturn)
	fmt.Fprintf(writer, "EXCESS RETURN (α):  %+.2f%%\n", res.ExcessReturn)
	fmt.Fprintf(writer, "=========================================================================\n")
}

func monitorPromptTimeframeChoice() string {
	purchaseDate := monitorGetPipelinePurchaseDate()
	fmt.Println("-----------------------------------------------------")
	fmt.Println("Choose Monitoring Simulator timeframe:")
	fmt.Println("1. 1 Year Historical Backtest [Default]")
	fmt.Printf("2. Same as performance simulation date (%s)\n", purchaseDate)
	fmt.Println("3. Custom Start Date (YYYY-MM-DD)")
	fmt.Println("-----------------------------------------------------")
	fmt.Print("Enter choice (1-3, default: 1): ")

	var choiceStr string
	fmt.Scanln(&choiceStr)
	choiceStr = strings.TrimSpace(choiceStr)

	switch choiceStr {
	case "2":
		return purchaseDate
	case "3":
		fmt.Print("Enter custom start date (YYYY-MM-DD): ")
		var dateStr string
		fmt.Scanln(&dateStr)
		return strings.TrimSpace(dateStr)
	default:
		return ""
	}
}

func monitorGetPipelinePurchaseDate() string {
	file, err := os.Open("config/pipeline.yaml")
	if err != nil {
		return "2026-01-01"
	}
	defer file.Close()
	var cfg struct {
		PurchaseDate any `yaml:"purchase_date"`
	}
	if err := yaml.NewDecoder(file).Decode(&cfg); err != nil {
		return "2026-01-01"
	}
	if cfg.PurchaseDate == nil {
		return "2026-01-01"
	}
	if s, ok := cfg.PurchaseDate.(string); ok {
		return s
	}
	if slice, ok := cfg.PurchaseDate.([]any); ok && len(slice) > 0 {
		if s, ok := slice[0].(string); ok {
			return s
		}
	}
	return "2026-01-01"
}

func monitorGetPipelineStrategy() string {
	file, err := os.Open("config/pipeline.yaml")
	if err != nil {
		return "balanced"
	}
	defer file.Close()
	var cfg struct {
		Strategy any `yaml:"strategy"`
	}
	if err := yaml.NewDecoder(file).Decode(&cfg); err != nil {
		return "balanced"
	}
	if cfg.Strategy == nil {
		return "balanced"
	}
	if s, ok := cfg.Strategy.(string); ok {
		return s
	}
	if slice, ok := cfg.Strategy.([]any); ok && len(slice) > 0 {
		if s, ok := slice[0].(string); ok {
			return s
		}
	}
	return "balanced"
}
