package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/cache"
)

var pipelineShowCmd = &cli.Command{
	Name:      "show",
	Usage:     "Display picks, proposals, and selections for a specific pipeline run",
	ArgsUsage: "<run_id>",
	Action:    runPipelineShow,
}

func runPipelineShow(ctx context.Context, c *cli.Command) error {
	if c.NArg() < 1 {
		return fmt.Errorf("usage: mycase pipeline show <run_id>")
	}
	runID := c.Args().First()

	db := cache.GetDB()
	if db == nil {
		fmt.Println("Cache is not initialised (data/cache.db could not be opened at startup).")
		return nil
	}

	// --- Run metadata ---
	run, err := db.GetRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("run %q: %w", runID, err)
	}

	fmt.Println("====================================================================")
	fmt.Printf("  Pipeline Run: %s\n", run.RunID)
	fmt.Println("====================================================================")
	fmt.Printf("  Status:     %s %s\n", statusIcon(run.Status), run.Status)
	fmt.Printf("  Portfolio:  %s\n", run.Portfolio)
	fmt.Printf("  Method:     %s\n", run.Method)
	fmt.Printf("  Started:    %s\n", run.StartedAt.Format("2006-01-02 15:04:05"))
	if !run.CompletedAt.IsZero() {
		fmt.Printf("  Completed:  %s (%.0fs)\n", run.CompletedAt.Format("2006-01-02 15:04:05"), run.CompletedAt.Sub(run.StartedAt).Seconds())
	}
	fmt.Println()

	// --- Index picks ---
	allPicks, err := db.GetAllIndexPicks(ctx, runID)
	if err != nil {
		return fmt.Errorf("reading index picks: %w", err)
	}
	if len(allPicks) > 0 {
		fmt.Printf("  Index Picks (%d total)\n", len(allPicks))
		fmt.Println("  " + strings.Repeat("-", 70))
		fmt.Printf("  %-12s %-10s %6s %6s %8s  %s\n", "Index", "Ticker", "Rank", "Score", "Weight", "Sector")
		fmt.Println("  " + strings.Repeat("-", 70))
		for _, p := range allPicks {
			scoreStr := "—"
			if p.Score != 0 {
				scoreStr = fmt.Sprintf("%.1f", p.Score)
			}
			sectorStr := p.Sector
			if sectorStr == "" {
				sectorStr = "—"
			}
			fmt.Printf("  %-12s %-10s %6d %6s %7.2f%%  %s\n",
				p.IndexName, p.Ticker, p.Rank, scoreStr, p.Weight*100, sectorStr)
		}
		fmt.Println()
	}

	// --- Proposals ---
	for _, stage := range []string{"draft", "optimized", "final"} {
		proposals, err := db.GetProposals(ctx, runID, stage)
		if err != nil {
			return fmt.Errorf("reading %s proposals: %w", stage, err)
		}
		if len(proposals) == 0 {
			continue
		}
		fmt.Printf("  Proposals — %s (%d stocks)\n", stage, len(proposals))
		fmt.Println("  " + strings.Repeat("-", 56))
		fmt.Printf("  %-10s %6s %6s %8s  %s\n", "Ticker", "Rank", "Score", "Weight", "Sector")
		fmt.Println("  " + strings.Repeat("-", 56))
		for _, p := range proposals {
			scoreStr := "—"
			if p.Score != 0 {
				scoreStr = fmt.Sprintf("%.1f", p.Score)
			}
			sectorStr := p.Sector
			if sectorStr == "" {
				sectorStr = "—"
			}
			fmt.Printf("  %-10s %6d %6s %7.2f%%  %s\n",
				p.Ticker, p.Rank, scoreStr, p.Weight*100, sectorStr)
		}
		fmt.Println()
	}

	// --- Selections ---
	selections, err := db.GetSelections(ctx, runID)
	if err != nil {
		return fmt.Errorf("reading selections: %w", err)
	}
	if len(selections) > 0 {
		fmt.Printf("  Final Selections (%d stocks)\n", len(selections))
		fmt.Println("  " + strings.Repeat("-", 72))
		fmt.Printf("  %-10s %6s %6s %8s %-10s %s\n", "Ticker", "Rank", "Score", "Weight", "Action", "Prev Rank")
		fmt.Println("  " + strings.Repeat("-", 72))
		for _, s := range selections {
			scoreStr := "—"
			if s.Score != 0 {
				scoreStr = fmt.Sprintf("%.1f", s.Score)
			}
			actionStr := s.Action
			if actionStr == "" {
				actionStr = "—"
			}
			prevStr := "—"
			if s.PrevRank > 0 {
				prevStr = fmt.Sprintf("#%d", s.PrevRank)
			}
			fmt.Printf("  %-10s %6d %6s %7.2f%% %-10s %s\n",
				s.Ticker, s.Rank, scoreStr, s.Weight*100, actionStr, prevStr)
		}
		fmt.Println()
	}

	if len(allPicks) == 0 && len(selections) == 0 {
		fmt.Println("  No data recorded for this run (pipeline may have been interrupted).")
	}

	return nil
}
