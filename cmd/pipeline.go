package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
)

var PipelineCommand = &cli.Command{
	Name:  "pipeline",
	Usage: "Run the automated selection → report → execution pipeline",
	Flags: []cli.Flag{
		&cli.BoolFlag{Name: "exec-only", Usage: "Start directly from execution steps (auth + basket)"},
		&cli.StringFlag{Name: "config", Value: "config/pipeline.yaml", Usage: "Path to pipeline YAML configuration file"},
		&cli.StringFlag{Name: "index", Aliases: []string{"i"}, Usage: "Index to pick stocks from (e.g. nifty50, smallcap250)"},
		&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Usage: "Path to custom CSV/XLSX file"},
		&cli.StringFlag{Name: "strategy", Aliases: []string{"method", "m"}, Usage: "Scoring strategy (balanced, aggressive, conservative, multibagger, value)"},
		&cli.IntFlag{Name: "top", Aliases: []string{"top-n", "n"}, Usage: "Number of top stocks to pick"},
		&cli.StringFlag{Name: "golden", Aliases: []string{"golden-copy"}, Usage: "Path to golden copy CSV for hysteresis and rebalancing band"},
		&cli.IntFlag{Name: "capital", Usage: "Initial capital for performance simulation"},
		&cli.StringFlag{Name: "purchase-date", Aliases: []string{"date"}, Usage: "Purchase date for performance simulation (YYYY-MM-DD)"},
		&cli.FloatFlag{Name: "rebalance-tolerance", Usage: "Rebalancing weight tolerance % (e.g. 0.10 for 0.10%)"},
		&cli.IntFlag{Name: "hysteresis-buffer", Usage: "Extra ranks to allow existing holdings to drift"},
	},
	Action: runPipeline,
	Commands: []*cli.Command{
		pipelineHistoryCmd,
		pipelineShowCmd,
		pipelineDiffCmd,
	},
}

func runPipeline(ctx context.Context, c *cli.Command) error {
	execOnly := c.Bool("exec-only")
	configPath := c.String("config")

	fmt.Println("====================================================================")
	fmt.Println("             Go Mycase Automated Pipeline Runner                 ")
	fmt.Println("====================================================================")

	var cfg config.PipelineConfig
	configFile, err := os.Open(configPath)
	if err == nil {
		defer configFile.Close()
		if err := yaml.NewDecoder(configFile).Decode(&cfg); err != nil {
			return fmt.Errorf("parsing config file %s: %w", configPath, err)
		}
	} else if c.IsSet("config") {
		return fmt.Errorf("opening config file %s: %w", configPath, err)
	}

	if c.IsSet("index") {
		idx := c.String("index")
		if idx != "" {
			cfg.Indices = []string{idx}
			if !c.IsSet("golden") {
				cfg.GoldenCopyPath = filepath.Join("data", idx+".csv")
			}
		}
	}

	if c.IsSet("file") {
		f := c.String("file")
		if f != "" {
			cfg.Files = []string{f}
			cfg.File = f
			if !c.IsSet("index") {
				cfg.Indices = nil
			}
		}
	}

	if c.IsSet("strategy") {
		cfg.Strategy = c.String("strategy")
	}

	if c.IsSet("top") {
		cfg.TopN = int(c.Int("top"))
	}

	if c.IsSet("golden") {
		cfg.GoldenCopyPath = c.String("golden")
	}

	if c.IsSet("capital") {
		cfg.Capital = int(c.Int("capital"))
	}

	if c.IsSet("purchase-date") {
		cfg.PurchaseDate = c.String("purchase-date")
	}

	if c.IsSet("rebalance-tolerance") {
		cfg.RebalanceTolerancePct = c.Float("rebalance-tolerance")
	}

	if c.IsSet("hysteresis-buffer") {
		cfg.HysteresisRankBuffer = int(c.Int("hysteresis-buffer"))
	}

	reader := bufio.NewReader(os.Stdin)

	// Clean up stale cache files from previous days
	if files, err := filepath.Glob("data/.cache/*"); err == nil {
		today := time.Now().Format("2006-01-02")
		for _, f := range files {
			if !strings.Contains(f, today) {
				_ = os.Remove(f)
			}
		}
	}

	if !execOnly && cfg.GoldenCopyPath != "" {
		if info, err := os.Stat(cfg.GoldenCopyPath); err == nil {
			today := time.Now().Format("2006-01-02")
			if info.ModTime().Format("2006-01-02") == today {
				fmt.Printf("\nGolden copy %s was already updated today (%s).\n", cfg.GoldenCopyPath, today)
				fmt.Print("Would you like to skip analysis and jump straight to Zerodha authentication & basket execution? (y/n, default: n): ")
				skipChoice, _ := reader.ReadString('\n')
				skipChoice = strings.ToLower(strings.TrimSpace(skipChoice))
				if skipChoice == "y" || skipChoice == "yes" {
					execOnly = true
				}
			}
		}
	}

	type pipelineSource struct {
		name     string
		filePath string
		isIndex  bool
	}

	var sources []pipelineSource
	for _, f := range cfg.Files {
		if strings.TrimSpace(f) != "" {
			sources = append(sources, pipelineSource{
				name:     csvloader.GetUniverseName(f),
				filePath: f,
				isIndex:  false,
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

	sourcesCount := len(sources)

	totalSteps := 1
	if execOnly {
		totalSteps += 2
	} else {
		totalSteps += sourcesCount
		if sourcesCount > 1 {
			totalSteps += 3
		}
		totalSteps += 5
	}

	runTimestamp := time.Now().Format("20060102_150405")
	dateStr := runTimestamp[:8]
	stepCounter := 1

	// Pipeline run tracking variables (function scope for defer/completion).
	var db *cache.Cache
	var runID string

	if !execOnly {
		if sourcesCount == 0 {
			return fmt.Errorf("no indices or files configured in pipeline.yaml")
		}

		// --- Pipeline run tracking (DuckDB) ---
		goldenBase := csvloader.GetUniverseName(cfg.GoldenCopyPath)
		runID = cache.NewRunID()
		db = cache.GetDB()
		if db != nil {
			pipelineRun := cache.PipelineRun{
				RunID:     runID,
				StartedAt: time.Now(),
				Portfolio: goldenBase,
				Method:    cfg.Strategy,
			}
			if err := db.InsertRun(ctx, pipelineRun); err != nil {
				fmt.Printf("[pipeline] Warning: failed to record pipeline run: %v\n", err)
				db = nil
			} else {
				defer func() {
					if db != nil {
						_ = db.FailRun(ctx, runID)
					}
				}()
			}
		}

		var outputCSVs []string
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
			}
			if src.isIndex {
				fmt.Printf("\n[Step %d/%d] Running %s stock selection on index %s...\n", stepCounter, totalSteps, cfg.Strategy, src.name)
				opts.IndexName = src.name
			} else {
				fmt.Printf("\n[Step %d/%d] Running %s stock selection on file %s...\n", stepCounter, totalSteps, cfg.Strategy, src.filePath)
				opts.FilePath = src.filePath
				opts.DisplayName = src.name
			}
			if sourcesCount > 1 {
				opts.SkipScuttlebutt = true
			}
			if err := runPickWithOpts(ctx, opts); err != nil {
				return fmt.Errorf("step %d (pick %s): %w", stepCounter, src.name, err)
			}
			outputCSVs = append(outputCSVs, outPath)

			// Persist index picks to DuckDB
			if db != nil {
				if picks := csvWeightsToIndexPicks(outPath, src.name); len(picks) > 0 {
					if err := db.InsertIndexPicks(ctx, runID, src.name, picks); err != nil {
						fmt.Printf("[pipeline] Warning: failed to persist index picks for %s: %v\n", src.name, err)
					}
				}
			}

			stepCounter++
		}

		var sourceCSV string
		goldenCSV := cfg.GoldenCopyPath

		if sourcesCount > 1 {
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
					fmt.Printf("\n[Step %d/%d] Combined %d unique tickers from DB index_picks.\n", stepCounter, totalSteps, len(combinedTickers))
				}
			}

			// Fallback: combine via CSV if DB path didn't yield tickers.
			if len(combinedTickers) == 0 {
				combineCSV := filepath.Join("data", "candidates", "temp", fmt.Sprintf("combine_%s.csv", goldenBase))
				if err := os.MkdirAll(filepath.Dir(combineCSV), 0755); err != nil {
					fmt.Printf("Warning: Failed to create temp directory: %v\n", err)
				}
				fmt.Printf("\n[Step %d/%d] Combining stockpicker outputs into %s (fallback)...\n", stepCounter, totalSteps, combineCSV)
				if err := csvloader.CombineMultipleCSVs(outputCSVs, combineCSV); err != nil {
					return fmt.Errorf("step %d (combine): %w", stepCounter, err)
				}
				// Read tickers from the combined CSV.
				weights, _ := csvloader.ReadCSVWeights(combineCSV)
				for t := range weights {
					combinedTickers = append(combinedTickers, t)
				}
				_ = os.Remove(combineCSV)
			}
			stepCounter++

			proposalTopN := cfg.TopN + 5
			fmt.Printf("\n[Step %d/%d] Running stockpicker on combined candidates to select top %d candidates...\n", stepCounter, totalSteps, proposalTopN)
			outPath := filepath.Join("data", "candidates", "proposals", fmt.Sprintf("%s_%s_%s.csv", dateStr, goldenBase, cfg.Strategy))
			opts := &stockpicker.Options{
				Tickers:            combinedTickers,
				Method:             cfg.Strategy,
				TopN:               proposalTopN,
				RangeStr:           "3mo",
				GoldenPath:         cfg.GoldenCopyPath,
				RebalanceTolerance: cfg.RebalanceTolerancePct,
				HysteresisBuffer:   cfg.HysteresisRankBuffer,
				DisplayName:        goldenBase,
				OutputFile:         outPath,
			}
			if err := runPickWithOpts(ctx, opts); err != nil {
				return fmt.Errorf("step %d (pick combined): %w", stepCounter, err)
			}

			// Persist draft proposals to DuckDB
			if db != nil {
				if proposals := csvWeightsToProposals(outPath); len(proposals) > 0 {
					if err := db.InsertProposals(ctx, runID, "draft", proposals); err != nil {
						fmt.Printf("[pipeline] Warning: failed to persist draft proposals: %v\n", err)
					}
				}
			}

			stepCounter++

			fmt.Printf("\nWould you like to manually remove shares from the proposal before finalizing? (y/n, default: n): ")
			choice, _ := reader.ReadString('\n')
			choice = strings.ToLower(strings.TrimSpace(choice))
			if choice == "y" || choice == "yes" {
				fmt.Printf("\n>>> ACTION REQUIRED: Please open and edit the proposal file: %s\n", outPath)
				fmt.Println("    Once you have removed unwanted shares, save the file.")
				fmt.Print("    Press Enter to continue and run weight optimization...")
				_, _ = reader.ReadString('\n')
			}

			// Prune step must read from file (user may have edited the proposal CSV above).
			fmt.Printf("\n[Step %d/%d] Running stockpicker to prune to top %d stocks...\n", stepCounter, totalSteps, cfg.TopN)
			optimPath := filepath.Join("data", "candidates", "proposals", fmt.Sprintf("%s_%s_%s_optim.csv", dateStr, goldenBase, cfg.Strategy))
			opts2 := &stockpicker.Options{
				FilePath:           outPath,
				Method:             cfg.Strategy,
				TopN:               cfg.TopN,
				RangeStr:           "3mo",
				GoldenPath:         cfg.GoldenCopyPath,
				RebalanceTolerance: cfg.RebalanceTolerancePct,
				HysteresisBuffer:   cfg.HysteresisRankBuffer,
				DisplayName:        goldenBase,
				OutputFile:         optimPath,
			}
			if err := runPickWithOpts(ctx, opts2); err != nil {
				return fmt.Errorf("step %d (pick prune): %w", stepCounter, err)
			}
			sourceCSV = optimPath

			// Persist optimized proposals to DuckDB
			if db != nil {
				if proposals := csvWeightsToProposals(optimPath); len(proposals) > 0 {
					if err := db.InsertProposals(ctx, runID, "optimized", proposals); err != nil {
						fmt.Printf("[pipeline] Warning: failed to persist optimized proposals: %v\n", err)
					}
				}
			}

			stepCounter++
		} else {
			sourceCSV = outputCSVs[0]
		}

		// Update Golden Copy
		fmt.Printf("\n[Step %d/%d] Updating the %s golden copy...\n", stepCounter, totalSteps, goldenCSV)
		csvloader.PrintComparisonReport(sourceCSV, goldenCSV, cfg.Strategy)

		comparisonReportPath := filepath.Join("report", fmt.Sprintf("%s_%s", goldenBase, cfg.Strategy), "executions", fmt.Sprintf("%s_02_comparison.txt", dateStr))
		pipelineOfferToOpenReport(reader, comparisonReportPath)

		fmt.Printf("Would you like to update the golden copy %s with the new candidates? (y/n, default: y): ", goldenCSV)
		updateChoice, _ := reader.ReadString('\n')
		updateChoice = strings.ToLower(strings.TrimSpace(updateChoice))
		if updateChoice == "" || updateChoice == "y" || updateChoice == "yes" {
			if _, err := os.Stat(goldenCSV); err == nil {
				backupDir := filepath.Join("data", "backups", goldenBase)
				if err := os.MkdirAll(backupDir, 0755); err != nil {
					fmt.Printf("Warning: Failed to create backups directory: %v\n", err)
				}
				backupName := fmt.Sprintf("bk_%s.csv", time.Now().Format("20060102_150405"))
				backupPath := filepath.Join(backupDir, backupName)
				if err := pipelineCopyFile(goldenCSV, backupPath); err != nil {
					fmt.Printf("Warning: Failed to create backup of golden copy: %v\n", err)
				} else {
					fmt.Printf("Created backup of golden copy: %s\n", backupPath)
				}
			}
			if err := csvloader.MergeGoldenCopy(sourceCSV, goldenCSV); err != nil {
				return fmt.Errorf("updating golden copy: %w", err)
			}
			fmt.Printf("Successfully updated %s with new candidates. Exited tickers kept at 0.0000 weight.\n", goldenCSV)
			fmt.Printf("\n>>> ACTION REQUIRED: If you wish to manually tweak the golden copy (%s), do it now.\n", goldenCSV)
			fmt.Print("Press Enter to continue once you have reviewed the file...")
			_, _ = reader.ReadString('\n')
		} else {
			fmt.Println("Skipped golden copy update. Exiting pipeline.")
			return nil
		}
		stepCounter++

		// Generate portfolio report
		fmt.Printf("\n[Step %d/%d] Generating the portfolio report...\n", stepCounter, totalSteps)
		if err := runReportWithParams(ctx, goldenCSV, cfg.Strategy); err != nil {
			return fmt.Errorf("step %d (report): %w", stepCounter, err)
		}
		portfolioReportPath := filepath.Join("report", fmt.Sprintf("%s_%s", goldenBase, cfg.Strategy), "executions", fmt.Sprintf("%s_03_portfolio_report.txt", dateStr))
		pipelineOfferToOpenReport(reader, portfolioReportPath)
		stepCounter++

		// Performance simulation
		fmt.Printf("\n[Step %d/%d] Running performance simulation...\n", stepCounter, totalSteps)
		fmt.Printf("Enter capital (default %d): ", cfg.Capital)
		capInput, _ := reader.ReadString('\n')
		capital := strings.TrimSpace(capInput)
		if capital == "" {
			capital = strconv.Itoa(cfg.Capital)
		}
		fmt.Printf("Enter purchase date YYYY-MM-DD (default %s): ", cfg.PurchaseDate)
		dateInput, _ := reader.ReadString('\n')
		dateVal := strings.TrimSpace(dateInput)
		if dateVal == "" {
			dateVal = cfg.PurchaseDate
		}
		capFloat, err := strconv.ParseFloat(capital, 64)
		if err != nil {
			capFloat = float64(cfg.Capital)
		}
		if err := runPerfWithParams(ctx, goldenCSV, capFloat, dateVal, "09:30"); err != nil {
			return fmt.Errorf("step %d (performance): %w", stepCounter, err)
		}
		stepCounter++

		// Monitoring simulation
		fmt.Printf("\n[Step %d/%d] Running monitoring tool...\n", stepCounter, totalSteps)
		fmt.Println("Choose Monitoring Simulator timeframe:")
		fmt.Println("1. 1 Year Historical Backtest [Default]")
		fmt.Printf("2. Same as performance simulation date (%s)\n", dateVal)
		fmt.Print("Enter choice (1-2, default: 1): ")
		timeframeChoice, _ := reader.ReadString('\n')
		timeframeChoice = strings.TrimSpace(timeframeChoice)

		monDate := ""
		if timeframeChoice == "2" {
			monDate = dateVal
		}
		if err := runMonitorWithParams(ctx, goldenCSV, true, "moderate", float64(cfg.Capital), monDate, cfg.Strategy, runTimestamp, true); err != nil {
			return fmt.Errorf("step %d (monitor): %w", stepCounter, err)
		}
		monitoringReportPath := filepath.Join("report", fmt.Sprintf("%s_%s", goldenBase, cfg.Strategy), "simulations", fmt.Sprintf("%s_monitoring.txt", runTimestamp))
		pipelineOfferToOpenReport(reader, monitoringReportPath)
		stepCounter++
	}

	var usDetectedSources []string
	if stockpicker.IsUSIndex(cfg.GoldenCopyPath) {
		usDetectedSources = append(usDetectedSources, csvloader.GetUniverseName(cfg.GoldenCopyPath))
	}
	for _, idx := range cfg.Indices {
		if stockpicker.IsUSIndex(idx) {
			uName := idx
			alreadyAdded := slices.Contains(usDetectedSources, uName)
			if !alreadyAdded {
				usDetectedSources = append(usDetectedSources, uName)
			}
		}
	}
	for _, f := range cfg.Files {
		if stockpicker.IsUSIndex(f) {
			uName := csvloader.GetUniverseName(f)
			alreadyAdded := slices.Contains(usDetectedSources, uName)
			if !alreadyAdded {
				usDetectedSources = append(usDetectedSources, uName)
			}
		}
	}

	if len(usDetectedSources) > 0 {
		fmt.Printf("\n[Step %d/%d] US market portfolio detected (%v). Skipping Zerodha Indian broker authentication & basket execution.\n", stepCounter, totalSteps, usDetectedSources)
	} else {
		// Auth step
		fmt.Printf("\n[Step %d/%d] Setting up Zerodha authentication...\n", stepCounter, totalSteps)
		fmt.Print("Would you like to setup authentication now? (y/n, default: y): ")
		authChoice, _ := reader.ReadString('\n')
		authChoice = strings.ToLower(strings.TrimSpace(authChoice))
		if authChoice == "" || authChoice == "y" || authChoice == "yes" {
			if err := runAuthCmd(ctx); err != nil {
				return fmt.Errorf("step %d (auth): %w", stepCounter, err)
			}
		} else {
			fmt.Println("Skipping authorization setup.")
		}
		stepCounter++

		// Basket execution
		fmt.Printf("\n[Step %d/%d] Executing mycase basket orders...\n", stepCounter, totalSteps)
		fmt.Print("Would you like to execute the basket orders? (y/n, default: y): ")
		execChoice, _ := reader.ReadString('\n')
		execChoice = strings.ToLower(strings.TrimSpace(execChoice))
		if execChoice == "" || execChoice == "y" || execChoice == "yes" {
			goldenBase := csvloader.GetUniverseName(cfg.GoldenCopyPath)
			basketFile := "data/" + goldenBase + ".csv"
			if err := runBasketWithParams(ctx, true, basketFile, false); err != nil {
				return fmt.Errorf("step %d (basket): %w", stepCounter, err)
			}
		} else {
			fmt.Println("Skipping basket execution.")
		}
	}

	if !execOnly {
		goldenBase := csvloader.GetUniverseName(cfg.GoldenCopyPath)
		fmt.Println("\n====================================================================")
		fmt.Println("               Generated Reports & Files Summary                    ")
		fmt.Println("====================================================================")
		for idx, indexName := range cfg.Indices {
			fmt.Printf("%d. %s Candidates:\n", idx+1, indexName)
			fmt.Printf("   - CSV Output:         data/candidates/index_picks/%s_%s.csv\n", indexName, cfg.Strategy)
			fmt.Printf("   - Selection reasons:  report/%s_%s/executions/%s_01_selection_reasons.txt\n", indexName, cfg.Strategy, dateStr)
		}
		summaryIdx := len(cfg.Indices) + 1
		if len(cfg.Indices) > 1 {
			fmt.Printf("%d. Combined Candidates:  data/candidates/temp/combine_%s.csv (temporary, deleted)\n", summaryIdx, goldenBase)
			summaryIdx++
			fmt.Printf("%d. Draft Candidates (Top %d): data/candidates/proposals/%s_%s_%s.csv (manually editable)\n", summaryIdx, cfg.TopN+5, dateStr, goldenBase, cfg.Strategy)
			summaryIdx++
			fmt.Printf("%d. Finalized Top %d Candidates (Optimized):\n", summaryIdx, cfg.TopN)
			fmt.Printf("   - CSV Output:         data/candidates/proposals/%s_%s_%s_optim.csv\n", dateStr, goldenBase, cfg.Strategy)
			fmt.Printf("   - Scuttlebutt Check:  report/%s_%s/research/%s_scuttlebutt.txt\n", goldenBase, cfg.Strategy, dateStr)
			fmt.Printf("   - Selection reasons:  report/%s_%s/executions/%s_01_selection_reasons.txt\n", goldenBase, cfg.Strategy, dateStr)
			summaryIdx++
		}
		fmt.Printf("%d. Golden Copy Portfolio:%s\n", summaryIdx, cfg.GoldenCopyPath)
		summaryIdx++
		fmt.Printf("%d. Explanation Report:  report/%s_%s/executions/%s_03_portfolio_report.txt\n", summaryIdx, goldenBase, cfg.Strategy, dateStr)
		fmt.Println("====================================================================")
	}

	fmt.Println("\n====================================================================")
	fmt.Println("               Pipeline Completed Successfully!                     ")
	fmt.Println("====================================================================")

	// Mark pipeline run as completed in DuckDB.
	if db != nil {
		if err := db.CompleteRun(ctx, runID); err != nil {
			fmt.Printf("[pipeline] Warning: failed to mark run as completed: %v\n", err)
		} else {
			db = nil // prevent deferred FailRun from firing
		}
	}

	return nil
}

func pipelineOfferToOpenReport(reader *bufio.Reader, filePath string) {
	fmt.Printf("Would you like to open the report file %s now? (y/n, default: y): ", filePath)
	choice, _ := reader.ReadString('\n')
	choice = strings.ToLower(strings.TrimSpace(choice))
	if choice == "" || choice == "y" || choice == "yes" {
		_ = exec.Command("open", filePath).Run()
	}
}

func pipelineCopyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// csvWeightsToIndexPicks reads a ticker/weight CSV and converts to cache.IndexPick slice.
func csvWeightsToIndexPicks(csvPath, indexName string) []cache.IndexPick {
	weights, err := csvloader.ReadCSVWeights(csvPath)
	if err != nil {
		return nil
	}
	// Sort by weight descending for rank assignment.
	type tw struct {
		ticker string
		weight float64
	}
	sorted := make([]tw, 0, len(weights))
	for t, w := range weights {
		sorted = append(sorted, tw{t, w})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].weight > sorted[j].weight
	})

	picks := make([]cache.IndexPick, 0, len(sorted))
	for i, s := range sorted {
		picks = append(picks, cache.IndexPick{
			IndexName: indexName,
			Ticker:    s.ticker,
			Rank:      i + 1,
			Weight:    s.weight,
		})
	}
	return picks
}

// csvWeightsToProposals reads a ticker/weight CSV and converts to cache.Proposal slice.
func csvWeightsToProposals(csvPath string) []cache.Proposal {
	weights, err := csvloader.ReadCSVWeights(csvPath)
	if err != nil {
		return nil
	}
	type tw struct {
		ticker string
		weight float64
	}
	sorted := make([]tw, 0, len(weights))
	for t, w := range weights {
		sorted = append(sorted, tw{t, w})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].weight > sorted[j].weight
	})

	proposals := make([]cache.Proposal, 0, len(sorted))
	for i, s := range sorted {
		proposals = append(proposals, cache.Proposal{
			Ticker: s.ticker,
			Weight: s.weight,
			Rank:   i + 1,
		})
	}
	return proposals
}
