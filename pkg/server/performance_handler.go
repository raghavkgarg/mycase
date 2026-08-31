package server

import (
	"net/http"
	"time"

	"log/slog"

	"github.com/raghavkgarg/mycase/pkg/attribution"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
)

// ── /api/portfolio/{name}/performance ─────────────────────────────────────────
//
// Returns live performance-attribution data for the dashboard performance tab:
// a daily NAV series (portfolio + benchmark) for the equity curve, vs-benchmark
// metrics (alpha, beta, information ratio, tracking error), and a selection /
// rebalancing return decomposition. Prices are sourced through the Server's
// PriceFetcher (US → Schwab, else Yahoo). Mirrors tax_handler.go's shape:
// nil-dependency guard + an `available` flag so the frontend degrades cleanly.

type navPointJSON struct {
	Date      string  `json:"date"`
	Portfolio float64 `json:"portfolio"`
	Benchmark float64 `json:"benchmark"`
}

type attributionJSON struct {
	TradingDays      int     `json:"trading_days"`
	From             string  `json:"from"`
	To               string  `json:"to"`
	InitialCapital   float64 `json:"initial_capital"`
	FinalValue       float64 `json:"final_value"`
	BenchmarkFinal   float64 `json:"benchmark_final"`
	TotalReturn      float64 `json:"total_return"`
	BenchmarkReturn  float64 `json:"benchmark_return"`
	CAGR             float64 `json:"cagr"`
	BenchmarkCAGR    float64 `json:"benchmark_cagr"`
	Alpha            float64 `json:"alpha"`
	Beta             float64 `json:"beta"`
	InformationRatio float64 `json:"information_ratio"`
	TrackingError    float64 `json:"tracking_error"`
	MaxDrawdown      float64 `json:"max_drawdown"`
	Sharpe           float64 `json:"sharpe"`
}

type decompositionJSON struct {
	ActiveReturn    float64 `json:"active_return"`
	PortfolioReturn float64 `json:"portfolio_return"`
	BenchmarkReturn float64 `json:"benchmark_return"`
	BuyHoldReturn   float64 `json:"buy_hold_return"`
	Selection       float64 `json:"selection"`
	Rebalancing     float64 `json:"rebalancing"`
	Rebalances      int     `json:"rebalances"`
}

func (s *Server) handlePerformance(w http.ResponseWriter, r *http.Request) {
	if s.fetcher == nil {
		writeJSON(w, map[string]any{
			"available": false,
			"message":   "Performance data unavailable — price fetcher not configured.",
		})
		return
	}

	name := r.PathValue("name")
	weights, tickers, err := csvloader.LoadBasketCSV("data/" + name + ".csv")
	if err != nil {
		writeError(w, http.StatusNotFound, "portfolio not found: "+err.Error())
		return
	}

	var holdings []attribution.Holding
	for _, tk := range tickers {
		if wt := weights[tk]; wt > 0 {
			holdings = append(holdings, attribution.Holding{Ticker: tk, Weight: wt})
		}
	}
	if len(holdings) == 0 {
		writeJSON(w, map[string]any{
			"available": false,
			"message":   "No holdings with positive weight in this portfolio.",
		})
		return
	}

	ctx := r.Context()

	// Window: last year, keyed in US market time.
	nyLoc, err := time.LoadLocation("America/New_York")
	if err != nil {
		nyLoc = time.UTC
	}
	to := time.Now().In(nyLoc)
	from := to.AddDate(-1, 0, 0)
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		if t, perr := time.ParseInLocation("2006-01-02", sinceStr, nyLoc); perr == nil {
			from = t
		}
	}

	portfolioName := csvloader.GetUniverseName("data/" + name + ".csv")
	tracker := attribution.NewTracker(s.fetcher, slog.Default())
	cfg := attribution.Config{
		From:     from,
		To:       to,
		Location: nyLoc,
	}

	points, err := tracker.BuildNAVSeries(ctx, holdings, cfg)
	if err != nil {
		writeJSON(w, map[string]any{
			"available": false,
			"message":   "Could not build NAV series: " + err.Error(),
		})
		return
	}

	res := attribution.Attribution(points, cfg.RiskFree)

	// Persist best-effort (same as the CLI path) so the series is retained.
	if s.cache != nil {
		if store := attribution.NewStore(s.cache.Conn()); store != nil {
			if perr := store.InsertNAVPoints(ctx, portfolioName, points); perr != nil {
				slog.WarnContext(ctx, "performance_handler.nav_persist_failed", "error", perr.Error())
			}
		}
	}

	// Decomposition (best-effort — needs rebalance history from the cache).
	var decomp *decompositionJSON
	var history []attribution.RebalanceEvent
	if s.cache != nil {
		if h, herr := attribution.LoadRebalanceHistory(ctx, s.cache, portfolioName, cfg.From); herr == nil {
			history = h
		}
	}
	if d, derr := tracker.Decompose(ctx, attribution.DecomposeInput{Holdings: holdings, History: history}, cfg); derr == nil {
		decomp = &decompositionJSON{
			ActiveReturn:    d.ActiveReturn,
			PortfolioReturn: d.PortfolioReturn,
			BenchmarkReturn: d.BenchmarkReturn,
			BuyHoldReturn:   d.BuyHoldReturn,
			Selection:       d.Selection,
			Rebalancing:     d.Rebalancing,
			Rebalances:      d.Rebalances,
		}
	}

	navJSON := make([]navPointJSON, 0, len(points))
	for _, p := range points {
		navJSON = append(navJSON, navPointJSON{
			Date:      p.Date.Format("2006-01-02"),
			Portfolio: p.PortfolioValue,
			Benchmark: p.BenchmarkValue,
		})
	}

	writeJSON(w, map[string]any{
		"available":  true,
		"benchmark":  attribution.DefaultBenchmark,
		"nav_series": navJSON,
		"metrics": attributionJSON{
			TradingDays:      res.TradingDays,
			From:             res.From.Format("2006-01-02"),
			To:               res.To.Format("2006-01-02"),
			InitialCapital:   res.InitialCapital,
			FinalValue:       res.FinalValue,
			BenchmarkFinal:   res.BenchmarkFinal,
			TotalReturn:      res.TotalReturn,
			BenchmarkReturn:  res.BenchmarkReturn,
			CAGR:             res.CAGR,
			BenchmarkCAGR:    res.BenchmarkCAGR,
			Alpha:            res.Alpha,
			Beta:             res.Beta,
			InformationRatio: res.InformationRatio,
			TrackingError:    res.TrackingError,
			MaxDrawdown:      res.MaxDrawdown,
			Sharpe:           res.Sharpe,
		},
		"decomposition": decomp,
	})
}
