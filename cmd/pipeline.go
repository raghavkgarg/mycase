package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"

	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
)

// PipelineConfig holds the resolved pipeline configuration.
type PipelineConfig struct {
	Indices               []string `yaml:"indices"`
	Strategy              string   `yaml:"strategy"`
	TopN                  int      `yaml:"top_n"`
	GoldenCopyPath        string   `yaml:"golden_copy_path"`
	Capital               int      `yaml:"capital"`
	PurchaseDate          string   `yaml:"purchase_date"`
	RebalanceTolerancePct float64  `yaml:"rebalance_tolerance_pct"`
	HysteresisRankBuffer  int      `yaml:"hysteresis_rank_buffer"`
}

type rawPipelineConfig struct {
	Indices               []string    `yaml:"indices"`
	Strategy              interface{} `yaml:"strategy"`
	TopN                  interface{} `yaml:"top_n"`
	GoldenCopyPath        interface{} `yaml:"golden_copy_path"`
	Capital               interface{} `yaml:"capital"`
	PurchaseDate          interface{} `yaml:"purchase_date"`
	RebalanceTolerancePct interface{} `yaml:"rebalance_tolerance_pct"`
	HysteresisRankBuffer  interface{} `yaml:"hysteresis_rank_buffer"`
}

func resolveFirst[T any](val interface{}, defaultVal T) T {
	if val == nil {
		return defaultVal
	}
	if v, ok := val.(T); ok {
		return v
	}
	if slice, ok := val.([]interface{}); ok && len(slice) > 0 {
		if v, ok := slice[0].(T); ok {
			return v
		}
		var temp interface{} = slice[0]
		switch any(defaultVal).(type) {
		case int:
			if f, ok := temp.(float64); ok {
				var ret interface{} = int(f)
				return ret.(T)
			}
			if i, ok := temp.(int); ok {
				var ret interface{} = i
				return ret.(T)
			}
		case float64:
			if f, ok := temp.(float64); ok {
				var ret interface{} = f
				return ret.(T)
			}
			if i, ok := temp.(int); ok {
				var ret interface{} = float64(i)
				return ret.(T)
			}
		}
	}
	switch any(defaultVal).(type) {
	case int:
		if f, ok := val.(float64); ok {
			var ret interface{} = int(f)
			return ret.(T)
		}
	case float64:
		if i, ok := val.(int); ok {
			var ret interface{} = float64(i)
			return ret.(T)
		}
	}
	return defaultVal
}

func (cfg *PipelineConfig) UnmarshalYAML(value *yaml.Node) error {
	type alias rawPipelineConfig
	var a alias
	if err := value.Decode(&a); err != nil {
		return err
	}
	cfg.Indices = a.Indices
	cfg.Strategy = resolveFirst(a.Strategy, "balanced")
	cfg.TopN = resolveFirst(a.TopN, 20)
	cfg.GoldenCopyPath = resolveFirst(a.GoldenCopyPath, "data/microsmall.csv")
	cfg.Capital = resolveFirst(a.Capital, 100000)
	cfg.PurchaseDate = resolveFirst(a.PurchaseDate, "2026-01-01")
	tol := resolveFirst(a.RebalanceTolerancePct, 0.10)
	if tol < 0 {
		tol = 0.10
	}
	cfg.RebalanceTolerancePct = tol
	buf := resolveFirst(a.HysteresisRankBuffer, 5)
	if buf < 0 {
		buf = 5
	}
	cfg.HysteresisRankBuffer = buf
	return nil
}

var PipelineCommand = &cli.Command{
	Name:  "pipeline",
	Usage: "Run the automated selection → report → execution pipeline",
	Flags: []cli.Flag{
		&cli.BoolFlag{Name: "exec-only", Usage: "Start directly from execution steps (auth + basket)"},
		&cli.StringFlag{Name: "config", Value: "config/pipeline.yaml", Usage: "Path to pipeline YAML configuration file"},
	},
	Action: runPipeline,
}

func runPipeline(ctx context.Context, c *cli.Command) error {
	execOnly := c.Bool("exec-only")
	configPath := c.String("config")

	fmt.Println("====================================================================")
	fmt.Println("             Go Mycase Automated Pipeline Runner                 ")
	fmt.Println("====================================================================")

	configFile, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("opening config file %s: %w", configPath, err)
	}
	defer configFile.Close()

	var cfg PipelineConfig
	if err := yaml.NewDecoder(configFile).Decode(&cfg); err != nil {
		return fmt.Errorf("parsing config file %s: %w", configPath, err)
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

	totalSteps := 1
	if execOnly {
		totalSteps += 2
	} else {
		totalSteps += len(cfg.Indices)
		if len(cfg.Indices) > 1 {
			totalSteps += 3
		}
		totalSteps += 5
	}

	runTimestamp := time.Now().Format("20060102_150405")
	dateStr := runTimestamp[:8]
	stepCounter := 1

	if !execOnly {
		if len(cfg.Indices) == 0 {
			return fmt.Errorf("no indices configured in pipeline.yaml")
		}

		var outputCSVs []string
		for _, indexName := range cfg.Indices {
			fmt.Printf("\n[Step %d/%d] Running %s stock selection on %s...\n", stepCounter, totalSteps, cfg.Strategy, indexName)
			outPath := filepath.Join("data", "candidates", "index_picks", fmt.Sprintf("%s_%s.csv", indexName, cfg.Strategy))
			opts := &stockpicker.Options{
				IndexName:          indexName,
				Method:             cfg.Strategy,
				TopN:               cfg.TopN,
				RangeStr:           "3mo",
				GoldenPath:         cfg.GoldenCopyPath,
				RebalanceTolerance: cfg.RebalanceTolerancePct,
				HysteresisBuffer:   cfg.HysteresisRankBuffer,
				OutputFile:         outPath,
			}
			if len(cfg.Indices) > 1 {
				opts.SkipScuttlebutt = true
			}
			if err := runPickWithOpts(ctx, opts); err != nil {
				return fmt.Errorf("step %d (pick %s): %w", stepCounter, indexName, err)
			}
			outputCSVs = append(outputCSVs, outPath)
			stepCounter++
		}

		var sourceCSV string
		goldenCSV := cfg.GoldenCopyPath
		goldenBase := csvloader.GetUniverseName(goldenCSV)
		combineCSV := filepath.Join("data", "candidates", "temp", fmt.Sprintf("combine_%s.csv", goldenBase))

		if len(cfg.Indices) > 1 {
			if err := os.MkdirAll(filepath.Dir(combineCSV), 0755); err != nil {
				fmt.Printf("Warning: Failed to create temp directory: %v\n", err)
			}
			fmt.Printf("\n[Step %d/%d] Combining stockpicker outputs into %s...\n", stepCounter, totalSteps, combineCSV)
			if err := csvloader.CombineMultipleCSVs(outputCSVs, combineCSV); err != nil {
				return fmt.Errorf("step %d (combine): %w", stepCounter, err)
			}
			fmt.Printf("Combined file successfully generated at %s.\n", combineCSV)
			stepCounter++

			proposalTopN := cfg.TopN + 5
			fmt.Printf("\n[Step %d/%d] Running stockpicker on combined candidates to select top %d candidates...\n", stepCounter, totalSteps, proposalTopN)
			outPath := filepath.Join("data", "candidates", "proposals", fmt.Sprintf("%s_%s_%s.csv", dateStr, goldenBase, cfg.Strategy))
			opts := &stockpicker.Options{
				FilePath:           combineCSV,
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
			_ = os.Remove(combineCSV)
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
		if err := runBasketWithParams(ctx, true, basketFile); err != nil {
			return fmt.Errorf("step %d (basket): %w", stepCounter, err)
		}
	} else {
		fmt.Println("Skipping basket execution.")
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
