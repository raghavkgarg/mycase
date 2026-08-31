package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/render"
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
	c := cache.GetDB()
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

	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []string{
			string(e.Kind), e.Ticker, e.RangeKey,
			fmt.Sprintf("%d", e.Rows),
			e.FetchedAt.UTC().Format("2006-01-02 15:04:05"),
		})
	}
	render.TableWithOpts(os.Stdout, render.TableOpts{
		Headers: []string{"Type", "Ticker", "Range", "Rows", "Fetched At (UTC)"},
		Rows:    rows,
		Align:   []render.Alignment{render.AlignLeft, render.AlignLeft, render.AlignLeft, render.AlignRight, render.AlignLeft},
	})
	return nil
}

func runCacheClear(ctx context.Context, c *cli.Command) error {
	db := cache.GetDB()
	if db == nil {
		fmt.Println("Cache is not initialised.")
		return nil
	}
	ticker := c.String("ticker")
	all := c.Bool("all")
	switch {
	case ticker != "":
		if err := db.ClearTicker(ctx, ticker); err != nil {
			return fmt.Errorf("clearing ticker %s: %w", ticker, err)
		}
		fmt.Printf("Cleared cache for ticker: %s\n", ticker)
	case all:
		if err := db.ClearAll(ctx); err != nil {
			return fmt.Errorf("clearing cache: %w", err)
		}
		fmt.Println("Cache cleared.")
	default:
		return fmt.Errorf("specify --ticker <ticker> or --all")
	}
	return nil
}
