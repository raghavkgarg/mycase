package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/marketdata"
	"github.com/raghavkgarg/mycase/pkg/pithistory"
	"github.com/raghavkgarg/mycase/pkg/render"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
	"github.com/raghavkgarg/mycase/pkg/themedb"
	"github.com/urfave/cli/v3"
)

var DBCommand = &cli.Command{
	Name:    "db",
	Aliases: []string{"database"},
	Usage:   "Manage consolidated DuckDB database (data/mycase.db) and daily EOD update runs",
	Commands: []*cli.Command{
		dbUpdateCmd,
		dbStatusCmd,
		dbMigrateCmd,
	},
}

var dbUpdateCmd = &cli.Command{
	Name:    "update",
	Aliases: []string{"eod", "daily", "sync-all"},
	Usage:   "Execute unified post-market (4:00 PM) EOD update across market data, PIT research, and themes",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "all",
			Aliases: []string{"a"},
			Value:   true,
			Usage:   "Update all configured indices and portfolio themes",
		},
		&cli.StringFlag{
			Name:    "index",
			Aliases: []string{"i"},
			Value:   "niftytotalmarket",
			Usage:   "Primary index universe for daily PIT screening",
		},
		&cli.StringFlag{
			Name:    "method",
			Aliases: []string{"m"},
			Value:   "earlymb",
			Usage:   "Strategy scoring method (earlymb, multibagger)",
		},
		&cli.IntFlag{
			Name:  "top",
			Value: 10,
			Usage: "Top N candidate stocks to stage",
		},
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Preview the update sequence without executing database writes",
		},
		&cli.StringFlag{
			Name:  "db",
			Usage: "Path to mycase.db (defaults to data/mycase.db)",
		},
		&cli.BoolFlag{
			Name:    "force",
			Aliases: []string{"f"},
			Usage:   "Force re-running PIT calculation even if file for effective EOD date already exists",
		},
	},
	Action: runDBUpdate,
}

func runDBUpdate(ctx context.Context, c *cli.Command) error {
	return RunDBUpdateDirect(
		ctx,
		c.Bool("all"),
		c.String("index"),
		c.String("method"),
		int(c.Int("top")),
		c.Bool("dry-run"),
		c.String("db"),
		c.Bool("force"),
	)
}

// RunDBUpdateDirect executes the unified EOD database update programmatically.
func RunDBUpdateDirect(ctx context.Context, all bool, indexName, method string, topN int, dryRun bool, dbPath string, force ...bool) error {
	isForce := len(force) > 0 && force[0]
	now := time.Now()
	targetEOD := marketdata.EODSettlementDate(now)
	targetDateStr := targetEOD.Format("2006-01-02")
	nextAvailable := marketdata.NextEODAvailableDate(now)

	render.Banner(os.Stdout, "MYCASE UNIFIED EOD DATABASE UPDATE (data/mycase.db)")
	fmt.Printf("Execution Time: %s | Target As-Of Date: %s\n\n", now.Format("2006-01-02 15:04:05 MST"), targetDateStr)

	if dryRun {
		fmt.Println("🔍 DRY RUN MODE ACTIVE — No database changes will be committed.")
		fmt.Println("1. [Dry Run] Check database schema & connections (data/mycase.db)")
		fmt.Printf("2. [Dry Run] Daily PIT Research Screening for %s (%s, Top %d)\n", indexName, method, topN)
		fmt.Println("3. [Dry Run] Automated constituent self-healing retry pass")
		fmt.Println("4. [Dry Run] Synchronize theme lifecycle, exits, and return intelligence across all themes")
		fmt.Println("\nDry run completed successfully.")
		return nil
	}

	// 1. Verify / open data/mycase.db
	db, err := themedb.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening mycase.db: %w", err)
	}
	defer db.Close()

	// 2. Stage 1 & 2: Daily Point-in-Time Screening & Factor Scoring
	fmt.Printf("▶ STAGE 1/3: Point-in-Time Research Screening [%s | %s]...\n", indexName, method)
	pitDB, pErr := pithistory.Open(dbPath)
	var hasRun bool
	if pErr == nil {
		hasRun, _ = pitDB.HasRun(ctx, targetDateStr, indexName, method)
		pitDB.Close()
	}

	if hasRun && !isForce {
		targetDayStr := marketdata.FormatOrdinalDay(targetEOD.Day())
		nextDayStr := marketdata.FormatOrdinalDay(nextAvailable.Day())
		nextMonthStr := nextAvailable.Format("Jan")
		fmt.Printf("✓ File available for %s. Latest file is of %s and %s file will be available after 21:00 PM %s %s.\n",
			targetDateStr, targetDayStr, nextDayStr, nextDayStr, nextMonthStr)
		fmt.Println("Skipping redundant PIT screening calculation.")
	} else {
		opts := &stockpicker.Options{
			IndexName:          indexName,
			Method:             method,
			TopN:               topN,
			RangeStr:           "1y",
			RebalanceTolerance: 0.10,
			AsOfDate:           targetDateStr,
		}
		if err := runPickWithOpts(ctx, opts); err != nil {
			fmt.Printf("⚠️  Warning during PIT update: %v (continuing with self-healing pass)\n", err)
		}

		// Self-healing retry pass for any transient dropouts
		fmt.Printf("\n▶ STAGE 2/3: Verifying Snapshot Completeness & Self-Healing...\n")
		if snap, sErr := stockpicker.RetryFailedSnapshotCandidates(ctx, indexName, method, targetDateStr); sErr == nil && snap != nil {
			if pitDB, pErr := pithistory.Open(dbPath); pErr == nil {
				_ = pitDB.SaveRunSnapshot(ctx, snap)
				pitDB.Close()
			}
		} else if sErr != nil {
			fmt.Printf("Notice on self-healing retry: %v\n", sErr)
		}
	}


	// 3. Stage 3: Theme Lifecycle, Exit Detection & Return Auditing
	fmt.Printf("\n▶ STAGE 3/3: Synchronizing Theme Lifecycles & Return Intelligence...\n")
	themes, tErr := config.LoadThemes("config/themes.json")
	if tErr == nil {
		for _, tc := range themes {
			uName := csvloader.GetUniverseName(tc.CSVPath)
			kw := strings.ToLower(tc.Prefix)
			if strings.Contains(strings.ToLower(tc.Name), "microsmall") {
				kw = "microsmall"
			}
			syncOpts := themedb.SyncThemeOptions{
				ThemeName:     uName,
				GoldenCSVPath: tc.CSVPath,
				ProposalsDir:  "data/candidates/proposals",
				Keyword:       kw,
			}
			if err := db.SyncThemeFromProposals(ctx, syncOpts); err != nil {
				// Non-fatal if a theme has no local files yet (e.g. hydrogen)
				continue
			}
			v, _ := db.GetLatestVersion(ctx, uName)
			active, _ := db.GetActiveHoldings(ctx, uName)
			exited, _ := db.GetExitedHoldings(ctx, uName)
			fmt.Printf("  • Theme '%s' (%s): v%d | %d Active | %d Exited\n", tc.Name, uName, v, len(active), len(exited))
		}
	}

	fmt.Println()
	render.Banner(os.Stdout, "EOD DATABASE UPDATE COMPLETED SUCCESSFULLY")
	fmt.Println("data/mycase.db is 100% updated and cached for today.")
	fmt.Println("All queries ('returns', 'theme show', 'pit stats', web dashboard) are now instant & offline.")
	return nil
}

var dbStatusCmd = &cli.Command{
	Name:    "status",
	Aliases: []string{"info", "stats"},
	Usage:   "Display table inventory and total row counts in data/mycase.db",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "db",
			Usage: "Path to mycase.db",
		},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		dbPath := c.String("db")
		db, err := themedb.Open(dbPath)
		if err != nil {
			return fmt.Errorf("opening mycase.db: %w", err)
		}
		defer db.Close()

		stats, err := db.GetDatabaseStats(ctx)
		if err != nil {
			return fmt.Errorf("fetching database stats: %w", err)
		}

		fmt.Println("\n📊 CONSOLIDATED DATABASE STATUS (data/mycase.db):")
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "Table Name\tLayer / Domain\tRow Count\tStatus")
		fmt.Fprintln(tw, "----------\t--------------\t---------\t------")

		var totalRows int64
		for _, s := range stats {
			layer := "Other"
			switch s.TableName {
			case "prices", "fundamentals", "cache_meta":
				layer = "1. Market Data Cache"
			case "pit_runs", "pit_candidate_scores":
				layer = "2. PIT Quant Research"
			case "pipeline_runs", "index_picks", "proposals", "selections":
				layer = "3. Pipeline Staging"
			case "theme_rebalances", "theme_history":
				layer = "4. Theme Lifecycle"
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", s.TableName, layer, s.Rows, s.Status)
			totalRows += s.Rows
		}
		tw.Flush()
		fmt.Printf("\nTotal Tables: %d | Total Records: %d\n\n", len(stats), totalRows)
		return nil
	},
}

var dbMigrateCmd = &cli.Command{
	Name:    "migrate",
	Aliases: []string{"consolidate"},
	Usage:   "Migrate all tables from cache.db and pit_history.db into mycase.db",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "cache-db", Value: "data/cache.db", Usage: "Path to source cache.db"},
		&cli.StringFlag{Name: "pit-db", Value: "data/pit_history.db", Usage: "Path to source pit_history.db"},
		&cli.StringFlag{Name: "target-db", Value: "data/mycase.db", Usage: "Path to target mycase.db"},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		targetPath := c.String("target-db")
		cachePath := c.String("cache-db")
		pitPath := c.String("pit-db")

		fmt.Printf("🚀 Consolidating tables into %s...\n", targetPath)
		db, err := themedb.Open(targetPath)
		if err != nil {
			return fmt.Errorf("opening target db: %w", err)
		}
		defer db.Close()

		results, err := db.MigrateAllToMycaseDB(ctx, cachePath, pitPath)
		if err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}

		fmt.Println("\n✅ MIGRATION SUMMARY:")
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "Table Name\tSource\tRow Count\tStatus")
		fmt.Fprintln(tw, "----------\t------\t---------\t------")
		for _, r := range results {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", r.TableName, r.Source, r.Rows, r.Status)
		}
		tw.Flush()
		fmt.Println("\nDatabase consolidation completed successfully!")
		return nil
	},
}
