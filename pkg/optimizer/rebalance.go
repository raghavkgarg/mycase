package optimizer

import (
	"slices"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/costs"
)

// DetectExits returns tickers that are active (weight > 0) in the golden copy
// but absent from the new selection. These should receive weight 0 to trigger
// liquidation in the basket.
func DetectExits(goldenWeights map[string]float64, newSelectionKeys []string) []string {
	var exits []string
	for ticker, weight := range goldenWeights {
		if weight > 0.00001 && !slices.Contains(newSelectionKeys, ticker) {
			exits = append(exits, ticker)
		}
	}
	return exits
}

// FilterMicroTransactions partitions orders into kept and filtered slices.
// An order is filtered when its total transaction cost exceeds thresholdPct of trade value,
// e.g. thresholdPct=0.005 rejects trades where costs > 0.5% of the order value.
// A zero thresholdPct disables filtering (all orders are kept).
func FilterMicroTransactions(
	orders []broker.Order,
	quotes map[string]float64,
	model costs.CostModel,
	thresholdPct float64,
) (kept, filtered []broker.Order) {
	if thresholdPct <= 0 {
		return orders, nil
	}
	for _, o := range orders {
		price := o.Price
		if price <= 0 {
			price = quotes["NSE:"+o.TradingSymbol]
		}
		bd := model.Calculate(o.TransactionType, o.Quantity, price)
		if bd.CostRatio > thresholdPct {
			filtered = append(filtered, o)
		} else {
			kept = append(kept, o)
		}
	}
	return kept, filtered
}
