package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gkgarg24/mycase/pkg/csvloader"

	"gopkg.in/yaml.v3"
)

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

func main() {
	execOnly := flag.Bool("exec-only", false, "Start directly from execution steps (Step 9 & 10)")
	configPath := flag.String("config", "config/pipeline.yaml", "Path to pipeline YAML configuration file")
	flag.Parse()

	fmt.Println("====================================================================")
	fmt.Println("             Go Mycase Automated Pipeline Runner                 ")
	fmt.Println("====================================================================")

	// Load configuration
	configFile, err := os.Open(*configPath)
	if err != nil {
		fmt.Printf("Error opening config file %s: %v\n", *configPath, err)
		return
	}
	defer configFile.Close()

	var cfg PipelineConfig
	if err := yaml.NewDecoder(configFile).Decode(&cfg); err != nil {
		fmt.Printf("Error parsing config file %s: %v\n", *configPath, err)
		return
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

	// Auto Execution-Only Mode Detection
	if !*execOnly && cfg.GoldenCopyPath != "" {
		if info, err := os.Stat(cfg.GoldenCopyPath); err == nil {
			today := time.Now().Format("2006-01-02")
			if info.ModTime().Format("2006-01-02") == today {
				fmt.Printf("\nGolden copy %s was already updated today (%s).\n", cfg.GoldenCopyPath, today)
				fmt.Print("Would you like to skip analysis and jump straight to Zerodha authentication & basket execution? (y/n, default: n): ")
				skipChoice, _ := reader.ReadString('\n')
				skipChoice = strings.ToLower(strings.TrimSpace(skipChoice))
				if skipChoice == "y" || skipChoice == "yes" {
					*execOnly = true
				}
			}
		}
	}

	// Step 0: Build all binaries to bin/ directory
	fmt.Println("\n[Step 0] Building all CLI tools to bin/ directory...")
	if err := os.MkdirAll("bin", 0755); err != nil {
		fmt.Printf("Error creating bin directory: %v\n", err)
		return
	}

	var targets []struct {
		src string
		dst string
	}

	if *execOnly {
		targets = []struct {
			src string
			dst string
		}{
			{"cmd/setup_auth/main.go", "bin/setup_auth"},
			{"cmd/basket/main.go", "bin/basket"},
		}
	} else {
		targets = []struct {
			src string
			dst string
		}{
			{"cmd/stockpicker/main.go", "bin/stockpicker"},
			{"cmd/report/main.go", "bin/report"},
			{"cmd/performance/main.go", "bin/performance"},
			{"cmd/monitoring/main.go", "bin/monitoring"},
			{"cmd/setup_auth/main.go", "bin/setup_auth"},
			{"cmd/basket/main.go", "bin/basket"},
			{"cmd/optimize_weights/main.go", "bin/optimize_weights"},
			{"cmd/holdings/main.go", "bin/holdings"},
		}
	}

	for _, t := range targets {
		fmt.Printf("  Building %s...\n", t.dst)
		if err := runCmd("go", "build", "-o", t.dst, t.src); err != nil {
			fmt.Printf("Error compiling %s: %v\n", t.dst, err)
			return
		}
	}

	// Calculate total steps dynamically
	totalSteps := 1 // Step 0 is Building
	if *execOnly {
		totalSteps += 2 // Auth, Basket
	} else {
		totalSteps += len(cfg.Indices) // Individual Index stock picks
		if len(cfg.Indices) > 1 {
			totalSteps += 3 // Combine, Combined stockpicker (top 25), Weight optimization (top 20)
		}
		totalSteps += 5 // Update Golden, Report, Perf, Mon, Auth, Basket
	}

	runTimestamp := time.Now().Format("20060102_150405")
	dateStr := runTimestamp[:8]
	stepCounter := 1

	if !*execOnly {
		if len(cfg.Indices) == 0 {
			fmt.Println("Error: No indices configured in pipeline.yaml")
			return
		}

		var outputCSVs []string
		// Running stockpicker selection individually for each configured index.
		for _, indexName := range cfg.Indices {
			fmt.Printf("\n[Step %d/%d] Running %s stock selection on %s...\n", stepCounter, totalSteps, cfg.Strategy, indexName)
			outPath := filepath.Join("data", "candidates", "index_picks", fmt.Sprintf("%s_%s.csv", indexName, cfg.Strategy))
			args := []string{"-index", indexName, "-method", cfg.Strategy, "-top", strconv.Itoa(cfg.TopN)}
			if len(cfg.Indices) > 1 {
				args = append(args, "-skip-scuttlebutt")
			}
			args = append(args, "-golden", cfg.GoldenCopyPath, "-rebalance-tolerance", fmt.Sprintf("%.4f", cfg.RebalanceTolerancePct), "-hysteresis-buffer", strconv.Itoa(cfg.HysteresisRankBuffer), "-out", outPath)
			if err := runCmd("./bin/stockpicker", args...); err != nil {
				fmt.Printf("Error running Step %d: %v\n", stepCounter, err)
				return
			}
			outputCSVs = append(outputCSVs, outPath)
			stepCounter++
		}

		var sourceCSV string
		goldenCSV := cfg.GoldenCopyPath
		goldenBase := csvloader.GetUniverseName(goldenCSV)
		combineCSV := filepath.Join("data", "candidates", "temp", fmt.Sprintf("combine_%s.csv", goldenBase))

		// Combine candidate lists if multiple indices are configured
		if len(cfg.Indices) > 1 {
			if err := os.MkdirAll(filepath.Dir(combineCSV), 0755); err != nil {
				fmt.Printf("Warning: Failed to create temp directory for combine: %v\n", err)
			}
			fmt.Printf("\n[Step %d/%d] Combining stockpicker outputs into %s...\n", stepCounter, totalSteps, combineCSV)
			if err := csvloader.CombineMultipleCSVs(outputCSVs, combineCSV); err != nil {
				fmt.Printf("Error combining CSVs: %v\n", err)
				return
			}
			fmt.Printf("Combined file successfully generated at %s.\n", combineCSV)
			stepCounter++

			// Run stockpicker on combined candidates to select TopN + 5 (25) candidates
			proposalTopN := cfg.TopN + 5
			fmt.Printf("\n[Step %d/%d] Running stockpicker on combined candidates to select top %d candidates...\n", stepCounter, totalSteps, proposalTopN)
			outPath := filepath.Join("data", "candidates", "proposals", fmt.Sprintf("%s_%s_%s.csv", dateStr, goldenBase, cfg.Strategy))
			if err := runCmd("./bin/stockpicker", "-file", combineCSV, "-method", cfg.Strategy, "-top", strconv.Itoa(proposalTopN), "-golden", cfg.GoldenCopyPath, "-rebalance-tolerance", fmt.Sprintf("%.4f", cfg.RebalanceTolerancePct), "-hysteresis-buffer", strconv.Itoa(cfg.HysteresisRankBuffer), "-name", goldenBase, "-out", outPath); err != nil {
				fmt.Printf("Error running Step %d: %v\n", stepCounter, err)
				return
			}

			if err := os.Remove(combineCSV); err != nil {
				fmt.Printf("Warning: could not delete temporary file %s: %v\n", combineCSV, err)
			}
			stepCounter++

			// Ask if user wants to manually remove shares
			fmt.Printf("\nWould you like to manually remove shares from the proposal before finalizing? (y/n, default: n): ")
			choice, _ := reader.ReadString('\n')
			choice = strings.ToLower(strings.TrimSpace(choice))
			if choice == "y" || choice == "yes" {
				fmt.Printf("\n>>> ACTION REQUIRED: Please open and edit the proposal file: %s\n", outPath)
				fmt.Println("    Once you have removed unwanted shares, save the file.")
				fmt.Print("    Press Enter to continue and run weight optimization...")
				_, _ = reader.ReadString('\n')
			}

			// Run stockpicker on manually edited file to select top N and compute weights
			fmt.Printf("\n[Step %d/%d] Running stockpicker to prune to top %d stocks...\n", stepCounter, totalSteps, cfg.TopN)
			optimPath := filepath.Join("data", "candidates", "proposals", fmt.Sprintf("%s_%s_%s_optim.csv", dateStr, goldenBase, cfg.Strategy))
			if err := runCmd("./bin/stockpicker", "-file", outPath, "-method", cfg.Strategy, "-top", strconv.Itoa(cfg.TopN), "-golden", cfg.GoldenCopyPath, "-rebalance-tolerance", fmt.Sprintf("%.4f", cfg.RebalanceTolerancePct), "-hysteresis-buffer", strconv.Itoa(cfg.HysteresisRankBuffer), "-name", goldenBase, "-out", optimPath); err != nil {
				fmt.Printf("Error running Step %d: %v\n", stepCounter, err)
				return
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
		offerToOpenReport(reader, comparisonReportPath)

		fmt.Printf("Would you like to update the golden copy %s with the new candidates? (y/n, default: y): ", goldenCSV)
		updateChoice, _ := reader.ReadString('\n')
		updateChoice = strings.ToLower(strings.TrimSpace(updateChoice))
		if updateChoice == "" || updateChoice == "y" || updateChoice == "yes" {
			// Create a backup of the golden copy before merging
			if _, err := os.Stat(goldenCSV); err == nil {
				backupDir := filepath.Join("data", "backups", goldenBase)
				if err := os.MkdirAll(backupDir, 0755); err != nil {
					fmt.Printf("Warning: Failed to create backups directory: %v\n", err)
				}
				backupName := fmt.Sprintf("bk_%s.csv", time.Now().Format("20060102_150405"))
				backupPath := filepath.Join(backupDir, backupName)
				if err := copyFile(goldenCSV, backupPath); err != nil {
					fmt.Printf("Warning: Failed to create backup of golden copy: %v\n", err)
				} else {
					fmt.Printf("Created backup of golden copy: %s\n", backupPath)
				}
			}

			if err := csvloader.MergeGoldenCopy(sourceCSV, goldenCSV); err != nil {
				fmt.Printf("Error updating golden copy: %v\n", err)
				return
			}
			fmt.Printf("Successfully updated %s with new candidates. Exited tickers kept at 0.0000 weight.\n", goldenCSV)
			fmt.Printf("\n>>> ACTION REQUIRED: If you wish to manually tweak the golden copy (%s), do it now.\n", goldenCSV)
			fmt.Print("Press Enter to continue once you have reviewed the file...")
			_, _ = reader.ReadString('\n')
		} else {
			fmt.Println("Skipped golden copy update. Exiting pipeline.")
			return
		}
		stepCounter++

		// Generate portfolio report
		fmt.Printf("\n[Step %d/%d] Generating the portfolio report...\n", stepCounter, totalSteps)
		if err := runCmd("./bin/report", "-file", goldenCSV, "-method", cfg.Strategy); err != nil {
			fmt.Printf("Error running Step %d: %v\n", stepCounter, err)
			return
		}
		portfolioReportPath := filepath.Join("report", fmt.Sprintf("%s_%s", goldenBase, cfg.Strategy), "executions", fmt.Sprintf("%s_03_portfolio_report.txt", dateStr))
		offerToOpenReport(reader, portfolioReportPath)
		stepCounter++

		// Simulate historical performance
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

		if err := runCmd("./bin/performance", "-file", goldenCSV, "-capital", capital, "-date", dateVal); err != nil {
			fmt.Printf("Error running Step %d: %v\n", stepCounter, err)
			return
		}
		stepCounter++

		// Run monitoring tool
		fmt.Printf("\n[Step %d/%d] Running monitoring tool...\n", stepCounter, totalSteps)
		fmt.Println("Choose Monitoring Simulator timeframe:")
		fmt.Println("1. 1 Year Historical Backtest [Default]")
		fmt.Printf("2. Same as performance simulation date (%s)\n", dateVal)
		fmt.Print("Enter choice (1-2, default: 1): ")
		timeframeChoice, _ := reader.ReadString('\n')
		timeframeChoice = strings.TrimSpace(timeframeChoice)

		var monArgs []string
		monArgs = append(monArgs, "-file", goldenCSV, "-interactive", "-strategy", cfg.Strategy, "-timestamp", runTimestamp)
		if timeframeChoice == "2" {
			monArgs = append(monArgs, "-date", dateVal)
		}

		if err := runCmd("./bin/monitoring", monArgs...); err != nil {
			fmt.Printf("Error running Step %d: %v\n", stepCounter, err)
			return
		}
		monitoringReportPath := filepath.Join("report", fmt.Sprintf("%s_%s", goldenBase, cfg.Strategy), "simulations", fmt.Sprintf("%s_monitoring.txt", runTimestamp))
		offerToOpenReport(reader, monitoringReportPath)
		stepCounter++
	}

	// Step: Establish/renew Zerodha Kite Connect session authentication.
	fmt.Printf("\n[Step %d/%d] Setting up Zerodha authentication...\n", stepCounter, totalSteps)
	fmt.Print("Would you like to setup authentication now? (y/n, default: y): ")
	authChoice, _ := reader.ReadString('\n')
	authChoice = strings.ToLower(strings.TrimSpace(authChoice))
	if authChoice == "" || authChoice == "y" || authChoice == "yes" {
		if err := runCmd("./bin/setup_auth"); err != nil {
			fmt.Printf("Error setting up auth: %v\n", err)
			return
		}
	} else {
		fmt.Println("Skipping authorization setup.")
	}
	stepCounter++

	// Step: Calculate required shares and execute the basket order on Zerodha.
	fmt.Printf("\n[Step %d/%d] Executing mycase basket orders...\n", stepCounter, totalSteps)
	fmt.Print("Would you like to execute the basket orders? (y/n, default: y): ")
	execChoice, _ := reader.ReadString('\n')
	execChoice = strings.ToLower(strings.TrimSpace(execChoice))
	if execChoice == "" || execChoice == "y" || execChoice == "yes" {
		goldenBase := csvloader.GetUniverseName(cfg.GoldenCopyPath)
		if err := runCmd("./bin/basket", "--live", "--", goldenBase); err != nil {
			fmt.Printf("Error executing basket: %v\n", err)
			return
		}
	} else {
		fmt.Println("Skipping basket execution.")
	}

	if !*execOnly {
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
			goldenBase := csvloader.GetUniverseName(cfg.GoldenCopyPath)
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
		goldenBase := csvloader.GetUniverseName(cfg.GoldenCopyPath)
		fmt.Printf("%d. Explanation Report:  report/%s_%s/executions/%s_03_portfolio_report.txt\n", summaryIdx, goldenBase, cfg.Strategy, dateStr)
		fmt.Println("====================================================================")
	}

	fmt.Println("\n====================================================================")
	fmt.Println("               Pipeline Completed Successfully!                     ")
	fmt.Println("====================================================================")
}

func offerToOpenReport(reader *bufio.Reader, filePath string) {
	fmt.Printf("Would you like to open the report file %s now? (y/n, default: y): ", filePath)
	choice, _ := reader.ReadString('\n')
	choice = strings.ToLower(strings.TrimSpace(choice))
	if choice == "" || choice == "y" || choice == "yes" {
		_ = exec.Command("open", filePath).Run()
	}
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}



