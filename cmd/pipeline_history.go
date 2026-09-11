package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/render"
)

var pipelineHistoryCmd = &cli.Command{
	Name:  "history",
	Usage: "List recent pipeline runs recorded in DuckDB",
	Flags: []cli.Flag{
		&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 20, Usage: "Maximum number of runs to display"},
		&cli.StringFlag{Name: "portfolio", Aliases: []string{"p"}, Usage: "Filter by portfolio name (e.g. us_sp500, microsmall)"},
	},
	Action: runPipelineHistory,
}

func runPipelineHistory(ctx context.Context, c *cli.Command) error {
	db := cache.GetDB()
	if db == nil {
		fmt.Println("Cache is not initialised (data/cache.db could not be opened at startup).")
		return nil
	}

	limit := int(c.Int("limit"))
	portfolio := c.String("portfolio")

	var runs []cache.PipelineRun
	var err error
	if portfolio != "" {
		runs, err = db.ListRunsByPortfolio(ctx, portfolio, limit)
	} else {
		runs, err = db.ListRuns(ctx, limit)
	}
	if err != nil {
		return fmt.Errorf("listing pipeline runs: %w", err)
	}

	if len(runs) == 0 {
		fmt.Println("No pipeline runs recorded yet.")
		return nil
	}

	rows := make([][]string, 0, len(runs))
	for _, r := range runs {
		duration := "—"
		if !r.CompletedAt.IsZero() {
			d := r.CompletedAt.Sub(r.StartedAt)
			if d.Minutes() >= 1 {
				duration = fmt.Sprintf("%.1fm", d.Minutes())
			} else {
				duration = fmt.Sprintf("%.0fs", d.Seconds())
			}
		}
		rows = append(rows, []string{
			r.RunID,
			statusIcon(r.Status) + " " + string(r.Status),
			r.Portfolio,
			r.Method,
			duration,
			r.StartedAt.Format("2006-01-02 15:04:05"),
		})
	}

	render.Table(os.Stdout,
		[]string{"Run ID", "Status", "Portfolio", "Method", "Duration", "Started"},
		rows,
	)

	fmt.Printf("\n%d run(s) shown. Use 'mycase pipeline show <run_id>' for details.\n", len(runs))
	return nil
}

func statusIcon(s cache.RunStatus) string {
	switch s {
	case cache.RunStatusCompleted:
		return "✅"
	case cache.RunStatusFailed:
		return "❌"
	case cache.RunStatusRunning:
		return "⏳"
	case cache.RunStatusCancelled:
		return "⛔"
	default:
		return "?"
	}
}
