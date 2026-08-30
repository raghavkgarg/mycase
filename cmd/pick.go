package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/pithistory"
	"github.com/raghavkgarg/mycase/pkg/selectiontracker"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

var PickCommand = &cli.Command{
	Name:  "pick",
	Usage: "Select top stocks from an index or file",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "index", Aliases: []string{"i"}, Value: "smallcap250", Usage: "Index to pick stocks from"},
		&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Usage: "Path to custom CSV file (takes precedence over --index)"},
		&cli.StringFlag{Name: "method", Aliases: []string{"m"}, Value: "balanced", Usage: "Scoring strategy (balanced, aggressive, conservative, multibagger, earlymb, value)"},
		&cli.IntFlag{Name: "top", Value: 20, Usage: "Number of top stocks to pick"},
		&cli.StringFlag{Name: "range", Value: "3mo", Usage: "Historical data range (3mo, 6mo, 1y)"},
		&cli.BoolFlag{Name: "skip-scuttlebutt", Usage: "Skip qualitative scuttlebutt checklist report"},
		&cli.StringFlag{Name: "golden", Usage: "Path to golden copy CSV for hysteresis and rebalancing band"},
		&cli.FloatFlag{Name: "rebalance-tolerance", Value: 0.10, Usage: "Rebalancing weight tolerance %% (e.g. 0.10 for 0.10%%)"},
		&cli.IntFlag{Name: "hysteresis-buffer", Value: 5, Usage: "Extra ranks to allow existing holdings to drift"},
		&cli.StringFlag{Name: "name", Usage: "Custom display name for output files"},
		&cli.StringFlag{Name: "out", Usage: "Custom output CSV path"},
	},
	Action: runPick,
}

func runPick(ctx context.Context, c *cli.Command) error {
	return runPickWithOpts(ctx, pickOptsFromCmd(c))
}

func pickOptsFromCmd(c *cli.Command) *stockpicker.Options {
	rangeStr := strings.ToLower(strings.TrimSpace(c.String("range")))
	if rangeStr == "1yr" || rangeStr == "1year" {
		rangeStr = "1y"
	}
	return &stockpicker.Options{
		IndexName:          c.String("index"),
		FilePath:           c.String("file"),
		Method:             c.String("method"),
		TopN:               int(c.Int("top")),
		RangeStr:           rangeStr,
		SkipScuttlebutt:    c.Bool("skip-scuttlebutt"),
		GoldenPath:         c.String("golden"),
		RebalanceTolerance: c.Float("rebalance-tolerance"),
		HysteresisBuffer:   int(c.Int("hysteresis-buffer")),
		DisplayName:        c.String("name"),
		OutputFile:         c.String("out"),
	}
}

func runPickWithOpts(ctx context.Context, opts *stockpicker.Options) error {
	rangeStr := opts.RangeStr
	if rangeStr != "3mo" && rangeStr != "6mo" && rangeStr != "1y" {
		return fmt.Errorf("unsupported range '%s'. Supported ranges: 3mo, 6mo, 1y", rangeStr)
	}

	tickersSrc, err := stockpicker.LoadConstituents(opts.FilePath, opts.IndexName)
	if err != nil {
		return fmt.Errorf("loading constituents: %w", err)
	}
	displayNameVal := tickersSrc.Name
	if opts.DisplayName != "" {
		displayNameVal = opts.DisplayName
	}
	stockpicker.PrintHeader(displayNameVal, opts.Method, opts.TopN, rangeStr, opts.FilePath)

	goldenWeights := stockpicker.LoadGoldenWeights(opts.GoldenPath)

	// Combine index tickers with golden copy holdings to ensure existing holdings are evaluated for safety/exits
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

	fullHistory, activeKeys := stockpicker.FetchHistoricalPrices(ctx, combinedTickers)
	if len(activeKeys) == 0 {
		fmt.Println("No active tickers loaded. Exiting...")
		return nil
	}

	slicedPrices, benchmarkPrices, err := stockpicker.GetBenchmarkAndSlicedPrices(ctx, tickersSrc.Name, activeKeys, fullHistory, rangeStr)
	if err != nil {
		return fmt.Errorf("fetching benchmark prices: %w", err)
	}

	cfg, err := stockpicker.LoadStrategyConfig(opts.Method)
	if err != nil {
		fmt.Printf("Warning: Failed to load config/mfs.json: %v. Using defaults.\n", err)
	}

	fmt.Printf("Fetching fundamentals from Yahoo Finance...\n")
	fundamentals, err := yfinance.FetchFundamentals(ctx, activeKeys)
	if err != nil {
		fmt.Printf("Warning: Failed to fetch fundamentals: %v. Using fallbacks.\n", err)
	}

	stockpicker.InjectGovernance(fundamentals, cfg.Governance)
	tracker := selectiontracker.New()

	if cfg.HardFilters != nil {
		activeKeys = stockpicker.ApplySafetyFilters(ctx, activeKeys, opts.Method, cfg.HardFilters, fundamentals, fullHistory, tracker)
	} else {
		tracker.InitialCount = len(activeKeys)
	}

	if len(activeKeys) == 0 {
		fmt.Println("No candidate stocks remaining after hard filters. Exiting...")
		return nil
	}

	var selectedKeys []string
	var finalWeights map[string]float64
	var scores map[string]float64

	if opts.Method == "value" {
		scores = stockpicker.ScoreValue(ctx, activeKeys, fundamentals, fullHistory, cfg.HardFilters)
		selectedKeys = stockpicker.SelectTopNValue(activeKeys, scores, fundamentals, cfg.HardFilters, opts.TopN, goldenWeights, opts.HysteresisBuffer, tracker)
		finalWeights = stockpicker.NormalizeValueWeights(selectedKeys, scores, fundamentals, cfg.HardFilters, goldenWeights, opts.RebalanceTolerance)
	} else if opts.Method == "earlymb" || opts.Method == "early_multibagger" {
		scores = stockpicker.ScoreEarlyMultibagger(ctx, activeKeys, fundamentals, fullHistory, cfg.HardFilters)
		selectedKeys = stockpicker.SelectTopNEarlyMultibagger(activeKeys, scores, fundamentals, fullHistory, cfg.HardFilters, opts.TopN, goldenWeights, opts.HysteresisBuffer, tracker)
		finalWeights = stockpicker.NormalizeEarlyMultibaggerWeights(selectedKeys, scores, fundamentals, cfg.HardFilters, goldenWeights, opts.RebalanceTolerance)
	} else if opts.Method == "multibagger" {
		scores = stockpicker.ScoreMultibagger(ctx, activeKeys, fundamentals, fullHistory, cfg.HardFilters)
		selectedKeys = stockpicker.SelectTopNMultibagger(activeKeys, scores, fundamentals, cfg.HardFilters, opts.TopN, goldenWeights, opts.HysteresisBuffer, tracker)
		finalWeights = stockpicker.NormalizeMultibaggerWeights(selectedKeys, scores, fundamentals, cfg.HardFilters, goldenWeights, opts.RebalanceTolerance)
	} else {
		selectedKeys = stockpicker.SelectTopNStandard(activeKeys, slicedPrices, benchmarkPrices, fundamentals, cfg.Weights, opts.TopN, goldenWeights, opts.HysteresisBuffer, tracker)
		finalWeights = stockpicker.NormalizeStandardWeights(selectedKeys, slicedPrices, benchmarkPrices, fundamentals, cfg.Weights, goldenWeights, opts.RebalanceTolerance)
	}

	sort.Slice(selectedKeys, func(i, j int) bool {
		return finalWeights[selectedKeys[i]] > finalWeights[selectedKeys[j]]
	})

	if opts.Method == "earlymb" || opts.Method == "early_multibagger" {
		stockpicker.PrintEarlyMultibaggerTable(selectedKeys, finalWeights, scores, fundamentals, fullHistory, displayNameVal, opts.Method)
		if !opts.SkipScuttlebutt {
			stockpicker.PrintScuttlebutt(selectedKeys, fundamentals, displayNameVal, opts.Method)
		}
	} else if opts.Method == "value" || opts.Method == "multibagger" {
		stockpicker.PrintMultibaggerTable(selectedKeys, finalWeights, scores, fundamentals, fullHistory, displayNameVal, opts.Method)
		if !opts.SkipScuttlebutt {
			stockpicker.PrintScuttlebutt(selectedKeys, fundamentals, displayNameVal, opts.Method)
		}
	} else {
		stockpicker.PrintStandardTable(selectedKeys, finalWeights, fullHistory, displayNameVal, opts.Method)
	}

	sectors := make(map[string]string)
	resultDates := make(map[string]string)
	for ticker, fund := range fundamentals {
		sectors[ticker] = fund.Sector
		resultDates[ticker] = fund.ResultPrevComing
	}

	// 1. Structural Funnel Accounting Validation
	if _, fErr := tracker.BuildFunnel(); fErr != nil {
		fmt.Printf("⚠️  Funnel Validation Notice: %v\n", fErr)
	}

	// 2. PIT Snapshot Persistence & Run-to-Run Diffing (Priorities 2, 7 & 8)
	todayStr := time.Now().Format("2006-01-02")
	candidateMap := make(map[string]stockpicker.CandidateScoreDetail)
	selectedSet := make(map[string]bool)
	for _, s := range selectedKeys {
		selectedSet[s] = true
	}

	rRegime := 1.0
	if tracker.RegimeMultiplier > 0 {
		rRegime = tracker.RegimeMultiplier
	} else if len(benchmarkPrices) >= 50 {
		rRegime = yfinance.CalculateSmoothedBenchmarkRegime(benchmarkPrices, 50, 0.20)
	}

	// Add all constituents to candidateMap
	for _, t := range combinedTickers {
		reason, isSafetyDrop := tracker.SafetyReasons[t]
		rawScore, hasRaw := tracker.RawScores[t]
		effScore, hasEff := tracker.EffectiveScores[t]
		if !hasEff && hasRaw {
			effScore = rawScore * rRegime
		}

		var compRS, vcpRatio, rvolZ, ppScore, delivDelta float64
		if hist, ok := fullHistory[t]; ok && len(hist.Closes) >= 60 {
			compRS, _, _, _ = yfinance.CalculateCompositeRS(hist.Closes, benchmarkPrices)
			vcpRatio, _ = yfinance.CalculateVCPTightness(hist.Closes, hist.Opens)
			rvolZ = yfinance.CalculateWinsorizedRVOLZScore(hist.Volumes, 5, 50, 4.0)
			ppScore, _ = yfinance.CalculateDecayedPocketPivot(hist.Closes, hist.Opens, hist.Volumes, 10, 0.25)
			delivDelta = (fundamentals[t].DeliveryPct / 100.0) - 0.35
		}

		candidateMap[t] = stockpicker.CandidateScoreDetail{
			Ticker:          t,
			PassedStage1:    !isSafetyDrop && hasRaw,
			RejectionReason: reason,
			RawScore:        rawScore,
			EffectiveScore:  effScore,
			CompositeRS:     compRS,
			VCPRatio:        vcpRatio,
			RVOLZScore:      rvolZ,
			DecayedPP:       ppScore,
			DeliveryDelta:   delivDelta,
			Selected:        selectedSet[t],
			FinalWeight:     finalWeights[t],
			Sector:          sectors[t],
		}
	}

	currentSnap := &stockpicker.PITRunSnapshot{
		AsOfDate:          todayStr,
		IndexName:         displayNameVal,
		Method:            opts.Method,
		RegimeMultiplier:  rRegime,
		TotalConstituents: len(combinedTickers),
		Stage1Count:       len(activeKeys),
		SelectedCount:     len(selectedKeys),
		Candidates:        candidateMap,
	}

	if prevSnap, pErr := stockpicker.LoadPreviousSnapshot(displayNameVal, opts.Method, todayStr); pErr == nil && prevSnap != nil {
		diff := stockpicker.DiffSnapshots(prevSnap, currentSnap)
		stockpicker.PrintDiffReport(diff)
	}

	if snapPath, sErr := stockpicker.SaveRunSnapshot(currentSnap); sErr == nil {
		fmt.Printf("Saved PIT Run Snapshot to %s\n", snapPath)
	}

	if pitDB, err := pithistory.Open(""); err == nil {
		if err := pitDB.SaveRunSnapshot(ctx, currentSnap); err == nil {
			fmt.Printf("Persisted Point-in-Time Run Snapshot to DuckDB (%s)\n", pithistory.DefaultDBPath)
		} else {
			fmt.Printf("Warning: Failed to persist to DuckDB: %v\n", err)
		}
		pitDB.Close()
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
	if err := stockpicker.SavePortfolioToCSV(selectedKeys, finalWeights, outPath); err != nil {
		return fmt.Errorf("writing output file: %w", err)
	}

	if opts.GoldenPath != "" && len(goldenWeights) > 0 {
		csvloader.PrintComparisonReport(outPath, opts.GoldenPath, opts.Method)
	}

	return nil
}
