package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/broker/schwab"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/rawstore"
	"github.com/raghavkgarg/mycase/pkg/render"
)

// RawCommand groups operations over the raw-response archive (data/raw/**),
// the disposable capture/replay store (docs/03-roadmap.md Phase 11). It exposes
// retention pruning (R-store-3) and schema-blind triage (R-store-4): list the
// archive, print a capture, or resolve a capture's path for piping to jless/jq.
var RawCommand = &cli.Command{
	Name:  "raw",
	Usage: "Inspect and manage the raw API-response archive (data/raw)",
	Commands: []*cli.Command{
		{
			Name:    "ls",
			Aliases: []string{"list"},
			Usage:   "List archived responses (newest first)",
			Description: "Parses the archive filename convention into columns. Filter with the\n" +
				"flags below (case-insensitive substring match); combine freely.",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "source", Aliases: []string{"s"}, Usage: "Filter by source (e.g. schwab, yahoo)"},
				&cli.StringFlag{Name: "endpoint", Aliases: []string{"e"}, Usage: "Filter by endpoint (e.g. quotes, fundamentals)"},
				&cli.StringFlag{Name: "symbol", Aliases: []string{"t"}, Usage: "Filter by symbol/ticker"},
				&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 0, Usage: "Show at most N most-recent entries (0 = all)"},
			},
			Action: runRawLs,
		},
		{
			Name:      "show",
			Usage:     "Pretty-print the newest archived response matching a query",
			ArgsUsage: "[query]",
			Description: "query is a case-insensitive substring matched against the symbol first,\n" +
				"then the whole filename. With no query, shows the single newest capture.\n" +
				"JSON bodies are pretty-printed; non-JSON is emitted verbatim.",
			Action: runRawShow,
		},
		{
			Name:      "path",
			Usage:     "Print the path of the newest archived response matching a query",
			ArgsUsage: "[query]",
			Description: "Resolves the same way as `show` but prints only the path, for piping:\n" +
				"  jless \"$(mycase raw path AAPL)\"   |   jq . \"$(mycase raw path schwab__quotes)\"\n" +
				"With no query, prints the archive directory when it is empty, else the newest file.",
			Action: runRawPath,
		},
		{
			Name:      "inspect",
			Usage:     "Compare a Schwab fundamentals capture's raw wire fields vs. what the mapper produced",
			ArgsUsage: "[symbol]",
			Description: "Tier-2 triage (R-store-5): parses the newest archived Schwab\n" +
				"/instruments (projection=fundamental) response exactly as production does,\n" +
				"reruns mapSchwabFundamentals, and shows each raw wire field beside the\n" +
				"marketdata.Fundamentals value it produced. Purpose-built to localize the\n" +
				"`pick 0/N` bug (MarketCap landing as 0 despite HTTP 200s): it disambiguates\n" +
				"a wire zero from a mapper zero and flags wire keys the struct fails to bind.\n" +
				"symbol filters to a ticker (the capture endpoint is \"instruments\").",
			Action: runRawInspect,
		},
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

func runRawLs(_ context.Context, c *cli.Command) error {
	store := rawstore.NewDefault("")
	entries, err := store.List(rawstore.ListFilter{
		Source:   c.String("source"),
		Endpoint: c.String("endpoint"),
		Symbol:   c.String("symbol"),
	})
	if err != nil {
		return fmt.Errorf("listing raw archive: %w", err)
	}
	if len(entries) == 0 {
		fmt.Println("Raw archive is empty (or no captures match the filter).")
		return nil
	}

	if limit := int(c.Int("limit")); limit > 0 && limit < len(entries) {
		entries = entries[:limit]
	}

	rows := make([][]string, 0, len(entries))
	var total int64
	for _, e := range entries {
		sym := e.Symbol
		if sym == "" {
			sym = "-"
		}
		rows = append(rows, []string{
			e.Source, e.Endpoint, sym,
			e.When.Format("2006-01-02 15:04:05"),
			humanSize(e.Size),
		})
		total += e.Size
	}
	render.TableWithOpts(os.Stdout, render.TableOpts{
		Headers: []string{"Source", "Endpoint", "Symbol", "When", "Size"},
		Rows:    rows,
		Align:   []render.Alignment{render.AlignLeft, render.AlignLeft, render.AlignLeft, render.AlignLeft, render.AlignRight},
		Footer:  []string{fmt.Sprintf("%d file(s)", len(entries)), "", "", "", humanSize(total)},
	})
	return nil
}

func runRawShow(_ context.Context, c *cli.Command) error {
	query := c.Args().First()
	store := rawstore.NewDefault("")
	path, ok := store.ResolvePath(query)
	if !ok {
		return noMatchErr(query)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	// Pretty-print JSON; fall back to verbatim for anything that doesn't parse.
	var buf bytes.Buffer
	if json.Indent(&buf, data, "", "  ") == nil {
		fmt.Println(buf.String())
	} else {
		os.Stdout.Write(data)
		fmt.Println()
	}
	return nil
}

func runRawPath(_ context.Context, c *cli.Command) error {
	query := c.Args().First()
	store := rawstore.NewDefault("")
	path, ok := store.ResolvePath(query)
	if !ok {
		if query == "" {
			// Nothing captured yet — print the dir so callers can inspect it.
			fmt.Println(store.RawDir())
			return nil
		}
		return noMatchErr(query)
	}
	fmt.Println(path)
	return nil
}

func runRawInspect(_ context.Context, c *cli.Command) error {
	symbol := c.Args().First()
	store := rawstore.NewDefault("")

	// The Schwab fundamentals capture endpoint is "instruments" (the projection
	// is a query param, not a path segment), so filter on that.
	entries, err := store.List(rawstore.ListFilter{
		Source:   "schwab",
		Endpoint: "instruments",
		Symbol:   symbol,
	})
	if err != nil {
		return fmt.Errorf("listing schwab instruments captures: %w", err)
	}
	if len(entries) == 0 {
		if symbol != "" {
			return fmt.Errorf("no schwab instruments capture found for %q "+
				"(run a fundamentals fetch with MYCASE_CAPTURE unset first)", symbol)
		}
		return fmt.Errorf("no schwab instruments captures found " +
			"(run a fundamentals fetch with MYCASE_CAPTURE unset first)")
	}

	// entries is newest-first; inspect the most recent.
	e := entries[0]
	body, err := os.ReadFile(filepath.Join(store.RawDir(), e.Name))
	if err != nil {
		return fmt.Errorf("reading %s: %w", e.Name, err)
	}

	insp, err := schwab.InspectFundamentals(body)
	if err != nil {
		return fmt.Errorf("inspecting %s: %w", e.Name, err)
	}

	fmt.Printf("Capture: %s  (%s)\n", e.Name, e.When.Format("2006-01-02 15:04:05"))
	sym := insp.Symbol
	if sym == "" {
		sym = e.Symbol
	}
	fmt.Printf("Symbol: %s   instruments=%d   fundamental=%v\n\n",
		sym, insp.InstrumentCount, insp.HasFundamental)

	if len(insp.Fields) > 0 {
		rows := make([][]string, 0, len(insp.Fields))
		for _, fc := range insp.Fields {
			rows = append(rows, []string{fc.MappedField, fc.WireKey, fc.WireValue, fc.MappedValue, fc.Note})
		}
		render.TableWithOpts(os.Stdout, render.TableOpts{
			Headers: []string{"Mapped Field", "Wire Key", "Wire Value", "Mapped Value", "Note"},
			Rows:    rows,
			Align:   []render.Alignment{render.AlignLeft, render.AlignLeft, render.AlignRight, render.AlignRight, render.AlignLeft},
		})
	}

	if len(insp.Diagnostics) > 0 {
		fmt.Println("\nDiagnostics:")
		for _, d := range insp.Diagnostics {
			fmt.Printf("  • %s\n", d)
		}
	} else {
		fmt.Println("\nNo anomalies detected.")
	}
	return nil
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

func noMatchErr(query string) error {
	if query == "" {
		return fmt.Errorf("raw archive is empty")
	}
	return fmt.Errorf("no archived response matches %q", query)
}

func mb(bytes int64) float64 { return float64(bytes) / (1024 * 1024) }

// humanSize renders a byte count as a compact B/KB/MB string for the ls table.
func humanSize(n int64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
