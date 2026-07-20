package costs

import (
	"fmt"
	"time"
)

// Finance Act 2024 rates for listed equity, effective 23 July 2024.
// Pre-Budget 2024 rates were STCG 15%, LTCG 10% above ₹1 lakh.
const (
	STCGRate          = 0.20     // 20% on short-term gains (holding < 12 months)
	LTCGRate          = 0.125    // 12.5% on long-term gains (holding ≥ 12 months)
	LTCGExemption     = 125000.0 // ₹1.25 lakh annual LTCG exemption
	STCGThresholdDays = 365      // holding period boundary (12 months ≈ 365 calendar days)
)

// TaxClass classifies the capital gains treatment for a sell order.
type TaxClass int

const (
	TaxUnknown TaxClass = iota // purchase date unavailable; manual check required
	TaxSTCG                    // short-term capital gain (holding < 365 days)
	TaxLTCG                    // long-term capital gain (holding ≥ 365 days)
)

func (c TaxClass) String() string {
	switch c {
	case TaxSTCG:
		return "STCG"
	case TaxLTCG:
		return "LTCG"
	default:
		return "UNKNOWN"
	}
}

// TaxWarning describes the tax treatment for a single sell order.
type TaxWarning struct {
	Ticker        string
	Class         TaxClass
	HoldingDays   int     // -1 when purchase date is unknown
	EstimatedGain float64 // (sellPrice - avgCost) × qty; 0 when avgCost unknown
	EstimatedTax  float64 // estimated tax liability; 0 when gain or class unknown
	Note          string  // human-readable note for warning banner
}

// ClassifySell returns a TaxWarning for a SELL order.
//   - purchaseDate zero value → Class=TaxUnknown (purchase date not available from broker API).
//   - avgCost zero → gain and tax cannot be estimated.
func ClassifySell(ticker string, qty int, sellPrice, avgCost float64, purchaseDate time.Time) TaxWarning {
	w := TaxWarning{
		Ticker:      ticker,
		HoldingDays: -1,
	}

	if purchaseDate.IsZero() {
		w.Class = TaxUnknown
		w.Note = fmt.Sprintf("%s: purchase date unavailable — verify STCG (20%%) vs LTCG (12.5%% above ₹1.25L) manually", ticker)
		if avgCost > 0 {
			w.EstimatedGain = (sellPrice - avgCost) * float64(qty)
		}
		return w
	}

	holdingDays := int(time.Since(purchaseDate).Hours() / 24)
	w.HoldingDays = holdingDays

	if holdingDays < STCGThresholdDays {
		w.Class = TaxSTCG
	} else {
		w.Class = TaxLTCG
	}

	if avgCost > 0 {
		w.EstimatedGain = (sellPrice - avgCost) * float64(qty)
	}

	switch w.Class {
	case TaxSTCG:
		if w.EstimatedGain > 0 {
			w.EstimatedTax = w.EstimatedGain * STCGRate
		}
		w.Note = fmt.Sprintf("%s: STCG — held %d days (<365), gain taxed at 20%%", ticker, holdingDays)
	case TaxLTCG:
		if w.EstimatedGain > LTCGExemption {
			w.EstimatedTax = (w.EstimatedGain - LTCGExemption) * LTCGRate
		}
		w.Note = fmt.Sprintf("%s: LTCG — held %d days (≥365), gain taxed at 12.5%% above ₹1.25L", ticker, holdingDays)
	}

	return w
}
