package cmd

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"log/slog"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/attribution"
	"github.com/raghavkgarg/mycase/pkg/backtest"
	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/render"
)

var PerformanceCommand = &cli.Command{
	Name:  "performance",
	Usage: "Simulate portfolio performance from a purchase date to latest close",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Required: true, Usage: "Path to the portfolio CSV file"},
		&cli.FloatFlag{Name: "capital", Value: 100000.0, Usage: "Total capital invested"},
		&cli.StringFlag{Name: "date", Usage: "Purchase date in YYYY-MM-DD or YYYYMMDD format (IST, default: today)"},
		&cli.StringFlag{Name: "time", Value: "09:30", Usage: "Purchase time in HH:MM format (IST)"},
		&cli.BoolFlag{Name: "vs-benchmark", Usage: "Build a daily NAV series and report alpha / information ratio vs a passive benchmark, persisting the series to the cache"},
		&cli.BoolFlag{Name: "decompose", Usage: "With --vs-benchmark: decompose active return into selection (picks vs index) and rebalancing (re-selection vs holding the first basket) effects"},
		&cli.StringFlag{Name: "benchmark", Usage: "Benchmark ticker for --vs-benchmark (default: US:SPY)"},
		&cli.StringFlag{Name: "since", Usage: "Start date for --vs-benchmark NAV series in YYYY-MM-DD or YYYYMMDD (default: 1 year ago)"},
	},
	Action: runPerformance,
}

func runPerformance(ctx context.Context, c *cli.Command) error {
	if c.Bool("vs-benchmark") {
		return runVsBenchmark(ctx, c.String("file"), c.Float("capital"), c.String("since"), c.String("benchmark"), c.Bool("decompose"))
	}
	return runPerfWithParams(ctx, c.String("file"), c.Float("capital"), c.String("date"), c.String("time"))
}

func runPerfWithParams(ctx context.Context, filePath string, capital float64, targetDateStr, targetTimeStr string) error {
	if filePath == "" {
		return fmt.Errorf("--file parameter is required")
	}

	timeParts := strings.Split(targetTimeStr, ":")
	if len(timeParts) != 2 {
		return fmt.Errorf("invalid time format. Must be HH:MM")
	}
	targetHour, err := strconv.Atoi(timeParts[0])
	if err != nil {
		return fmt.Errorf("parsing hour: %w", err)
	}
	targetMin, err := strconv.Atoi(timeParts[1])
	if err != nil {
		return fmt.Errorf("parsing minute: %w", err)
	}

	istLoc := time.FixedZone("IST", 5*3600+30*60)
	nowIST := time.Now().In(istLoc)

	targetDate, err := parsePerfDate(targetDateStr, istLoc)
	if err != nil {
		return err
	}

	targetTime := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), targetHour, targetMin, 0, 0, istLoc)

	useDailyClose := false
	rangeStr := "1d"
	daysDiff := nowIST.Sub(targetTime).Hours() / 24.0

	if targetTime.Before(time.Date(nowIST.Year(), nowIST.Month(), nowIST.Day(), 0, 0, 0, 0, istLoc)) {
		rangeStr = "7d"
	}
	if daysDiff > 7.0 {
		useDailyClose = true
		switch {
		case daysDiff <= 30.0:
			rangeStr = "1mo"
		case daysDiff <= 90.0:
			rangeStr = "3mo"
		case daysDiff <= 180.0:
			rangeStr = "6mo"
		case daysDiff <= 365.0:
			rangeStr = "1y"
		case daysDiff <= 730.0:
			rangeStr = "2y"
		default:
			rangeStr = "5y"
		}
		fmt.Printf("Target purchase time is %.1f days ago (> 7 days). Switching to daily Close prices for %s (ignoring time flag).\n", daysDiff, targetTime.Format("2006-01-02"))
	}

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("opening file %s: %w", filePath, err)
	}
	defer file.Close()

	csvReader := csv.NewReader(file)
	records, err := csvReader.ReadAll()
	if err != nil {
		return fmt.Errorf("reading CSV: %w", err)
	}
	if len(records) < 2 {
		return fmt.Errorf("CSV file contains no data rows")
	}

	tickerIdx, weightIdx := -1, -1
	for i, h := range records[0] {
		hClean := strings.ToLower(strings.TrimSpace(h))
		if hClean == "ticker" {
			tickerIdx = i
		} else if hClean == "weight" {
			weightIdx = i
		}
	}
	if tickerIdx == -1 || weightIdx == -1 {
		return fmt.Errorf("invalid CSV format. Must contain 'ticker' and 'weight' columns")
	}

	var portfolio []backtest.Holding
	for _, record := range records[1:] {
		if len(record) <= tickerIdx || len(record) <= weightIdx {
			continue
		}
		ticker := strings.TrimSpace(record[tickerIdx])
		weightVal, err := strconv.ParseFloat(strings.TrimSpace(record[weightIdx]), 64)
		if err != nil || ticker == "" {
			continue
		}
		portfolio = append(portfolio, backtest.Holding{Ticker: ticker, Weight: weightVal})
	}
	if len(portfolio) == 0 {
		return fmt.Errorf("no valid stocks found in CSV")
	}

	if useDailyClose {
		fmt.Printf("Analyzing portfolio performance: Bought at Close on %s till latest Close...\n\n", targetTime.Format("2006-01-02"))
	} else {
		fmt.Printf("Analyzing portfolio performance: Bought on %s at %s IST till latest Close...\n\n", targetTime.Format("2006-01-02"), targetTime.Format("15:04"))
	}

	results := backtest.ValuatePortfolio(ctx, portfolio, capital, targetTime, useDailyClose, rangeStr, istLoc)

	out := os.Stdout
	var totalInitial, totalFinal float64
	rows := make([][]string, 0, len(results))
	for _, res := range results {
		if res.Err != nil {
			rows = append(rows, []string{res.Ticker, "ERROR", res.Err.Error(), "", "", "", "", ""})
			continue
		}
		rows = append(rows, []string{
			res.Ticker,
			fmt.Sprintf("%.4f", res.Weight),
			render.Currency(res.Allocated, "Rs. "),
			render.Currency(res.BuyPrice, "Rs. "),
			res.BuyTime,
			render.Currency(res.ClosePrice, "Rs. "),
			render.Currency(res.FinalValue, "Rs. "),
			render.PctRaw(res.PctReturn),
		})
		totalInitial += res.Allocated
		totalFinal += res.FinalValue
	}
	render.TableWithOpts(out, render.TableOpts{
		Headers: []string{"Ticker", "Weight", "Allocated", "Buy Price", "Buy Time/Date (IST)", "Close Price", "Final Value", "Return"},
		Rows:    rows,
		Align: []render.Alignment{
			render.AlignLeft, render.AlignRight, render.AlignRight, render.AlignRight,
			render.AlignLeft, render.AlignRight, render.AlignRight, render.AlignRight,
		},
	})

	netReturn := totalFinal - totalInitial
	pctReturn := (netReturn / totalInitial) * 100.0
	unallocated := capital - totalInitial

	render.Section(out, "Portfolio Performance")
	render.KV(out, []render.KVPair{
		{Key: "Total Allocated Capital", Value: render.Currency(totalInitial, "Rs. ")},
		{Key: "Unallocated Cash", Value: render.Currency(unallocated, "Rs. ")},
		{Key: "Total End of Day Value", Value: render.Currency(totalFinal+unallocated, "Rs. ")},
		{Key: "Net Profit/Loss", Value: render.PnL(netReturn, "Rs. ")},
		{Key: "Percentage Return", Value: render.PnLPct(pctReturn)},
	})
	return nil
}

func parsePerfDate(dateStr string, loc *time.Location) (time.Time, error) {
	if dateStr == "" {
		return time.Now().In(loc), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", dateStr, loc); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("20060102", dateStr, loc); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid date format: %s. Use YYYY-MM-DD or YYYYMMDD", dateStr)
}

// runVsBenchmark builds a daily NAV series for the portfolio versus a passive
// benchmark (default US:SPY), reports vs-benchmark metrics (alpha, beta,
// information ratio, tracking error), and persists the NAV series to the cache.
// runVsBenchmark builds a daily NAV series for the portfolio versus a passive
// benchmark (default US:SPY), reports vs-benchmark metrics (alpha, beta,
// information ratio, tracking error), persists the NAV series to the cache, and
// optionally decomposes active return into selection/rebalancing effects.
func runVsBenchmark(ctx context.Context, filePath string, capital float64, sinceStr, benchmark string, decompose bool) error {
	if filePath == "" {
		return fmt.Errorf("--file parameter is required")
	}

	weights, tickers, err := csvloader.LoadBasketCSV(filePath)
	if err != nil {
		return fmt.Errorf("loading portfolio %s: %w", filePath, err)
	}
	var holdings []attribution.Holding
	for _, tk := range tickers {
		if w := weights[tk]; w > 0 {
			holdings = append(holdings, attribution.Holding{Ticker: tk, Weight: w})
		}
	}
	if len(holdings) == 0 {
		return fmt.Errorf("no holdings with positive weight in %s", filePath)
	}

	// Range: [since, today]. Default since = 1 year ago.
	nyLoc, err := time.LoadLocation("America/New_York")
	if err != nil {
		nyLoc = time.UTC
	}
	to := time.Now().In(nyLoc)
	var from time.Time
	if sinceStr != "" {
		from, err = parsePerfDate(sinceStr, nyLoc)
		if err != nil {
			return err
		}
	} else {
		from = to.AddDate(-1, 0, 0)
	}

	portfolioName := csvloader.GetUniverseName(filePath)

	tracker := attribution.NewTracker(newDataRouter(), slog.Default())
	cfg := attribution.Config{
		InitialCapital: capital,
		From:           from,
		To:             to,
		Benchmark:      benchmark, // "" → DefaultBenchmark (US:SPY)
		Location:       nyLoc,
	}

	slog.InfoContext(ctx, "performance.vs_benchmark.start",
		"portfolio", portfolioName, "holdings", len(holdings),
		"from", from.Format("2006-01-02"), "to", to.Format("2006-01-02"),
		"benchmark", cfg.Benchmark, "decompose", decompose)

	points, err := tracker.BuildNAVSeries(ctx, holdings, cfg)
	if err != nil {
		return fmt.Errorf("building NAV series: %w", err)
	}

	res := attribution.Attribution(points, cfg.RiskFree)

	// Persist best-effort — a cache failure should not fail the report.
	if store := attribution.NewStore(cacheConn()); store != nil {
		if perr := store.InsertNAVPoints(ctx, portfolioName, points); perr != nil {
			slog.WarnContext(ctx, "performance.nav_persist_failed", "error", perr.Error())
		} else {
			slog.InfoContext(ctx, "performance.nav_persisted", "portfolio", portfolioName, "points", len(points))
		}
	}

	printAttribution(portfolioName, cfg.Benchmark, res)

	if decompose {
		if derr := printDecomposition(ctx, tracker, holdings, portfolioName, cfg); derr != nil {
			// Decomposition is additive insight; a failure should not fail the
			// whole report (the core attribution above already printed).
			slog.WarnContext(ctx, "performance.decompose_failed", "error", derr.Error())
			fmt.Printf("\n(return decomposition unavailable: %v)\n", derr)
		}
	}
	return nil
}

// printDecomposition loads the portfolio's rebalance history from the cache and
// prints the selection/rebalancing breakdown of active return.
func printDecomposition(ctx context.Context, tracker *attribution.Tracker, holdings []attribution.Holding, portfolioName string, cfg attribution.Config) error {
	var history []attribution.RebalanceEvent
	if db := cache.GetDB(); db != nil {
		h, err := attribution.LoadRebalanceHistory(ctx, db, portfolioName, cfg.From)
		if err != nil {
			return err
		}
		history = h
	}

	d, err := tracker.Decompose(ctx, attribution.DecomposeInput{
		Holdings: holdings,
		History:  history,
		// TaxSaving is left 0 here: harvest realization is surfaced by the
		// `mycase tax` command, not recomputed on the performance path.
	}, cfg)
	if err != nil {
		return err
	}
	printDecompositionResult(d)
	return nil
}

func printDecompositionResult(d attribution.Decomposition) {
	out := os.Stdout
	render.Section(out, "Return Decomposition")
	render.KV(out, []render.KVPair{
		{Key: "Period", Value: fmt.Sprintf("%s → %s (%d trading days, %d rebalances)", d.From.Format("2006-01-02"), d.To.Format("2006-01-02"), d.TradingDays, d.Rebalances)},
		{Key: "Portfolio return", Value: render.PnLPct(d.PortfolioReturn * 100)},
		{Key: "Benchmark return", Value: render.PnLPct(d.BenchmarkReturn * 100)},
		{Key: "Active return", Value: render.PnLPct(d.ActiveReturn * 100)},
	})
	pairs := []render.KVPair{
		{Key: "Selection effect", Value: fmt.Sprintf("%s   (picks vs index, first basket held)", render.PnLPct(d.Selection*100))},
		{Key: "Rebalancing effect", Value: fmt.Sprintf("%s   (re-selection vs holding first basket)", render.PnLPct(d.Rebalancing*100))},
	}
	if d.Tax != 0 {
		pairs = append(pairs, render.KVPair{Key: "Tax effect", Value: fmt.Sprintf("%s   (realized TLH saving / initial capital)", render.PnLPct(d.Tax*100))})
	}
	render.KV(out, pairs)
	fmt.Println("  (Selection + Rebalancing = Active return)")
}

// cacheConn returns the global cache's *sql.DB, or nil if the cache is unset.
func cacheConn() *sql.DB {
	if c := cache.GetDB(); c != nil {
		return c.Conn()
	}
	return nil
}

func printAttribution(portfolio, benchmark string, r attribution.Result) {
	out := os.Stdout
	render.Section(out, "Performance vs "+benchmark)
	render.KV(out, []render.KVPair{
		{Key: "Portfolio", Value: portfolio},
		{Key: "Period", Value: fmt.Sprintf("%s → %s (%d trading days)", r.From.Format("2006-01-02"), r.To.Format("2006-01-02"), r.TradingDays)},
		{Key: "Initial Capital", Value: render.Currency(r.InitialCapital, "$")},
		{Key: "Portfolio Final", Value: fmt.Sprintf("%s  (%s)", render.Currency(r.FinalValue, "$"), render.PnLPct(r.TotalReturn*100))},
		{Key: "Benchmark Final", Value: fmt.Sprintf("%s  (%s)", render.Currency(r.BenchmarkFinal, "$"), render.PnLPct(r.BenchmarkReturn*100))},
	})
	render.KV(out, []render.KVPair{
		{Key: "Portfolio CAGR", Value: render.PnLPct(r.CAGR * 100)},
		{Key: "Benchmark CAGR", Value: render.PnLPct(r.BenchmarkCAGR * 100)},
		{Key: "Alpha (annualized)", Value: render.PnLPct(r.Alpha * 100)},
		{Key: "Beta", Value: fmt.Sprintf("%.3f", r.Beta)},
		{Key: "Information Ratio", Value: fmt.Sprintf("%.3f", r.InformationRatio)},
		{Key: "Tracking Error", Value: fmt.Sprintf("%.2f%%", r.TrackingError*100)},
		{Key: "Max Drawdown", Value: fmt.Sprintf("%.2f%%", r.MaxDrawdown*100)},
		{Key: "Sharpe Ratio", Value: fmt.Sprintf("%.3f", r.Sharpe)},
	})
}
