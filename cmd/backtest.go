package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/backtest"
	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

var BacktestCommand = &cli.Command{
	Name:      "backtest",
	Usage:     "Run historical backtest simulation on a portfolio",
	ArgsUsage: "[portfolio-name]",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Usage: "Path to portfolio CSV (overrides positional arg)"},
		&cli.FloatFlag{Name: "capital", Value: 100000.0, Usage: "Initial capital in INR"},
		&cli.StringFlag{Name: "from", Required: true, Usage: "Start date (YYYY-MM-DD)"},
		&cli.StringFlag{Name: "to", Usage: "End date (YYYY-MM-DD, default: today)"},
		&cli.StringFlag{Name: "rebalance", Value: "quarterly", Usage: "Rebalance frequency: monthly, quarterly, drift-triggered"},
		&cli.FloatFlag{Name: "slippage", Value: 0.1, Usage: "Slippage per trade in % (e.g. 0.1 = 0.1%)"},
		&cli.StringFlag{Name: "benchmark", Value: "", Usage: "Benchmark ticker (default: from config/defaults.json)"},
		&cli.FloatFlag{Name: "drift-threshold", Value: 5.0, Usage: "Drift % to trigger rebalance (drift-triggered mode)"},
	},
	Action: runBacktest,
}

func runBacktest(ctx context.Context, c *cli.Command) error {
	filename := c.String("file")
	if filename == "" {
		if arg := c.Args().Get(0); arg != "" {
			cleaned := cleanBasketArg(arg)
			if strings.HasSuffix(cleaned, ".csv") {
				filename = "data/" + cleaned
			} else {
				filename = "data/" + cleaned + ".csv"
			}
		}
	}
	if filename == "" {
		return fmt.Errorf("--file or a portfolio name argument is required")
	}

	fromTime, err := parsePerfDate(c.String("from"), time.FixedZone("IST", 5*3600+30*60))
	if err != nil {
		return fmt.Errorf("--from: %w", err)
	}

	ist := time.FixedZone("IST", 5*3600+30*60)
	toTime := time.Now().In(ist)
	if toStr := c.String("to"); toStr != "" {
		toTime, err = parsePerfDate(toStr, ist)
		if err != nil {
			return fmt.Errorf("--to: %w", err)
		}
	}

	rebalFreq := backtest.RebalanceFreq(c.String("rebalance"))
	switch rebalFreq {
	case backtest.FreqMonthly, backtest.FreqQuarterly, backtest.FreqDrift:
	default:
		return fmt.Errorf("unknown rebalance frequency %q; use monthly, quarterly, or drift-triggered", rebalFreq)
	}

	capital := c.Float("capital")
	slippage := c.Float("slippage") / 100.0
	benchmark := c.String("benchmark")
	if benchmark == "" {
		benchmark = broker.LoadMarketConfig().Benchmark
	}
	driftThreshold := c.Float("drift-threshold") / 100.0

	// Load portfolio
	weights, keys, err := csvloader.LoadBasketCSV(filename)
	if err != nil {
		return fmt.Errorf("loading %s: %w", filename, err)
	}
	if len(keys) == 0 {
		return fmt.Errorf("no tickers found in %s", filename)
	}

	holdings := make([]backtest.Holding, 0, len(keys))
	for _, k := range keys {
		w := weights[k]
		if w > 0 {
			holdings = append(holdings, backtest.Holding{Ticker: k, Weight: w})
		}
	}
	if len(holdings) == 0 {
		return fmt.Errorf("no active (non-zero weight) tickers in %s", filename)
	}

	fmt.Printf("Fetching price data for %d tickers + benchmark (%s)...\n", len(holdings), benchmark)

	// Fetch price data for all tickers + benchmark concurrently
	type fetchResult struct {
		ticker string
		hist   *yfinance.HistoricalData
		err    error
	}

	resultCh := make(chan fetchResult, len(holdings)+1)
	for _, h := range holdings {
		go func(ticker string) {
			hist, ferr := yfinance.FetchHistoricalByDateRange(ctx, ticker, fromTime, toTime)
			resultCh <- fetchResult{ticker: ticker, hist: hist, err: ferr}
		}(h.Ticker)
	}
	go func() {
		hist, ferr := yfinance.FetchHistoricalByDateRange(ctx, benchmark, fromTime, toTime)
		resultCh <- fetchResult{ticker: benchmark, hist: hist, err: ferr}
	}()

	priceData := make(map[string]*yfinance.HistoricalData, len(holdings))
	var benchData *yfinance.HistoricalData
	var fetchErrors []string

	for range len(holdings) + 1 {
		r := <-resultCh
		if r.err != nil {
			fetchErrors = append(fetchErrors, fmt.Sprintf("%s: %v", r.ticker, r.err))
			continue
		}
		if r.ticker == benchmark {
			benchData = r.hist
		} else {
			priceData[r.ticker] = r.hist
		}
	}
	if len(fetchErrors) > 0 {
		return fmt.Errorf("failed to fetch price data:\n  %s", strings.Join(fetchErrors, "\n  "))
	}

	cfg := backtest.SimConfig{
		InitialCapital:  capital,
		From:            fromTime,
		To:              toTime,
		Rebalance:       rebalFreq,
		SlippagePct:     slippage,
		BenchmarkTicker: benchmark,
		DriftThreshold:  driftThreshold,
	}

	fmt.Printf("Running backtest: %s → %s, rebalance=%s, slippage=%.2f%%\n\n",
		fromTime.Format("2006-01-02"), toTime.Format("2006-01-02"),
		rebalFreq, slippage*100)

	res, err := backtest.Run(holdings, priceData, benchData, cfg)
	if err != nil {
		return fmt.Errorf("backtest failed: %w", err)
	}

	printBacktestResults(res, capital, benchmark, rebalFreq, slippage)
	return nil
}

func printBacktestResults(res backtest.SimResult, capital float64, benchmark string, freq backtest.RebalanceFreq, slippage float64) {
	div := strings.Repeat("─", 52)
	fmt.Println(div)
	fmt.Printf("  BACKTEST RESULTS\n")
	fmt.Println(div)

	if len(res.Snapshots) > 0 {
		first := res.Snapshots[0]
		last := res.Snapshots[len(res.Snapshots)-1]
		fmt.Printf("  Period:       %s → %s (%d trading days)\n",
			first.Date.Format("2006-01-02"), last.Date.Format("2006-01-02"), res.TradingDays)
	}
	fmt.Printf("  Rebalance:    %s (%d times, slippage %.2f%%)\n",
		freq, res.RebalanceCount, slippage*100)
	fmt.Println(div)

	fmt.Printf("  %-22s  %+8.2f%%   vs  %+8.2f%%  (%s)\n",
		"Total Return", res.TotalReturn*100, res.BenchmarkReturn*100, benchmark)
	fmt.Printf("  %-22s  %+8.2f%%   vs  %+8.2f%%\n",
		"CAGR", res.CAGR*100, res.BenchmarkCAGR*100)
	fmt.Printf("  %-22s  %8.2f%%\n", "Max Drawdown", res.MaxDrawdown*100)
	fmt.Println(div)
	fmt.Printf("  %-22s  %8.2f\n", "Sharpe Ratio", res.SharpeRatio)
	fmt.Printf("  %-22s  %8.2f\n", "Sortino Ratio", res.SortinoRatio)
	fmt.Printf("  %-22s  %8.2f\n", "Calmar Ratio", res.CalmarRatio)
	fmt.Printf("  %-22s  %+8.2f%%\n", "Alpha (Jensen)", res.Alpha*100)
	fmt.Printf("  %-22s  %8.2f\n", "Beta", res.Beta)
	fmt.Println(div)

	if len(res.Snapshots) > 0 {
		finalPort := res.Snapshots[len(res.Snapshots)-1].PortfolioValue
		finalBench := capital * (1 + res.BenchmarkReturn)
		fmt.Printf("  %-22s  Rs. %12.2f\n", "Initial Capital", capital)
		fmt.Printf("  %-22s  Rs. %12.2f\n", "Final Portfolio", finalPort)
		fmt.Printf("  %-22s  Rs. %12.2f\n", "Final Benchmark", finalBench)
	}
	fmt.Println(div)

	// Year-by-year breakdown when period spans multiple years
	printYearlyBreakdown(res.Snapshots)
}

func printYearlyBreakdown(snapshots []backtest.DailySnapshot) {
	if len(snapshots) < 2 {
		return
	}
	firstYear := snapshots[0].Date.Year()
	lastYear := snapshots[len(snapshots)-1].Date.Year()
	if lastYear == firstYear {
		return
	}

	fmt.Printf("\n  %-8s  %10s  %10s  %10s\n", "Year", "Portfolio", "Benchmark", "Excess")
	fmt.Println("  " + strings.Repeat("─", 42))

	// Find year-start snapshot (first snapshot of each year)
	type yearMark struct {
		year int
		snap backtest.DailySnapshot
	}
	var marks []yearMark
	prev := snapshots[0]
	marks = append(marks, yearMark{year: prev.Date.Year(), snap: prev})
	for _, s := range snapshots[1:] {
		if s.Date.Year() != prev.Date.Year() {
			marks = append(marks, yearMark{year: s.Date.Year(), snap: s})
		}
		prev = s
	}
	marks = append(marks, yearMark{year: -1, snap: snapshots[len(snapshots)-1]})

	for i := 0; i+1 < len(marks); i++ {
		start := marks[i].snap
		end := marks[i+1].snap
		if start.PortfolioValue <= 0 || start.BenchmarkValue <= 0 {
			continue
		}
		portRet := (end.PortfolioValue - start.PortfolioValue) / start.PortfolioValue * 100
		benchRet := (end.BenchmarkValue - start.BenchmarkValue) / start.BenchmarkValue * 100
		excess := portRet - benchRet
		yearLabel := fmt.Sprintf("%d", marks[i].year)
		if marks[i+1].year == -1 {
			yearLabel += "*"
		}
		fmt.Printf("  %-8s  %+9.2f%%  %+9.2f%%  %+9.2f%%\n",
			yearLabel, portRet, benchRet, excess)
	}
	fmt.Println("  (* partial year)")
}
