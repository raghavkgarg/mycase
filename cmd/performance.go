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
		&cli.StringFlag{Name: "benchmark", Usage: "Benchmark ticker for --vs-benchmark (default: US:SPY)"},
		&cli.StringFlag{Name: "since", Usage: "Start date for --vs-benchmark NAV series in YYYY-MM-DD or YYYYMMDD (default: 1 year ago)"},
	},
	Action: runPerformance,
}

func runPerformance(ctx context.Context, c *cli.Command) error {
	if c.Bool("vs-benchmark") {
		return runVsBenchmark(ctx, c.String("file"), c.Float("capital"), c.String("since"), c.String("benchmark"))
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

	fmt.Printf("%-15s %-8s %-12s %-12s %-22s %-12s %-12s %-10s\n", "Ticker", "Weight", "Allocated", "Buy Price", "Buy Time/Date (IST)", "Close Price", "Final Value", "Return")
	fmt.Println(strings.Repeat("-", 112))

	var totalInitial, totalFinal float64
	for _, res := range results {
		if res.Err != nil {
			fmt.Printf("%-15s ERROR: %v\n", res.Ticker, res.Err)
			continue
		}
		fmt.Printf("%-15s %-8.4f Rs. %-8.2f Rs. %-8.2f %-22s Rs. %-8.2f Rs. %-8.2f %+.2f%%\n",
			res.Ticker, res.Weight, res.Allocated, res.BuyPrice, res.BuyTime, res.ClosePrice, res.FinalValue, res.PctReturn)
		totalInitial += res.Allocated
		totalFinal += res.FinalValue
	}

	fmt.Println(strings.Repeat("-", 112))
	netReturn := totalFinal - totalInitial
	pctReturn := (netReturn / totalInitial) * 100.0
	unallocated := capital - totalInitial

	fmt.Printf("\n--- Portfolio Performance ---\n")
	fmt.Printf("Total Allocated Capital:  Rs. %.2f\n", totalInitial)
	fmt.Printf("Unallocated Cash:         Rs. %.2f\n", unallocated)
	fmt.Printf("Total End of Day Value:   Rs. %.2f\n", totalFinal+unallocated)
	fmt.Printf("Net Profit/Loss:          Rs. %+.2f\n", netReturn)
	fmt.Printf("Percentage Return:        %+.2f%%\n", pctReturn)
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
func runVsBenchmark(ctx context.Context, filePath string, capital float64, sinceStr, benchmark string) error {
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
		"benchmark", cfg.Benchmark)

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
	return nil
}

// cacheConn returns the global cache's *sql.DB, or nil if the cache is unset.
func cacheConn() *sql.DB {
	if c := cache.GetDB(); c != nil {
		return c.Conn()
	}
	return nil
}

func printAttribution(portfolio, benchmark string, r attribution.Result) {
	fmt.Printf("\n--- Performance vs %s ---\n", benchmark)
	fmt.Printf("Portfolio:            %s\n", portfolio)
	fmt.Printf("Period:               %s → %s (%d trading days)\n",
		r.From.Format("2006-01-02"), r.To.Format("2006-01-02"), r.TradingDays)
	fmt.Printf("Initial Capital:      $%.2f\n", r.InitialCapital)
	fmt.Printf("Portfolio Final:      $%.2f  (%+.2f%%)\n", r.FinalValue, r.TotalReturn*100)
	fmt.Printf("Benchmark Final:      $%.2f  (%+.2f%%)\n", r.BenchmarkFinal, r.BenchmarkReturn*100)
	fmt.Println(strings.Repeat("-", 48))
	fmt.Printf("Portfolio CAGR:       %+.2f%%\n", r.CAGR*100)
	fmt.Printf("Benchmark CAGR:       %+.2f%%\n", r.BenchmarkCAGR*100)
	fmt.Printf("Alpha (annualized):   %+.2f%%\n", r.Alpha*100)
	fmt.Printf("Beta:                 %.3f\n", r.Beta)
	fmt.Printf("Information Ratio:    %.3f\n", r.InformationRatio)
	fmt.Printf("Tracking Error:       %.2f%%\n", r.TrackingError*100)
	fmt.Printf("Max Drawdown:         %.2f%%\n", r.MaxDrawdown*100)
	fmt.Printf("Sharpe Ratio:         %.3f\n", r.Sharpe)
}
