package optimizer

import (
	"math"
)

// CalculateDailyReturns calculates simple daily returns from a slice of daily close prices.
// Returns a slice of length len(prices)-1.
func CalculateDailyReturns(prices []float64) []float64 {
	if len(prices) < 2 {
		return nil
	}
	returns := make([]float64, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		if prices[i-1] != 0 {
			returns[i-1] = (prices[i] - prices[i-1]) / prices[i-1]
		}
	}
	return returns
}

// CalculateVolatility calculates the standard deviation of daily returns.
func CalculateVolatility(returns []float64) float64 {
	n := len(returns)
	if n < 2 {
		return 0.0
	}

	var sum float64
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(n)

	var sqDiffSum float64
	for _, r := range returns {
		diff := r - mean
		sqDiffSum += diff * diff
	}

	// Sample standard deviation (n-1 degrees of freedom)
	variance := sqDiffSum / float64(n-1)
	return math.Sqrt(variance)
}

// CalculateMean calculates the mean of a slice of floats
func CalculateMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// CalculateCovariance calculates the sample covariance between two float slices
func CalculateCovariance(x, y []float64) float64 {
	n := len(x)
	if n != len(y) || n < 2 {
		return 0
	}
	meanX := CalculateMean(x)
	meanY := CalculateMean(y)
	var sum float64
	for i := range n {
		sum += (x[i] - meanX) * (y[i] - meanY)
	}
	return sum / float64(n-1)
}

// CalculateDownsideDeviation calculates daily downside deviation relative to a target return (typically 0)
func CalculateDownsideDeviation(returns []float64, target float64) float64 {
	n := len(returns)
	if n < 2 {
		return 0.0
	}
	var sumSq float64
	var count int
	for _, r := range returns {
		if r < target {
			diff := r - target
			sumSq += diff * diff
			count++
		}
	}
	if count < 2 {
		return 0.005 // fallback min downside dev (0.5%)
	}
	return math.Sqrt(sumSq / float64(n-1))
}

// CalculateTotalReturn calculates the percentage return over a price series
func CalculateTotalReturn(prices []float64) float64 {
	if len(prices) < 2 {
		return 0
	}
	return (prices[len(prices)-1] - prices[0]) / prices[0]
}

// CalculateUlcerIndex calculates the Ulcer Index (drawdown depth and duration indicator)
func CalculateUlcerIndex(prices []float64) float64 {
	n := len(prices)
	if n == 0 {
		return 0.0
	}
	var maxPrice float64
	var sumSqDrawdown float64
	for _, p := range prices {
		if p > maxPrice {
			maxPrice = p
		}
		if maxPrice > 0 {
			drawdown := 100.0 * (p - maxPrice) / maxPrice
			sumSqDrawdown += drawdown * drawdown
		}
	}
	return math.Sqrt(sumSqDrawdown / float64(n))
}
