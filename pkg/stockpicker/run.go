package stockpicker

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/selectiontracker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// PickResult holds the structured output of a stock selection run.
// Callers can use this to persist results to DuckDB or other stores.
type PickResult struct {
	SelectedKeys []string
	Weights      map[string]float64 // ticker → weight
	Scores       map[string]float64 // ticker → score (nil for standard method)
	Sectors      map[string]string  // ticker → sector
}

// Run executes the full stock selection pipeline for the given options.
// This is the exported, non-interactive equivalent of cmd/pick.go's runPickWithOpts.
func Run(ctx context.Context, opts *Options) error {
	_, err := RunWithResult(ctx, opts)
	return err
}

// RunWithResult executes the full stock selection pipeline and returns structured results
// alongside writing the CSV output. The PickResult can be used to persist data to DuckDB.
func RunWithResult(ctx context.Context, opts *Options) (*PickResult, error) {
	rangeStr := opts.RangeStr
	if rangeStr != "3mo" && rangeStr != "6mo" && rangeStr != "1y" {
		return nil, fmt.Errorf("unsupported range '%s'. Supported ranges: 3mo, 6mo, 1y", rangeStr)
	}

	tickersSrc, err := LoadConstituents(opts.FilePath, opts.IndexName)
	if len(opts.Tickers) > 0 {
		// Use pre-built ticker list directly (from DuckDB or in-memory).
		name := opts.DisplayName
		if name == "" {
			name = "custom"
		}
		tickersSrc = &TickersSource{Name: name, Tickers: opts.Tickers}
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading constituents: %w", err)
	}
	displayNameVal := tickersSrc.Name
	if opts.DisplayName != "" {
		displayNameVal = opts.DisplayName
	}
	PrintHeader(displayNameVal, opts.Method, opts.TopN, rangeStr, opts.FilePath)

	goldenWeights := LoadGoldenWeights(opts.GoldenPath)

	// Combine index tickers with golden copy holdings to ensure existing holdings are evaluated
	allTickersMap := make(map[string]bool)
	var combinedTickers []string
	for _, t := range tickersSrc.Tickers {
		if !allTickersMap[t] {
			allTickersMap[t] = true
			combinedTickers = append(combinedTickers, t)
		}
	}
	for t := range goldenWeights {
		if !allTickersMap[t] {
			allTickersMap[t] = true
			combinedTickers = append(combinedTickers, t)
		}
	}

	fullHistory, activeKeys := FetchHistoricalPrices(ctx, combinedTickers)
	if len(activeKeys) == 0 {
		fmt.Println("No active tickers loaded. Exiting...")
		return nil, nil
	}

	slicedPrices, benchmarkPrices, err := GetBenchmarkAndSlicedPrices(ctx, tickersSrc.Name, activeKeys, fullHistory, rangeStr)
	if err != nil {
		return nil, fmt.Errorf("fetching benchmark prices: %w", err)
	}

	cfg, err := LoadStrategyConfig(opts.Method)
	if err != nil {
		fmt.Printf("Warning: Failed to load config/mfs.json: %v. Using defaults.\n", err)
	}

	fmt.Printf("Fetching fundamentals from Yahoo Finance...\n")
	fundamentals, err := yfinance.FetchFundamentals(ctx, activeKeys)
	if err != nil {
		fmt.Printf("Warning: Failed to fetch fundamentals: %v. Using fallbacks.\n", err)
	}

	InjectGovernance(fundamentals, cfg.Governance)
	tracker := selectiontracker.New()

	if cfg.HardFilters != nil {
		activeKeys = ApplySafetyFilters(ctx, activeKeys, opts.Method, cfg.HardFilters, fundamentals, fullHistory, tracker)
	} else {
		tracker.InitialCount = len(activeKeys)
	}

	if len(activeKeys) == 0 {
		fmt.Println("No candidate stocks remaining after hard filters. Exiting...")
		return nil, nil
	}

	var selectedKeys []string
	var finalWeights map[string]float64
	var scores map[string]float64

	if opts.Method == "value" {
		scores = ScoreValue(ctx, activeKeys, fundamentals, fullHistory, cfg.HardFilters)
		selectedKeys = SelectTopNValue(activeKeys, scores, fundamentals, cfg.HardFilters, opts.TopN, goldenWeights, opts.HysteresisBuffer, tracker)
		finalWeights = NormalizeValueWeights(selectedKeys, scores, fundamentals, cfg.HardFilters, goldenWeights, opts.RebalanceTolerance)
	} else if opts.Method == "multibagger" {
		scores = ScoreMultibagger(ctx, activeKeys, fundamentals, fullHistory, cfg.HardFilters)
		selectedKeys = SelectTopNMultibagger(activeKeys, scores, fundamentals, cfg.HardFilters, opts.TopN, goldenWeights, opts.HysteresisBuffer, tracker)
		finalWeights = NormalizeMultibaggerWeights(selectedKeys, scores, fundamentals, cfg.HardFilters, goldenWeights, opts.RebalanceTolerance)
	} else {
		selectedKeys = SelectTopNStandard(activeKeys, slicedPrices, benchmarkPrices, fundamentals, cfg.Weights, opts.TopN, goldenWeights, opts.HysteresisBuffer, tracker)
		finalWeights = NormalizeStandardWeights(selectedKeys, slicedPrices, benchmarkPrices, fundamentals, cfg.Weights, goldenWeights, opts.RebalanceTolerance)
	}

	sort.Slice(selectedKeys, func(i, j int) bool {
		return finalWeights[selectedKeys[i]] > finalWeights[selectedKeys[j]]
	})

	if opts.Method == "value" || opts.Method == "multibagger" {
		PrintMultibaggerTable(selectedKeys, finalWeights, scores, fundamentals, fullHistory, displayNameVal, opts.Method)
		if !opts.SkipScuttlebutt {
			PrintScuttlebutt(selectedKeys, fundamentals, displayNameVal, opts.Method)
		}
	} else {
		PrintStandardTable(selectedKeys, finalWeights, fullHistory, displayNameVal, opts.Method)
	}

	sectors := make(map[string]string)
	resultDates := make(map[string]string)
	for ticker, fund := range fundamentals {
		sectors[ticker] = fund.Sector
		resultDates[ticker] = fund.ResultPrevComing
	}
	if err := tracker.SaveReport(displayNameVal, opts.Method, goldenWeights, sectors, finalWeights, resultDates); err != nil {
		fmt.Printf("Warning: Failed to save selection reasons report: %v\n", err)
	}

	outPath := opts.OutputFile
	if outPath == "" {
		if opts.FilePath != "" {
			dateStr := time.Now().Format("20060102")
			outPath = filepath.Join("data", "candidates", "proposals", fmt.Sprintf("%s_%s_%s.csv", dateStr, displayNameVal, opts.Method))
		} else {
			outPath = filepath.Join("data", "candidates", "index_picks", fmt.Sprintf("%s_%s.csv", displayNameVal, opts.Method))
		}
	}
	if err := SavePortfolioToCSV(selectedKeys, finalWeights, outPath); err != nil {
		return nil, fmt.Errorf("writing output file: %w", err)
	}

	if opts.GoldenPath != "" && len(goldenWeights) > 0 {
		csvloader.PrintComparisonReport(outPath, opts.GoldenPath, opts.Method)
	}

	// Build result for callers that want structured data (e.g., DuckDB persistence).
	result := &PickResult{
		SelectedKeys: selectedKeys,
		Weights:      finalWeights,
		Scores:       scores,
		Sectors:      make(map[string]string, len(selectedKeys)),
	}
	for _, k := range selectedKeys {
		if f, ok := fundamentals[k]; ok {
			result.Sectors[k] = f.Sector
		}
	}

	return result, nil
}
