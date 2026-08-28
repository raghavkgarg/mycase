package tax

import (
	"fmt"
	"sort"
	"strconv"
	"time"
)

// RealizedGain is the result of matching a sell against one or more buy lots.
// A single sell can span multiple lots (e.g. selling 100 shares consumes two
// 50-share lots), producing one RealizedGain per lot consumed.
type RealizedGain struct {
	Ticker        string
	Quantity      float64   // shares matched from this lot
	Proceeds      float64   // sale proceeds attributable to these shares (net of nothing; fees tracked separately)
	CostBasis     float64   // cost basis of the matched shares
	Gain          float64   // Proceeds - CostBasis
	AcquiredAt    time.Time // lot acquisition date
	SoldAt        time.Time // sale date
	HoldingDays   int
	LongTerm      bool // true if held ≥ 365 days
	LotID         string
	TransactionID string // the sell transaction that realized this gain
}

// FIFOResult holds the outcome of replaying a transaction history.
type FIFOResult struct {
	// OpenLots are the remaining (unsold) lots after replaying all transactions,
	// grouped by ticker and ordered oldest-first within each ticker.
	OpenLots map[string][]Lot
	// RealizedGains are all gains/losses realized by sells, in chronological order.
	RealizedGains []RealizedGain
	// Warnings collects non-fatal issues (e.g. a sell with no matching lots).
	Warnings []string
}

// BuildLots replays a chronological transaction history using FIFO matching and
// returns the remaining open lots plus all realized gains. Transactions are
// sorted by trade date (stable) before replay, so callers may pass them in any
// order.
//
// FIFO semantics: each SELL consumes the oldest open lots first. If a SELL
// exceeds available shares (e.g. corporate action, short, or missing buy
// history), the excess is recorded as a warning and a synthetic zero-basis lot
// is NOT created — the unmatched proceeds are dropped from realized-gain
// accounting to avoid overstating gains.
func BuildLots(txns []Transaction) *FIFOResult {
	res := &FIFOResult{
		OpenLots: make(map[string][]Lot),
	}

	// Chronological, stable order. Ties broken by BUY-before-SELL so a same-day
	// buy is available to a same-day sell.
	sorted := make([]Transaction, len(txns))
	copy(sorted, txns)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].TradedAt.Equal(sorted[j].TradedAt) {
			return sorted[i].Type == TxnBuy && sorted[j].Type == TxnSell
		}
		return sorted[i].TradedAt.Before(sorted[j].TradedAt)
	})

	// Per-ticker sequence counter for stable lot IDs.
	seq := make(map[string]int)

	for _, t := range sorted {
		switch t.Type {
		case TxnBuy:
			seq[t.Ticker]++
			// Fees on a buy increase cost basis.
			costPerShare := t.Price
			if t.Quantity > 0 && t.Fees != 0 {
				costPerShare += t.Fees / t.Quantity
			}
			lot := Lot{
				ID:           lotID(t.Ticker, t.TradedAt, seq[t.Ticker]),
				Ticker:       t.Ticker,
				Quantity:     t.Quantity,
				CostPerShare: costPerShare,
				AcquiredAt:   t.TradedAt,
				Source:       "schwab_txn",
			}
			res.OpenLots[t.Ticker] = append(res.OpenLots[t.Ticker], lot)

		case TxnSell:
			res.matchSell(t)
		}
	}

	// Ensure open lots are oldest-first per ticker (they already are by
	// insertion order, but normalize defensively).
	for ticker, lots := range res.OpenLots {
		sort.SliceStable(lots, func(i, j int) bool {
			return lots[i].AcquiredAt.Before(lots[j].AcquiredAt)
		})
		res.OpenLots[ticker] = lots
	}

	return res
}

// matchSell consumes open lots oldest-first to satisfy a sell transaction,
// appending a RealizedGain per lot consumed.
func (res *FIFOResult) matchSell(t Transaction) {
	lots := res.OpenLots[t.Ticker]
	remaining := t.Quantity
	// Sell fees reduce proceeds; allocate proportionally across matched shares.
	feePerShare := 0.0
	if t.Quantity > 0 {
		feePerShare = t.Fees / t.Quantity
	}

	var kept []Lot
	i := 0
	for ; i < len(lots) && remaining > 0; i++ {
		lot := lots[i]
		matchQty := lot.Quantity
		if matchQty > remaining {
			matchQty = remaining
		}

		proceeds := matchQty * (t.Price - feePerShare)
		costBasis := matchQty * lot.CostPerShare
		holdingDays := -1
		longTerm := false
		if !lot.AcquiredAt.IsZero() {
			holdingDays = int(t.TradedAt.Sub(lot.AcquiredAt).Hours() / 24)
			longTerm = holdingDays >= longTermThresholdDays
		}

		res.RealizedGains = append(res.RealizedGains, RealizedGain{
			Ticker:        t.Ticker,
			Quantity:      matchQty,
			Proceeds:      proceeds,
			CostBasis:     costBasis,
			Gain:          proceeds - costBasis,
			AcquiredAt:    lot.AcquiredAt,
			SoldAt:        t.TradedAt,
			HoldingDays:   holdingDays,
			LongTerm:      longTerm,
			LotID:         lot.ID,
			TransactionID: t.ID,
		})

		lot.Quantity -= matchQty
		remaining -= matchQty
		if lot.Quantity > epsilon {
			kept = append(kept, lot) // partially consumed lot survives
		}
	}
	// Append any lots not touched.
	if i < len(lots) {
		kept = append(kept, lots[i:]...)
	}

	if remaining > epsilon {
		res.Warnings = append(res.Warnings, unmatchedSellWarning(t.Ticker, remaining))
	}

	if len(kept) == 0 {
		delete(res.OpenLots, t.Ticker)
	} else {
		res.OpenLots[t.Ticker] = kept
	}
}

const epsilon = 1e-9

func lotID(ticker string, acquired time.Time, seq int) string {
	return ticker + "_" + acquired.Format("20060102") + "_" + strconv.Itoa(seq)
}

func unmatchedSellWarning(ticker string, qty float64) string {
	return fmt.Sprintf("sell of %s exceeded available lots by %.4f shares (missing buy history?) — excess ignored", ticker, qty)
}
