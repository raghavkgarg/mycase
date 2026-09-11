package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/themedb"
	"github.com/urfave/cli/v3"
)

var ThemeCommand = &cli.Command{
	Name:    "theme",
	Aliases: []string{"themes"},
	Usage:   "Manage theme lifecycle history and rebalance versions in mycase.db",
	Commands: []*cli.Command{
		themeSyncCmd,
		themeHistoryCmd,
		themeShowCmd,
	},
}

var themeSyncCmd = &cli.Command{
	Name:    "sync",
	Aliases: []string{"backfill"},
	Usage:   "Synchronize or backfill theme history from proposals and golden copies into mycase.db",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "theme",
			Aliases: []string{"t"},
			Value:   "microsmall",
			Usage:   "Theme identifier (e.g. 'microsmall', 'modularmicro')",
		},
		&cli.StringFlag{
			Name:    "golden",
			Aliases: []string{"g"},
			Usage:   "Optional path to golden copy CSV (e.g. 'data/microsmall.csv')",
		},
		&cli.StringFlag{
			Name:    "proposals",
			Aliases: []string{"p"},
			Value:   "data/candidates/proposals",
			Usage:   "Directory containing historical proposal CSV files",
		},
		&cli.StringFlag{
			Name:    "db",
			Usage:   "Path to mycase.db DuckDB database file",
		},
		&cli.BoolFlag{
			Name:    "all",
			Usage:   "Synchronize all configured themes from config/themes.json",
		},
	},
	Action: runThemeSync,
}

func runThemeSync(ctx context.Context, c *cli.Command) error {
	dbPath := c.String("db")
	db, err := themedb.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening mycase.db: %w", err)
	}
	defer db.Close()

	proposalsDir := c.String("proposals")
	syncOne := func(themeName, goldenPath, keyword string) error {
		fmt.Printf("🔄 Syncing theme '%s' (Golden: %s)... ", themeName, goldenPath)
		opts := themedb.SyncThemeOptions{
			ThemeName:     themeName,
			GoldenCSVPath: goldenPath,
			ProposalsDir:  proposalsDir,
			Keyword:       keyword,
		}
		if err := db.SyncThemeFromProposals(ctx, opts); err != nil {
			fmt.Println("❌ Error")
			return err
		}
		v, _ := db.GetLatestVersion(ctx, themeName)
		active, _ := db.GetActiveHoldings(ctx, themeName)
		exited, _ := db.GetExitedHoldings(ctx, themeName)
		fmt.Printf("✅ (Recorded %d versions | %d Active | %d Exited)\n", v, len(active), len(exited))
		return nil
	}

	if c.Bool("all") {
		cfgPath := "config/themes.json"
		themes, err := config.LoadThemes(cfgPath)
		if err != nil {
			return fmt.Errorf("loading themes config: %w", err)
		}
		for _, tc := range themes {
			uName := csvloader.GetUniverseName(tc.CSVPath)
			kw := strings.ToLower(tc.Prefix)
			if strings.Contains(strings.ToLower(tc.Name), "microsmall") {
				kw = "microsmall"
			}
			if err := syncOne(uName, tc.CSVPath, kw); err != nil {
				fmt.Printf("Warning: failed to sync theme %s: %v\n", uName, err)
			}
		}
		return nil
	}

	themeArg := c.String("theme")
	goldenPath := c.String("golden")
	themeName := themeArg

	if themes, err := config.LoadThemes("config/themes.json"); err == nil {
		for _, tc := range themes {
			cleanArg := strings.ToLower(strings.TrimSpace(themeArg))
			uName := csvloader.GetUniverseName(tc.CSVPath)
			if strings.EqualFold(tc.Name, themeArg) ||
				strings.EqualFold(tc.Prefix, themeArg) ||
				strings.EqualFold(uName, cleanArg) ||
				strings.Contains(strings.ToLower(tc.Name), cleanArg) ||
				strings.Contains(strings.ToLower(tc.CSVPath), cleanArg) {
				if goldenPath == "" {
					goldenPath = tc.CSVPath
				}
				themeName = uName
				break
			}
		}
	}
	if goldenPath == "" {
		goldenPath = fmt.Sprintf("data/%s.csv", themeName)
	}
	themeName = csvloader.GetUniverseName(goldenPath)

	kw := strings.ToLower(themeName)
	if strings.Contains(kw, "microsmall") {
		kw = "microsmall"
	}

	return syncOne(themeName, goldenPath, kw)
}

func resolveThemeArg(themeArg string) string {
	cleanArg := strings.ToLower(strings.TrimSpace(themeArg))
	if themes, err := config.LoadThemes("config/themes.json"); err == nil {
		for _, tc := range themes {
			uName := csvloader.GetUniverseName(tc.CSVPath)
			if strings.EqualFold(tc.Name, themeArg) ||
				strings.EqualFold(tc.Prefix, themeArg) ||
				strings.EqualFold(uName, cleanArg) ||
				strings.Contains(strings.ToLower(tc.Name), cleanArg) ||
				strings.Contains(strings.ToLower(tc.CSVPath), cleanArg) {
				return uName
			}
		}
	}
	return cleanArg
}

var themeHistoryCmd = &cli.Command{
	Name:    "history",
	Usage:   "Show rebalance versions and turnover events for a theme",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "theme",
			Aliases: []string{"t"},
			Value:   "microsmall",
			Usage:   "Theme identifier (e.g. 'microsmall')",
		},
		&cli.StringFlag{
			Name:  "db",
			Usage: "Path to mycase.db DuckDB database file",
		},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		dbPath := c.String("db")
		db, err := themedb.Open(dbPath)
		if err != nil {
			return fmt.Errorf("opening mycase.db: %w", err)
		}
		defer db.Close()

		themeName := resolveThemeArg(c.String("theme"))
		rebalances, err := db.GetRebalances(ctx, themeName)
		if err != nil {
			return fmt.Errorf("fetching rebalances: %w", err)
		}
		if len(rebalances) == 0 {
			fmt.Printf("No rebalance history recorded for theme '%s' in mycase.db.\nUse 'mycase theme sync --theme %s' to backfill.\n", themeName, themeName)
			return nil
		}

		fmt.Printf("\n📜 REBALANCE TIMELINE FOR '%s' (data/mycase.db):\n", themeName)
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "Version\tEffective Date\tStatus\tTurnover %\tNAV\tNotes")
		fmt.Fprintln(tw, "-------\t--------------\t------\t----------\t---\t-----")
		for _, r := range rebalances {
			fmt.Fprintf(tw, "v%d\t%s\t%s\t%5.2f%%\t₹%.2f\t%s\n",
				r.Version, r.EffectiveDate.Format("2006-01-02"), r.Status, r.TurnoverPct, r.TotalNAV, r.Notes)
		}
		tw.Flush()
		fmt.Println()
		return nil
	},
}

var themeShowCmd = &cli.Command{
	Name:    "show",
	Usage:   "Display active or exited constituents for a theme",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "theme",
			Aliases: []string{"t"},
			Value:   "microsmall",
			Usage:   "Theme identifier",
		},
		&cli.BoolFlag{
			Name:    "exited",
			Aliases: []string{"e"},
			Usage:   "Show only exited rebalanced constituents",
		},
		&cli.StringFlag{
			Name:  "db",
			Usage: "Path to mycase.db DuckDB database file",
		},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		dbPath := c.String("db")
		db, err := themedb.Open(dbPath)
		if err != nil {
			return fmt.Errorf("opening mycase.db: %w", err)
		}
		defer db.Close()

		themeName := resolveThemeArg(c.String("theme"))
		showExited := c.Bool("exited")

		if showExited {
			exited, err := db.GetExitedHoldings(ctx, themeName)
			if err != nil {
				return err
			}
			fmt.Printf("\n🔄 EXITED CONSTITUENTS FOR '%s' (%d items):\n", themeName, len(exited))
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "Symbol\tExit Version\tAction\tPrior Weight\tExit Reason")
			fmt.Fprintln(tw, "------\t------------\t------\t------------\t-----------")
			for _, it := range exited {
				fmt.Fprintf(tw, "%s\tv%d\t%s\t%6.2f%%\t%s\n",
					it.Symbol, it.Version, it.Action, it.PrevWeight*100.0, it.ExitReason)
			}
			tw.Flush()
			fmt.Println()
			return nil
		}

		active, err := db.GetActiveHoldings(ctx, themeName)
		if err != nil {
			return err
		}
		v, _ := db.GetLatestVersion(ctx, themeName)
		fmt.Printf("\n🎯 ACTIVE CONSTITUENTS FOR '%s' [v%d] (%d items):\n", themeName, v, len(active))
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "Symbol\tAction\tWeight\tPrior Weight")
		fmt.Fprintln(tw, "------\t------\t------\t------------")
		for _, it := range active {
			fmt.Fprintf(tw, "%s\t%s\t%6.2f%%\t%6.2f%%\n",
				it.Symbol, it.Action, it.TargetWeight*100.0, it.PrevWeight*100.0)
		}
		tw.Flush()
		fmt.Println()
		return nil
	},
}
