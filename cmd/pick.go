package cmd

import (
	"context"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
)

var PickCommand = &cli.Command{
	Name:  "pick",
	Usage: "Select top stocks from an index or file",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "index", Aliases: []string{"i"}, Usage: "Index to pick stocks from (default from config/defaults.json)"},
		&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Usage: "Path to custom CSV file (takes precedence over --index)"},
		&cli.StringFlag{Name: "method", Aliases: []string{"m"}, Usage: "Scoring strategy (balanced, aggressive, conservative, multibagger, value, us_quality_momentum) (default from config/defaults.json)"},
		&cli.IntFlag{Name: "top", Usage: "Number of top stocks to pick (default from config/defaults.json)"},
		&cli.StringFlag{Name: "range", Usage: "Historical data range: 3mo, 6mo, 1y (default from config/defaults.json)"},
		&cli.BoolFlag{Name: "skip-scuttlebutt", Usage: "Skip qualitative scuttlebutt checklist report"},
		&cli.StringFlag{Name: "golden", Usage: "Path to golden copy CSV for hysteresis and rebalancing band"},
		&cli.FloatFlag{Name: "rebalance-tolerance", Value: 0.10, Usage: "Rebalancing weight tolerance %% (e.g. 0.10 for 0.10%%)"},
		&cli.IntFlag{Name: "hysteresis-buffer", Value: 5, Usage: "Extra ranks to allow existing holdings to drift"},
		&cli.StringFlag{Name: "name", Usage: "Custom display name for output files"},
		&cli.StringFlag{Name: "out", Usage: "Custom output CSV path"},
	},
	Action: runPick,
}

func runPick(ctx context.Context, c *cli.Command) error {
	return runPickWithOpts(ctx, pickOptsFromCmd(c))
}

func pickOptsFromCmd(c *cli.Command) *stockpicker.Options {
	defaults := config.LoadUserDefaults("config/defaults.json")

	indexName := c.String("index")
	if indexName == "" {
		indexName = defaults.Index
	}
	if indexName == "" {
		indexName = "sp500"
	}

	method := c.String("method")
	if method == "" {
		method = defaults.Method
	}
	if method == "" {
		method = "us_quality_momentum"
	}

	topN := c.Int("top")
	if topN == 0 {
		topN = defaults.TopN
	}
	if topN == 0 {
		topN = 20
	}

	rangeStr := c.String("range")
	if rangeStr == "" {
		rangeStr = defaults.Range
	}
	if rangeStr == "" {
		rangeStr = "3mo"
	}
	rangeStr = strings.ToLower(strings.TrimSpace(rangeStr))
	if rangeStr == "1yr" || rangeStr == "1year" {
		rangeStr = "1y"
	}
	if rangeStr != "3mo" && rangeStr != "6mo" && rangeStr != "1y" {
		rangeStr = "3mo"
	}

	return &stockpicker.Options{
		IndexName:          indexName,
		FilePath:           c.String("file"),
		Method:             method,
		TopN:               topN,
		RangeStr:           rangeStr,
		SkipScuttlebutt:    c.Bool("skip-scuttlebutt"),
		GoldenPath:         c.String("golden"),
		RebalanceTolerance: c.Float("rebalance-tolerance"),
		HysteresisBuffer:   c.Int("hysteresis-buffer"),
		DisplayName:        c.String("name"),
		OutputFile:         c.String("out"),
	}
}

// runPickWithOpts wires the data router (Schwab for US tickers, Yahoo otherwise)
// and delegates to the unified stockpicker.RunWithResult implementation.
func runPickWithOpts(ctx context.Context, opts *stockpicker.Options) error {
	if opts.DataFetcher == nil {
		opts.DataFetcher = newDataRouter()
	}
	return stockpicker.Run(ctx, opts)
}
