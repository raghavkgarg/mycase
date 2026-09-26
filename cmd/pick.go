package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/marketdata"
	"github.com/raghavkgarg/mycase/pkg/pithistory"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
)

var PickCommand = &cli.Command{
	Name:  "pick",
	Usage: "Select top stocks from an index or file",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "index", Aliases: []string{"i"}, Usage: "Index to pick stocks from (default from config/defaults.json)"},
		&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Usage: "Path to custom CSV file (takes precedence over --index)"},
		&cli.StringFlag{Name: "method", Aliases: []string{"m"}, Usage: "Scoring strategy (balanced, aggressive, conservative, multibagger, earlymb, value, us_quality_momentum, fairprice) (default from config/defaults.json)"},
		&cli.IntFlag{Name: "top", Usage: "Number of top stocks to pick (default from config/defaults.json)"},
		&cli.StringFlag{Name: "range", Usage: "Historical data range: 3mo, 6mo, 1y (default from config/defaults.json)"},
		&cli.BoolFlag{Name: "skip-scuttlebutt", Usage: "Skip qualitative scuttlebutt checklist report"},
		&cli.StringFlag{Name: "golden", Usage: "Path to golden copy CSV for hysteresis and rebalancing band"},
		&cli.FloatFlag{Name: "rebalance-tolerance", Value: 0.10, Usage: "Rebalancing weight tolerance %% (e.g. 0.10 for 0.10%%)"},
		&cli.IntFlag{Name: "hysteresis-buffer", Value: 5, Usage: "Extra ranks to allow existing holdings to drift"},
		&cli.IntFlag{Name: "cooldown-days", Value: 30, Usage: "Days to bar recently exited holdings from re-entering (anti-churn)"},
		&cli.IntFlag{Name: "cooldown-bypass-rank", Value: 5, Usage: "High-conviction rank threshold that bypasses the re-entry cooldown"},
		&cli.StringFlag{Name: "name", Usage: "Custom display name for output files"},
		&cli.StringFlag{Name: "out", Usage: "Custom output CSV path"},
		&cli.BoolFlag{Name: "force", Aliases: []string{"F"}, Usage: "Force re-running stock pick calculation even if snapshot already exists"},
		&cli.BoolFlag{Name: "analysis", Aliases: []string{"a"}, Usage: "Run deep quantitative deduction analysis using DuckDB"},
		&cli.StringFlag{Name: "market", Aliases: []string{"mkt"}, Usage: "Target market: 'india' or 'us' (defaults to config/defaults.json or auto-detected from --index)"},
	},
	Action: runPick,
}

func runPick(ctx context.Context, c *cli.Command) error {
	if c.Bool("analysis") {
		return RunPitAnalysisDirect(ctx, c.String("index"), c.String("method"), c.String("market"))
	}
	opts := pickOptsFromCmd(c)

	// The active market's holiday-aware settlement clock is the single authority
	// for this run's trading-day decisions (as-of, next-available, based-on).
	// Injected into opts so stockpicker shares the same clock (it cannot import
	// broker itself — layering — so the aware clock rides in as a value).
	clk := broker.TradingClock()
	opts.Clock = clk

	targetEOD := clk.SettlementDate(time.Now())
	targetDateStr := targetEOD.Format("2006-01-02")
	if opts.AsOfDate == "" {
		opts.AsOfDate = targetDateStr
	} else {
		// Normalize a caller-supplied non-trading date (weekend OR exchange
		// holiday) to the last settled trading day. The aware clock handles both
		// in one call, replacing the old hand-rolled Saturday/Sunday-only check.
		if parsed, err := time.Parse("2006-01-02", opts.AsOfDate); err == nil {
			loc := clk.Loc
			if loc == nil {
				loc = time.UTC
			}
			parsedLocal := time.Date(parsed.Year(), parsed.Month(), parsed.Day(), clk.CutoffHour, 0, 0, 0, loc)
			if !clk.IsTradingDay(parsedLocal) {
				opts.AsOfDate = clk.SettlementDate(parsedLocal).Format("2006-01-02")
			}
		}
	}

	cleanIndex := strings.NewReplacer(",", "_", " ", "_", "^", "").Replace(opts.IndexName)
	var syncTime time.Time
	if pitDB, err := pithistory.Open(""); err == nil {
		hasRun, _ := pitDB.HasRun(ctx, opts.AsOfDate, cleanIndex, opts.Method)
		if !hasRun {
			hasRun, _ = pitDB.HasRun(ctx, opts.AsOfDate, opts.IndexName, opts.Method)
		}
		if hasRun {
			syncTime, _ = pitDB.GetRunSyncTime(ctx, opts.AsOfDate, cleanIndex, opts.Method)
			if syncTime.IsZero() {
				syncTime, _ = pitDB.GetRunSyncTime(ctx, opts.AsOfDate, opts.IndexName, opts.Method)
			}
		}

		// Skip guard for index runs when snapshot already exists: display cached run details without network I/O
		if opts.FilePath == "" && opts.IndexName != "" && hasRun && !opts.Force {
			nextAvailable := clk.NextEODAvailable(time.Now())
			targetDayStr := marketdata.FormatOrdinalDay(targetEOD.Day())
			nextDayStr := marketdata.FormatOrdinalDay(nextAvailable.Day())
			nextMonthStr := nextAvailable.Format("Jan")
			if syncTime.IsZero() {
				syncTime = clk.LastSettledEOD(time.Now())
			}
			loc := clk.Loc
			if loc == nil {
				loc = time.UTC
			}
			basedOnStr := fmt.Sprintf("%s EOD (Synced: %s)", opts.AsOfDate, syncTime.In(loc).Format("2006-01-02 15:04:05 MST"))
			fmt.Printf("✓ Snapshot for %s (%s, %s) is already present in DuckDB (%s).\n",
				opts.AsOfDate, opts.IndexName, opts.Method, pithistory.DefaultDBPath)
			fmt.Printf("  Based on:         %s\n", basedOnStr)
			fmt.Printf("  Latest market data is of %s. Next market settlement file will be available after the %s %s cutoff.\n",
				targetDayStr, nextDayStr, nextMonthStr)
			fmt.Printf("  Displaying saved run details (no network fetch). Use --force to re-calculate.\n\n")

			displayCachedRunOutput(ctx, pitDB, opts, basedOnStr)
			pitDB.Close()
			return nil
		}
		pitDB.Close()
	}

	if syncTime.IsZero() {
		syncTime = clk.LastSettledEOD(time.Now())
	}
	loc := clk.Loc
	if loc == nil {
		loc = time.UTC
	}
	opts.BasedOn = fmt.Sprintf("%s EOD (Synced: %s)", opts.AsOfDate, syncTime.In(loc).Format("2006-01-02 15:04:05 MST"))

	return runPickWithOpts(ctx, opts)
}

func pickOptsFromCmd(c *cli.Command) *stockpicker.Options {
	defaults := config.LoadUserDefaults(config.Path("defaults.json"))

	indexName := c.String("index")
	if indexName == "" {
		indexName = defaults.Index
	}
	if indexName == "" {
		indexName = "sp500"
	}

	method := c.String("method")
	if method == "" {
		method = defaults.Method
	}
	if method == "" {
		method = "us_quality_momentum"
	}

	topN := c.Int("top")
	if topN == 0 {
		topN = defaults.TopN
	}
	if topN == 0 {
		topN = 20
	}

	rangeStr := c.String("range")
	if rangeStr == "" {
		rangeStr = defaults.Range
	}
	if rangeStr == "" {
		rangeStr = "3mo"
	}
	rangeStr = strings.ToLower(strings.TrimSpace(rangeStr))
	if rangeStr == "1yr" || rangeStr == "1year" {
		rangeStr = "1y"
	}
	if rangeStr != "3mo" && rangeStr != "6mo" && rangeStr != "1y" {
		rangeStr = "3mo"
	}

	return &stockpicker.Options{
		IndexName:                           indexName,
		FilePath:                            c.String("file"),
		Method:                              method,
		TopN:                                topN,
		RangeStr:                            rangeStr,
		SkipScuttlebutt:                     c.Bool("skip-scuttlebutt"),
		GoldenPath:                          c.String("golden"),
		RebalanceTolerance:                  c.Float("rebalance-tolerance"),
		HysteresisBuffer:                    int(c.Int("hysteresis-buffer")),
		HysteresisMinScoreDelta:             3.0,
		HysteresisRequireGrowthAcceleration: true,
		CooldownDays:                        int(c.Int("cooldown-days")),
		CooldownBypassRank:                  int(c.Int("cooldown-bypass-rank")),
		DisplayName:                         c.String("name"),
		OutputFile:                          c.String("out"),
		Force:                               c.Bool("force"),
	}
}

// runPickWithOpts wires the data router (Schwab for US tickers, Yahoo otherwise)
// and delegates to the unified stockpicker.RunWithResult implementation.
func runPickWithOpts(ctx context.Context, opts *stockpicker.Options) error {
	if opts.DataFetcher == nil {
		opts.DataFetcher = newDataRouter()
	}
	// Ensure every cmd-originated pick shares the active market's holiday-aware
	// settlement clock as its single trading-day authority. runPick sets this
	// explicitly; the pipeline/pit callers build Options inline, so default it
	// here (the one funnel into stockpicker from cmd) when unset. stockpicker
	// treats a zero clock as bare NSE, so this is the composition root supplying
	// the aware value it can legally assemble (broker) but stockpicker cannot.
	if opts.Clock.Loc == nil {
		opts.Clock = broker.TradingClock()
	}
	result, err := stockpicker.RunWithResult(ctx, opts)
	if err != nil {
		return err
	}

	// Persist the point-in-time run snapshot to DuckDB. Done here (composition root)
	// rather than in pkg/stockpicker, because pithistory imports stockpicker — the
	// reverse edge would be an import cycle (R16 layering).
	if result != nil && result.PITSnapshot != nil {
		if pitDB, dbErr := pithistory.Open(""); dbErr == nil {
			if sErr := pitDB.SaveRunSnapshot(ctx, result.PITSnapshot); sErr == nil {
				fmt.Printf("Persisted Point-in-Time Run Snapshot to DuckDB (%s)\n", pithistory.DefaultDBPath)
			} else {
				fmt.Printf("Warning: failed to persist PIT snapshot to DuckDB: %v\n", sErr)
			}
			pitDB.Close()
		}
	}
	return nil
}

// displayCachedRunOutput loads and renders the previous run's results without any network I/O.
func displayCachedRunOutput(ctx context.Context, pitDB *pithistory.DB, opts *stockpicker.Options, basedOnStr string) {
	// Resolve the same identity the writer used so we read from the directory the
	// run was actually filed under (honors --name/--index over a --file name).
	loadedName := opts.IndexName
	if opts.FilePath != "" {
		loadedName = stockpicker.GetUniverseName(opts.FilePath)
	}
	displayNameVal := stockpicker.DisplayName(opts, loadedName)
	identity := stockpicker.PickIdentity(displayNameVal, opts.Method)

	cleanIndex := strings.NewReplacer(",", "_", " ", "_", "^", "").Replace(opts.IndexName)

	// 1. Try loading and printing the saved selection explanation report file
	reportDir := filepath.Join("report", identity, "executions")
	dateStr := strings.ReplaceAll(opts.AsOfDate, "-", "")

	matches, _ := filepath.Glob(filepath.Join(reportDir, fmt.Sprintf("%s_*_selection_reasons.txt", dateStr)))
	if len(matches) == 0 {
		matches, _ = filepath.Glob(filepath.Join(reportDir, "*_selection_reasons.txt"))
	}

	if len(matches) > 0 {
		sort.Strings(matches)
		latestReport := matches[len(matches)-1]
		content, err := os.ReadFile(latestReport)
		if err == nil {
			genStr := fmt.Sprintf("Generated:        %s", time.Now().Format("2006-01-02 15:04:05 MST"))
			lines := strings.Split(string(content), "\n")
			var outLines []string
			headerFound := false
			for _, line := range lines {
				if strings.HasPrefix(line, "Based on:") {
					outLines = append(outLines, fmt.Sprintf("Based on:         %s", basedOnStr))
					headerFound = true
					continue
				}
				if strings.HasPrefix(line, "Generated:") {
					if !headerFound {
						outLines = append(outLines, fmt.Sprintf("Based on:         %s", basedOnStr))
					}
					outLines = append(outLines, genStr)
					continue
				}
				outLines = append(outLines, line)
			}
			reportText := strings.Join(outLines, "\n")
			stockpicker.PrintHeader(displayNameVal, opts.Method, opts.TopN, opts.RangeStr, opts.FilePath, basedOnStr)
			fmt.Print(reportText)
			fmt.Printf("\nSelection explanation report loaded from %s\n", latestReport)
			portfolioPath := filepath.Join("data", "candidates", "index_picks", fmt.Sprintf("%s.csv", identity))
			if _, pErr := os.Stat(portfolioPath); pErr == nil {
				fmt.Printf("Portfolio CSV: %s\n", portfolioPath)
			}
			incubatorPath := filepath.Join("data", "candidates", "index_picks", fmt.Sprintf("%s_incubator.csv", identity))
			if _, iErr := os.Stat(incubatorPath); iErr == nil {
				fmt.Printf("Incubator Watchlist: %s\n", incubatorPath)
			}
			return
		}
	}

	// 2. Fallback: Reconstruct and render directly from DuckDB
	stockpicker.PrintHeader(displayNameVal, opts.Method, opts.TopN, opts.RangeStr, opts.FilePath, basedOnStr)
	runs, err := pitDB.GetRunHistory(ctx, cleanIndex, opts.Method, 1)
	if err == nil && len(runs) > 0 {
		r := runs[0]
		fmt.Printf("\n====================================================================\n")
		fmt.Printf("             Stock Selection & Rejection Explanation Report\n")
		fmt.Printf("====================================================================\n")
		fmt.Printf("Index/File:       %s\n", displayNameVal)
		fmt.Printf("Strategy Preset:  %s\n", opts.Method)
		fmt.Printf("Based on:         %s\n", basedOnStr)
		fmt.Printf("Generated:        %s\n", time.Now().Format("2006-01-02 15:04:05 MST"))
		fmt.Printf("====================================================================\n\n")

		fmt.Printf("--- SUMMARY ---\n")
		fmt.Printf("Initial pool size:                     %d constituents\n", r.TotalConstituents)
		fmt.Printf("Passed Stage 1 Safety/Hard Filters:    %d stocks\n", r.Stage1Survivors)
		fmt.Printf("Eliminated by Stage 1 Safety Filters:  %d stocks\n", r.TotalConstituents-r.Stage1Survivors)
		fmt.Printf("Final Selected Stocks:                 %d stocks\n\n", r.SelectedCount)
	}
}
