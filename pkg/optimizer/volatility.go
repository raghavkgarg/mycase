package optimizer

// OptimizeInverseVolatility calculates weights proportional to 1/volatility.
// Returns a map of ticker to normalized weight.
func OptimizeInverseVolatility(tickers []string, priceHistory map[string][]float64) map[string]float64 {
	weights := make(map[string]float64)
	volatilities := make(map[string]float64)
	var sumInverseVol float64

	for _, ticker := range tickers {
		prices := priceHistory[ticker]
		var vol float64
		if len(prices) >= 2 {
			returns := CalculateDailyReturns(prices)
			vol = CalculateVolatility(returns)
		}

		// Safeguard against zero volatility or tiny prices
		if vol <= 0.00001 {
			vol = 0.05 // default fallback volatility (5% daily volatility)
		}
		volatilities[ticker] = vol
		sumInverseVol += 1.0 / vol
	}

	// Calculate normalized weights
	for _, ticker := range tickers {
		vol := volatilities[ticker]
		weights[ticker] = (1.0 / vol) / sumInverseVol
	}

	return weights
}
