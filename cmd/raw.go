package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/rawstore"
)

// RawCommand groups operations over the raw-response archive (data/raw/**),
// the disposable capture/replay store (docs/roadmap.md Phase 11). Today it
// exposes retention pruning; triage subcommands (ls/show) arrive in later
// R-store tasks.
var RawCommand = &cli.Command{
	Name:  "raw",
	Usage: "Inspect and manage the raw API-response archive (data/raw)",
	Commands: []*cli.Command{
		{
			Name:  "prune",
			Usage: "Enforce retention (age + total-size ceilings) over the raw archive",
			Description: "Deletes archived responses that exceed the retention ceilings, oldest-first.\n" +
				"Ceilings come from config/defaults.json (\"raw\" block), overridable by env\n" +
				"(MYCASE_RAW_RETAIN_DAYS, MYCASE_RAW_MAX_SIZE_MB) and the flags below\n" +
				"(flag > env > config > default). A ceiling of 0 disables that dimension.\n" +
				"This also runs automatically, best-effort, at the end of every command.",
			Flags: []cli.Flag{
				&cli.IntFlag{Name: "retain-days", Value: -1, Usage: "Delete captures older than N days (0 disables; default from config)"},
				&cli.IntFlag{Name: "max-size-mb", Value: -1, Usage: "Cap total archive size in MB (0 disables; default from config)"},
			},
			Action: runRawPrune,
		},
	},
}

func runRawPrune(_ context.Context, c *cli.Command) error {
	rawCfg := config.LoadUserDefaults(config.Path("defaults.json")).Raw
	ret := rawstore.ResolveRetention(rawCfg, int(c.Int("retain-days")), int(c.Int("max-size-mb")))

	store := rawstore.NewDefault("")
	res, err := store.Prune(ret)
	if err != nil {
		return fmt.Errorf("pruning raw archive: %w", err)
	}

	if res.Removed() == 0 {
		fmt.Printf("Nothing to prune — archive within limits (%d files, %.1f MB).\n",
			res.Remaining, mb(res.RemainingSize))
		return nil
	}
	fmt.Printf("Pruned %d file(s) — %d by age, %d by size; freed %.1f MB.\n",
		res.Removed(), res.RemovedByAge, res.RemovedBySize, mb(res.FreedBytes))
	fmt.Printf("Remaining: %d file(s), %.1f MB.\n", res.Remaining, mb(res.RemainingSize))
	return nil
}

func mb(bytes int64) float64 { return float64(bytes) / (1024 * 1024) }
