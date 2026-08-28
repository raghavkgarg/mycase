package tax

import (
	"sort"
	"time"

	"github.com/raghavkgarg/mycase/pkg/broker"
)

// SequencePlan is the result of tax-optimizing a set of orders. It reorders the
// order slice so that loss-harvesting sells execute before buys, and surfaces
// wash-sale warnings for any buy that would repurchase a security sold at a
// loss within the 30-day window.
type SequencePlan struct {
	// Orders is the reordered slice, ready to hand to the executor (which
	// executes in slice order).
	Orders []broker.Order
	// HarvestSells lists tickers being sold at a loss (tax-beneficial).
	HarvestSells []string
	// WashSaleWarnings flags buys that violate the wash-sale rule against a
	// loss sell in the same batch or recent history.
	WashSaleWarnings []WashSaleViolation
	// EstTaxSaving is the total estimated federal tax reduction from the
	// harvested losses in this batch.
	EstTaxSaving float64
}

// SequenceParams configures tax-aware order sequencing.
type SequenceParams struct {
	AsOf       time.Time
	STCGRate   float64
	LTCGRate   float64
	OpenLots   map[string][]Lot     // current lots per ticker (for loss/holding classification)
	RecentBuys map[string]time.Time // ticker → most recent buy date (wash-sale history)
}

// key builds the full ticker key (EXCHANGE:SYMBOL) used across lot maps.
func orderKey(o broker.Order) string {
	if o.Exchange == "" {
		return o.TradingSymbol
	}
	return o.Exchange + ":" + o.TradingSymbol
}

// TaxOptimizeOrders reorders orders to maximize tax-loss harvesting benefit and
// detects wash-sale risks. Sequencing rules:
//  1. SELL orders that realize a loss are placed first (harvest the loss).
//  2. Other SELL orders (gains) follow.
//  3. BUY orders execute last.
//
// Rationale: executing loss sells first guarantees the harvest is captured even
// if a later order fails; it also front-loads cash for the buys. A BUY of a
// ticker sold at a loss within 30 days is flagged as a wash-sale violation
// (which would disallow the harvested loss).
func TaxOptimizeOrders(orders []broker.Order, prices map[string]float64, p SequenceParams) SequencePlan {
	if p.AsOf.IsZero() {
		p.AsOf = time.Now()
	}
	if p.STCGRate == 0 {
		p.STCGRate = 0.37
	}
	if p.LTCGRate == 0 {
		p.LTCGRate = 0.20
	}

	plan := SequencePlan{}

	type classified struct {
		order    broker.Order
		isSell   bool
		isLoss   bool
		lossAmt  float64 // negative
		longTerm bool
	}

	classifiedOrders := make([]classified, 0, len(orders))
	lossSellTickers := make(map[string]bool)

	for _, o := range orders {
		key := orderKey(o)
		c := classified{order: o}
		if o.TransactionType == "SELL" || o.TransactionType == "S" {
			c.isSell = true
			price := o.Price
			if price == 0 {
				price = prices[key]
			}
			if price == 0 {
				price = o.Ltp
			}
			gain, longTerm := estimateSellGain(key, float64(o.Quantity), price, p)
			c.longTerm = longTerm
			if gain < 0 {
				c.isLoss = true
				c.lossAmt = gain
				lossSellTickers[key] = true
				plan.HarvestSells = append(plan.HarvestSells, key)
				rate := p.STCGRate
				if longTerm {
					rate = p.LTCGRate
				}
				plan.EstTaxSaving += -gain * rate
			}
		}
		classifiedOrders = append(classifiedOrders, c)
	}

	// Wash-sale detection: a BUY of a ticker sold at a loss in this batch (or
	// bought within 30 days per history) disallows the loss.
	for _, o := range orders {
		if o.TransactionType != "BUY" && o.TransactionType != "B" {
			continue
		}
		key := orderKey(o)
		if lossSellTickers[key] {
			plan.WashSaleWarnings = append(plan.WashSaleWarnings, WashSaleViolation{
				Ticker:   key,
				SellDate: p.AsOf,
				BuyDate:  p.AsOf,
				Note:     "buying a security sold at a loss in the same batch — wash sale disallows the loss",
			})
		}
		if bd, ok := p.RecentBuys[key]; ok && !bd.IsZero() && daysBetween(bd, p.AsOf) <= washSaleDays {
			// A recent buy plus a fresh loss sell elsewhere is only a concern
			// if we're also harvesting this ticker; covered above. This branch
			// catches replacement-into-recently-bought names.
			if lossSellTickers[key] {
				plan.WashSaleWarnings = append(plan.WashSaleWarnings, WashSaleViolation{
					Ticker:    key,
					BuyDate:   bd,
					SellDate:  p.AsOf,
					DaysApart: daysBetween(bd, p.AsOf),
					Note:      "recent buy within 30 days of loss sale",
				})
			}
		}
	}

	// Stable sort: loss sells (0) → gain sells (1) → buys (2).
	sort.SliceStable(classifiedOrders, func(i, j int) bool {
		return seqRank(classifiedOrders[i].isSell, classifiedOrders[i].isLoss) <
			seqRank(classifiedOrders[j].isSell, classifiedOrders[j].isLoss)
	})

	plan.Orders = make([]broker.Order, len(classifiedOrders))
	for i, c := range classifiedOrders {
		plan.Orders[i] = c.order
	}
	return plan
}

func seqRank(isSell, isLoss bool) int {
	switch {
	case isSell && isLoss:
		return 0
	case isSell:
		return 1
	default:
		return 2
	}
}

// estimateSellGain estimates the realized gain/loss for selling `qty` shares of
// `key` at `price`, using FIFO against the open lots. Returns the total gain
// (negative for a loss) and whether the consumed lots are predominantly
// long-term.
func estimateSellGain(key string, qty, price float64, p SequenceParams) (gain float64, longTerm bool) {
	lots := p.OpenLots[key]
	remaining := qty
	var totalGain, ltQty, totalQty float64
	for _, lot := range lots {
		if remaining <= epsilon {
			break
		}
		matchQty := lot.Quantity
		if matchQty > remaining {
			matchQty = remaining
		}
		totalGain += matchQty * (price - lot.CostPerShare)
		totalQty += matchQty
		if lot.IsLongTerm(p.AsOf) {
			ltQty += matchQty
		}
		remaining -= matchQty
	}
	if totalQty == 0 {
		return 0, false
	}
	return totalGain, ltQty >= totalQty/2
}
