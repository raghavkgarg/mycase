package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/raghavkgarg/mycase/pkg/broker/zerodha"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/themereturn"
	"github.com/urfave/cli/v3"
)

var ReturnsCommand = &cli.Command{
	Name:    "returns",
	Aliases: []string{"perf", "theme-return"},
	Usage:   "Evaluate audited Triple Returns (HPR, XIRR/MWR, TWR), dividends, and rebalancing gains per theme",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "theme",
			Aliases: []string{"t"},
			Value:   "microsmall",
			Usage:   "Theme identifier from config/themes.json (e.g. 'microsmall', 'kk', 'ai')",
		},
		&cli.StringFlag{
			Name:    "file",
			Aliases: []string{"f"},
			Usage:   "Optional path to golden copy portfolio CSV (e.g. 'data/microsmall.csv')",
		},
		&cli.BoolFlag{
			Name:    "live",
			Aliases: []string{"l"},
			Usage:   "Fetch live market LTPs via Zerodha Kite Connect API",
		},
		&cli.StringFlag{
			Name:    "account",
			Aliases: []string{"a"},
			Value:   "CBR420",
			Usage:   "Trading/Demat account ID in portfolio.db",
		},
		&cli.StringFlag{
			Name:  "db",
			Usage: "Path to portfolio.db DuckDB database file (auto-detected if omitted)",
		},
		&cli.BoolFlag{
			Name:  "lifecycle",
			Value: true,
			Usage: "Include historical exited rebalancing trades and closed P&L",
		},
		&cli.BoolFlag{
			Name:    "detail",
			Aliases: []string{"d"},
			Value:   true,
			Usage:   "Display holding-by-holding return matrix and execution tranches",
		},
		&cli.StringFlag{
			Name:    "benchmark",
			Aliases: []string{"b"},
			Value:   "NIFTY50_TRI",
			Usage:   "Benchmark index for Alpha comparison in portfolio.db",
		},
		&cli.BoolFlag{
			Name:  "all",
			Usage: "Evaluate all themes configured in config/themes.json sequentially",
		},
		&cli.StringFlag{
			Name:  "format",
			Value: "table",
			Usage: "Output format: 'table' or 'json'",
		},
	},
	Action: runReturns,
}

func runReturns(ctx context.Context, c *cli.Command) error {
	dbPath := c.String("db")
	db, err := themereturn.OpenDB(dbPath)
	if err != nil {
		return fmt.Errorf("opening portfolio database: %w", err)
	}
	defer db.Close()

	liveMode := c.Bool("live")
	ltpMap := make(map[string]float64)

	// Fetch live quotes from Zerodha if requested
	if liveMode {
		b := zerodha.New(true, "config/config.json")
		if rawHoldings, hErr := b.GetHoldings(); hErr == nil {
			for _, h := range rawHoldings {
				ltpMap[h.TradingSymbol] = h.LastPrice
			}
		} else {
			fmt.Printf("Warning: failed to fetch live Kite holdings: %v. Falling back to cached/buy prices.\n", hErr)
		}
	}

	opts := themereturn.ThemeReturnOptions{
		AccountID:        c.String("account"),
		PortfolioDBPath:  dbPath,
		Benchmark:        c.String("benchmark"),
		IncludeLifecycle: c.Bool("lifecycle"),
		Detail:           c.Bool("detail"),
		Live:             liveMode,
	}

	format := c.String("format")
	evaluateAll := c.Bool("all")

	if evaluateAll {
		themes, err := config.LoadThemes("config/themes.json")
		if err != nil {
			return fmt.Errorf("loading themes: %w", err)
		}

		var reports []*themereturn.ThemeReturnReport
		for _, tc := range themes {
			matched, err := themereturn.ResolveTheme(tc.Name, "", "config/themes.json")
			if err != nil {
				continue
			}
			rep, err := themereturn.EvaluateThemeReturn(db, matched, ltpMap, opts)
			if err != nil {
				continue
			}
			reports = append(reports, rep)
			if format != "json" {
				fmt.Print(themereturn.RenderThemeReport(rep, opts))
			}
		}

		if format == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(reports)
		}
		return nil
	}

	themeArg := c.String("theme")
	fileArg := c.String("file")

	matched, err := themereturn.ResolveTheme(themeArg, fileArg, "config/themes.json")
	if err != nil {
		return fmt.Errorf("resolving theme: %w", err)
	}

	report, err := themereturn.EvaluateThemeReturn(db, matched, ltpMap, opts)
	if err != nil {
		return fmt.Errorf("evaluating theme return: %w", err)
	}

	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	fmt.Print(themereturn.RenderThemeReport(report, opts))
	return nil
}
