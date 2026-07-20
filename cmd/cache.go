package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

var CacheCommand = &cli.Command{
	Name:  "cache",
	Usage: "Inspect and manage the persistent DuckDB price/fundamentals cache",
	Commands: []*cli.Command{
		{
			Name:   "status",
			Usage:  "Show cached tickers, ranges, and row counts",
			Action: runCacheStatus,
		},
		{
			Name:  "clear",
			Usage: "Clear cached data",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "ticker", Aliases: []string{"t"}, Usage: "Clear cache for a specific ticker (e.g. NSE:TCS)"},
				&cli.BoolFlag{Name: "all", Usage: "Clear the entire cache"},
			},
			Action: runCacheClear,
		},
	},
}

func runCacheStatus(ctx context.Context, _ *cli.Command) error {
	c := yfinance.GetCache()
	if c == nil {
		fmt.Println("Cache is not initialised (data/cache.db could not be opened at startup).")
		return nil
	}
	entries, err := c.Status(ctx)
	if err != nil {
		return fmt.Errorf("reading cache status: %w", err)
	}
	if len(entries) == 0 {
		fmt.Println("Cache is empty.")
		return nil
	}

	fmt.Printf("%-14s %-20s %-10s %-8s %-26s\n", "Type", "Ticker", "Range", "Rows", "Fetched At (UTC)")
	fmt.Println(strings.Repeat("-", 82))
	for _, e := range entries {
		fmt.Printf("%-14s %-20s %-10s %-8d %-26s\n",
			e.Kind, e.Ticker, e.RangeKey, e.Rows,
			e.FetchedAt.UTC().Format("2006-01-02 15:04:05"))
	}
	return nil
}

func runCacheClear(ctx context.Context, c *cli.Command) error {
	cache := yfinance.GetCache()
	if cache == nil {
		fmt.Println("Cache is not initialised.")
		return nil
	}
	ticker := c.String("ticker")
	all := c.Bool("all")
	switch {
	case ticker != "":
		if err := cache.ClearTicker(ctx, ticker); err != nil {
			return fmt.Errorf("clearing ticker %s: %w", ticker, err)
		}
		fmt.Printf("Cleared cache for ticker: %s\n", ticker)
	case all:
		if err := cache.ClearAll(ctx); err != nil {
			return fmt.Errorf("clearing cache: %w", err)
		}
		fmt.Println("Cache cleared.")
	default:
		return fmt.Errorf("specify --ticker <ticker> or --all")
	}
	return nil
}
