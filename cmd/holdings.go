package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/gkgarg24/mycase/pkg/config"
	"github.com/gkgarg24/mycase/pkg/csvloader"
	"github.com/gkgarg24/mycase/pkg/kiteclient"
	"github.com/gkgarg24/mycase/pkg/portfolio"
	"github.com/gkgarg24/mycase/pkg/printer"
)

var HoldingsCommand = &cli.Command{
	Name:  "holdings",
	Usage: "Snapshot of current Zerodha holdings",
	Flags: []cli.Flag{
		&cli.BoolFlag{Name: "live", Usage: "Use live Zerodha API (default: dry-run mock mode)"},
	},
	Action: runHoldings,
}

func runHoldings(ctx context.Context, c *cli.Command) error {
	liveMode := c.Bool("live")

	fmt.Println("====================================================================")
	fmt.Println("                 Go Mycase Holdings Snapshot                     ")
	if liveMode {
		fmt.Println("                 [LIVE MODE]                                        ")
	} else {
		fmt.Println("                 [DRY RUN / MOCK MODE]                              ")
	}
	fmt.Println("====================================================================")

	client, isMock := kiteclient.LoadAndInitClient("config/config.json", liveMode)

	rawHoldings, err := portfolio.FetchAndMergeHoldings(client, isMock)
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
			Name:    tc.Name,
			Prefix:  tc.Prefix,
			CSVPath: tc.CSVPath,
			Tickers: tickers,
		})
	}

	var uncategorizedHoldings []portfolio.Holding
	for _, h := range rawHoldings {
		keyNSE := "NSE:" + h.TradingSymbol
		keyBSE := "BSE:" + h.TradingSymbol

		matched := false
		for i, g := range groups {
			if g.Tickers[keyNSE] || g.Tickers[keyBSE] {
				groups[i].Holdings = append(groups[i].Holdings, h)
				matched = true
				break
			}
		}
		if !matched {
			uncategorizedHoldings = append(uncategorizedHoldings, h)
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
