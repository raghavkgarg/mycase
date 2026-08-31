package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/render"
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

	out := os.Stdout
	render.Section(out, "Pipeline Run: "+run.RunID)
	meta := []render.KVPair{
		{Key: "Status", Value: fmt.Sprintf("%s %s", statusIcon(run.Status), run.Status)},
		{Key: "Portfolio", Value: run.Portfolio},
		{Key: "Method", Value: run.Method},
		{Key: "Started", Value: run.StartedAt.Format("2006-01-02 15:04:05")},
	}
	if !run.CompletedAt.IsZero() {
		meta = append(meta, render.KVPair{
			Key:   "Completed",
			Value: fmt.Sprintf("%s (%.0fs)", run.CompletedAt.Format("2006-01-02 15:04:05"), run.CompletedAt.Sub(run.StartedAt).Seconds()),
		})
	}
	render.KV(out, meta)
	fmt.Fprintln(out)

	// --- Index picks ---
	allPicks, err := db.GetAllIndexPicks(ctx, runID)
	if err != nil {
		return fmt.Errorf("reading index picks: %w", err)
	}
	if len(allPicks) > 0 {
		render.Section(out, fmt.Sprintf("Index Picks (%d total)", len(allPicks)))
		rows := make([][]string, 0, len(allPicks))
		for _, p := range allPicks {
			rows = append(rows, []string{
				p.IndexName, p.Ticker, fmt.Sprintf("%d", p.Rank),
				scoreOrDash(p.Score), weightPct(p.Weight), dashIfEmpty(p.Sector),
			})
		}
		render.TableWithOpts(out, render.TableOpts{
			Headers: []string{"Index", "Ticker", "Rank", "Score", "Weight", "Sector"},
			Rows:    rows,
			Align:   []render.Alignment{render.AlignLeft, render.AlignLeft, render.AlignRight, render.AlignRight, render.AlignRight, render.AlignLeft},
		})
		fmt.Fprintln(out)
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
		render.Section(out, fmt.Sprintf("Proposals — %s (%d stocks)", stage, len(proposals)))
		rows := make([][]string, 0, len(proposals))
		for _, p := range proposals {
			rows = append(rows, []string{
				p.Ticker, fmt.Sprintf("%d", p.Rank),
				scoreOrDash(p.Score), weightPct(p.Weight), dashIfEmpty(p.Sector),
			})
		}
		render.TableWithOpts(out, render.TableOpts{
			Headers: []string{"Ticker", "Rank", "Score", "Weight", "Sector"},
			Rows:    rows,
			Align:   []render.Alignment{render.AlignLeft, render.AlignRight, render.AlignRight, render.AlignRight, render.AlignLeft},
		})
		fmt.Fprintln(out)
	}

	// --- Selections ---
	selections, err := db.GetSelections(ctx, runID)
	if err != nil {
		return fmt.Errorf("reading selections: %w", err)
	}
	if len(selections) > 0 {
		render.Section(out, fmt.Sprintf("Final Selections (%d stocks)", len(selections)))
		rows := make([][]string, 0, len(selections))
		for _, s := range selections {
			prevStr := "—"
			if s.PrevRank > 0 {
				prevStr = fmt.Sprintf("#%d", s.PrevRank)
			}
			rows = append(rows, []string{
				s.Ticker, fmt.Sprintf("%d", s.Rank),
				scoreOrDash(s.Score), weightPct(s.Weight), dashIfEmpty(s.Action), prevStr,
			})
		}
		render.TableWithOpts(out, render.TableOpts{
			Headers: []string{"Ticker", "Rank", "Score", "Weight", "Action", "Prev Rank"},
			Rows:    rows,
			Align:   []render.Alignment{render.AlignLeft, render.AlignRight, render.AlignRight, render.AlignRight, render.AlignLeft, render.AlignRight},
		})
		fmt.Fprintln(out)
	}

	if len(allPicks) == 0 && len(selections) == 0 {
		fmt.Println("  No data recorded for this run (pipeline may have been interrupted).")
	}

	return nil
}

// scoreOrDash formats a score, showing "—" when zero (unscored).
func scoreOrDash(score float64) string {
	if score == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f", score)
}

// weightPct formats a fractional weight (0.08) as a plain percentage ("8.00%").
// Unlike render.Pct, it emits no sign — weights are non-negative magnitudes.
func weightPct(w float64) string {
	return fmt.Sprintf("%.2f%%", w*100)
}

// dashIfEmpty returns "—" for empty strings, else the string unchanged.
func dashIfEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
