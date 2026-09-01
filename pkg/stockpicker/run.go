package stockpicker

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/selectiontracker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// PickResult holds the structured output of a stock selection run.
// Callers can use this to persist results to DuckDB or other stores.
type PickResult struct {
	SelectedKeys []string
	Weights      map[string]float64       // ticker → weight
	Scores       map[string]float64       // ticker → score (nil for standard method)
	Sectors      map[string]string        // ticker → sector
	Ranks        map[string]int           // ticker → 1-based raw rank at selection time
	Drivers      map[string]DriverMetrics // ticker → structured driver metrics
}

// DriverMetrics mirrors selectiontracker.DriverMetrics as the structured numeric
// drivers surfaced on PickResult, so downstream persistence (the DuckDB selections
// table) can consume them without importing selectiontracker.
type DriverMetrics struct {
	TTMGrowth   float64
	RevenueCAGR float64
	DSODelta    float64
	RSI         float64
	Momentum1Y  float64
	FCFYield    float64
	ROIC        float64
}

// Run executes the full stock selection pipeline for the given options.
func Run(ctx context.Context, opts *Options) error {
	_, err := RunWithResult(ctx, opts)
	return err
}

// RunWithResult executes the full stock selection pipeline and returns structured results
// alongside writing the CSV output. The PickResult can be used to persist data to DuckDB.
//
// If opts.DataFetcher is set, fundamentals and historical data are fetched through it
// (routing US tickers to Schwab, others to Yahoo). Otherwise, falls back to direct yfinance calls.
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

	fullHistory, activeKeys := fetchHistoricalPricesVia(ctx, opts.DataFetcher, combinedTickers)
	if len(activeKeys) == 0 {
		fmt.Println("No active tickers loaded. Exiting...")
		return nil, nil
	}

	slicedPrices, benchmarkPrices, err := getBenchmarkAndSlicedPricesVia(ctx, opts.DataFetcher, tickersSrc.Name, activeKeys, fullHistory, rangeStr)
	if err != nil {
		return nil, fmt.Errorf("fetching benchmark prices: %w", err)
	}

	cfg, err := LoadStrategyConfig(opts.Method)
	if err != nil {
		fmt.Printf("Warning: Failed to load config/mfs.json: %v. Using defaults.\n", err)
	}

	fundamentals, err := fetchFundamentalsVia(ctx, opts.DataFetcher, activeKeys)
	if err != nil {
		fmt.Printf("Warning: Failed to fetch fundamentals: %v. Using fallbacks.\n", err)
	}

	InjectGovernance(fundamentals, cfg.Governance)
	tracker := selectiontracker.New()

	if opts.Method == "us_quality_momentum" {
		// US-specific hard filters (market cap, ADV, positive FCF only)
		activeKeys = ApplyUSHardFilters(ctx, activeKeys, cfg.HardFilters, fundamentals, tracker)
	} else if cfg.HardFilters != nil {
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
	} else if opts.Method == "us_quality_momentum" {
		scores = ScoreUSQualityMomentum(ctx, activeKeys, fundamentals, fullHistory, cfg.HardFilters)
		selectedKeys = SelectTopNUSQM(activeKeys, scores, fundamentals, cfg.HardFilters, opts.TopN, goldenWeights, opts.HysteresisBuffer, tracker)
		finalWeights = NormalizeUSQMWeights(selectedKeys, scores, fundamentals, cfg.HardFilters, goldenWeights, opts.RebalanceTolerance)
	} else {
		selectedKeys = SelectTopNStandard(activeKeys, slicedPrices, benchmarkPrices, fundamentals, cfg.Weights, opts.TopN, goldenWeights, opts.HysteresisBuffer, tracker)
		finalWeights = NormalizeStandardWeights(selectedKeys, slicedPrices, benchmarkPrices, fundamentals, cfg.Weights, goldenWeights, opts.RebalanceTolerance)
	}

	sort.Slice(selectedKeys, func(i, j int) bool {
		return finalWeights[selectedKeys[i]] > finalWeights[selectedKeys[j]]
	})

	if opts.Method == "value" || opts.Method == "multibagger" || opts.Method == "us_quality_momentum" {
		PrintMultibaggerTable(selectedKeys, finalWeights, scores, fundamentals, fullHistory, displayNameVal, opts.Method)
		if !opts.SkipScuttlebutt && opts.Method != "us_quality_momentum" {
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
	prevDrivers := loadPreviousDriverStrings(ctx, displayNameVal, opts.Method)
	if err := tracker.SaveReport(displayNameVal, opts.Method, goldenWeights, sectors, finalWeights, resultDates, prevDrivers); err != nil {
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
		Ranks:        make(map[string]int, len(selectedKeys)),
		Drivers:      make(map[string]DriverMetrics, len(selectedKeys)),
	}
	for _, k := range selectedKeys {
		if f, ok := fundamentals[k]; ok {
			result.Sectors[k] = f.Sector
		}
		if r, ok := tracker.RawRanks[k]; ok {
			result.Ranks[k] = r
		}
		if dm, ok := tracker.DriverValues[k]; ok {
			result.Drivers[k] = DriverMetrics{
				TTMGrowth:   dm.TTMGrowth,
				RevenueCAGR: dm.RevenueCAGR,
				DSODelta:    dm.DSODelta,
				RSI:         dm.RSI,
				Momentum1Y:  dm.Momentum1Y,
				FCFYield:    dm.FCFYield,
				ROIC:        dm.ROIC,
			}
		}
	}

	return result, nil
}

// loadPreviousDriverStrings fetches the previous completed run's structured
// selections from DuckDB and reconstructs each ticker's driver summary string in
// the same format the given scoring method emits. This replaces selectiontracker's
// old approach of re-parsing the prior text report (roadmap Phase 8). Returns nil
// when the cache is unavailable or there is no prior run — callers treat nil as
// "first run" (no cross-run delta).
func loadPreviousDriverStrings(ctx context.Context, portfolio, method string) map[string]string {
	db := cache.GetDB()
	if db == nil {
		return nil
	}
	prev, err := db.GetPreviousSelections(ctx, portfolio, method)
	if err != nil || len(prev) == 0 {
		return nil
	}
	out := make(map[string]string, len(prev))
	for _, s := range prev {
		out[s.Ticker] = formatDriverStringFromMetrics(method, s)
	}
	return out
}

// formatDriverStringFromMetrics rebuilds the human-readable driver summary from a
// stored selection's numeric metrics, matching the per-method format produced at
// scoring time so formatDriverDelta can parse both sides consistently.
func formatDriverStringFromMetrics(method string, s cache.Selection) string {
	switch method {
	case "multibagger":
		// Note: institutional stake is not persisted structurally; omit it from the
		// reconstruction. formatDriverDelta tolerates a missing trailing metric.
		return fmt.Sprintf("TTM Growth: %+.1f%% (3Y: %+.1f%%), ROCE: %.1f%%",
			s.TTMGrowth*100.0, s.RevenueCagr*100.0, s.ROIC*100.0)
	case "value":
		return fmt.Sprintf("Forward PE: %.1f, FCF Yield: %.1f%%", 0.0, s.FCFYield*100.0)
	default:
		// us_quality_momentum and others: no delta format is defined; return the
		// current-style summary so the report still shows values.
		return fmt.Sprintf("ROIC: %.1f%%, FCF Yield: %.1f%%", s.ROIC*100.0, s.FCFYield*100.0)
	}
}

// fetchFundamentalsVia uses the DataFetcher if available, otherwise falls back to yfinance.
func fetchFundamentalsVia(ctx context.Context, fetcher DataFetcher, tickers []string) (map[string]yfinance.Fundamentals, error) {
	if fetcher != nil {
		fmt.Printf("Fetching fundamentals via router...\n")
		return fetcher.FetchFundamentals(ctx, tickers)
	}
	fmt.Printf("Fetching fundamentals from Yahoo Finance...\n")
	return yfinance.FetchFundamentals(ctx, tickers)
}

// fetchHistoricalPricesVia uses the DataFetcher if available for historical data,
// otherwise falls back to the direct yfinance concurrent pool.
func fetchHistoricalPricesVia(ctx context.Context, fetcher DataFetcher, rawTickers []string) (map[string]*yfinance.HistoricalData, []string) {
	if fetcher == nil {
		return FetchHistoricalPrices(ctx, rawTickers)
	}
	return fetchHistoricalPricesWithFetcher(ctx, fetcher, rawTickers)
}

// getBenchmarkAndSlicedPricesVia routes the benchmark fetch through the DataFetcher if available.
func getBenchmarkAndSlicedPricesVia(ctx context.Context, fetcher DataFetcher, indexName string, activeKeys []string, fullHistory map[string]*yfinance.HistoricalData, rangeStr string) (map[string][]float64, []float64, error) {
	if fetcher == nil {
		return GetBenchmarkAndSlicedPrices(ctx, indexName, activeKeys, fullHistory, rangeStr)
	}

	benchSym := GetBenchmarkSymbolForIndex(indexName, activeKeys)
	fmt.Printf("Fetching historical benchmark prices for %s (%s)...\n", benchSym, rangeStr)
	benchmarkPrices, err := fetcher.FetchHistoricalPrices(ctx, benchSym, rangeStr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch benchmark %s: %w", benchSym, err)
	}

	slicedPriceHistory := make(map[string][]float64)
	for _, t := range activeKeys {
		prices := fullHistory[t].Closes
		if len(prices) > len(benchmarkPrices) {
			slicedPriceHistory[t] = prices[len(prices)-len(benchmarkPrices):]
		} else {
			slicedPriceHistory[t] = prices
		}
	}

	return slicedPriceHistory, benchmarkPrices, nil
}
