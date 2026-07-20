package optimizer

import "slices"

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
