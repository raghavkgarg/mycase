package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/broker/schwab"
	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/render"
	"github.com/raghavkgarg/mycase/pkg/tax"
)

// TaxCommand groups tax-lot tracking and tax-loss-harvesting subcommands.
var TaxCommand = &cli.Command{
	Name:  "tax",
	Usage: "FIFO lot tracking and tax-loss harvesting (US)",
	Commands: []*cli.Command{
		taxImportCommand,
		taxStatusCommand,
		taxHarvestCommand,
	},
}

var taxImportCommand = &cli.Command{
	Name:  "import",
	Usage: "Import broker transaction history and rebuild FIFO lots",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "broker", Value: "schwab", Usage: "Broker to import from (schwab)"},
		&cli.IntFlag{Name: "years", Value: 3, Usage: "Number of years of history to import"},
	},
	Action: runTaxImport,
}

var taxStatusCommand = &cli.Command{
	Name:   "status",
	Usage:  "Show open lots and YTD realized gains/losses",
	Action: runTaxStatus,
}

var taxHarvestCommand = &cli.Command{
	Name:  "harvest",
	Usage: "Identify tax-loss harvesting candidates",
	Flags: []cli.Flag{
		&cli.BoolFlag{Name: "live", Usage: "Use live broker quotes (default: mock)"},
		&cli.FloatFlag{Name: "min-loss", Value: 50.0, Usage: "Minimum unrealized loss (USD) to consider"},
	},
	Action: runTaxHarvest,
}

func runTaxImport(ctx context.Context, c *cli.Command) error {
	brokerName := c.String("broker")
	if brokerName != "schwab" {
		return fmt.Errorf("tax import currently supports only --broker schwab (got %q)", brokerName)
	}

	db := cache.GetDB()
	if db == nil {
		return fmt.Errorf("DuckDB cache not available — cannot store lots")
	}

	client := newSchwabClient()
	if client == nil {
		return fmt.Errorf("schwab credentials not configured — run 'mycase auth --broker schwab' first")
	}

	hash, err := client.FetchAccountHash(ctx)
	if err != nil {
		return fmt.Errorf("fetching account hash: %w", err)
	}

	years := c.Int("years")
	to := time.Now()
	from := to.AddDate(-years, 0, 0)

	fmt.Printf("Importing Schwab TRADE transactions from %s to %s...\n",
		from.Format("2006-01-02"), to.Format("2006-01-02"))

	// Schwab caps each request at ~1 year; chunk the window.
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
	fmt.Printf("Fetched %d raw records → %d normalized buy/sell transactions.\n", len(raw), len(txns))
	if len(txns) == 0 {
		fmt.Println("No trade transactions to import.")
		return nil
	}

	if err := db.InsertTransactions(ctx, txns); err != nil {
		return fmt.Errorf("storing transactions: %w", err)
	}

	// Rebuild FIFO lots and realized gains from the full stored history.
	return rebuildLots(ctx, db)
}

// rebuildLots replays all stored transactions via FIFO and persists the
// resulting open lots and realized gains.
func rebuildLots(ctx context.Context, db *cache.Cache) error {
	allTxns, err := db.GetTransactions(ctx)
	if err != nil {
		return fmt.Errorf("loading transactions: %w", err)
	}

	result := tax.BuildLots(allTxns)

	if err := db.ReplaceOpenLots(ctx, result.OpenLots); err != nil {
		return fmt.Errorf("storing lots: %w", err)
	}
	if err := db.ReplaceRealizedGains(ctx, result.RealizedGains); err != nil {
		return fmt.Errorf("storing realized gains: %w", err)
	}

	lotCount := 0
	for _, lots := range result.OpenLots {
		lotCount += len(lots)
	}
	fmt.Printf("Rebuilt %d open lots across %d tickers, %d realized-gain records.\n",
		lotCount, len(result.OpenLots), len(result.RealizedGains))

	for _, w := range result.Warnings {
		fmt.Printf("  ⚠️  %s\n", w)
	}
	return nil
}

func runTaxStatus(ctx context.Context, c *cli.Command) error {
	db := cache.GetDB()
	if db == nil {
		return fmt.Errorf("DuckDB cache not available")
	}

	openLots, err := db.GetOpenLots(ctx)
	if err != nil {
		return fmt.Errorf("loading lots: %w", err)
	}

	realized, err := db.GetRealizedGains(ctx, time.Time{})
	if err != nil {
		return fmt.Errorf("loading realized gains: %w", err)
	}

	out := os.Stdout
	render.Banner(out, "Tax Lot Status")

	if len(openLots) == 0 {
		fmt.Println("No open lots. Run 'mycase tax import --broker schwab' first.")
	} else {
		tickers := make([]string, 0, len(openLots))
		for t := range openLots {
			tickers = append(tickers, t)
		}
		sort.Strings(tickers)

		render.Section(out, fmt.Sprintf("Open Lots (%d tickers)", len(tickers)))
		asOf := time.Now()
		rows := make([][]string, 0, len(tickers))
		for _, t := range tickers {
			for _, lot := range openLots[t] {
				term := "short"
				if lot.IsLongTerm(asOf) {
					term = "long"
				}
				rows = append(rows, []string{
					lot.Ticker,
					lot.AcquiredAt.Format("2006-01-02"),
					fmt.Sprintf("%.2f", lot.Quantity),
					render.Currency(lot.CostPerShare, "$"),
					render.Currency(lot.CostBasis(), "$"),
					term,
				})
			}
		}
		render.TableWithOpts(out, render.TableOpts{
			Headers: []string{"Ticker", "Acquired", "Qty", "Cost/Sh", "Basis", "Term"},
			Rows:    rows,
			Align:   []render.Alignment{render.AlignLeft, render.AlignLeft, render.AlignRight, render.AlignRight, render.AlignRight, render.AlignLeft},
		})
	}

	// YTD summary.
	yearStart := time.Date(time.Now().Year(), 1, 1, 0, 0, 0, 0, time.Local)
	ytd := tax.SummarizeRealized(realized, yearStart)
	allTime := tax.SummarizeRealized(realized, time.Time{})

	render.Section(out, fmt.Sprintf("Realized Gains/Losses (YTD %d)", time.Now().Year()))
	printSummary(ytd)
	render.Section(out, "Realized Gains/Losses (all time)")
	printSummary(allTime)

	return nil
}

func printSummary(s tax.RealizedSummary) {
	render.KV(os.Stdout, []render.KVPair{
		{Key: "Short-term", Value: fmt.Sprintf("gains %s, losses %s, net %s", render.Currency(s.ShortTermGain, "$"), render.Currency(s.ShortTermLoss, "$"), render.Currency(s.NetShortTerm, "$"))},
		{Key: "Long-term", Value: fmt.Sprintf("gains %s, losses %s, net %s", render.Currency(s.LongTermGain, "$"), render.Currency(s.LongTermLoss, "$"), render.Currency(s.NetLongTerm, "$"))},
		{Key: "Net total", Value: fmt.Sprintf("%s  (%d realized lots)", render.Currency(s.NetTotal, "$"), s.Count)},
	})
}

func runTaxHarvest(ctx context.Context, c *cli.Command) error {
	db := cache.GetDB()
	if db == nil {
		return fmt.Errorf("DuckDB cache not available")
	}

	openLots, err := db.GetOpenLots(ctx)
	if err != nil {
		return fmt.Errorf("loading lots: %w", err)
	}
	if len(openLots) == 0 {
		fmt.Println("No open lots. Run 'mycase tax import --broker schwab' first.")
		return nil
	}

	// Fetch current prices for held tickers via the data router (Schwab/Yahoo).
	tickers := make([]string, 0, len(openLots))
	for t := range openLots {
		tickers = append(tickers, t)
	}
	router := newDataRouter()
	prices, err := router.FetchQuotes(ctx, tickers)
	if err != nil {
		return fmt.Errorf("fetching quotes: %w", err)
	}

	recentBuys, err := db.LatestBuyDates(ctx)
	if err != nil {
		return fmt.Errorf("loading buy dates: %w", err)
	}

	params := tax.DefaultHarvestParams()
	params.MinLoss = c.Float("min-loss")
	params.RecentBuys = recentBuys

	candidates := tax.FindHarvestCandidates(openLots, prices, tickers, params)

	out := os.Stdout
	render.Banner(out, "Tax-Loss Harvesting Candidates")
	if len(candidates) == 0 {
		fmt.Println("No harvestable losses above the minimum threshold.")
		return nil
	}

	var totalSaving float64
	rows := make([][]string, 0, len(candidates))
	for _, h := range candidates {
		term := "ST"
		if h.LongTerm {
			term = "LT"
		}
		wash := ""
		if h.WashSaleRisk {
			wash = "⚠️ RISK"
		}
		rows = append(rows, []string{
			h.Ticker,
			fmt.Sprintf("%.2f", h.Quantity),
			render.Currency(h.UnrealizedLoss, "$"),
			render.Currency(h.EstTaxSaving, "$"),
			term,
			wash,
		})
		totalSaving += h.EstTaxSaving
	}
	render.TableWithOpts(out, render.TableOpts{
		Headers: []string{"Ticker", "Qty", "Loss", "Tax Saving", "Term", "Wash?"},
		Rows:    rows,
		Align:   []render.Alignment{render.AlignLeft, render.AlignRight, render.AlignRight, render.AlignRight, render.AlignLeft, render.AlignLeft},
	})
	fmt.Printf("\nTotal estimated tax saving if all harvested: %s\n", render.Currency(totalSaving, "$"))
	fmt.Println("\nNote: verify wash-sale windows before selling. Losses on positions")
	fmt.Println("bought within 30 days (flagged ⚠️) may be disallowed.")

	return nil
}
