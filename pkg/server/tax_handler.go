package server

import (
	"net/http"
	"sort"
	"time"

	"github.com/raghavkgarg/mycase/pkg/tax"
)

// ── /api/portfolio/{name}/tax ─────────────────────────────────────────────────
//
// Returns tax-lot state for the dashboard tax tab: open lots, YTD and all-time
// realized gain/loss summaries, and current tax-loss-harvesting candidates with
// wash-sale flags. Data comes from the DuckDB tables populated by
// `mycase tax import`.

type taxLotRow struct {
	Ticker       string  `json:"ticker"`
	Acquired     string  `json:"acquired"`
	Quantity     float64 `json:"quantity"`
	CostPerShare float64 `json:"cost_per_share"`
	CostBasis    float64 `json:"cost_basis"`
	MarketValue  float64 `json:"market_value"`
	Unrealized   float64 `json:"unrealized"`
	LongTerm     bool    `json:"long_term"`
	HoldingDays  int     `json:"holding_days"`
}

type harvestRow struct {
	Ticker         string  `json:"ticker"`
	Quantity       float64 `json:"quantity"`
	UnrealizedLoss float64 `json:"unrealized_loss"`
	EstTaxSaving   float64 `json:"est_tax_saving"`
	LongTerm       bool    `json:"long_term"`
	WashSaleRisk   bool    `json:"wash_sale_risk"`
	Substitute     string  `json:"substitute"`
	Note           string  `json:"note"`
}

type realizedSummaryJSON struct {
	ShortTermGain float64 `json:"short_term_gain"`
	ShortTermLoss float64 `json:"short_term_loss"`
	NetShortTerm  float64 `json:"net_short_term"`
	LongTermGain  float64 `json:"long_term_gain"`
	LongTermLoss  float64 `json:"long_term_loss"`
	NetLongTerm   float64 `json:"net_long_term"`
	NetTotal      float64 `json:"net_total"`
	Count         int     `json:"count"`
}

type washSaleRow struct {
	Ticker    string `json:"ticker"`
	SellDate  string `json:"sell_date"`
	BuyDate   string `json:"buy_date"`
	DaysApart int    `json:"days_apart"`
	Note      string `json:"note"`
}

func (s *Server) handleTax(w http.ResponseWriter, r *http.Request) {
	if s.cache == nil {
		writeJSON(w, map[string]any{
			"available": false,
			"message":   "Tax data unavailable — DuckDB cache not initialized.",
		})
		return
	}

	ctx := r.Context()

	openLots, err := s.cache.GetOpenLots(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading lots: "+err.Error())
		return
	}
	if len(openLots) == 0 {
		writeJSON(w, map[string]any{
			"available": false,
			"message":   "No tax lots found. Run 'mycase tax import --broker schwab' to bootstrap.",
		})
		return
	}

	// Current prices for held tickers (best-effort via broker quotes).
	tickers := make([]string, 0, len(openLots))
	for t := range openLots {
		tickers = append(tickers, t)
	}
	prices, _ := s.broker.GetQuotes(tickers)

	asOf := time.Now()

	// Build lot rows with unrealized P&L.
	var lotRows []taxLotRow
	for _, t := range tickers {
		price := prices[t]
		for _, lot := range openLots[t] {
			mv := lot.Quantity * price
			basis := lot.CostBasis()
			lotRows = append(lotRows, taxLotRow{
				Ticker:       lot.Ticker,
				Acquired:     lot.AcquiredAt.Format("2006-01-02"),
				Quantity:     lot.Quantity,
				CostPerShare: lot.CostPerShare,
				CostBasis:    basis,
				MarketValue:  mv,
				Unrealized:   mv - basis,
				LongTerm:     lot.IsLongTerm(asOf),
				HoldingDays:  lot.HoldingDays(asOf),
			})
		}
	}
	sort.SliceStable(lotRows, func(i, j int) bool {
		return lotRows[i].Ticker < lotRows[j].Ticker
	})

	// Realized summaries.
	realized, err := s.cache.GetRealizedGains(ctx, time.Time{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading realized gains: "+err.Error())
		return
	}
	yearStart := time.Date(asOf.Year(), 1, 1, 0, 0, 0, 0, time.Local)
	ytd := toRealizedJSON(tax.SummarizeRealized(realized, yearStart))
	allTime := toRealizedJSON(tax.SummarizeRealized(realized, time.Time{}))

	// Harvest candidates.
	recentBuys, _ := s.cache.LatestBuyDates(ctx)
	params := tax.DefaultHarvestParams()
	params.AsOf = asOf
	params.RecentBuys = recentBuys
	candidates := tax.FindHarvestCandidates(openLots, prices, tickers, params)

	harvestRows := make([]harvestRow, 0, len(candidates))
	for _, c := range candidates {
		harvestRows = append(harvestRows, harvestRow{
			Ticker:         c.Ticker,
			Quantity:       c.Quantity,
			UnrealizedLoss: c.UnrealizedLoss,
			EstTaxSaving:   c.EstTaxSaving,
			LongTerm:       c.LongTerm,
			WashSaleRisk:   c.WashSaleRisk,
			Substitute:     c.Substitute,
			Note:           c.Note,
		})
	}

	// Wash-sale calendar: recent buys still inside the 30-day window.
	var washRows []washSaleRow
	for ticker, bd := range recentBuys {
		days := int(asOf.Sub(bd).Hours() / 24)
		if days >= 0 && days <= 30 {
			washRows = append(washRows, washSaleRow{
				Ticker:    ticker,
				BuyDate:   bd.Format("2006-01-02"),
				DaysApart: days,
				Note:      "selling at a loss within 30 days of this buy would disallow the loss",
			})
		}
	}
	sort.SliceStable(washRows, func(i, j int) bool {
		return washRows[i].DaysApart < washRows[j].DaysApart
	})

	var totalSaving float64
	for _, h := range harvestRows {
		totalSaving += h.EstTaxSaving
	}

	writeJSON(w, map[string]any{
		"available":          true,
		"lots":               lotRows,
		"realized_ytd":       ytd,
		"realized_all_time":  allTime,
		"harvest_candidates": harvestRows,
		"harvest_total_est":  totalSaving,
		"wash_sale_calendar": washRows,
		"as_of":              asOf.Format("2006-01-02"),
	})
}

func toRealizedJSON(s tax.RealizedSummary) realizedSummaryJSON {
	return realizedSummaryJSON{
		ShortTermGain: s.ShortTermGain,
		ShortTermLoss: s.ShortTermLoss,
		NetShortTerm:  s.NetShortTerm,
		LongTermGain:  s.LongTermGain,
		LongTermLoss:  s.LongTermLoss,
		NetLongTerm:   s.NetLongTerm,
		NetTotal:      s.NetTotal,
		Count:         s.Count,
	}
}
