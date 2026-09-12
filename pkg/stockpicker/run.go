package stockpicker

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/marketdata"
	"github.com/raghavkgarg/mycase/pkg/selectiontracker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// PickResult holds the structured output of a stock selection run.
// Callers can use this to persist results to DuckDB or other stores.
type PickResult struct {
	Weights      map[string]float64       // ticker → weight
	Scores       map[string]float64       // ticker → score (nil for standard method)
	Sectors      map[string]string        // ticker → sector
	Ranks        map[string]int           // ticker → 1-based raw rank at selection time
	Drivers      map[string]DriverMetrics // ticker → structured driver metrics
	PITSnapshot  *PITRunSnapshot          // point-in-time run snapshot for DuckDB persistence by the command layer
	SelectedKeys []string
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
	basedOnStr := opts.BasedOn
	if basedOnStr == "" {
		if opts.AsOfDate != "" {
			basedOnStr = opts.AsOfDate + " 21:00:00 IST"
		} else {
			basedOnStr = marketdata.LastSettledEODTime(time.Now()).Format("2006-01-02 15:04:05 MST")
		}
	}
	PrintHeader(displayNameVal, opts.Method, opts.TopN, rangeStr, opts.FilePath, basedOnStr)

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

	fullHistory, activeKeys, failedKeys := fetchHistoricalPricesVia(ctx, opts.DataFetcher, combinedTickers)
	if len(activeKeys) == 0 {
		slog.WarnContext(ctx, "pick.no_active_tickers", "index", tickersSrc.Name)
		return nil, nil
	}

	slicedPrices, benchmarkPrices, err := getBenchmarkAndSlicedPricesVia(ctx, opts.DataFetcher, tickersSrc.Name, activeKeys, fullHistory, rangeStr)
	if err != nil {
		return nil, fmt.Errorf("fetching benchmark prices: %w", err)
	}

	cfg, err := LoadStrategyConfig(opts.Method)
	if err != nil {
		slog.WarnContext(ctx, "pick.config_load_failed", "path", "config/mfs.json", "err", err, "fallback", "defaults")
	}

	fundamentals, err := fetchFundamentalsVia(ctx, opts.DataFetcher, activeKeys)
	if err != nil {
		slog.WarnContext(ctx, "pick.fundamentals_fetch_failed", "err", err, "fallback", "imputed")
	}

	// Backfill sectors from the constituents CSV where the provider left them
	// empty (Schwab returns no sector for US tickers). Fixes US sector caps
	// collapsing to "Unknown" (Phase 10a). No-op when the CSV carries no sector.
	InjectSectors(fundamentals, tickersSrc.Sectors)

	InjectGovernance(fundamentals, cfg.Governance)
	tracker := selectiontracker.New()
	tracker.BasedOn = basedOnStr
	// Record upstream data-fetch failures as a first-class funnel bucket (EBM
	// DataFetchFailed thread) so failed tickers don't contaminate Stage-1
	// survivor quantiles and are diffed separately from genuine gate exits.
	for _, f := range failedKeys {
		tracker.RecordFetchFailure(f, "DATA_FETCH_FAILED: historical price bars unavailable")
	}

	if opts.Method == "us_quality_momentum" {
		// US-specific hard filters (market cap, ADV, positive FCF only)
		activeKeys = ApplyUSHardFilters(ctx, activeKeys, cfg.HardFilters, fundamentals, tracker)
	} else if cfg.HardFilters != nil {
		activeKeys = ApplySafetyFilters(ctx, activeKeys, opts.Method, cfg.HardFilters, fundamentals, fullHistory, tracker, goldenWeights)
	} else {
		tracker.InitialCount = len(activeKeys)
	}

	if len(activeKeys) == 0 {
		slog.WarnContext(ctx, "pick.no_candidates_after_filters")
		return nil, nil
	}

	// For EarlyMB, ensure Stage-1 survivors have full delivery history (>= 25 sessions)
	// for Pillar 4 institutional accumulation delta scoring and PIT snapshotting.
	if (opts.Method == "earlymb" || opts.Method == "early_multibagger") && len(activeKeys) > 0 {
		enrichDeliveryHistory(ctx, activeKeys, fundamentals)
	}

	var selectedKeys []string
	var finalWeights map[string]float64
	var scores map[string]float64

	// Anti-churn cooldown: block recently-exited tickers from re-entering for a
	// configurable window (unless they rank high enough to bypass). Loaded from
	// prior selection reports keyed on the golden universe name.
	goldenBase := csvloader.GetUniverseName(opts.GoldenPath)
	if goldenBase == "" {
		goldenBase = displayNameVal
	}
	recentExits := LoadRecentExits(goldenBase, goldenWeights, opts.CooldownDays, time.Now())
	if len(recentExits) > 0 {
		slog.InfoContext(ctx, "pick.cooldown_loaded", "recent_exits", len(recentExits), "cooldown_days", opts.CooldownDays)
	}

	smartHysteresis := SmartHysteresisConfig{
		MinScoreDelta:             opts.HysteresisMinScoreDelta,
		RequireGrowthAcceleration: opts.HysteresisRequireGrowthAcceleration,
	}

	if opts.Method == "value" {
		scores = ScoreValue(ctx, activeKeys, fundamentals, fullHistory, cfg.HardFilters)
		selectedKeys = SelectTopNValueWithCooldown(activeKeys, scores, fundamentals, cfg.HardFilters, opts.TopN, goldenWeights, opts.HysteresisBuffer, tracker, recentExits, opts.CooldownDays, opts.CooldownBypassRank, smartHysteresis)
		finalWeights = NormalizeValueWeights(selectedKeys, scores, fundamentals, cfg.HardFilters, goldenWeights, opts.RebalanceTolerance)
	} else if opts.Method == "multibagger" {
		scores = ScoreMultibagger(ctx, activeKeys, fundamentals, fullHistory, cfg.HardFilters)
		selectedKeys = SelectTopNMultibaggerWithCooldown(activeKeys, scores, fundamentals, cfg.HardFilters, opts.TopN, goldenWeights, opts.HysteresisBuffer, tracker, recentExits, opts.CooldownDays, opts.CooldownBypassRank, smartHysteresis)
		finalWeights = NormalizeMultibaggerWeights(selectedKeys, scores, fundamentals, cfg.HardFilters, goldenWeights, opts.RebalanceTolerance)
	} else if opts.Method == "earlymb" || opts.Method == "early_multibagger" {
		scores = ScoreEarlyMultibagger(ctx, activeKeys, fundamentals, fullHistory, cfg.HardFilters)
		selectedKeys = SelectTopNEarlyMultibaggerWithCooldown(activeKeys, scores, fundamentals, fullHistory, cfg.HardFilters, opts.TopN, goldenWeights, opts.HysteresisBuffer, tracker, recentExits, opts.CooldownDays, opts.CooldownBypassRank, smartHysteresis)
		finalWeights = NormalizeEarlyMultibaggerWeights(selectedKeys, scores, fundamentals, cfg.HardFilters, goldenWeights, opts.RebalanceTolerance)
	} else if opts.Method == "us_quality_momentum" {
		scores = ScoreUSQualityMomentum(ctx, activeKeys, fundamentals, fullHistory, cfg.HardFilters)
		selectedKeys = SelectTopNUSQMWithCooldown(activeKeys, scores, fundamentals, fullHistory, cfg.HardFilters, opts.TopN, goldenWeights, opts.HysteresisBuffer, tracker, recentExits, opts.CooldownDays, opts.CooldownBypassRank, smartHysteresis)
		finalWeights = NormalizeUSQMWeights(selectedKeys, scores, fundamentals, cfg.HardFilters, goldenWeights, opts.RebalanceTolerance)
	} else {
		selectedKeys = SelectTopNStandardWithCooldown(activeKeys, slicedPrices, benchmarkPrices, fundamentals, cfg.Weights, opts.TopN, goldenWeights, opts.HysteresisBuffer, tracker, recentExits, opts.CooldownDays, opts.CooldownBypassRank, smartHysteresis)
		finalWeights = NormalizeStandardWeights(selectedKeys, slicedPrices, benchmarkPrices, fundamentals, cfg.Weights, goldenWeights, opts.RebalanceTolerance)
	}

	sort.Slice(selectedKeys, func(i, j int) bool {
		return finalWeights[selectedKeys[i]] > finalWeights[selectedKeys[j]]
	})

	if opts.Method == "earlymb" || opts.Method == "early_multibagger" {
		PrintEarlyMultibaggerTable(selectedKeys, finalWeights, scores, fundamentals, fullHistory, displayNameVal, opts.Method)
		if !opts.SkipScuttlebutt {
			PrintScuttlebutt(selectedKeys, fundamentals, displayNameVal, opts.Method)
		}
	} else if opts.Method == "value" || opts.Method == "multibagger" || opts.Method == "us_quality_momentum" {
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

	// Structural funnel accounting validation (non-fatal).
	if _, fErr := tracker.BuildFunnel(); fErr != nil {
		slog.WarnContext(ctx, "pick.funnel_validation", "err", fErr)
	}

	// Build a point-in-time run snapshot: file-based save + run-to-run diff here
	// (all within pkg/stockpicker). DuckDB persistence is done by the command layer
	// (cmd/pick.go) to keep stockpicker from importing pithistory (layering: pithistory
	// imports stockpicker, so the reverse edge would be a cycle).
	todayStr := opts.AsOfDate
	if todayStr == "" {
		todayStr = marketdata.EODSettlementDate(time.Now()).Format("2006-01-02")
	}
	rRegime := 1.0
	if tracker.RegimeMultiplier > 0 {
		rRegime = tracker.RegimeMultiplier
	} else if len(benchmarkPrices) >= 50 {
		rRegime = yfinance.CalculateSmoothedBenchmarkRegime(benchmarkPrices, 50, 0.20)
	}
	selectedSet := make(map[string]bool, len(selectedKeys))
	for _, s := range selectedKeys {
		selectedSet[s] = true
	}
	failedSet := make(map[string]bool, len(failedKeys))
	for _, f := range failedKeys {
		failedSet[f] = true
	}
	candidateMap := make(map[string]CandidateScoreDetail, len(combinedTickers))
	for _, t := range combinedTickers {
		isFetchFailed := failedSet[t]
		reason, isSafetyDrop := tracker.SafetyReasons[t]
		if isFetchFailed {
			reason = "DATA_FETCH_FAILED: historical price bars unavailable"
		}
		rawScore, hasRaw := tracker.RawScores[t]
		effScore, hasEff := tracker.EffectiveScores[t]
		if !hasEff && hasRaw {
			effScore = rawScore * rRegime
		}
		var compRS, vcpRatio, rvolZ, ppScore, delivDelta float64
		var delivInsufficient bool
		if hist, ok := fullHistory[t]; ok && len(hist.Closes) >= 60 {
			compRS, _, _, _ = yfinance.CalculateCompositeRS(hist.Closes, benchmarkPrices, t)
			vcpRatio, _ = yfinance.CalculateVCPTightness(hist.Closes, hist.Opens)
			rvolZ = yfinance.CalculateWinsorizedRVOLZScore(hist.Volumes, 5, 50, 4.0)
			ppScore, _ = yfinance.CalculateDecayedPocketPivot(hist.Closes, hist.Opens, hist.Volumes, 10, 0.25)
			var dErr error
			delivDelta, _, _, dErr = yfinance.GetDeliveryDelta(fundamentals[t].DeliveryHistory, time.Now(), 1)
			delivInsufficient = (dErr != nil)
		}
		candidateMap[t] = CandidateScoreDetail{
			Ticker:                     t,
			PassedStage1:               !isFetchFailed && !isSafetyDrop && hasRaw,
			DataFetchFailed:            isFetchFailed,
			Pillar4InsufficientHistory: delivInsufficient,
			RejectionReason:            reason,
			RawScore:                   rawScore,
			EffectiveScore:             effScore,
			CompositeRS:                compRS,
			VCPRatio:                   vcpRatio,
			RVOLZScore:                 rvolZ,
			DecayedPP:                  ppScore,
			DeliveryDelta:              delivDelta,
			Selected:                   selectedSet[t],
			FinalWeight:                finalWeights[t],
			Sector:                     sectors[t],
		}
	}
	pitSnapshot := &PITRunSnapshot{
		AsOfDate:          todayStr,
		IndexName:         displayNameVal,
		Method:            opts.Method,
		RegimeMultiplier:  rRegime,
		TotalConstituents: len(combinedTickers),
		Stage1Count:       len(activeKeys),
		SelectedCount:     len(selectedKeys),
		Candidates:        candidateMap,
	}
	if prevSnap, pErr := LoadPreviousSnapshot(displayNameVal, opts.Method, todayStr); pErr == nil && prevSnap != nil {
		PrintDiffReport(DiffSnapshots(prevSnap, pitSnapshot))
	}
	if snapPath, sErr := SaveRunSnapshot(pitSnapshot); sErr == nil {
		slog.InfoContext(ctx, "pick.snapshot_saved", "path", snapPath)
	}

	prevDrivers := loadPreviousDriverStrings(ctx, displayNameVal, opts.Method)
	if err := tracker.SaveReport(displayNameVal, opts.Method, goldenWeights, sectors, finalWeights, resultDates, prevDrivers); err != nil {
		slog.WarnContext(ctx, "pick.report_save_failed", "err", err)
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

	// Pre-breakout incubator watchlist (EBM): high-quality Stage-1 survivors
	// below the regime hurdle, ranked by hurdle gap + VCP tightness. Written on
	// every earlymb run so pullback regimes still surface coiling setups.
	if opts.Method == "earlymb" || opts.Method == "early_multibagger" {
		incubatorPath := filepath.Join("data", "candidates", "index_picks", fmt.Sprintf("%s_%s_incubator.csv", displayNameVal, opts.Method))
		_, _ = GenerateIncubatorWatchlist(activeKeys, scores, fundamentals, fullHistory, tracker, incubatorPath)
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
	result.PITSnapshot = pitSnapshot

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
		slog.InfoContext(ctx, "pick.fundamentals_fetch", "source", "router", "count", len(tickers))
		return fetcher.FetchFundamentals(ctx, tickers)
	}
	slog.InfoContext(ctx, "pick.fundamentals_fetch", "source", "yahoo", "count", len(tickers))
	return yfinance.FetchFundamentals(ctx, tickers)
}

// fetchHistoricalPricesVia uses the DataFetcher if available for historical data,
// otherwise falls back to the direct yfinance concurrent pool.
func fetchHistoricalPricesVia(ctx context.Context, fetcher DataFetcher, rawTickers []string) (map[string]*yfinance.HistoricalData, []string, []string) {
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
	slog.InfoContext(ctx, "pick.benchmark_fetch", "symbol", benchSym, "range", rangeStr)
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

// enrichDeliveryHistory checks if any Stage-1 survivor tickers are missing delivery history
// (less than 25 settled sessions) and batch-fetches the complete series from NSE via Python,
// attaching the history to the in-memory fundamentals map and updating the DuckDB cache.
func enrichDeliveryHistory(ctx context.Context, tickers []string, fundamentals map[string]yfinance.Fundamentals) {
	var missing []string
	for _, t := range tickers {
		if strings.HasPrefix(t, "NSE:") || strings.HasPrefix(t, "BSE:") || strings.HasSuffix(t, ".NS") {
			if f, ok := fundamentals[t]; ok && len(f.DeliveryHistory) < 25 {
				missing = append(missing, t)
			}
		}
	}
	if len(missing) == 0 {
		return
	}

	fmt.Printf("📦 Fetching NSE delivery data for %d Stage-1 candidates...\n", len(missing))
	slog.InfoContext(ctx, "pick.enrich_delivery_history", "count", len(missing))
	delSeries, err := yfinance.FetchNselibDeliveryDataSeries(ctx, missing)
	if err != nil {
		fmt.Printf("⚠️ Warning: NSE delivery fetch failed: %v\n", err)
		slog.WarnContext(ctx, "pick.enrich_delivery_failed", "err", err)
		return
	}

	updatedCount := 0
	for _, t := range missing {
		cleanSym := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(t, "NSE:"), "BSE:"), ".NS")
		var series []yfinance.NSEDeliveryRecord
		if s, ok := delSeries[t]; ok {
			series = s
		} else if s, ok := delSeries[cleanSym]; ok {
			series = s
		}
		if len(series) > 0 {
			f := fundamentals[t]
			f.DeliveryHistory = series
			f.DeliveryPct = series[0].DeliveryPct
			f.DeliveryDate = series[0].Date
			f.DeliverableQty = series[0].DeliverableQty
			fundamentals[t] = f
			yfinance.StoreFundamentalsCache(ctx, t, &f)
			updatedCount++
		}
	}
	fmt.Printf("✅ Delivery history enriched: %d / %d candidates have full delivery history\n", updatedCount, len(missing))
}

