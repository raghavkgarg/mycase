package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/pithistory"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
)

var PitCommand = &cli.Command{
	Name:  "pit",
	Usage: "Point-in-Time research database management and empirical calibration analytics",
	Commands: []*cli.Command{
		{
			Name:  "update",
			Usage: "Execute daily PIT screening run and update data/pit_history.db",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "index", Aliases: []string{"i"}, Value: "microcap250,smallcap250", Usage: "Indices to evaluate"},
				&cli.StringFlag{Name: "method", Aliases: []string{"m"}, Value: "earlymb", Usage: "Strategy method (earlymb, multibagger)"},
				&cli.IntFlag{Name: "top", Aliases: []string{"t"}, Value: 10, Usage: "Number of top stocks to select"},
			},
			Action: runPitUpdate,
		},
		{
			Name:  "stats",
			Usage: "Display empirical percentile distributions and historical run stats from data/pit_history.db",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "index", Aliases: []string{"i"}, Value: "microcap250_smallcap250", Usage: "Index name to analyze"},
				&cli.StringFlag{Name: "method", Aliases: []string{"m"}, Value: "earlymb", Usage: "Strategy method to analyze"},
				&cli.IntFlag{Name: "days", Value: 60, Usage: "Rolling history lookback window in calendar days (0 for all)"},
				&cli.StringFlag{Name: "ticker", Usage: "Optional specific ticker to view score trajectory for"},
			},
			Action: runPitStats,
		},
	},
}

func runPitUpdate(ctx context.Context, c *cli.Command) error {
	indexVal := c.String("index")
	methodVal := c.String("method")
	topN := int(c.Int("top"))

	fmt.Printf("Executing Point-in-Time Daily Screening Update for %s (%s)...\n", indexVal, methodVal)
	opts := &stockpicker.Options{
		IndexName:          indexVal,
		Method:             methodVal,
		TopN:               topN,
		RangeStr:           "1y",
		RebalanceTolerance: 0.10,
	}

	if err := runPickWithOpts(ctx, opts); err != nil {
		return fmt.Errorf("daily pit update failed: %w", err)
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

	if ticker != "" {
		if !strings.HasPrefix(ticker, "NSE:") {
			ticker = "NSE:" + ticker
		}
		hist, err := db.GetCandidateHistory(ctx, ticker, 20)
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
		fmt.Printf("%-12s | %-8s | %-9s | %-9s | %-8s | %-8s | %-8s | %-8s\n",
			"AsOf Date", "Stage-1", "Raw Score", "Eff Score", "VCP ATR", "RVOL Z", "Deliv Δ", "Selected")
		fmt.Printf("----------------------------------------------------------------------------------------\n")
		for _, r := range hist {
			stage1Str := "PASS"
			if !r.PassedStage1 {
				stage1Str = "FAIL"
			}
			selStr := "NO"
			if r.Selected {
				selStr = fmt.Sprintf("YES (%.1f%%)", r.FinalWeight*100.0)
			}
			fmt.Printf("%-12s | %-8s | %9.1f | %9.1f | %8.2f | %+8.1f | %+7.1f%% | %-8s\n",
				r.AsOfDate, stage1Str, r.RawScore, r.EffectiveScore, r.VCPRatio, r.RVOLZScore, r.DeliveryDelta*100.0, selStr)
		}
		fmt.Printf("========================================================================================\n")
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
