package cmd

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"

	"github.com/gkgarg24/mycase/pkg/config"
	"github.com/gkgarg24/mycase/pkg/csvloader"
	"github.com/gkgarg24/mycase/pkg/monitoring"
	"github.com/gkgarg24/mycase/pkg/yfinance"
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
		&cli.StringFlag{Name: "strategy", Value: "balanced", Usage: "Weighting strategy preset (balanced, aggressive, conservative, multibagger)"},
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

	params := monitorPresetParams(style)
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
	fmt.Printf("- Consecutive Quarters Exit: %d\n", params.ConsecutiveQuartersExit)
	fmt.Printf("- DSO Deterioration Trigger: %.1f%%\n", params.DSODeteriorationThreshold*100.0)
	fmt.Printf("- CapEx YoY Reinvestment Cap: %.2f\n", params.MaxCapExYoYMultiplier)
	fmt.Printf("- SMA 200 Consecutive Days: %d\n", params.SMADays)
	fmt.Printf("- Rebalance Frequency: Every %d months\n", params.RebalanceMonths)
	fmt.Printf("- Max Weight Drift: %.1f%%\n\n", params.MaxWeightDrift*100.0)

	fmt.Println("Fetching financial data and price histories...")
	histData, benchData, fundamentals, mockedTickers, isMockUsed := monitorLoadAllData(tickers)
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

	activeStrategy := strategy
	if !strategyExplicit {
		activeStrategy = monitorGetPipelineStrategy()
	}

	generateMonitorReport(simResult, filePath, style, isMockUsed, params, activeStrategy, timestamp)
	return nil
}

func monitorPresetParams(style string) monitoring.PolicyParams {
	switch strings.ToLower(style) {
	case "hyper-aggressive":
		return monitoring.PolicyParams{
			ConsecutiveQuartersExit:   1,
			DSODeteriorationThreshold: 0.10,
			SMADays:                   5,
			RebalanceMonths:           3,
			MaxWeightDrift:            0.12,
		}
	case "passive":
		return monitoring.PolicyParams{
			ConsecutiveQuartersExit:   3,
			DSODeteriorationThreshold: 0.25,
			SMADays:                   20,
			RebalanceMonths:           12,
			MaxWeightDrift:            0.20,
		}
	default:
		return monitoring.PolicyParams{
			ConsecutiveQuartersExit:   2,
			DSODeteriorationThreshold: 0.15,
			SMADays:                   10,
			RebalanceMonths:           6,
			MaxWeightDrift:            0.15,
		}
	}
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
		return monitorPresetParams("hyper-aggressive")
	case 3:
		return monitorPresetParams("passive")
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
			ConsecutiveQuartersExit:   quarters,
			DSODeteriorationThreshold: dsoDeterioration / 100.0,
			SMADays:                   smaDays,
			RebalanceMonths:           rebalanceMonths,
			MaxWeightDrift:            maxDrift / 100.0,
		}
	default:
		return monitorPresetParams("moderate")
	}
}

func monitorLoadAllData(tickers []string) (
	map[string]*yfinance.HistoricalData,
	*yfinance.HistoricalData,
	map[string]yfinance.Fundamentals,
	map[string]bool,
	bool,
) {
	histData := make(map[string]*yfinance.HistoricalData)
	var benchData *yfinance.HistoricalData
	fundamentals := make(map[string]yfinance.Fundamentals)

	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		b, err := yfinance.FetchHistoricalDataWithTimestamps("^NSEI", "2y")
		if err == nil && b != nil && len(b.Closes) >= 200 {
			mu.Lock()
			benchData = b
			mu.Unlock()
		}
	}()

	liveHist := make(map[string]*yfinance.HistoricalData)
	liveFunds := make(map[string]yfinance.Fundamentals)

	for _, t := range tickers {
		wg.Add(2)
		go func(ticker string) {
			defer wg.Done()
			h, err := yfinance.FetchHistoricalDataWithTimestamps(ticker, "2y")
			if err == nil && h != nil && len(h.Closes) >= 200 {
				mu.Lock()
				liveHist[ticker] = h
				mu.Unlock()
			}
		}(t)
		go func(ticker string) {
			defer wg.Done()
			funds, err := yfinance.FetchFundamentals([]string{ticker})
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

	// Seeded local rand for 100% reproducible mock price paths.
	localRand := rand.New(rand.NewSource(42))
	nDays := 504

	if benchData == nil {
		benchCloses := make([]float64, nDays)
		benchOpens := make([]float64, nDays)
		benchVolumes := make([]float64, nDays)
		benchTimestamps := make([]int64, nDays)
		currBench := 18500.0
		for i := 0; i < nDays; i++ {
			currBench += currBench * (0.15/252.0 + (localRand.Float64()-0.5)*0.015)
			benchCloses[i] = currBench
			benchOpens[i] = currBench * (1.0 + (localRand.Float64()-0.5)*0.005)
			benchVolumes[i] = 1000000.0 + localRand.Float64()*500000.0
			benchTimestamps[i] = time.Now().AddDate(0, 0, -(nDays - 1 - i)).Unix()
		}
		benchData = &yfinance.HistoricalData{Timestamps: benchTimestamps, Closes: benchCloses, Opens: benchOpens, Volumes: benchVolumes}
	}

	mockMeta := map[string]struct {
		sector    string
		cagr      float64
		ttm       float64
		dsoPrev   float64
		dsoLatest float64
		retTrend  float64
	}{
		"NSE:ATLANTAELE": {sector: "Industrials", cagr: 0.287, ttm: 0.488, dsoPrev: 103.2, dsoLatest: 83.6, retTrend: 0.45},
		"NSE:THYROCARE":  {sector: "Healthcare", cagr: 0.167, ttm: 0.218, dsoPrev: 39.1, dsoLatest: 32.8, retTrend: 0.20},
		"NSE:NETWEB":     {sector: "Technology", cagr: 0.704, ttm: 0.900, dsoPrev: 115.2, dsoLatest: 112.0, retTrend: 0.75},
		"NSE:AEGISLOG":   {sector: "Energy", cagr: -0.011, ttm: 0.232, dsoPrev: 37.4, dsoLatest: 21.1, retTrend: 0.18},
		"NSE:NH":         {sector: "Healthcare", cagr: 0.207, ttm: 0.440, dsoPrev: 37.0, dsoLatest: 30.3, retTrend: 0.35},
		"NSE:MINDACORP":  {sector: "Industrials", cagr: 0.135, ttm: 0.223, dsoPrev: 59.7, dsoLatest: 58.7, retTrend: 0.22},
		"NSE:CCAVENUE":   {sector: "Technology", cagr: 0.605, ttm: 1.033, dsoPrev: 8.2, dsoLatest: 5.1, retTrend: 0.90},
		"NSE:APLLTD":     {sector: "Healthcare", cagr: 0.095, ttm: 0.123, dsoPrev: 78.1, dsoLatest: 74.2, retTrend: 0.10},
		"NSE:CHALET":     {sector: "Consumer Cyclical", cagr: 0.349, ttm: 0.612, dsoPrev: 13.9, dsoLatest: 9.1, retTrend: 0.55},
		"NSE:BELRISE":    {sector: "Industrials", cagr: 0.142, ttm: 0.147, dsoPrev: 70.0, dsoLatest: 67.0, retTrend: 0.14},
		"NSE:ETHOSLTD":   {sector: "Consumer Cyclical", cagr: 0.269, ttm: 0.288, dsoPrev: 5.0, dsoLatest: 4.0, retTrend: 0.27},
		"NSE:SMLMAH":     {sector: "Industrials", cagr: 0.159, ttm: 0.192, dsoPrev: 41.0, dsoLatest: 35.0, retTrend: 0.18},
	}

	mockUsed := false
	mockedTickers := make(map[string]bool)

	for _, t := range tickers {
		h, hasLiveHist := liveHist[t]
		f, hasLiveFund := liveFunds[t]
		if hasLiveHist && hasLiveFund {
			histData[t] = h
			fundamentals[t] = f
		} else {
			mockUsed = true
			mockedTickers[t] = true
			meta, exists := mockMeta[t]
			if !exists {
				meta = struct {
					sector    string
					cagr      float64
					ttm       float64
					dsoPrev   float64
					dsoLatest float64
					retTrend  float64
				}{sector: "General", cagr: 0.10, ttm: 0.12, dsoPrev: 50.0, dsoLatest: 45.0, retTrend: 0.12}
			}
			fundamentals[t] = yfinance.Fundamentals{
				Sector:                  meta.sector,
				HeldPercentInstitutions: 0.15,
				TTMRevenue:              100.0 * (1.0 + meta.ttm),
				AnnualRevenue: []yfinance.AnnualMetric{
					{Date: "2021-03-31", Value: 100.0 / math.Pow(1.0+meta.cagr, 3.0)},
					{Date: "2022-03-31", Value: 100.0 / math.Pow(1.0+meta.cagr, 2.0)},
					{Date: "2023-03-31", Value: 100.0 / (1.0 + meta.cagr)},
					{Date: "2024-03-31", Value: 100.0},
				},
				AnnualAccountsReceivable: []yfinance.AnnualMetric{
					{Date: "2023-03-31", Value: (meta.dsoPrev / 365.0) * (100.0 / (1.0 + meta.cagr))},
					{Date: "2024-03-31", Value: (meta.dsoLatest / 365.0) * 100.0},
				},
			}
			tickerNDays := nDays
			if benchData != nil {
				tickerNDays = len(benchData.Closes)
			}
			closes := make([]float64, tickerNDays)
			opens := make([]float64, tickerNDays)
			volumes := make([]float64, tickerNDays)
			timestamps := make([]int64, tickerNDays)
			currPrice := 500.0 + localRand.Float64()*1000.0
			for i := 0; i < tickerNDays; i++ {
				currPrice += currPrice * (meta.retTrend/252.0 + (localRand.Float64()-0.5)*0.02)
				closes[i] = currPrice
				opens[i] = currPrice * (1.0 + (localRand.Float64()-0.5)*0.008)
				volumes[i] = 10000.0 + localRand.Float64()*50000.0
				if benchData != nil {
					timestamps[i] = benchData.Timestamps[i]
				} else {
					timestamps[i] = time.Now().AddDate(0, 0, -(tickerNDays - 1 - i)).Unix()
				}
			}
			histData[t] = &yfinance.HistoricalData{Timestamps: timestamps, Closes: closes, Opens: opens, Volumes: volumes}
		}
	}
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
	fmt.Fprintf(writer, "Policy Preset:    %s\n", strings.Title(style))
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
		PurchaseDate interface{} `yaml:"purchase_date"`
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
	if slice, ok := cfg.PurchaseDate.([]interface{}); ok && len(slice) > 0 {
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
		Strategy interface{} `yaml:"strategy"`
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
	if slice, ok := cfg.Strategy.([]interface{}); ok && len(slice) > 0 {
		if s, ok := slice[0].(string); ok {
			return s
		}
	}
	return "balanced"
}
