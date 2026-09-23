package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/render"
)

// HolidaysCommand is the read-only operator view of the trading-holiday calendar
// that lives in the `holidays` table of data/mycase.db. Holidays are seeded and
// refreshed operationally (see docs/18-runbook.md and holiday.sql) — this command
// does not write; it exists so an operator can verify the table is populated
// (e.g. on a fresh machine or after the yearly refresh) rather than discovering
// an empty calendar months later when a run fires on a holiday.
var HolidaysCommand = &cli.Command{
	Name:  "holidays",
	Usage: "Inspect the trading-holiday calendar (holidays table in mycase.db)",
	Commands: []*cli.Command{
		holidaysStatusCmd,
		holidaysListCmd,
	},
}

var holidaysStatusCmd = &cli.Command{
	Name:   "status",
	Usage:  "Show per-exchange holiday counts and date range; flags unseeded exchanges",
	Action: runHolidaysStatus,
}

var holidaysListCmd = &cli.Command{
	Name:  "list",
	Usage: "List all holiday dates for an exchange",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "exchange", Aliases: []string{"e"}, Value: "NYSE", Usage: "Exchange key (NYSE or NSE)"},
	},
	Action: runHolidaysList,
}

// runHolidaysStatus prints per-exchange counts + date range and, for any known
// exchange with no rows, a loud NOT SEEDED line. It exits non-zero when a known
// exchange is unseeded so scripts / a human can detect the misconfiguration
// deliberately (the empty calendar otherwise degrades silently to weekend-only).
func runHolidaysStatus(ctx context.Context, _ *cli.Command) error {
	store := broker.HolidayStore()
	if store == nil {
		return fmt.Errorf("cache DB is not open (data/mycase.db) — cannot read holidays")
	}
	stats, err := store.HolidayStatus(ctx)
	if err != nil {
		return fmt.Errorf("reading holiday status: %w", err)
	}

	counts := make(map[string]broker.HolidayStat, len(stats))
	for _, s := range stats {
		counts[s.Exchange] = s
	}

	rows := make([][]string, 0, len(broker.KnownExchanges()))
	var unseeded []string
	for _, ex := range broker.KnownExchanges() {
		if s, ok := counts[ex]; ok && s.Count > 0 {
			rows = append(rows, []string{ex, fmt.Sprintf("%d", s.Count), s.MinDate + " → " + s.MaxDate})
		} else {
			rows = append(rows, []string{ex, "0", "NOT SEEDED"})
			unseeded = append(unseeded, ex)
		}
	}
	render.TableWithOpts(os.Stdout, render.TableOpts{
		Headers: []string{"Exchange", "Holidays", "Date range"},
		Rows:    rows,
		Align:   []render.Alignment{render.AlignLeft, render.AlignRight, render.AlignLeft},
	})

	if len(unseeded) > 0 {
		fmt.Printf("\n⚠  Unseeded: %v — trading-day logic is WEEKEND-ONLY for these until seeded.\n", unseeded)
		fmt.Println("   Seed with:  duckdb data/mycase.db < holiday.sql   (see docs/18-runbook.md)")
		return fmt.Errorf("%d exchange(s) have no holiday data", len(unseeded))
	}
	fmt.Println("\n✓  All known exchanges seeded.")
	return nil
}

func runHolidaysList(ctx context.Context, c *cli.Command) error {
	store := broker.HolidayStore()
	if store == nil {
		return fmt.Errorf("cache DB is not open (data/mycase.db) — cannot read holidays")
	}
	exchange := c.String("exchange")
	dates := store.Holidays(exchange)
	if len(dates) == 0 {
		fmt.Printf("No holidays seeded for %s.\n", exchange)
		fmt.Println("Seed with:  duckdb data/mycase.db < holiday.sql   (see docs/18-runbook.md)")
		return fmt.Errorf("no holiday data for %s", exchange)
	}
	rows := make([][]string, 0, len(dates))
	for _, d := range dates {
		rows = append(rows, []string{d})
	}
	render.TableWithOpts(os.Stdout, render.TableOpts{
		Headers: []string{exchange + " holiday"},
		Rows:    rows,
		Align:   []render.Alignment{render.AlignLeft},
	})
	fmt.Printf("\n%d holidays for %s.\n", len(dates), exchange)
	return nil
}
