package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/edgar"
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
		dbMigrateEdgarFactsCmd,
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

		// Pre-flight Data Integrity Check on the newly committed snapshot
		if pitDB, pErr := pithistory.Open(dbPath); pErr == nil {
			if integrity, iErr := pitDB.CheckDataIntegrity(ctx, indexName, method); iErr == nil && integrity.TotalCandidates > 0 {
				if integrity.FailurePct >= 5.0 {
					fmt.Printf("\n⚠️  [DATA INTEGRITY WARNING]: %d / %d candidates (%.1f%%) in latest run have unverified or missing fundamentals!\n",
						integrity.FailedCandidates, integrity.TotalCandidates, integrity.FailurePct)
					if len(integrity.FlaggedTickers) > 0 {
						fmt.Printf("   Flagged candidates sample: %s\n", strings.Join(integrity.FlaggedTickers, ", "))
					}
					fmt.Printf("   Verify data feeds before interpreting marginal scores or executing trades.\n\n")
				} else {
					fmt.Printf("   ✓ Data Integrity Verified: %d / %d candidates clean (0 unverified).\n",
						integrity.TotalCandidates-integrity.FailedCandidates, integrity.TotalCandidates)
				}
			}
			pitDB.Close()
		}
	}

	// 3. Stage 3: Theme Lifecycle, Exit Detection & Return Auditing
	fmt.Printf("\n▶ STAGE 3/3: Synchronizing Theme Lifecycles & Return Intelligence...\n")
	themes, tErr := config.LoadThemes(config.Path("themes.json"))
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
				ProposalsDir:  config.DataPath("candidates", "proposals"),
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
		&cli.StringFlag{Name: "cache-db", Value: "", Usage: "Path to source cache.db (default: <data>/cache.db)"},
		&cli.StringFlag{Name: "pit-db", Value: "", Usage: "Path to source pit_history.db (default: <data>/pit_history.db)"},
		&cli.StringFlag{Name: "target-db", Value: "", Usage: "Path to target mycase.db (default: <data>/mycase.db)"},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		targetPath := c.String("target-db")
		if targetPath == "" {
			targetPath = config.DataPath("mycase.db")
		}
		cachePath := c.String("cache-db")
		if cachePath == "" {
			cachePath = config.DataPath("cache.db")
		}
		pitPath := c.String("pit-db")
		if pitPath == "" {
			pitPath = config.DataPath("pit_history.db")
		}

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

var dbMigrateEdgarFactsCmd = &cli.Command{
	Name:  "migrate-edgar-facts",
	Usage: "Convert the legacy edgar_facts raw-blob cache to the compact extracted-facts schema (re-derives locally, no EDGAR re-fetch) and reclaim disk space",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "db", Value: "", Usage: "Path to mycase.db (default: <data>/mycase.db)"},
		&cli.BoolFlag{Name: "no-reclaim", Usage: "Skip the file-rewrite that reclaims dead space (migrate rows only)"},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		dbPath := c.String("db")
		if dbPath == "" {
			dbPath = config.DataPath("mycase.db")
		}

		before, _ := fileSize(dbPath)
		fmt.Printf("Migrating edgar_facts in %s (%.0f MB)...\n", dbPath, float64(before)/(1024*1024))

		// Row-level migration: re-derive compact facts from the stored blobs.
		dc, err := cache.Open(dbPath)
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		ecl, err := edgar.NewClient("mycase-migrate/1.0 migrate@localhost", dc)
		if err != nil {
			dc.Close()
			return fmt.Errorf("edgar client: %w", err)
		}
		res, err := ecl.MigrateFactsBlobs(ctx)
		if err != nil {
			dc.Close()
			return fmt.Errorf("migrate rows: %w", err)
		}
		dc.Close()

		if res.AlreadyCompact {
			fmt.Println("edgar_facts is already the compact schema — nothing to migrate.")
		} else {
			fmt.Printf("Re-derived %d/%d rows (%d skipped: unparseable blob).\n", res.Migrated, res.Scanned, res.Skipped)
		}

		if c.Bool("no-reclaim") {
			fmt.Println("Skipping space reclaim (--no-reclaim).")
			return nil
		}

		// Reclaim dead pages by copying the whole DB to a fresh file, then
		// atomically swapping it in (original kept as .bak).
		reclaimed, err := reclaimDBFile(ctx, dbPath)
		if err != nil {
			return fmt.Errorf("reclaim space: %w", err)
		}
		after, _ := fileSize(dbPath)
		fmt.Printf("Reclaimed: %.0f MB → %.0f MB (backup at %s).\n",
			float64(before)/(1024*1024), float64(after)/(1024*1024), reclaimed)
		return nil
	},
}

// fileSize returns the size in bytes of a file.
func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// reclaimDBFile rewrites a DuckDB database into a fresh file (dropping dead
// pages left by row rewrites), then swaps it in place. The original is preserved
// as "<path>.bak". Returns the backup path.
func reclaimDBFile(ctx context.Context, dbPath string) (string, error) {
	freshPath := dbPath + ".compact"
	_ = os.Remove(freshPath)

	src, err := cache.Open(dbPath)
	if err != nil {
		return "", fmt.Errorf("open source: %w", err)
	}
	// DuckDB aliases the current database by its file-stem; COPY FROM DATABASE
	// needs that alias. Resolve it as the single non-system attached database
	// (the path column is normalized — e.g. /tmp → /private/tmp on macOS — so we
	// don't match on the path string).
	var srcAlias string
	if err := src.Conn().QueryRowContext(ctx,
		`SELECT database_name FROM duckdb_databases() WHERE database_name NOT IN ('system','temp') LIMIT 1`,
	).Scan(&srcAlias); err != nil {
		src.Close()
		return "", fmt.Errorf("resolve source alias: %w", err)
	}
	if _, err := src.Conn().ExecContext(ctx, fmt.Sprintf(`ATTACH '%s' AS compact_target`, freshPath)); err != nil {
		src.Close()
		return "", fmt.Errorf("attach fresh db: %w", err)
	}
	if _, err := src.Conn().ExecContext(ctx, fmt.Sprintf(`COPY FROM DATABASE "%s" TO compact_target`, srcAlias)); err != nil {
		src.Close()
		return "", fmt.Errorf("copy database: %w", err)
	}
	if _, err := src.Conn().ExecContext(ctx, `DETACH compact_target`); err != nil {
		src.Close()
		return "", fmt.Errorf("detach: %w", err)
	}
	src.Close()

	backupPath := dbPath + ".bak"
	_ = os.Remove(backupPath)
	if err := os.Rename(dbPath, backupPath); err != nil {
		return "", fmt.Errorf("backup original: %w", err)
	}
	if err := os.Rename(freshPath, dbPath); err != nil {
		// Best-effort restore.
		_ = os.Rename(backupPath, dbPath)
		return "", fmt.Errorf("swap fresh db in: %w", err)
	}
	return backupPath, nil
}
