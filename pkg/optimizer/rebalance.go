package optimizer

import (
	"slices"

	brokertypes "github.com/raghavkgarg/mycase/pkg/broker/types"
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
	orders []brokertypes.Order,
	quotes map[string]float64,
	model costs.CostModel,
	thresholdPct float64,
) (kept, filtered []brokertypes.Order) {
	return FilterMicroTransactionsWithExits(orders, quotes, model, thresholdPct, nil)
}

// FilterMicroTransactionsWithExits partitions orders into kept and filtered slices,
// preserving full exit orders (where target weight <= 0) regardless of transaction cost ratio.
func FilterMicroTransactionsWithExits(
	orders []brokertypes.Order,
	quotes map[string]float64,
	model costs.CostModel,
	thresholdPct float64,
	basket map[string]float64,
) (kept, filtered []brokertypes.Order) {
	if thresholdPct <= 0 {
		return orders, nil
	}
	for _, o := range orders {
		// Full exit orders (where target weight is 0.0) bypass micro-transaction filtering to ensure complete liquidation
		isExit := false
		if o.TransactionType == "SELL" && basket != nil {
			for k, w := range basket {
				if (k == o.TradingSymbol || k == "NSE:"+o.TradingSymbol || k == "BSE:"+o.TradingSymbol) && w <= 0.0 {
					isExit = true
					break
				}
			}
		}
		if isExit {
			kept = append(kept, o)
			continue
		}

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
