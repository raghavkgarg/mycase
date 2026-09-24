package stockpicker

import (
	"fmt"
	"math"

	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// CheckSentryTrendRupture checks if a stock violates the Tier-3 Sentry Level-3 Trend Rupture criteria:
// 1. Price < 0.95 * SMA200 (breaking down >5% below 200-day simple moving average)
// 2. Price < 0.80 * 52W High (severe peak drawdown > 20% from 52-week high)
func CheckSentryTrendRupture(hist *yfinance.HistoricalData) (isRupture bool, reason string) {
	if hist == nil || len(hist.Closes) == 0 {
		return false, ""
	}

	// Filter out NaNs and non-positive closes
	var validCloses []float64
	var validOpens []float64
	for i, c := range hist.Closes {
		if !math.IsNaN(c) && c > 0 {
			validCloses = append(validCloses, c)
			if len(hist.Opens) > i && !math.IsNaN(hist.Opens[i]) && hist.Opens[i] > 0 {
				validOpens = append(validOpens, hist.Opens[i])
			} else {
				validOpens = append(validOpens, c)
			}
		}
	}

	if len(validCloses) == 0 {
		return false, ""
	}

	currentPrice := validCloses[len(validCloses)-1]

	// 1. Calculate SMA200 if sufficient history is present (>= 150 sessions)
	if len(validCloses) >= 150 {
		window := min(len(validCloses), 200)
		var sum200 float64
		for i := len(validCloses) - window; i < len(validCloses); i++ {
			sum200 += validCloses[i]
		}
		sma200 := sum200 / float64(window)
		if sma200 > 0 && currentPrice < 0.95*sma200 {
			return true, fmt.Sprintf("Price (₹%.1f) broke < 0.95x 200-SMA (₹%.1f)", currentPrice, sma200)
		}
	}

	// 2. Calculate 52-Week High (last 260 trading days)
	lookback := min(len(validCloses), 260)
	var maxHigh float64
	for i := len(validCloses) - lookback; i < len(validCloses); i++ {
		c := validCloses[i]
		if c > maxHigh {
			maxHigh = c
		}
		if len(validOpens) > i && validOpens[i] > maxHigh {
			maxHigh = validOpens[i]
		}
	}

	if maxHigh > 0 && currentPrice < 0.80*maxHigh {
		dd := ((maxHigh - currentPrice) / maxHigh) * 100.0
		return true, fmt.Sprintf("Peak drawdown -%.1f%% exceeds -20%% limit (52W High ₹%.1f)", dd, maxHigh)
	}

	return false, ""
}
