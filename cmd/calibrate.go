package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/backtest"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
	"github.com/raghavkgarg/mycase/pkg/universe"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

var CalibrateCommand = &cli.Command{
	Name:  "calibrate",
	Usage: "Run rolling Spearman Rank IC and empirical parameter calibration for strategy pillars",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "index", Aliases: []string{"i"}, Value: "microcap250,smallcap250", Usage: "Index constituents to evaluate"},
		&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Usage: "Path to custom CSV file (takes precedence over --index)"},
		&cli.StringFlag{Name: "method", Aliases: []string{"m"}, Value: "earlymb", Usage: "Strategy method (earlymb, multibagger)"},
		&cli.IntFlag{Name: "step", Value: 21, Usage: "Rolling evaluation step in trading days (e.g. 21 for monthly)"},
		&cli.IntFlag{Name: "forward", Value: 21, Usage: "Forward return horizon in trading days (e.g. 21 for 21-day forward return)"},
		&cli.FloatFlag{Name: "train-ratio", Value: 0.70, Usage: "In-sample training split fraction (e.g. 0.70 for 70% train / 30% test)"},
		&cli.BoolFlag{Name: "save-snapshot", Usage: "Save current constituent snapshot to data/universe_snapshots/"},
	},
	Action: runCalibrate,
}

func runCalibrate(ctx context.Context, c *cli.Command) error {
	indexName := c.String("index")
	filePath := c.String("file")
	method := strings.ToLower(strings.TrimSpace(c.String("method")))
	stepDays := int(c.Int("step"))
	forwardDays := int(c.Int("forward"))
	trainRatio := c.Float("train-ratio")
	saveSnap := c.Bool("save-snapshot")

	fmt.Printf("====================================================================\n")
	fmt.Printf("          Go Mycase Strategy Pillar IC Calibration Engine           \n")
	fmt.Printf("====================================================================\n")
	fmt.Printf("Strategy:    %s\n", method)
	fmt.Printf("Universe:    %s\n", indexName)
	fmt.Printf("Step / Fwd:  %d Days / %d Days\n", stepDays, forwardDays)
	fmt.Printf("Train Split: %.0f%% In-Sample / %.0f%% Out-of-Sample\n", trainRatio*100.0, (1.0-trainRatio)*100.0)
	fmt.Printf("====================================================================\n\n")

	// 1. Resolve Universe Constituents
	tickersSrc, err := stockpicker.LoadConstituents(filePath, indexName)
	if err != nil {
		return fmt.Errorf("failed to load constituents for %s: %w", indexName, err)
	}
	tickers := tickersSrc.Tickers
	if saveSnap {
		if sErr := universe.SaveSnapshot(indexName, time.Now(), tickers); sErr != nil {
			fmt.Printf("Warning: Failed to save snapshot: %v\n", sErr)
		} else {
			fmt.Printf("Saved constituent snapshot for %s to %s\n", indexName, universe.SnapshotDir)
		}
	}

	if len(tickers) == 0 {
		return fmt.Errorf("no tickers loaded")
	}
	fmt.Printf("Loaded %d constituents for calibration.\n", len(tickers))

	// 2. Fetch Historical Prices (e.g. 3 years for multi-year IC estimation)
	fmt.Printf("Fetching historical price series (3y) for %d tickers...\n", len(tickers))
	fullHistory := make(map[string]*yfinance.HistoricalData)
	for i, t := range tickers {
		hist, hErr := yfinance.FetchHistoricalDataWithTimestamps(ctx, t, "3y")
		if hErr == nil && hist != nil && len(hist.Closes) >= 60 {
			fullHistory[t] = hist
		}
		if (i+1)%50 == 0 || i+1 == len(tickers) {
			fmt.Printf("Fetched %d / %d tickers...\n", i+1, len(tickers))
		}
	}

	// 3. Fetch Benchmark Series (^NSEI)
	benchSym := stockpicker.GetBenchmarkSymbolForIndex(indexName, tickers)
	fmt.Printf("Fetching benchmark data for %s (3y)...\n", benchSym)
	benchHist, bErr := yfinance.FetchHistoricalDataWithTimestamps(ctx, benchSym, "3y")
	if bErr != nil || benchHist == nil || len(benchHist.Closes) < 100 {
		return fmt.Errorf("failed to fetch benchmark history for %s: %v", benchSym, bErr)
	}

	// 4. Fetch Fundamentals
	fmt.Printf("Fetching fundamentals for %d active tickers...\n", len(fullHistory))
	activeKeys := make([]string, 0, len(fullHistory))
	for k := range fullHistory {
		activeKeys = append(activeKeys, k)
	}
	fundamentals, fErr := yfinance.FetchFundamentals(ctx, activeKeys)
	if fErr != nil {
		fmt.Printf("Warning: fetching fundamentals: %v\n", fErr)
	}

	// 5. Load Strategy Hard Filters
	hardFilters, _ := config.LoadHardFilters("config/mfs.json", method)

	// 6. Run Calibration Engine
	fmt.Printf("\nRunning Out-of-Sample Rolling Spearman IC Calibration...\n")
	summary, calibErr := backtest.RunEarlyMBCalibration(
		ctx,
		activeKeys,
		fullHistory,
		benchHist,
		fundamentals,
		hardFilters,
		stepDays,
		forwardDays,
		trainRatio,
	)
	if calibErr != nil {
		return fmt.Errorf("calibration failed: %w", calibErr)
	}

	// 7. Display Results
	printCalibrationReport(summary)
	return nil
}

func printCalibrationReport(s *backtest.CalibrationSummary) {
	fmt.Printf("\n========================================================================================\n")
	fmt.Printf("                EMPIRICAL INFORMATION COEFFICIENT (IC) CALIBRATION REPORT               \n")
	fmt.Printf("========================================================================================\n")
	fmt.Printf("Total Evaluated Periods: %d  |  Train Periods: %d  |  Held-Out Test Periods: %d\n",
		s.TotalPeriods, s.TrainPeriodCount, s.TestPeriodCount)
	fmt.Printf("----------------------------------------------------------------------------------------\n\n")

	fmt.Printf("--- 1. IN-SAMPLE TRAINING WINDOW STATS (First %d Periods) ---\n", s.TrainPeriodCount)
	fmt.Printf("%-24s | %-8s | %-8s | %-8s | %-8s | %-8s\n", "Pillar Metric", "Mean IC", "Std IC", "IR (IC/s)", "t-Stat", "Pos IC %")
	fmt.Printf("----------------------------------------------------------------------------------------\n")

	keys := []string{"composite_rs", "vcp_tightness", "winsorized_rvol", "decayed_pp", "delivery_delta", "raw_composite", "effective_composite"}
	names := map[string]string{
		"composite_rs":        "1. Composite RS",
		"vcp_tightness":       "2. VCP Tightness (Inv)",
		"winsorized_rvol":     "3A. Winsorized RVOL Z",
		"decayed_pp":          "3B. Decayed PP",
		"delivery_delta":      "4. Delivery Delta",
		"raw_composite":       "Raw Composite Score",
		"effective_composite": "Effective Score (x R)",
	}

	for _, k := range keys {
		st := s.TrainStats[k]
		fmt.Printf("%-24s | %+7.4f | %7.4f | %+7.3f | %+7.2f | %6.1f%%\n",
			names[k], st.MeanIC, st.StdIC, st.IR, st.TStat, st.PositivePct)
	}

	if s.TestPeriodCount > 0 && s.TestStats != nil {
		fmt.Printf("\n--- 2. HELD-OUT EVALUATION WINDOW STATS (Last %d Periods - Out-of-Sample) ---\n", s.TestPeriodCount)
		fmt.Printf("%-24s | %-8s | %-8s | %-8s | %-8s | %-8s\n", "Pillar Metric", "Mean IC", "Std IC", "IR (IC/s)", "t-Stat", "Pos IC %")
		fmt.Printf("----------------------------------------------------------------------------------------\n")
		for _, k := range keys {
			st := s.TestStats[k]
			fmt.Printf("%-24s | %+7.4f | %7.4f | %+7.3f | %+7.2f | %6.1f%%\n",
				names[k], st.MeanIC, st.StdIC, st.IR, st.TStat, st.PositivePct)
		}
	}

	fmt.Printf("\n--- 3. EMPIRICAL REFERENCE BOUNDS DERIVED FROM TRAINING DATA (P5 to P95) ---\n")
	fmt.Printf("  * Composite RS Bound:   [%+.1f%%, %+.1f%%]\n", s.CalibratedBounds.CompositeRS[0]*100.0, s.CalibratedBounds.CompositeRS[1]*100.0)
	fmt.Printf("  * VCP ATR Ratio Bound:  [%.2f, %.2f]\n", s.CalibratedBounds.VCPRatio[0], s.CalibratedBounds.VCPRatio[1])
	fmt.Printf("  * RVOL Z-Score Bound:   [%+.2fσ, %+.2fσ]\n", s.CalibratedBounds.WinsorizedRVOL[0], s.CalibratedBounds.WinsorizedRVOL[1])
	fmt.Printf("  * Decayed PP Bound:     [%.2f, %.2f]\n", s.CalibratedBounds.DecayedPP[0], s.CalibratedBounds.DecayedPP[1])
	fmt.Printf("  * Delivery Delta Bound: [%+.1f%%, %+.1f%%]\n", s.CalibratedBounds.DeliveryDelta[0]*100.0, s.CalibratedBounds.DeliveryDelta[1]*100.0)

	fmt.Printf("\n--- 4. RECOMMENDED INFORMATION-RATIO (IR) OPTIMAL WEIGHTS ---\n")
	for pillar, w := range s.RecommendedWeights {
		fmt.Printf("  * %-20s: %5.1f pts (%.1f%%)\n", pillar, w, w)
	}
	fmt.Printf("========================================================================================\n\n")
}
