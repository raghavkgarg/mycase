package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/golden"
	"github.com/urfave/cli/v3"
)

var GoldenCommand = &cli.Command{
	Name:  "golden",
	Usage: "Golden Triangle multi-strategy quantitative convergence engine (EMB + MB + Fair Price)",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "index", Aliases: []string{"i"}, Value: "niftytotalmarket", Usage: "Index name to analyze (default: niftytotalmarket)"},
		&cli.BoolFlag{Name: "analysis", Aliases: []string{"a"}, Usage: "Run Golden Triangle quantitative convergence analysis using DuckDB", Value: true},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		return RunGoldenAnalysis(ctx, c.String("index"))
	},
}

// RunGoldenAnalysis executes the Golden Triangle analysis pipeline against DuckDB.
func RunGoldenAnalysis(ctx context.Context, indexName string) error {
	dbPath := config.DataPath("mycase.db")
	duckCache, err := cache.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening duckdb database at %s: %w", dbPath, err)
	}
	defer duckCache.Close()

	report, err := golden.RunAnalysis(ctx, duckCache.Conn(), indexName)
	if err != nil {
		return fmt.Errorf("running golden triangle analysis: %w", err)
	}

	golden.RenderReport(os.Stdout, report)
	return nil
}
