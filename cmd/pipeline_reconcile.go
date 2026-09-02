package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/autopilot"
	"github.com/raghavkgarg/mycase/pkg/broker/schwab"
	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/render"
)

var pipelineReconcileCmd = &cli.Command{
	Name:      "reconcile",
	Usage:     "Reconcile a run's executed basket against actual broker fills",
	ArgsUsage: "<run_id>",
	Description: "Fetches the broker's TRADE transactions for the run's execution window, " +
		"aggregates the realized BUY value per ticker into realized weights, and " +
		"overwrites the run's \"final\" proposal stage. This upgrades the final stage " +
		"from submitted-intent weights (limit price × qty at confirm time) to weights " +
		"derived from actual fills, so performance attribution measures what really executed.",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "broker", Value: "schwab", Usage: "Broker to reconcile against (schwab)"},
		&cli.IntFlag{Name: "days", Value: 5, Usage: "Days after the run started to include fills (covers next-day/partial fills)"},
		&cli.BoolFlag{Name: "dry-run", Usage: "Show the reconciled weights without writing them"},
	},
	Action: runPipelineReconcile,
}

func runPipelineReconcile(ctx context.Context, c *cli.Command) error {
	if c.NArg() < 1 {
		return fmt.Errorf("usage: mycase pipeline reconcile <run_id>")
	}
	runID := c.Args().First()

	brokerName := c.String("broker")
	if brokerName != "schwab" {
		return fmt.Errorf("pipeline reconcile currently supports only --broker schwab (got %q)", brokerName)
	}

	db := cache.GetDB()
	if db == nil {
		return fmt.Errorf("DuckDB cache not available — cannot reconcile")
	}

	run, err := db.GetRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("run %q: %w", runID, err)
	}

	// Execution window: fills happen at/after the run started (the pipeline
	// generates the proposal, the investor confirms, orders fill same- or
	// next-day). Bound generously with --days to catch partial/next-day fills.
	from := run.StartedAt
	to := run.StartedAt.AddDate(0, 0, c.Int("days"))
	if now := time.Now(); to.After(now) {
		to = now
	}

	client := newSchwabClient()
	if client == nil {
		return fmt.Errorf("schwab credentials not configured — run 'mycase auth --broker schwab' first")
	}
	hash, err := client.FetchAccountHash(ctx)
	if err != nil {
		return fmt.Errorf("fetching account hash: %w", err)
	}

	out := os.Stdout
	render.Section(out, "Reconcile Run: "+run.RunID)
	render.KV(out, []render.KVPair{
		{Key: "Portfolio", Value: run.Portfolio},
		{Key: "Method", Value: run.Method},
		{Key: "Fill window", Value: fmt.Sprintf("%s → %s", from.Format("2006-01-02"), to.Format("2006-01-02"))},
	})
	fmt.Fprintln(out)

	// Schwab caps each request at ~1 year; chunk (window is normally days, so
	// this is usually a single call, but stay consistent with tax import).
	var raw []schwab.SchwabTransaction
	for chunkStart := from; chunkStart.Before(to); chunkStart = chunkStart.AddDate(1, 0, 0) {
		chunkEnd := chunkStart.AddDate(1, 0, 0)
		if chunkEnd.After(to) {
			chunkEnd = to
		}
		batch, err := client.FetchTransactions(ctx, hash, chunkStart, chunkEnd)
		if err != nil {
			return fmt.Errorf("fetching transactions [%s – %s]: %w",
				chunkStart.Format("2006-01-02"), chunkEnd.Format("2006-01-02"), err)
		}
		raw = append(raw, batch...)
	}

	txns := schwab.NormalizeTransactions(raw)
	realized := autopilot.RealizedStageProposals(txns, from, to)
	if len(realized) == 0 {
		fmt.Println("No BUY fills found in the window — nothing to reconcile.")
		fmt.Println("(The final stage keeps its submitted-intent weights.)")
		return nil
	}

	render.Section(out, fmt.Sprintf("Realized Basket (%d stocks)", len(realized)))
	rows := make([][]string, 0, len(realized))
	for _, p := range realized {
		rows = append(rows, []string{p.Ticker, fmt.Sprintf("%d", p.Rank), weightPct(p.Weight)})
	}
	render.TableWithOpts(out, render.TableOpts{
		Headers: []string{"Ticker", "Rank", "Realized Wt"},
		Rows:    rows,
		Align:   []render.Alignment{render.AlignLeft, render.AlignRight, render.AlignRight},
	})
	fmt.Fprintln(out)

	if c.Bool("dry-run") {
		fmt.Println("Dry run — no changes written. Re-run without --dry-run to persist.")
		return nil
	}

	// Overwrite the final stage: delete first so tickers that were submitted but
	// never filled don't linger with stale intent weights (UPSERT only touches
	// tickers present in the new rows).
	if err := db.DeleteProposalsStage(ctx, runID, "final"); err != nil {
		return fmt.Errorf("clearing prior final stage: %w", err)
	}
	if err := db.InsertProposals(ctx, runID, "final", realized); err != nil {
		return fmt.Errorf("writing realized final stage: %w", err)
	}

	fmt.Printf("Reconciled: final stage for %s now holds %d realized weights.\n", run.RunID, len(realized))
	fmt.Println("Performance attribution will now use these realized weights.")
	return nil
}
