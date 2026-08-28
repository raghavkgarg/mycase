// Package tax implements FIFO lot tracking and tax-loss harvesting (TLH) for
// the US equity portfolio. Lots track cost basis and acquisition date per
// purchase; the FIFO engine matches sells against oldest lots first to compute
// realized gains and holding-period classification (short vs long term). The
// TLH engine identifies loss-making positions worth harvesting, respecting the
// IRS 30-day wash-sale rule.
package tax

import "time"

// Lot represents a single purchase lot of a security — a quantity of shares
// acquired on a specific date at a specific per-share cost. FIFO matching
// consumes lots oldest-first when shares are sold.
type Lot struct {
	ID           string    // stable identifier (ticker + acquired + seq)
	Ticker       string    // full ticker with market prefix (e.g. "US:AAPL")
	Quantity     float64   // shares remaining in this lot (fractional allowed)
	CostPerShare float64   // acquisition cost per share, USD
	AcquiredAt   time.Time // acquisition date (for holding-period classification)
	Source       string    // "schwab_txn", "schwab_position", "manual", "csv"
}

// CostBasis returns the total remaining cost basis of the lot.
func (l Lot) CostBasis() float64 { return l.Quantity * l.CostPerShare }

// HoldingDays returns the number of days the lot has been held as of asOf.
func (l Lot) HoldingDays(asOf time.Time) int {
	if l.AcquiredAt.IsZero() {
		return -1
	}
	return int(asOf.Sub(l.AcquiredAt).Hours() / 24)
}

// IsLongTerm reports whether the lot qualifies for long-term capital gains
// treatment as of asOf (held ≥ 365 days).
func (l Lot) IsLongTerm(asOf time.Time) bool {
	d := l.HoldingDays(asOf)
	return d >= 0 && d >= longTermThresholdDays
}

// TxnType enumerates the kinds of transactions the FIFO engine understands.
type TxnType string

const (
	TxnBuy  TxnType = "BUY"
	TxnSell TxnType = "SELL"
)

// Transaction is a normalized buy/sell record used to reconstruct lots.
// It is broker-agnostic; the Schwab importer maps raw API records into these.
type Transaction struct {
	ID       string    // broker transaction/activity ID (idempotency key)
	Ticker   string    // full ticker with market prefix
	Type     TxnType   // BUY or SELL
	Quantity float64   // shares (always positive)
	Price    float64   // per-share price, USD
	Fees     float64   // total fees for the transaction, USD
	TradedAt time.Time // trade/settlement date
}

const (
	// longTermThresholdDays is the IRS holding period for long-term capital
	// gains: a security must be held for more than one year. The convention
	// used across the codebase (costs.USSTCGThresholdDays) treats ≥ 365 days
	// as long-term.
	longTermThresholdDays = 365

	// washSaleDays is the IRS wash-sale window: a loss is disallowed if a
	// substantially identical security is bought within 30 days before or
	// after the sale.
	washSaleDays = 30
)
