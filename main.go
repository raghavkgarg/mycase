package main

import (
	"context"
	"fmt"
	"os"

	mycmd "github.com/raghavkgarg/mycase/cmd"
	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
	"github.com/urfave/cli/v3"
)

var Version = "0.0.0-dev"
var GitCommit = "unknown"
var BuildDate = "unknown"

func main() {
	// Open DuckDB cache (best-effort; non-fatal if data/ doesn't exist yet).
	if c, err := cache.Open("data/cache.db"); err == nil {
		yfinance.SetCache(c)
		defer c.Close()
	}

	app := &cli.Command{
		Name:    "mycase",
		Usage:   "Portfolio basket & rebalancing engine",
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", Version, GitCommit, BuildDate),
		Commands: []*cli.Command{
			mycmd.PipelineCommand,
			mycmd.PickCommand,
			mycmd.OptimizeCommand,
			mycmd.ReportCommand,
			mycmd.PerformanceCommand,
			mycmd.MonitorCommand,
			mycmd.BasketCommand,
			mycmd.HoldingsCommand,
			mycmd.MergeCommand,
			mycmd.AuthCommand,
			mycmd.CacheCommand,
			mycmd.DaemonCommand,
			mycmd.BacktestCommand,
			mycmd.ServeCommand,
			mycmd.ConvertCommand,
		},
	}
	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
