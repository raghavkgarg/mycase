package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/portfolio"
	"github.com/raghavkgarg/mycase/pkg/printer"
	"github.com/raghavkgarg/mycase/pkg/render"
	"github.com/raghavkgarg/mycase/pkg/themereturn"
)

var HoldingsCommand = &cli.Command{
	Name:  "holdings",
	Usage: "Snapshot of current broker holdings",
	Flags: []cli.Flag{
		&cli.BoolFlag{Name: "live", Usage: "Use live broker API (default: dry-run mock mode)"},
	},
	Action: runHoldings,
}

func runHoldings(ctx context.Context, c *cli.Command) error {
	liveMode := c.Bool("live")

	mode := "DRY RUN / MOCK MODE"
	if liveMode {
		mode = "LIVE MODE"
	}
	render.Section(os.Stdout, fmt.Sprintf("Go Mycase Holdings Snapshot [%s]", mode))

	b, err := newBroker(liveMode)
	if err != nil {
		return fmt.Errorf("creating broker: %w", err)
	}

	rawHoldings, err := b.GetHoldings()
	if err != nil {
		return fmt.Errorf("fetching holdings: %w", err)
	}

	themeConfigs, err := config.LoadThemes("config/themes.json")
	if err != nil {
		fmt.Printf("Warning: Failed to load config/themes.json: %v. Using defaults.\n", err)
	}

	var groups []printer.ThemeGroup
	for _, tc := range themeConfigs {
		tickers, err := csvloader.LoadMyAllCSV(tc.CSVPath)
		if err != nil {
			fmt.Printf("Note: Could not load %s. Error: %v\n", tc.CSVPath, err)
			tickers = make(map[string]bool)
		}
		groups = append(groups, printer.ThemeGroup{
			Name:         tc.Name,
			Prefix:       tc.Prefix,
			CSVPath:      tc.CSVPath,
			TargetWeight: tc.TargetWeight,
			Tickers:      tickers,
		})
	}

	var uncategorizedHoldings []broker.Holding
	for _, h := range rawHoldings {
		tickerKey := h.Exchange + ":" + h.TradingSymbol
		baseSym := portfolio.StripSeriesSuffix(h.TradingSymbol)
		keyNSE := "NSE:" + h.TradingSymbol
		keyBSE := "BSE:" + h.TradingSymbol
		keyUS := "US:" + h.TradingSymbol
		baseKeyNSE := "NSE:" + baseSym
		baseKeyBSE := "BSE:" + baseSym

		matched := false
		for i, g := range groups {
			if g.Tickers[tickerKey] || g.Tickers[keyNSE] || g.Tickers[keyBSE] || g.Tickers[keyUS] ||
				g.Tickers[baseKeyNSE] || g.Tickers[baseKeyBSE] ||
				g.Tickers[h.TradingSymbol] || g.Tickers[baseSym] {
				groups[i].Holdings = append(groups[i].Holdings, h)
				matched = true
				break
			}
		}
		if !matched {
			uncategorizedHoldings = append(uncategorizedHoldings, h)
		}
	}

	// Enrich each theme with audited Triple Returns if portfolio.db is available
	ltpMap := make(map[string]float64)
	for _, h := range rawHoldings {
		ltpMap[h.TradingSymbol] = h.LastPrice
		ltpMap[portfolio.StripSeriesSuffix(h.TradingSymbol)] = h.LastPrice
	}

	if db, err := themereturn.OpenDB(""); err == nil {
		defer db.Close()
		for i, g := range groups {
			if len(g.Holdings) == 0 {
				continue
			}
			matched, mErr := themereturn.ResolveTheme(g.Name, g.CSVPath, "config/themes.json")
			if mErr != nil {
				continue
			}
			rep, rErr := themereturn.EvaluateThemeReturn(db, matched, ltpMap, themereturn.ThemeReturnOptions{
				AccountID:        "CBR420",
				IncludeLifecycle: true,
			})
			if rErr == nil && rep.ActiveInvestedValue > 0 {
				groups[i].ReturnBanner = themereturn.RenderBanner(rep)
			}
		}
	}

	output := printer.RenderHoldingsSnapshot(rawHoldings, groups, uncategorizedHoldings)
	fmt.Print(output)

	folder := "holding"
	if err := os.MkdirAll(folder, 0755); err == nil {
		dateStr := time.Now().Format("20060102")
		filename := filepath.Join(folder, "holding_"+dateStr+".txt")
		if err := os.WriteFile(filename, []byte(output), 0644); err == nil {
			fmt.Printf("Holdings snapshot saved to %s\n", filename)
		} else {
			fmt.Printf("Failed to save holdings snapshot file: %v\n", err)
		}
	} else {
		fmt.Printf("Failed to create holding directory: %v\n", err)
	}
	return nil
}
