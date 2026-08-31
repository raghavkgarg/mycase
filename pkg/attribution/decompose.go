package attribution

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// RebalanceEvent is one point in a portfolio's target-weight history: the target
// basket the strategy selected at a given time. It is reconstructed from the
// pipeline_runs + proposals(stage="optimized") tables (see docs/refactor.md
// Phase 5b) — one completed pipeline run == one rebalance.
type RebalanceEvent struct {
	When    time.Time
	RunID   string
	Weights map[string]float64 // ticker → target weight (need not sum to 1; renormalized on use)
}

// Decomposition splits a portfolio's excess return over the benchmark into the
// sources the roadmap cares about: did picking beat the index (selection), did
// re-selecting each quarter beat just holding the first basket (rebalancing),
// and what did tax-loss harvesting add (tax).
//
// All figures are total-return fractions over the analysed window, expressed so
// they attribute additively toward the active (vs-benchmark) return:
//
//	ActiveReturn ≈ Selection + Rebalancing   (+ Tax, reported separately)
//
// where:
//   - ActiveReturn   = actual portfolio total return − benchmark total return.
//   - Selection      = buy-and-hold-first-basket return − benchmark return.
//     (What the stock picks earned versus the index, with no re-selection.)
//   - Rebalancing    = actual portfolio return − buy-and-hold-first-basket return.
//     (What quarterly re-selection added or cost on top of the initial basket.)
//   - Tax            = realized tax saving from TLH as a fraction of initial
//     capital. Reported, not NAV-derived — it is a cash effect outside the
//     price series, so it is surfaced alongside rather than folded into the
//     price-return identity above.
type Decomposition struct {
	From           time.Time
	To             time.Time
	InitialCapital float64
	TradingDays    int

	ActiveReturn    float64 // portfolio − benchmark, total-return fraction
	PortfolioReturn float64
	BenchmarkReturn float64
	BuyHoldReturn   float64 // first basket held untouched over the window

	Selection   float64 // BuyHoldReturn − BenchmarkReturn
	Rebalancing float64 // PortfolioReturn − BuyHoldReturn
	Tax         float64 // realized TLH saving / InitialCapital (reported)

	Rebalances int // number of rebalance events inside the window
}

// DecomposeInput carries the inputs for a decomposition. TaxSaving is optional
// (pass 0 if unknown); it is expressed in the same currency as InitialCapital.
type DecomposeInput struct {
	// Holdings is the current/target basket, used to value the *actual* rebalanced
	// portfolio (same series the Tracker builds for --vs-benchmark).
	Holdings []Holding
	// History is the chronological target-weight history (oldest first is not
	// required; it is sorted internally). The earliest event on/after Config.From
	// defines the buy-and-hold-first-basket counterfactual. If empty, the current
	// Holdings are used as the first basket (no rebalancing effect measurable).
	History []RebalanceEvent
	// TaxSaving is the realized TLH benefit over the window, in currency units.
	TaxSaving float64
}

// Decompose builds the actual, benchmark, and buy-and-hold-counterfactual NAV
// series over cfg's window and returns the source attribution. It reuses
// BuildNAVSeries for the actual + benchmark path and builds the counterfactual
// from the earliest in-window rebalance basket held untouched.
func (t *Tracker) Decompose(ctx context.Context, in DecomposeInput, cfg Config) (Decomposition, error) {
	cfg = cfg.withDefaults()

	// Actual rebalanced portfolio + benchmark (the 5a series).
	actual, err := t.BuildNAVSeries(ctx, in.Holdings, cfg)
	if err != nil {
		return Decomposition{}, fmt.Errorf("actual NAV: %w", err)
	}

	// Determine the first basket for the buy-and-hold counterfactual.
	firstBasket := firstInWindowBasket(in.History, cfg.From, in.Holdings)

	// Build the counterfactual NAV: the first basket held untouched, valued over
	// the *same* trading days as the actual series so the two are comparable.
	buyHold, err := t.BuildNAVSeries(ctx, firstBasket, cfg)
	if err != nil {
		return Decomposition{}, fmt.Errorf("buy-and-hold NAV: %w", err)
	}

	res := Attribution(actual, cfg.RiskFree)
	bhRes := Attribution(buyHold, cfg.RiskFree)

	d := Decomposition{
		From:            res.From,
		To:              res.To,
		InitialCapital:  res.InitialCapital,
		TradingDays:     res.TradingDays,
		PortfolioReturn: res.TotalReturn,
		BenchmarkReturn: res.BenchmarkReturn,
		BuyHoldReturn:   bhRes.TotalReturn,
		Rebalances:      countInWindow(in.History, cfg.From, cfg.To),
	}
	d.ActiveReturn = d.PortfolioReturn - d.BenchmarkReturn
	d.Selection = d.BuyHoldReturn - d.BenchmarkReturn
	d.Rebalancing = d.PortfolioReturn - d.BuyHoldReturn
	if res.InitialCapital > 0 {
		d.Tax = in.TaxSaving / res.InitialCapital
	}

	t.log.InfoContext(ctx, "decompose.built",
		"days", d.TradingDays, "rebalances", d.Rebalances,
		"active", d.ActiveReturn, "selection", d.Selection,
		"rebalancing", d.Rebalancing, "tax", d.Tax)

	return d, nil
}

// firstInWindowBasket returns the target basket to hold for the counterfactual:
// the weights of the earliest rebalance event on/after `from`. If none qualifies
// (or history is empty), it falls back to the earliest event overall, and
// failing that, to fallback (the current holdings).
func firstInWindowBasket(history []RebalanceEvent, from time.Time, fallback []Holding) []Holding {
	if len(history) == 0 {
		return fallback
	}
	sorted := append([]RebalanceEvent(nil), history...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].When.Before(sorted[j].When) })

	chosen := -1
	for i, ev := range sorted {
		if !ev.When.Before(from) { // When >= from
			chosen = i
			break
		}
	}
	if chosen == -1 {
		chosen = 0 // all events predate the window; use the earliest known basket
	}
	return weightsToHoldings(sorted[chosen].Weights)
}

func weightsToHoldings(w map[string]float64) []Holding {
	out := make([]Holding, 0, len(w))
	for tk, wt := range w {
		if wt > 0 {
			out = append(out, Holding{Ticker: tk, Weight: wt})
		}
	}
	// Deterministic order (BuildNAVSeries renormalizes, so order is cosmetic).
	sort.Slice(out, func(i, j int) bool { return out[i].Ticker < out[j].Ticker })
	return out
}

func countInWindow(history []RebalanceEvent, from, to time.Time) int {
	n := 0
	for _, ev := range history {
		if !ev.When.Before(from) && !ev.When.After(to) {
			n++
		}
	}
	return n
}
