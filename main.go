package main

import (
	"context"
	"fmt"
	"os"

	mycmd "github.com/gkgarg24/mycase/cmd"
	"github.com/urfave/cli/v3"
)

var Version = "0.0.0-dev"
var GitCommit = "unknown"
var BuildDate = "unknown"

func main() {
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
		},
	}
	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
