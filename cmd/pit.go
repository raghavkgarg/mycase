package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/marketdata"
	"github.com/raghavkgarg/mycase/pkg/pithistory"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
)

var PitCommand = &cli.Command{
	Name:  "pit",
	Usage: "Point-in-Time research database management and empirical calibration analytics",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "index", Aliases: []string{"i"}, Value: "niftytotalmarket", Usage: "Index name to analyze"},
		&cli.StringFlag{Name: "method", Aliases: []string{"m"}, Value: "earlymb", Usage: "Strategy method to analyze"},
		&cli.StringFlag{Name: "market", Aliases: []string{"mkt"}, Usage: "Target market: 'india' or 'us' (defaults to config/defaults.json or auto-detected from --index)"},
		&cli.BoolFlag{Name: "analysis", Aliases: []string{"a"}, Usage: "Run deep quantitative deduction analysis using DuckDB"},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		if c.Bool("analysis") {
			return runPitAnalysis(ctx, c)
		}
		return runPitStats(ctx, c)
	},
	Commands: []*cli.Command{
		{
			Name:  "update",
			Usage: "Execute daily PIT screening run and update data/mycase.db",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "index", Aliases: []string{"i"}, Value: "microcap250,smallcap250", Usage: "Indices to evaluate"},
				&cli.StringFlag{Name: "method", Aliases: []string{"m"}, Value: "earlymb", Usage: "Strategy method (earlymb, multibagger)"},
				&cli.IntFlag{Name: "top", Aliases: []string{"t"}, Value: 10, Usage: "Number of top stocks to select"},
				&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "Force re-running PIT calculation even if file already exists"},
			},
			Action: runPitUpdate,
		},
		{
			Name:  "stats",
			Usage: "Display empirical percentile distributions and historical run stats from data/mycase.db",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "index", Aliases: []string{"i"}, Value: "microcap250_smallcap250", Usage: "Index name to analyze"},
				&cli.StringFlag{Name: "method", Aliases: []string{"m"}, Value: "earlymb", Usage: "Strategy method to analyze"},
				&cli.StringFlag{Name: "market", Aliases: []string{"mkt"}, Usage: "Target market: 'india' or 'us' (defaults to config/defaults.json or auto-detected from --index)"},
				&cli.IntFlag{Name: "days", Value: 60, Usage: "Rolling history lookback window in calendar days (0 for all)"},
				&cli.StringFlag{Name: "ticker", Usage: "Optional specific ticker to view score trajectory for"},
				&cli.BoolFlag{Name: "analysis", Aliases: []string{"a"}, Usage: "Run deep quantitative deduction analysis using DuckDB"},
				&cli.BoolFlag{Name: "shadow", Aliases: []string{"s"}, Usage: "Display Stage-1 Shadow Mode divergence (Legacy vs Relief Gate)"},
			},
			Action: func(ctx context.Context, c *cli.Command) error {
				if c.Bool("shadow") {
					return runPitShadow(ctx, c)
				}
				if c.Bool("analysis") {
					return runPitAnalysis(ctx, c)
				}
				return runPitStats(ctx, c)
			},
		},
		{
			Name:    "analysis",
			Aliases: []string{"analyze"},
			Usage:   "Run deep quantitative and operational deduction analysis using DuckDB",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "index", Aliases: []string{"i"}, Value: "niftytotalmarket", Usage: "Index name to analyze"},
				&cli.StringFlag{Name: "method", Aliases: []string{"m"}, Value: "earlymb", Usage: "Strategy method to analyze"},
				&cli.StringFlag{Name: "market", Aliases: []string{"mkt"}, Usage: "Target market: 'india' or 'us' (defaults to config/defaults.json or auto-detected from --index)"},
				&cli.IntFlag{Name: "days", Value: 60, Usage: "Rolling history lookback window in calendar days (0 for all)"},
				&cli.StringFlag{Name: "ticker", Usage: "Optional specific ticker to view score trajectory for"},
				&cli.BoolFlag{Name: "churn", Usage: "Display Gate Churn Rate and Pool Stability Oscillator"},
			},
			Action: runPitAnalysis,
		},
		{
			Name:    "stage",
			Aliases: []string{"staging", "pre-prod"},
			Usage:   "Generate Tier-2 pre-production staging basket (data/pre_microsmall.csv) from top qualified candidates",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "index", Aliases: []string{"i"}, Value: "niftytotalmarket", Usage: "Index name to stage"},
				&cli.StringFlag{Name: "method", Aliases: []string{"m"}, Value: "earlymb", Usage: "Strategy method (earlymb)"},
				&cli.IntFlag{Name: "top", Aliases: []string{"n", "t"}, Value: 15, Usage: "Number of top candidates to stage"},
				&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Value: "data/pre_microsmall.csv", Usage: "Output CSV file path"},
				&cli.StringFlag{Name: "exclude", Aliases: []string{"x"}, Usage: "Path to live basket CSV whose active holdings should be excluded from staging (e.g. data/microsmall.csv)"},
			},
			Action: runPitStage,
		},
		{
			Name:    "sentry",
			Aliases: []string{"holding-sentry", "decay-audit"},
			Usage:   "Evaluate active live holdings for technical decay and distribution (Levels 1-3 Sentry)",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "basket", Aliases: []string{"b"}, Value: "data/microsmall.csv", Usage: "Path to live portfolio basket CSV"},
				&cli.StringFlag{Name: "staged", Aliases: []string{"s"}, Value: "data/pre_microsmall.csv", Usage: "Path to pre-production staged candidates CSV"},
				&cli.BoolFlag{Name: "apply", Usage: "Automatically execute and apply proposed Level-3 rebalance swaps to the live basket CSV"},
				&cli.BoolFlag{Name: "strict-sector", Usage: "Enforce strict max 3 stocks per sector (e.g. substituting PARKHOSPS with BALRAMCHIN)"},
			},
			Action: runPitSentry,
		},
		{
			Name:    "retry",
			Aliases: []string{"retry-failed"},
			Usage:   "Re-run historical price and fundamental fetch specifically for failed tickers in a PIT snapshot",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "index", Aliases: []string{"i"}, Value: "niftytotalmarket", Usage: "Index name to retry"},
				&cli.StringFlag{Name: "method", Aliases: []string{"m"}, Value: "earlymb", Usage: "Strategy method (earlymb, multibagger)"},
				&cli.StringFlag{Name: "date", Aliases: []string{"d"}, Value: "2026-09-01", Usage: "As-of date of the PIT snapshot to heal (defaults to latest)"},
			},
			Action: runPitRetry,
		},
		{
			Name:    "consensus",
			Aliases: []string{"synergy", "dual-leaders"},
			Usage:   "Display cross-strategy consensus leaderboard combining Multibagger and Early Multibagger",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "date", Aliases: []string{"d"}, Usage: "As-of date (YYYY-MM-DD), defaults to latest available"},
				&cli.IntFlag{Name: "top", Aliases: []string{"n", "t"}, Value: 15, Usage: "Number of top consensus candidates to display"},
			},
			Action: runPitConsensus,
		},
	},
}

func runPitUpdate(ctx context.Context, c *cli.Command) error {
	indexVal := c.String("index")
	methodVal := c.String("method")
	topN := int(c.Int("top"))
	isForce := c.Bool("force")

	now := time.Now()
	clk := broker.TradingClock()
	targetEOD := clk.SettlementDate(now)
	targetDateStr := targetEOD.Format("2006-01-02")
	nextAvailable := clk.NextEODAvailable(now)

	cleanIndex := strings.NewReplacer(",", "_", " ", "_", "^", "").Replace(indexVal)
	pitDB, err := pithistory.Open("")
	if err == nil {
		hasRun, _ := pitDB.HasRun(ctx, targetDateStr, cleanIndex, methodVal)
		if !hasRun {
			hasRun, _ = pitDB.HasRun(ctx, targetDateStr, indexVal, methodVal)
		}
		pitDB.Close()
		if hasRun && !isForce {
			targetDayStr := marketdata.FormatOrdinalDay(targetEOD.Day())
			nextDayStr := marketdata.FormatOrdinalDay(nextAvailable.Day())
			nextMonthStr := nextAvailable.Format("Jan")
			fmt.Printf("✓ File available for %s. Latest file is of %s and %s file will be available after 21:00 PM %s %s.\n",
				targetDateStr, targetDayStr, nextDayStr, nextDayStr, nextMonthStr)
			return nil
		}
	}

	fmt.Printf("Executing Point-in-Time Daily Screening Update for %s (%s) [Target: %s]...\n", indexVal, methodVal, targetDateStr)
	opts := &stockpicker.Options{
		IndexName:          indexVal,
		Method:             methodVal,
		TopN:               topN,
		RangeStr:           "1y",
		RebalanceTolerance: 0.10,
		AsOfDate:           targetDateStr,
		Clock:              clk,
	}

	if err := runPickWithOpts(ctx, opts); err != nil {
		return fmt.Errorf("daily pit update failed: %w", err)
	}

	// Post-screening Data Integrity Verification
	if pitDB, pErr := pithistory.Open(""); pErr == nil {
		if integrity, iErr := pitDB.CheckDataIntegrity(ctx, indexVal, methodVal); iErr == nil && integrity.TotalCandidates > 0 {
			if integrity.FailurePct >= 5.0 {
				fmt.Printf("\n⚠️  [DATA INTEGRITY WARNING]: %d / %d candidates (%.1f%%) in latest run have unverified or missing fundamentals!\n",
					integrity.FailedCandidates, integrity.TotalCandidates, integrity.FailurePct)
				if len(integrity.FlaggedTickers) > 0 {
					fmt.Printf("   Flagged candidates sample: %s\n", strings.Join(integrity.FlaggedTickers, ", "))
				}
				fmt.Printf("   Verify data feeds before interpreting marginal scores or executing trades.\n\n")
			} else {
				fmt.Printf("   ✓ Data Integrity Verified: %d / %d candidates clean (0 unverified).\n",
					integrity.TotalCandidates-integrity.FailedCandidates, integrity.TotalCandidates)
			}
		}
		pitDB.Close()
	}

	fmt.Println("\nPoint-in-Time Daily Screening Update completed successfully.")
	return nil
}

func runPitStats(ctx context.Context, c *cli.Command) error {
	indexVal := strings.NewReplacer(",", "_", " ", "_", "^", "").Replace(c.String("index"))
	methodVal := c.String("method")
	days := int(c.Int("days"))
	ticker := c.String("ticker")

	db, err := pithistory.Open("")
	if err != nil {
		return fmt.Errorf("failed to open pit database: %w", err)
	}
	defer db.Close()

	// Pre-flight Data Integrity Check
	if integrity, iErr := db.CheckDataIntegrity(ctx, indexVal, methodVal); iErr == nil && integrity.TotalCandidates > 0 {
		if integrity.FailurePct >= 5.0 {
			fmt.Printf("⚠️  [DATA INTEGRITY WARNING]: %d / %d candidates (%.1f%%) in latest run have unverified or missing fundamentals!\n",
				integrity.FailedCandidates, integrity.TotalCandidates, integrity.FailurePct)
			if len(integrity.FlaggedTickers) > 0 {
				fmt.Printf("   Flagged candidates sample: %s\n", strings.Join(integrity.FlaggedTickers, ", "))
			}
			fmt.Printf("   Verify data feeds before interpreting marginal scores or executing trades.\n\n")
		}
	}

	if ticker != "" {
		if !strings.HasPrefix(ticker, "NSE:") {
			ticker = "NSE:" + ticker
		}
		targetIndex := ""
		if c.IsSet("index") {
			targetIndex = indexVal
		}
		targetMethod := ""
		if c.IsSet("method") {
			targetMethod = methodVal
		}
		hist, err := db.GetCandidateHistoryFiltered(ctx, ticker, targetIndex, targetMethod, 20)
		if err != nil {
			return fmt.Errorf("failed to query ticker history: %w", err)
		}
		fmt.Printf("========================================================================================\n")
		fmt.Printf("                HISTORICAL POINT-IN-TIME TRAJECTORY FOR %s               \n", ticker)
		fmt.Printf("========================================================================================\n")
		if len(hist) == 0 {
			fmt.Printf("No historical records found for %s in %s\n", ticker, pithistory.DefaultDBPath)
			return nil
		}
		fmt.Printf("%-12s | %-16s | %-12s | %-8s | %-9s | %-9s | %-8s | %-8s | %-8s | %-8s\n",
			"AsOf Date", "Index", "Method", "Stage-1", "Raw Score", "Eff Score", "VCP ATR", "RVOL Z", "Deliv Δ", "Selected")
		fmt.Printf("----------------------------------------------------------------------------------------------------------------------------\n")
		for _, r := range hist {
			stage1Str := "PASS"
			if !r.PassedStage1 {
				stage1Str = "FAIL"
			}
			selStr := "NO"
			if r.Selected {
				selStr = fmt.Sprintf("YES (%.1f%%)", r.FinalWeight*100.0)
			}
			fmt.Printf("%-12s | %-16s | %-12s | %-8s | %9.1f | %9.1f | %8.2f | %+8.1f | %+7.1f%% | %-8s\n",
				r.AsOfDate, r.IndexName, r.Method, stage1Str, r.RawScore, r.EffectiveScore, r.VCPRatio, r.RVOLZScore, r.DeliveryDelta*100.0, selStr)
		}
		fmt.Printf("============================================================================================================================\n")
		return nil
	}

	// General Stats
	q, err := db.GetEmpiricalQuantiles(ctx, indexVal, methodVal, days)
	if err != nil {
		return fmt.Errorf("failed to compute empirical quantiles: %w", err)
	}

	runs, err := db.GetRunHistory(ctx, indexVal, methodVal, 10)
	if err != nil {
		return fmt.Errorf("failed to query run history: %w", err)
	}

	fmt.Printf("========================================================================================\n")
	fmt.Printf("            POINT-IN-TIME RESEARCH DATABASE ANALYTICS (DuckDB)          \n")
	fmt.Printf("========================================================================================\n")
	fmt.Printf("Database:   %s\n", pithistory.DefaultDBPath)
	fmt.Printf("Universe:   %s\n", indexVal)
	fmt.Printf("Strategy:   %s\n", methodVal)
	if days > 0 {
		fmt.Printf("Window:     Last %d Days\n", days)
	} else {
		fmt.Printf("Window:     All Historical Records\n")
	}
	fmt.Printf("----------------------------------------------------------------------------------------\n")
	fmt.Printf("--- 1. EMPIRICAL RAW SCORE QUANTILE DISTRIBUTION (Stage-1 Survivors, N=%.0f) ---\n", q["samples"])
	fmt.Printf("  * P90 (Top Decile Floor)  : %5.1f pts\n", q["p90"])
	fmt.Printf("  * P75 (Top Quartile Floor): %5.1f pts\n", q["p75"])
	fmt.Printf("  * P50 (Median Score)      : %5.1f pts\n", q["p50"])
	fmt.Printf("  * P40 (Empirical Cutoff)  : %5.1f pts\n", q["p40"])
	fmt.Printf("  * P25 (Lower Quartile)    : %5.1f pts\n", q["p25"])
	fmt.Printf("----------------------------------------------------------------------------------------\n")
	fmt.Printf("--- 2. RECENT POINT-IN-TIME SCREENING RUNS ---\n")
	fmt.Printf("%-12s | %-10s | %-10s | %-12s | %-10s\n", "Date", "Regime R", "Total Pool", "Stage-1 Pass", "Selected")
	fmt.Printf("----------------------------------------------------------------------------------------\n")
	for _, r := range runs {
		fmt.Printf("%-12s | %10.2f | %10d | %12d | %10d\n",
			r.AsOfDate, r.RegimeMultiplier, r.TotalConstituents, r.Stage1Survivors, r.SelectedCount)
	}
	fmt.Printf("========================================================================================\n")

	return nil
}

func runPitAnalysis(ctx context.Context, c *cli.Command) error {
	indexVal := strings.NewReplacer(",", "_", " ", "_", "^", "").Replace(c.String("index"))
	methodVal := c.String("method")
	marketVal := c.String("market")
	days := int(c.Int("days"))
	if c.Bool("churn") {
		db, err := pithistory.Open("")
		if err != nil {
			return fmt.Errorf("failed to open pit database: %w", err)
		}
		defer db.Close()
		return db.PrintGateChurnReport(ctx, indexVal, methodVal, days)
	}
	return RunPitAnalysisWithOptions(ctx, indexVal, methodVal, days, marketVal)
}

// RunPitAnalysisDirect executes the DuckDB deep analysis engine directly without CLI context overhead.
func RunPitAnalysisDirect(ctx context.Context, indexName, method string, marketOverride ...string) error {
	return RunPitAnalysisWithOptions(ctx, indexName, method, 60, marketOverride...)
}

// RunPitAnalysisWithOptions executes the DuckDB deep analysis engine with custom lookback days.
func RunPitAnalysisWithOptions(ctx context.Context, indexName, method string, days int, marketOverride ...string) error {
	indexVal := strings.NewReplacer(",", "_", " ", "_", "^", "").Replace(indexName)
	if indexVal == "" {
		indexVal = "niftytotalmarket"
	}
	if method == "" {
		method = "earlymb"
	}

	db, err := pithistory.Open("")
	if err != nil {
		return fmt.Errorf("failed to open pit database: %w", err)
	}
	defer db.Close()

	return db.RunDeepAnalysis(ctx, indexVal, method, days, marketOverride...)
}

func runPitShadow(ctx context.Context, c *cli.Command) error {
	indexVal := c.String("index")
	method := c.String("method")
	if method == "" {
		method = "earlymb"
	}

	db, err := pithistory.Open("")
	if err != nil {
		return fmt.Errorf("failed to open pit database: %w", err)
	}
	defer db.Close()

	latestDate, err := db.GetLatestRunDate(ctx, indexVal, method)
	if err != nil || latestDate == "" {
		latestDate, _ = db.GetLatestRunDate(ctx, "niftytotalmarket", method)
		if latestDate == "" {
			return fmt.Errorf("no historical runs found for %s (%s)", indexVal, method)
		}
	}

	return db.PrintShadowDivergence(ctx, latestDate, indexVal, method)
}

func runPitRetry(ctx context.Context, c *cli.Command) error {
	indexVal := c.String("index")
	methodVal := c.String("method")
	dateVal := c.String("date")

	snap, err := stockpicker.RetryFailedSnapshotCandidates(ctx, indexVal, methodVal, dateVal)
	if err != nil {
		return fmt.Errorf("pit retry failed: %w", err)
	}

	db, err := pithistory.Open("")
	if err == nil {
		defer db.Close()
		if dbErr := db.SaveRunSnapshot(ctx, snap); dbErr != nil {
			fmt.Printf("Warning: failed to update DuckDB: %v\n", dbErr)
		} else {
			fmt.Printf("Successfully synchronized updated run snapshot into DuckDB (%s).\n", pithistory.DefaultDBPath)
		}
	}

	return nil
}

func runPitConsensus(ctx context.Context, c *cli.Command) error {
	dateVal := c.String("date")
	topN := int(c.Int("top"))

	db, err := pithistory.Open("")
	if err != nil {
		return fmt.Errorf("failed to open pit database: %w", err)
	}
	defer db.Close()

	return db.PrintConsensusLeaders(ctx, dateVal, topN)
}

func runPitStage(ctx context.Context, c *cli.Command) error {
	indexVal := strings.NewReplacer(",", "_", " ", "_", "^", "").Replace(c.String("index"))
	methodVal := c.String("method")
	topN := int(c.Int("top"))
	outPath := c.String("output")
	excludePath := c.String("exclude")

	db, err := pithistory.Open("")
	if err != nil {
		return fmt.Errorf("failed to open pit database: %w", err)
	}
	defer db.Close()

	staged, err := db.StagePreProduction(ctx, indexVal, methodVal, topN, outPath, excludePath)
	if err != nil {
		return fmt.Errorf("staging failed: %w", err)
	}

	pithistory.PrintStagingSummary(staged, outPath, excludePath)
	return nil
}

func runPitSentry(ctx context.Context, c *cli.Command) error {
	basketPath := c.String("basket")
	stagedPath := c.String("staged")
	apply := c.Bool("apply")
	strictSector := c.Bool("strict-sector")

	db, err := pithistory.Open("")
	if err != nil {
		return fmt.Errorf("failed to open pit database: %w", err)
	}
	defer db.Close()

	opts := pithistory.SentryOptions{
		StrictSector: strictSector,
	}

	results, overlaps, rebalances, err := db.EvaluateHoldingSentry(ctx, basketPath, stagedPath, opts)
	if err != nil {
		return fmt.Errorf("sentry evaluation failed: %w", err)
	}

	pithistory.PrintSentryReport(results, overlaps, rebalances, basketPath, stagedPath)

	if apply {
		if len(rebalances) == 0 {
			fmt.Println("\nNo Level-3 trend ruptures detected; live basket is healthy, no swaps needed.")
			return nil
		}
		if err := pithistory.ApplyRebalanceSwaps(basketPath, rebalances); err != nil {
			return fmt.Errorf("failed to apply rebalance swaps: %w", err)
		}
	}

	return nil
}
