package main

import (
	"context"
	"fmt"
	"os"

	"log/slog"

	mycmd "github.com/raghavkgarg/mycase/cmd"
	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/logging"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
	"github.com/urfave/cli/v3"
)

var Version = "0.0.0-dev"
var GitCommit = "unknown"
var BuildDate = "unknown"

// appLogger holds the process logger so the After hook can close its file.
var appLogger *logging.Logger

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
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "log-level",
				Usage:   "Log verbosity: debug, info, warn, error (default from config/defaults.json)",
				Sources: cli.EnvVars("MYCASE_LOG_LEVEL"),
			},
			&cli.StringFlag{
				Name:    "log-dir",
				Usage:   "Directory for JSON log files (default from config/defaults.json)",
				Sources: cli.EnvVars("MYCASE_LOG_DIR"),
			},
			&cli.BoolFlag{
				Name:  "quiet",
				Usage: "Suppress diagnostic logs on stderr (file logging still occurs)",
			},
			&cli.BoolFlag{
				Name:  "verbose",
				Usage: "Shorthand for --log-level debug",
			},
		},
		Before: func(ctx context.Context, c *cli.Command) (context.Context, error) {
			appLogger = setupLogging(c)
			slog.SetDefault(appLogger.Logger)

			reqID := logging.GenerateReqID(commandName(c))
			ctx = logging.WithReqID(ctx, reqID)
			slog.SetDefault(appLogger.With("req_id", reqID))

			slog.DebugContext(ctx, "command start",
				"command", commandName(c), "version", Version)
			return ctx, nil
		},
		After: func(_ context.Context, _ *cli.Command) error {
			if appLogger != nil {
				appLogger.Close()
			}
			return nil
		},
		Commands: []*cli.Command{
			mycmd.PipelineCommand,
			mycmd.AutopilotCommand,
			mycmd.PickCommand,
			mycmd.OptimizeCommand,
			mycmd.ReportCommand,
			mycmd.PerformanceCommand,
			mycmd.MonitorCommand,
			mycmd.BasketCommand,
			mycmd.HoldingsCommand,
			mycmd.TaxCommand,
			mycmd.MergeCommand,
			mycmd.AuthCommand,
			mycmd.CacheCommand,
			mycmd.DaemonCommand,
			mycmd.BacktestCommand,
			mycmd.ServeCommand,
			mycmd.ConvertCommand,
			mycmd.RetryCommand,
		},
	}
	if err := app.Run(context.Background(), os.Args); err != nil {
		// Log the failure (best-effort) and report to stderr for the user.
		if appLogger != nil {
			slog.Error("command failed", "error", err.Error())
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// setupLogging resolves logging config with precedence flag > env > config file >
// built-in default, then constructs the process logger and prunes old log files.
func setupLogging(c *cli.Command) *logging.Logger {
	defaults := config.LoadUserDefaults("config/defaults.json").Logging

	// Level: --verbose > --log-level/env > config > "info".
	level := defaults.Level
	if lv := c.String("log-level"); lv != "" {
		level = lv
	}
	if c.Bool("verbose") {
		level = "debug"
	}
	if level == "" {
		level = "info"
	}

	// Dir: --log-dir/env > config > default.
	dir := defaults.Dir
	if d := c.String("log-dir"); d != "" {
		dir = d
	}
	if dir == "" {
		dir = logging.DefaultDir
	}

	// File: config *bool (absent → true).
	file := true
	if defaults.File != nil {
		file = *defaults.File
	}

	retain := defaults.RetainDays
	if retain <= 0 {
		retain = logging.DefaultRetainDays
	}

	l := logging.SetupFile(logging.Config{
		Dir:        dir,
		Level:      level,
		File:       file,
		Quiet:      c.Bool("quiet"),
		RetainDays: retain,
	})
	logging.CleanOldLogs(dir, retain)
	return l
}

// commandName returns the name of the invoked subcommand, or "mycase" for the root.
func commandName(c *cli.Command) string {
	args := c.Args()
	if args.Len() > 0 {
		return args.First()
	}
	return c.Name
}
