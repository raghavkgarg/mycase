package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/render"
)

var pipelineDiffCmd = &cli.Command{
	Name:      "diff",
	Usage:     "Compare proposals or picks between two pipeline runs",
	ArgsUsage: "<run_id_1> <run_id_2>",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "stage", Aliases: []string{"s"}, Value: "optimized", Usage: "Proposal stage to compare (draft, optimized, final)"},
		&cli.BoolFlag{Name: "metrics", Aliases: []string{"m"}, Usage: "Also compare selection-level driver metrics (RSI, momentum, FCF yield, ROIC) between the two runs"},
	},
	Action: runPipelineDiff,
}

func runPipelineDiff(ctx context.Context, c *cli.Command) error {
	if c.NArg() < 2 {
		return fmt.Errorf("usage: mycase pipeline diff <run_id_1> <run_id_2>")
	}
	runID1 := c.Args().Get(0)
	runID2 := c.Args().Get(1)
	stage := c.String("stage")

	db := cache.GetDB()
	if db == nil {
		fmt.Println("Cache is not initialised (data/cache.db could not be opened at startup).")
		return nil
	}

	// Verify both runs exist.
	run1, err := db.GetRun(ctx, runID1)
	if err != nil {
		return fmt.Errorf("run %q: %w", runID1, err)
	}
	run2, err := db.GetRun(ctx, runID2)
	if err != nil {
		return fmt.Errorf("run %q: %w", runID2, err)
	}

	// Get proposals for the specified stage.
	proposals1, err := db.GetProposals(ctx, runID1, stage)
	if err != nil {
		return fmt.Errorf("reading proposals for %s: %w", runID1, err)
	}
	proposals2, err := db.GetProposals(ctx, runID2, stage)
	if err != nil {
		return fmt.Errorf("reading proposals for %s: %w", runID2, err)
	}

	// If no proposals at this stage, fall back to index_picks comparison.
	if len(proposals1) == 0 && len(proposals2) == 0 {
		return diffIndexPicks(ctx, db, run1, run2)
	}

	// Build maps.
	map1 := make(map[string]cache.Proposal, len(proposals1))
	for _, p := range proposals1 {
		map1[p.Ticker] = p
	}
	map2 := make(map[string]cache.Proposal, len(proposals2))
	for _, p := range proposals2 {
		map2[p.Ticker] = p
	}

	// Categorize: added, removed, changed, unchanged.
	type diffEntry struct {
		ticker  string
		action  string
		rank1   int
		rank2   int
		weight1 float64
		weight2 float64
	}
	var entries []diffEntry

	for ticker, p2 := range map2 {
		p1, existed := map1[ticker]
		if !existed {
			entries = append(entries, diffEntry{ticker: ticker, action: "added", rank2: p2.Rank, weight2: p2.Weight})
		} else if p1.Rank != p2.Rank || abs(p1.Weight-p2.Weight) > 0.0001 {
			entries = append(entries, diffEntry{ticker: ticker, action: "changed", rank1: p1.Rank, rank2: p2.Rank, weight1: p1.Weight, weight2: p2.Weight})
		}
	}
	for ticker, p1 := range map1 {
		if _, exists := map2[ticker]; !exists {
			entries = append(entries, diffEntry{ticker: ticker, action: "removed", rank1: p1.Rank, weight1: p1.Weight})
		}
	}

	out := os.Stdout
	render.Section(out, fmt.Sprintf("Pipeline Diff: %s stage", stage))
	render.KV(out, []render.KVPair{
		{Key: "Run A", Value: fmt.Sprintf("%s (%s, %s)", run1.RunID, run1.Portfolio, run1.StartedAt.Format("2006-01-02"))},
		{Key: "Run B", Value: fmt.Sprintf("%s (%s, %s)", run2.RunID, run2.Portfolio, run2.StartedAt.Format("2006-01-02"))},
		{Key: "Stocks", Value: fmt.Sprintf("%d in A | %d in B", len(proposals1), len(proposals2))},
	})
	fmt.Fprintln(out)

	if len(entries) == 0 {
		fmt.Println("  No differences found between the two runs.")
		return nil
	}

	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		var rankStr, wt1Str, wt2Str string
		switch e.action {
		case "added":
			rankStr = fmt.Sprintf("— → #%d", e.rank2)
			wt1Str = "—"
			wt2Str = weightPct(e.weight2)
		case "removed":
			rankStr = fmt.Sprintf("#%d → —", e.rank1)
			wt1Str = weightPct(e.weight1)
			wt2Str = "—"
		case "changed":
			rankStr = fmt.Sprintf("#%d → #%d", e.rank1, e.rank2)
			wt1Str = weightPct(e.weight1)
			wt2Str = weightPct(e.weight2)
		}
		rows = append(rows, []string{e.ticker, e.action, rankStr, wt1Str, wt2Str})
	}
	render.TableWithOpts(out, render.TableOpts{
		Headers: []string{"Ticker", "Change", "Rank A→B", "Wt A", "Wt B"},
		Rows:    rows,
		Align:   []render.Alignment{render.AlignLeft, render.AlignLeft, render.AlignRight, render.AlignRight, render.AlignRight},
	})

	// Summary.
	added, removed, changed := 0, 0, 0
	for _, e := range entries {
		switch e.action {
		case "added":
			added++
		case "removed":
			removed++
		case "changed":
			changed++
		}
	}
	fmt.Printf("\n  Summary: +%d added, -%d removed, ~%d changed\n", added, removed, changed)

	if c.Bool("metrics") {
		if err := diffSelectionMetrics(ctx, db, runID1, runID2); err != nil {
			return err
		}
	}
	return nil
}

// driverMetric describes one comparable selection-level driver metric.
type driverMetric struct {
	name string
	get  func(cache.Selection) float64
	// pct=true renders the value as a percentage (fraction × 100); otherwise a
	// plain 2-decimal number (used for RSI, which is already 0–100).
	pct bool
}

var selectionDriverMetrics = []driverMetric{
	{name: "RSI", get: func(s cache.Selection) float64 { return s.RSI }, pct: false},
	{name: "Momentum 1Y", get: func(s cache.Selection) float64 { return s.Momentum1Y }, pct: true},
	{name: "FCF Yield", get: func(s cache.Selection) float64 { return s.FCFYield }, pct: true},
	{name: "ROIC", get: func(s cache.Selection) float64 { return s.ROIC }, pct: true},
	{name: "TTM Growth", get: func(s cache.Selection) float64 { return s.TTMGrowth }, pct: true},
	{name: "Revenue CAGR", get: func(s cache.Selection) float64 { return s.RevenueCagr }, pct: true},
	{name: "DSO Delta", get: func(s cache.Selection) float64 { return s.DSODelta }, pct: false},
}

// diffSelectionMetrics compares the persisted selection-level driver metrics
// (RSI, momentum, FCF yield, ROIC, ...) for tickers held in both runs, emitting
// one row per (ticker, metric) whose value changed. It reads from the
// selections table via GetSelections; runs recorded before driver metrics were
// plumbed will simply show zeros.
func diffSelectionMetrics(ctx context.Context, db *cache.Cache, runID1, runID2 string) error {
	sel1, err := db.GetSelections(ctx, runID1)
	if err != nil {
		return fmt.Errorf("reading selections for %s: %w", runID1, err)
	}
	sel2, err := db.GetSelections(ctx, runID2)
	if err != nil {
		return fmt.Errorf("reading selections for %s: %w", runID2, err)
	}

	out := os.Stdout
	fmt.Fprintln(out)
	render.Section(out, "Selection Driver Metrics")

	if len(sel1) == 0 && len(sel2) == 0 {
		fmt.Println("  No selection metrics recorded for either run (run with a scoring method that records drivers).")
		return nil
	}

	map1 := make(map[string]cache.Selection, len(sel1))
	for _, s := range sel1 {
		map1[s.Ticker] = s
	}
	map2 := make(map[string]cache.Selection, len(sel2))
	for _, s := range sel2 {
		map2[s.Ticker] = s
	}

	// Compare only tickers present in both runs; added/removed tickers are
	// already surfaced by the proposals diff above.
	common := make([]string, 0, len(map1))
	for ticker := range map2 {
		if _, ok := map1[ticker]; ok {
			common = append(common, ticker)
		}
	}
	sort.Strings(common)

	var rows [][]string
	for _, ticker := range common {
		a, b := map1[ticker], map2[ticker]
		for _, m := range selectionDriverMetrics {
			va, vb := m.get(a), m.get(b)
			if va == 0 && vb == 0 {
				continue // metric not recorded by either run's method
			}
			if abs(va-vb) < 1e-9 {
				continue // unchanged
			}
			rows = append(rows, []string{
				ticker,
				m.name,
				fmtMetric(va, m.pct),
				fmtMetric(vb, m.pct),
				fmtMetricDelta(vb-va, m.pct),
			})
		}
	}

	if len(rows) == 0 {
		fmt.Println("  No driver-metric changes between the two runs for commonly-held tickers.")
		return nil
	}

	render.TableWithOpts(out, render.TableOpts{
		Headers: []string{"Ticker", "Metric", "A", "B", "Δ"},
		Rows:    rows,
		Align: []render.Alignment{
			render.AlignLeft, render.AlignLeft, render.AlignRight, render.AlignRight, render.AlignRight,
		},
	})
	return nil
}

func fmtMetric(v float64, pct bool) string {
	if pct {
		return fmt.Sprintf("%.2f%%", v*100.0)
	}
	return fmt.Sprintf("%.2f", v)
}

func fmtMetricDelta(d float64, pct bool) string {
	sign := "+"
	if d < 0 {
		sign = "-"
		d = -d
	}
	if pct {
		return fmt.Sprintf("%s%.2f%%", sign, d*100.0)
	}
	return fmt.Sprintf("%s%.2f", sign, d)
}

func diffIndexPicks(ctx context.Context, db *cache.Cache, run1, run2 cache.PipelineRun) error {
	picks1, err := db.GetAllIndexPicks(ctx, run1.RunID)
	if err != nil {
		return fmt.Errorf("reading picks for %s: %w", run1.RunID, err)
	}
	picks2, err := db.GetAllIndexPicks(ctx, run2.RunID)
	if err != nil {
		return fmt.Errorf("reading picks for %s: %w", run2.RunID, err)
	}

	if len(picks1) == 0 && len(picks2) == 0 {
		fmt.Println("No proposals or picks recorded for either run.")
		return nil
	}

	set1 := make(map[string]bool, len(picks1))
	for _, p := range picks1 {
		set1[p.Ticker] = true
	}
	set2 := make(map[string]bool, len(picks2))
	for _, p := range picks2 {
		set2[p.Ticker] = true
	}

	out := os.Stdout
	render.Section(out, "Pipeline Diff: index picks (no proposals available)")
	render.KV(out, []render.KVPair{
		{Key: "Run A", Value: fmt.Sprintf("%s (%d picks)", run1.RunID, len(picks1))},
		{Key: "Run B", Value: fmt.Sprintf("%s (%d picks)", run2.RunID, len(picks2))},
	})
	fmt.Fprintln(out)

	var added, removed []string
	for _, p := range picks2 {
		if !set1[p.Ticker] {
			added = append(added, p.Ticker)
		}
	}
	for _, p := range picks1 {
		if !set2[p.Ticker] {
			removed = append(removed, p.Ticker)
		}
	}

	if len(added) > 0 {
		fmt.Printf("  Added in B (%d): %s\n", len(added), strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		fmt.Printf("  Removed from A (%d): %s\n", len(removed), strings.Join(removed, ", "))
	}
	if len(added) == 0 && len(removed) == 0 {
		fmt.Println("  No differences in index picks between the two runs.")
	}
	return nil
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
