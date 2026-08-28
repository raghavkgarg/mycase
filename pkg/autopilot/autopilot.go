// Package autopilot implements the non-interactive quarterly rebalance pipeline.
// It runs pick → combine → prune → merge golden copy → compute orders → build proposal,
// all without user prompts. The result is a Proposal that can be reviewed and confirmed.
package autopilot

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/broker/schwab"
	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/costs"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/datafetcher"
	"github.com/raghavkgarg/mycase/pkg/optimizer"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
)

// RunConfig holds all parameters needed for a non-interactive autopilot run.
type RunConfig struct {
	PipelineCfg config.PipelineConfig
	Broker      broker.Broker
	ConfigPath  string // path to pipeline.yaml (for report context)
}

// RunResult holds the outcome of an autopilot pipeline run.
type RunResult struct {
	Proposal       *Proposal
	ReportPath     string
	SelectionPaths []string
	GoldenCopyPath string
}

// newDataRouter builds a datafetcher.Router from the pipeline config's Schwab
// credentials. If Schwab is not configured (or creds are missing), the router
// falls back to Yahoo Finance for all tickers.
func newDataRouter(cfg config.PipelineConfig) *datafetcher.Router {
	if !strings.EqualFold(cfg.Broker, "schwab") {
		return datafetcher.NewRouter(nil)
	}

	schwabConfigPath := cfg.SchwabConfig
	if schwabConfigPath == "" {
		schwabConfigPath = "config/schwab.json"
	}
	schwabTokenPath := cfg.SchwabToken
	if schwabTokenPath == "" {
		schwabTokenPath = "config/schwab_token.json"
	}

	app, err := schwab.LoadAppConfig(schwabConfigPath)
	if err != nil {
		fmt.Printf("[autopilot] Schwab config unavailable (%v); US tickers will use Yahoo Finance.\n", err)
		return datafetcher.NewRouter(nil)
	}

	tokenMgr := schwab.NewTokenManager(app, schwabTokenPath)
	return datafetcher.NewRouter(schwab.NewClient(tokenMgr))
}

// Run executes the full non-interactive pipeline:
//  1. Stock selection (per source index/file)
//  2. Combine + prune (if multiple sources)
//  3. Backup + merge golden copy
//  4. Compute rebalance orders
//  5. Build and persist proposal
//
// Returns the proposal for alerting/confirmation. Does not execute orders.
func Run(ctx context.Context, rc RunConfig) (*RunResult, error) {
	cfg := rc.PipelineCfg
	now := time.Now()
	dateStr := now.Format("20060102")
	runTimestamp := now.Format("20060102_150405")

	// Validate config
	if len(cfg.Indices) == 0 && len(cfg.Files) == 0 {
		return nil, fmt.Errorf("no indices or files configured in pipeline config")
	}
	if cfg.GoldenCopyPath == "" {
		return nil, fmt.Errorf("golden_copy_path is required for autopilot")
	}

	goldenBase := csvloader.GetUniverseName(cfg.GoldenCopyPath)

	// Build a data router that routes US tickers to Schwab (if configured) and
	// everything else to Yahoo Finance. Falls back to Yahoo if Schwab creds are absent.
	dataFetcher := newDataRouter(cfg)

	// Clean stale cache files from previous days
	cleanStaleCache()

	// --- Pipeline run tracking (DuckDB) ---
	runID := cache.NewRunID()
	db := cache.GetDB()
	if db != nil {
		pipelineRun := cache.PipelineRun{
			RunID:     runID,
			StartedAt: now,
			Portfolio: goldenBase,
			Method:    cfg.Strategy,
		}
		if err := db.InsertRun(ctx, pipelineRun); err != nil {
			fmt.Printf("[autopilot] Warning: failed to record pipeline run: %v\n", err)
			db = nil // disable further DB writes this run
		} else {
			// Mark run as failed on early exit; overridden by CompleteRun on success.
			defer func() {
				if db != nil {
					_ = db.FailRun(ctx, runID)
				}
			}()
		}
	}

	// --- Step 1: Stock selection per source ---
	type pipelineSource struct {
		name    string
		path    string
		isIndex bool
	}

	var sources []pipelineSource
	for _, f := range cfg.Files {
		if strings.TrimSpace(f) != "" {
			sources = append(sources, pipelineSource{
				name: csvloader.GetUniverseName(f),
				path: f,
			})
		}
	}
	for _, idx := range cfg.Indices {
		if strings.TrimSpace(idx) != "" {
			sources = append(sources, pipelineSource{
				name:    idx,
				isIndex: true,
			})
		}
	}

	var outputCSVs []string
	var selectionPaths []string

	for _, src := range sources {
		outPath := filepath.Join("data", "candidates", "index_picks", fmt.Sprintf("%s_%s.csv", src.name, cfg.Strategy))
		opts := &stockpicker.Options{
			Method:             cfg.Strategy,
			TopN:               cfg.TopN,
			RangeStr:           "3mo",
			GoldenPath:         cfg.GoldenCopyPath,
			RebalanceTolerance: cfg.RebalanceTolerancePct,
			HysteresisBuffer:   cfg.HysteresisRankBuffer,
			OutputFile:         outPath,
			DataFetcher:        dataFetcher,
		}
		if src.isIndex {
			opts.IndexName = src.name
		} else {
			opts.FilePath = src.path
			opts.DisplayName = src.name
		}
		if len(sources) > 1 {
			opts.SkipScuttlebutt = true
		}

		fmt.Printf("[autopilot] Picking from %s (strategy: %s, top %d)...\n", src.name, cfg.Strategy, cfg.TopN)
		result, err := stockpicker.RunWithResult(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("stock selection for %s: %w", src.name, err)
		}
		outputCSVs = append(outputCSVs, outPath)
		selectionPaths = append(selectionPaths, outPath)

		// Persist index picks to DuckDB
		if db != nil && result != nil {
			picks := pickResultToIndexPicks(src.name, result)
			if err := db.InsertIndexPicks(ctx, runID, src.name, picks); err != nil {
				fmt.Printf("[autopilot] Warning: failed to persist index picks for %s: %v\n", src.name, err)
			}
		}
	}

	// --- Step 2: Combine + prune (if multiple sources) ---
	var sourceCSV string
	if len(sources) > 1 {
		// Get combined tickers from DuckDB (eliminates temp combine CSV).
		var combinedTickers []string
		if db != nil {
			allPicks, err := db.GetAllIndexPicks(ctx, runID)
			if err == nil && len(allPicks) > 0 {
				seen := make(map[string]bool, len(allPicks))
				for _, p := range allPicks {
					if !seen[p.Ticker] {
						seen[p.Ticker] = true
						combinedTickers = append(combinedTickers, p.Ticker)
					}
				}
				fmt.Printf("[autopilot] Combined %d unique tickers from DB index_picks.\n", len(combinedTickers))
			}
		}

		// Fallback: combine via CSV if DB path didn't yield tickers.
		if len(combinedTickers) == 0 {
			combineCSV := filepath.Join("data", "candidates", "temp", fmt.Sprintf("combine_%s.csv", goldenBase))
			if err := os.MkdirAll(filepath.Dir(combineCSV), 0755); err != nil {
				return nil, fmt.Errorf("creating temp directory: %w", err)
			}
			fmt.Printf("[autopilot] Combining %d source CSVs (fallback)...\n", len(outputCSVs))
			if err := csvloader.CombineMultipleCSVs(outputCSVs, combineCSV); err != nil {
				return nil, fmt.Errorf("combining CSVs: %w", err)
			}
			// Read tickers from the combined CSV.
			weights, _ := csvloader.ReadCSVWeights(combineCSV)
			for t := range weights {
				combinedTickers = append(combinedTickers, t)
			}
			_ = os.Remove(combineCSV)
		}

		// Pick top N+5 from combined tickers
		proposalTopN := cfg.TopN + 5
		proposalPath := filepath.Join("data", "candidates", "proposals", fmt.Sprintf("%s_%s_%s.csv", dateStr, goldenBase, cfg.Strategy))
		opts := &stockpicker.Options{
			Tickers:            combinedTickers,
			Method:             cfg.Strategy,
			TopN:               proposalTopN,
			RangeStr:           "3mo",
			GoldenPath:         cfg.GoldenCopyPath,
			RebalanceTolerance: cfg.RebalanceTolerancePct,
			HysteresisBuffer:   cfg.HysteresisRankBuffer,
			DisplayName:        goldenBase,
			OutputFile:         proposalPath,
			DataFetcher:        dataFetcher,
		}
		fmt.Printf("[autopilot] Running combined pick (top %d)...\n", proposalTopN)
		draftResult, err := stockpicker.RunWithResult(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("combined pick: %w", err)
		}

		// Persist draft proposals to DuckDB
		if db != nil && draftResult != nil {
			proposals := pickResultToProposals(draftResult)
			if err := db.InsertProposals(ctx, runID, "draft", proposals); err != nil {
				fmt.Printf("[autopilot] Warning: failed to persist draft proposals: %v\n", err)
			}
		}

		// Prune to top N — pass draft result tickers directly (no file re-read).
		optimPath := filepath.Join("data", "candidates", "proposals", fmt.Sprintf("%s_%s_%s_optim.csv", dateStr, goldenBase, cfg.Strategy))
		pruneOpts := &stockpicker.Options{
			Method:             cfg.Strategy,
			TopN:               cfg.TopN,
			RangeStr:           "3mo",
			GoldenPath:         cfg.GoldenCopyPath,
			RebalanceTolerance: cfg.RebalanceTolerancePct,
			HysteresisBuffer:   cfg.HysteresisRankBuffer,
			DisplayName:        goldenBase,
			OutputFile:         optimPath,
			DataFetcher:        dataFetcher,
		}
		if draftResult != nil && len(draftResult.SelectedKeys) > 0 {
			pruneOpts.Tickers = draftResult.SelectedKeys
		} else {
			pruneOpts.FilePath = proposalPath // fallback: read the CSV we just wrote
		}
		fmt.Printf("[autopilot] Pruning to top %d...\n", cfg.TopN)
		optimResult, err := stockpicker.RunWithResult(ctx, pruneOpts)
		if err != nil {
			return nil, fmt.Errorf("prune pick: %w", err)
		}
		sourceCSV = optimPath
		selectionPaths = append(selectionPaths, optimPath)

		// Persist optimized proposals to DuckDB
		if db != nil && optimResult != nil {
			proposals := pickResultToProposals(optimResult)
			if err := db.InsertProposals(ctx, runID, "optimized", proposals); err != nil {
				fmt.Printf("[autopilot] Warning: failed to persist optimized proposals: %v\n", err)
			}
		}
	} else {
		sourceCSV = outputCSVs[0]
	}

	// --- Step 3: Read old weights, backup + merge golden copy ---
	oldWeights, _ := csvloader.ReadCSVWeights(cfg.GoldenCopyPath)

	// Backup
	if _, err := os.Stat(cfg.GoldenCopyPath); err == nil {
		backupDir := filepath.Join("data", "backups", goldenBase)
		_ = os.MkdirAll(backupDir, 0755)
		backupName := fmt.Sprintf("bk_%s.csv", now.Format("20060102_150405"))
		backupPath := filepath.Join(backupDir, backupName)
		if data, err := os.ReadFile(cfg.GoldenCopyPath); err == nil {
			_ = os.WriteFile(backupPath, data, 0644)
			fmt.Printf("[autopilot] Backed up golden copy to %s\n", backupPath)
		}
	}

	// Merge
	fmt.Printf("[autopilot] Merging %s into golden copy %s...\n", sourceCSV, cfg.GoldenCopyPath)
	if err := csvloader.MergeGoldenCopy(sourceCSV, cfg.GoldenCopyPath); err != nil {
		return nil, fmt.Errorf("merging golden copy: %w", err)
	}

	// Read new weights
	newWeights, _ := csvloader.ReadCSVWeights(cfg.GoldenCopyPath)

	// --- Step 4: Generate report ---
	reportPath := filepath.Join("report", fmt.Sprintf("%s_%s", goldenBase, cfg.Strategy), "executions", fmt.Sprintf("%s_03_portfolio_report.txt", dateStr))
	_ = runTimestamp // available for report naming if needed

	// Print comparison report (writes to report/ dir and stdout)
	csvloader.PrintComparisonReport(sourceCSV, cfg.GoldenCopyPath, cfg.Strategy)

	// --- Step 5: Compute rebalance orders ---
	fmt.Printf("[autopilot] Computing rebalance orders...\n")
	orders, quotes, err := computeOrders(cfg.GoldenCopyPath, rc.Broker)
	if err != nil {
		return nil, fmt.Errorf("computing orders: %w", err)
	}

	kept, filtered := optimizer.FilterMicroTransactions(orders, quotes, costs.DefaultZerodha, 0.005)

	// --- Step 6: Build proposal ---
	proposal := NewProposal(cfg.GoldenCopyPath, cfg.Strategy, cfg.Schedule.Frequency, cfg.Schedule.ProposalTTLDays)
	proposal.ReportPath = reportPath
	if len(selectionPaths) > 0 {
		proposal.SelectionPath = selectionPaths[len(selectionPaths)-1]
	}

	// Populate entries, exits, weight changes
	proposal.Entries, proposal.Exits, proposal.WeightChanges = diffPortfolio(oldWeights, newWeights)

	// Populate orders
	for _, o := range kept {
		proposal.Orders = append(proposal.Orders, ProposedOrder{
			Ticker:     o.TradingSymbol,
			Exchange:   o.Exchange,
			Action:     o.TransactionType,
			Quantity:   o.Quantity,
			LimitPrice: o.Price,
			Value:      float64(o.Quantity) * o.Price,
		})
		if o.TransactionType == "BUY" {
			proposal.TotalBuyValue += float64(o.Quantity) * o.Price
		} else {
			proposal.TotalSellValue += float64(o.Quantity) * o.Price
		}
	}
	for _, o := range filtered {
		proposal.FilteredOut = append(proposal.FilteredOut, ProposedOrder{
			Ticker:     o.TradingSymbol,
			Exchange:   o.Exchange,
			Action:     o.TransactionType,
			Quantity:   o.Quantity,
			LimitPrice: o.Price,
			Value:      float64(o.Quantity) * o.Price,
		})
	}

	// Estimated cost
	var totalCost float64
	for _, o := range kept {
		bd := costs.DefaultZerodha.Calculate(o.TransactionType, o.Quantity, o.Price)
		totalCost += bd.Total
	}
	proposal.EstimatedCost = totalCost

	// Tax warnings for sell orders
	for _, o := range kept {
		if o.TransactionType == "SELL" {
			tw := costs.ClassifySell(o.TradingSymbol, o.Quantity, o.Price, 0, time.Time{})
			if tw.Class != costs.TaxUnknown {
				proposal.TaxWarnings = append(proposal.TaxWarnings, fmt.Sprintf("%s: %s (qty %d, est. gain %s%.0f)", tw.Ticker, tw.Class.String(), o.Quantity, broker.LoadMarketConfig().Currency, tw.EstimatedGain))
			} else {
				proposal.TaxWarnings = append(proposal.TaxWarnings, fmt.Sprintf("%s: %s — check manually", tw.Ticker, tw.Note))
			}
		}
	}

	// Persist proposal
	if err := SaveProposal(proposal); err != nil {
		return nil, fmt.Errorf("saving proposal: %w", err)
	}
	fmt.Printf("[autopilot] Proposal saved: %s (expires %s)\n", proposal.ID, proposal.ExpiresAt.Format("2006-01-02"))

	// Mark pipeline run as completed in DuckDB.
	if db != nil {
		if err := db.CompleteRun(ctx, runID); err != nil {
			fmt.Printf("[autopilot] Warning: failed to mark run as completed: %v\n", err)
		} else {
			db = nil // prevent deferred FailRun from firing
		}
	}

	return &RunResult{
		Proposal:       proposal,
		ReportPath:     reportPath,
		SelectionPaths: selectionPaths,
		GoldenCopyPath: cfg.GoldenCopyPath,
	}, nil
}

// computeOrders calculates rebalance orders by comparing target weights to current holdings.
func computeOrders(goldenPath string, b broker.Broker) ([]broker.Order, map[string]float64, error) {
	targetWeights, basketKeys, err := csvloader.LoadBasketCSV(goldenPath)
	if err != nil {
		return nil, nil, fmt.Errorf("loading golden copy: %w", err)
	}

	holdings, err := b.GetHoldings()
	if err != nil {
		return nil, nil, fmt.Errorf("getting holdings: %w", err)
	}

	currentQty := make(map[string]int, len(holdings))
	for _, h := range holdings {
		currentQty[h.TradingSymbol] += h.Quantity + h.T1Quantity + h.T2Quantity
	}

	quotes, err := b.GetQuotes(basketKeys)
	if err != nil {
		return nil, nil, fmt.Errorf("getting quotes: %w", err)
	}

	// Total portfolio value
	var totalValue float64
	for _, key := range basketKeys {
		parts := strings.SplitN(key, ":", 2)
		sym := key
		if len(parts) == 2 {
			sym = parts[1]
		}
		totalValue += float64(currentQty[sym]) * quotes[key]
	}

	// Build orders
	var orders []broker.Order
	for _, key := range basketKeys {
		ltp := quotes[key]
		if ltp <= 0 {
			continue
		}
		w := targetWeights[key]
		parts := strings.SplitN(key, ":", 2)
		exchange, sym := "", key
		if len(parts) == 2 {
			exchange, sym = parts[0], parts[1]
		}
		targetQty := int(math.Round(totalValue * w / ltp))
		diff := targetQty - currentQty[sym]
		if diff == 0 {
			continue
		}
		txType := "BUY"
		price := math.Round((ltp+2)*10) / 10
		if diff < 0 {
			txType = "SELL"
			price = math.Round((ltp-2)*10) / 10
			diff = -diff
		}
		orders = append(orders, broker.Order{
			TradingSymbol:   sym,
			Exchange:        exchange,
			TransactionType: txType,
			Quantity:        diff,
			OrderType:       "LIMIT",
			Product:         "CNC",
			Price:           price,
			Ltp:             ltp,
		})
	}

	return orders, quotes, nil
}

// diffPortfolio compares old and new weight maps to determine entries, exits, and weight changes.
func diffPortfolio(oldWeights, newWeights map[string]float64) (entries []StockChange, exits []StockChange, changes []WeightDelta) {
	// Find entries (new tickers not in old, or old weight was 0)
	for ticker, newW := range newWeights {
		if newW < 0.0001 {
			continue
		}
		oldW, existed := oldWeights[ticker]
		if !existed || oldW < 0.0001 {
			entries = append(entries, StockChange{
				Ticker: ticker,
				Weight: newW,
				Reason: "New addition",
			})
		} else if math.Abs(newW-oldW) > 0.001 {
			changes = append(changes, WeightDelta{
				Ticker:    ticker,
				OldWeight: oldW,
				NewWeight: newW,
			})
		}
	}

	// Find exits (old tickers with weight > 0 now at 0 or absent)
	for ticker, oldW := range oldWeights {
		if oldW < 0.0001 {
			continue
		}
		newW, exists := newWeights[ticker]
		if !exists || newW < 0.0001 {
			exits = append(exits, StockChange{
				Ticker: ticker,
				Weight: 0,
				Reason: "Exited portfolio",
			})
		}
	}

	return entries, exits, changes
}

// cleanStaleCache removes cached files from previous days.
func cleanStaleCache() {
	files, err := filepath.Glob("data/.cache/*")
	if err != nil {
		return
	}
	today := time.Now().Format("2006-01-02")
	for _, f := range files {
		if !strings.Contains(f, today) {
			_ = os.Remove(f)
		}
	}
}

// pickResultToIndexPicks converts a stockpicker.PickResult into cache.IndexPick slice.
func pickResultToIndexPicks(indexName string, r *stockpicker.PickResult) []cache.IndexPick {
	picks := make([]cache.IndexPick, 0, len(r.SelectedKeys))
	for i, ticker := range r.SelectedKeys {
		p := cache.IndexPick{
			IndexName: indexName,
			Ticker:    ticker,
			Rank:      i + 1,
			Weight:    r.Weights[ticker],
			Sector:    r.Sectors[ticker],
		}
		if r.Scores != nil {
			p.Score = r.Scores[ticker]
		}
		picks = append(picks, p)
	}
	return picks
}

// pickResultToProposals converts a stockpicker.PickResult into cache.Proposal slice.
func pickResultToProposals(r *stockpicker.PickResult) []cache.Proposal {
	proposals := make([]cache.Proposal, 0, len(r.SelectedKeys))
	for i, ticker := range r.SelectedKeys {
		p := cache.Proposal{
			Ticker: ticker,
			Weight: r.Weights[ticker],
			Rank:   i + 1,
			Sector: r.Sectors[ticker],
		}
		if r.Scores != nil {
			p.Score = r.Scores[ticker]
		}
		proposals = append(proposals, p)
	}
	return proposals
}
