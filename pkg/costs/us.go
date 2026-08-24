package costs

import (
	"fmt"
	"time"
)

// US equity transaction cost constants.
// Schwab charges $0 commission for equity trades.
// SEC and TAF fees apply only to sell orders.
const (
	USCommission       = 0.0             // $0 commission (Schwab, Fidelity, etc.)
	SECFeeRate         = 0.000008        // ~$8.00 per $1M of sell proceeds (as of 2024)
	TAFPerShare        = 0.000166        // $0.000166 per share sold
	TAFMaxPerShare     = 0.01            // max $0.01 per share for TAF
	USSTCGThresholdDays = 365            // holding < 1 year = short-term
)

// US capital gains tax rates (2024 federal; state taxes vary).
const (
	USSTCGRate = 0.37   // worst-case ordinary income rate (37% top bracket)
	USLTCGRate = 0.20   // 20% for highest bracket; 15% for most taxpayers
	USWashSaleDays = 30 // cannot buy substantially identical within 30 days
)

// USCostModel handles US equity delivery transaction costs.
var DefaultUS = USCostModel{}

// USCostModel holds configurable US cost parameters.
type USCostModel struct {
	Commission float64 // per-order commission in $ (default 0)
}

// USCostBreakdown holds the per-component breakdown for a US equity order.
type USCostBreakdown struct {
	Commission float64
	SECFee     float64 // sell only
	TAF        float64 // sell only
	Total      float64
	TradeValue float64 // qty × price in USD
	CostRatio  float64 // Total / TradeValue
}

// Calculate returns the cost breakdown for a US equity order.
// txType must be "BUY" or "SELL". qty × price = trade value in USD.
func (m USCostModel) Calculate(txType string, qty int, price float64) USCostBreakdown {
	tradeValue := float64(qty) * price
	if tradeValue <= 0 {
		return USCostBreakdown{}
	}

	bd := USCostBreakdown{
		TradeValue: tradeValue,
		Commission: m.Commission,
	}

	isSell := len(txType) > 0 && (txType[0] == 'S' || txType[0] == 's')
	if isSell {
		bd.SECFee = SECFeeRate * tradeValue
		taf := TAFPerShare * float64(qty)
		maxTAF := TAFMaxPerShare * float64(qty)
		if taf > maxTAF {
			taf = maxTAF
		}
		bd.TAF = taf
	}

	bd.Total = bd.Commission + bd.SECFee + bd.TAF
	if tradeValue > 0 {
		bd.CostRatio = bd.Total / tradeValue
	}
	return bd
}

// USTaxClass classifies the capital gains treatment for a US sell order.
type USTaxClass int

const (
	USTaxUnknown   USTaxClass = iota // purchase date unavailable
	USTaxShortTerm                   // held < 1 year — taxed as ordinary income
	USTaxLongTerm                    // held ≥ 1 year — preferential rate (15% or 20%)
)

func (c USTaxClass) String() string {
	switch c {
	case USTaxShortTerm:
		return "Short-Term"
	case USTaxLongTerm:
		return "Long-Term"
	default:
		return "Unknown"
	}
}

// USTaxWarning describes the tax treatment for a US sell order.
type USTaxWarning struct {
	Ticker          string
	Class           USTaxClass
	HoldingDays     int
	EstimatedGain   float64 // (sellPrice - costBasis) × qty
	EstimatedTax    float64 // estimated federal tax
	WashSaleRisk    bool    // true if recently bought within 30 days
	Note            string
}

// ClassifyUSSell returns a USTaxWarning for a US SELL order.
// The wash sale rule: if you sell at a loss and buy the same (or substantially
// identical) security within 30 days before or after, the loss is disallowed.
func ClassifyUSSell(ticker string, qty int, sellPrice, costBasis float64, purchaseDate time.Time, recentBuyWithin30Days bool) USTaxWarning {
	w := USTaxWarning{
		Ticker:       ticker,
		HoldingDays:  -1,
		WashSaleRisk: recentBuyWithin30Days,
	}

	if purchaseDate.IsZero() {
		w.Class = USTaxUnknown
		w.Note = fmt.Sprintf("%s: purchase date unavailable — verify short-term vs long-term manually", ticker)
		if costBasis > 0 {
			w.EstimatedGain = (sellPrice - costBasis) * float64(qty)
		}
		return w
	}

	holdingDays := int(time.Since(purchaseDate).Hours() / 24)
	w.HoldingDays = holdingDays

	if holdingDays < USSTCGThresholdDays {
		w.Class = USTaxShortTerm
	} else {
		w.Class = USTaxLongTerm
	}

	if costBasis > 0 {
		w.EstimatedGain = (sellPrice - costBasis) * float64(qty)
	}

	switch w.Class {
	case USTaxShortTerm:
		if w.EstimatedGain > 0 {
			w.EstimatedTax = w.EstimatedGain * USSTCGRate
		}
		w.Note = fmt.Sprintf("%s: Short-Term — held %d days (<365), gain taxed as ordinary income (up to 37%%)", ticker, holdingDays)
	case USTaxLongTerm:
		if w.EstimatedGain > 0 {
			w.EstimatedTax = w.EstimatedGain * USLTCGRate
		}
		w.Note = fmt.Sprintf("%s: Long-Term — held %d days (≥365), gain taxed at 15%%/20%%", ticker, holdingDays)
	}

	if w.WashSaleRisk && w.EstimatedGain < 0 {
		w.Note += " ⚠️ WASH SALE RISK: loss may be disallowed (bought within 30 days)"
	}

	return w
}
