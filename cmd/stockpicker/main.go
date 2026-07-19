package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gkgarg24/mycase/pkg/selectiontracker"
	"github.com/gkgarg24/mycase/pkg/stockpicker"
	"github.com/gkgarg24/mycase/pkg/yfinance"
)

func main() {
	// 1. CLI flag parsing and validation
	opts, err := parseFlags()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// 2. Load constituents
	tickersSrc, err := stockpicker.LoadConstituents(opts.FilePath, opts.IndexName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	displayNameVal := tickersSrc.Name
	if opts.DisplayName != "" {
		displayNameVal = opts.DisplayName
	}
	stockpicker.PrintHeader(displayNameVal, opts.Method, opts.TopN, opts.RangeStr, opts.FilePath)

	// 3. Fetch historical prices concurrently
	fullHistory, activeKeys := stockpicker.FetchHistoricalPrices(tickersSrc.Tickers)
	if len(activeKeys) == 0 {
		fmt.Println("No active tickers loaded. Exiting...")
		return
	}

	// 4. Fetch benchmark prices and slice price histories
	slicedPrices, benchmarkPrices, err := stockpicker.GetBenchmarkAndSlicedPrices(activeKeys, fullHistory, opts.RangeStr)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// 5. Load strategy configs, hard filters, and governance
	cfg, err := stockpicker.LoadStrategyConfig(opts.Method)
	if err != nil {
		fmt.Printf("Warning: Failed to load config/mfs.json: %v. Using defaults.\n", err)
	}

	// Fetch fundamentals
	fmt.Printf("Fetching fundamentals from Yahoo Finance...\n")
	fundamentals, err := yfinance.FetchFundamentals(activeKeys)
	if err != nil {
		fmt.Printf("Warning: Failed to fetch fundamentals: %v. Using fallbacks.\n", err)
	}

	// Inject pledged percentages into fundamentals
	stockpicker.InjectGovernance(fundamentals, cfg.Governance)

	tracker := selectiontracker.New()

	// 6. Apply Safety/Hard Filters
	if cfg.HardFilters != nil {
		activeKeys = stockpicker.ApplySafetyFilters(activeKeys, opts.Method, cfg.HardFilters, fundamentals, fullHistory, tracker)
	} else {
		tracker.InitialCount = len(activeKeys)
	}

	if len(activeKeys) == 0 {
		fmt.Println("No candidate stocks remaining after hard filters. Exiting...")
		return
	}

	// 7. Scoring and selection
	var selectedKeys []string
	var finalWeights map[string]float64
	var scores map[string]float64

	goldenWeights := stockpicker.LoadGoldenWeights(opts.GoldenPath)

	if opts.Method == "multibagger" {
		scores = stockpicker.ScoreMultibagger(activeKeys, fundamentals, fullHistory, cfg.HardFilters)
		selectedKeys = stockpicker.SelectTopNMultibagger(activeKeys, scores, fundamentals, cfg.HardFilters, opts.TopN, goldenWeights, opts.HysteresisBuffer, tracker)
		finalWeights = stockpicker.NormalizeMultibaggerWeights(selectedKeys, scores, fundamentals, cfg.HardFilters, goldenWeights, opts.RebalanceTolerance)
	} else {
		selectedKeys = stockpicker.SelectTopNStandard(activeKeys, slicedPrices, benchmarkPrices, fundamentals, cfg.Weights, opts.TopN, goldenWeights, opts.HysteresisBuffer, tracker)
		finalWeights = stockpicker.NormalizeStandardWeights(selectedKeys, slicedPrices, benchmarkPrices, fundamentals, cfg.Weights, goldenWeights, opts.RebalanceTolerance)
	}

	// Sort final selection by weight descending
	sort.Slice(selectedKeys, func(i, j int) bool {
		return finalWeights[selectedKeys[i]] > finalWeights[selectedKeys[j]]
	})

	// 8. Display results
	if opts.Method == "multibagger" {
		stockpicker.PrintMultibaggerTable(selectedKeys, finalWeights, scores, fundamentals, fullHistory, displayNameVal)
		if !opts.SkipScuttlebutt {
			stockpicker.PrintScuttlebutt(selectedKeys, fundamentals, displayNameVal, opts.Method)
		}
	} else {
		stockpicker.PrintStandardTable(selectedKeys, finalWeights, fullHistory, displayNameVal, opts.Method)
	}

	// 8.5 Save selection reason report
	sectors := make(map[string]string)
	for ticker, fund := range fundamentals {
		sectors[ticker] = fund.Sector
	}
	if err := tracker.SaveReport(displayNameVal, opts.Method, goldenWeights, sectors, finalWeights); err != nil {
		fmt.Printf("Warning: Failed to save selection reasons report: %v\n", err)
	}

	// 9. Save output to CSV
	outPath := opts.OutputFile
	if outPath == "" {
		if opts.FilePath != "" {
			dateStr := time.Now().Format("20060102")
			outPath = filepath.Join("data", "candidates", "proposals", fmt.Sprintf("%s_%s_%s.csv", dateStr, displayNameVal, opts.Method))
		} else {
			outPath = filepath.Join("data", "candidates", "index_picks", fmt.Sprintf("%s_%s.csv", displayNameVal, opts.Method))
		}
	}
	if err := stockpicker.SavePortfolioToCSV(selectedKeys, finalWeights, outPath); err != nil {
		fmt.Printf("Error writing output file: %v\n", err)
		return
	}
}

// parseFlags registers flags, parses them, and validates the inputs.
func parseFlags() (*stockpicker.Options, error) {
	indexName := flag.String("index", "smallcap250", "Index to pick stocks from (see docs/stockpicker.md for all 21 supported indices)")
	filePath := flag.String("file", "", "Path to custom CSV file containing tickers (takes precedence over -index)")
	method := flag.String("method", "balanced", "Weighting method strategy preset (balanced, aggressive, conservative)")
	topN := flag.Int("top", 20, "Number of top stocks to pick")
	rangeStr := flag.String("range", "3mo", "Historical data range (3mo, 6mo, 1y)")
	skipScuttlebutt := flag.Bool("skip-scuttlebutt", false, "Skip generating qualitative scuttlebutt checklist report")
	golden := flag.String("golden", "", "Path to the existing golden copy CSV file to apply hysteresis buffer and rebalancing band")
	rebalanceTol := flag.Float64("rebalance-tolerance", 0.10, "Rebalancing weight tolerance percentage (e.g. 0.10 for 0.10%)")
	hysteresisBuf := flag.Int("hysteresis-buffer", 5, "Number of extra ranks to allow existing holdings to drift (default 5)")
	displayName := flag.String("name", "", "Custom display name for output files and reports (defaults to index or file base)")
	outputFile := flag.String("out", "", "Custom path to save the output CSV portfolio file (defaults to data/candidates/... folder)")
	flag.Parse()

	// Sanitize and validate range
	rStr := strings.ToLower(strings.TrimSpace(*rangeStr))
	if rStr == "1yr" || rStr == "1year" {
		rStr = "1y"
	}
	if rStr != "3mo" && rStr != "6mo" && rStr != "1y" {
		return nil, fmt.Errorf("unsupported range '%s'. Supported ranges: 3mo, 6mo, 1y", rStr)
	}

	return &stockpicker.Options{
		IndexName:          *indexName,
		FilePath:           *filePath,
		Method:             *method,
		TopN:               *topN,
		RangeStr:           rStr,
		SkipScuttlebutt:    *skipScuttlebutt,
		GoldenPath:         *golden,
		RebalanceTolerance: *rebalanceTol,
		HysteresisBuffer:   *hysteresisBuf,
		DisplayName:        *displayName,
		OutputFile:         *outputFile,
	}, nil
}
