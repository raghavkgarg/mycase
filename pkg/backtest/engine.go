package backtest

import (
	"fmt"
	"sort"
	"time"

	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// Run executes the backtest simulation.
// priceData must contain HistoricalData for every ticker in holdings.
// benchData is the benchmark price series (e.g. ^NSEI).
func Run(
	holdings []Holding,
	priceData map[string]*yfinance.HistoricalData,
	benchData *yfinance.HistoricalData,
	cfg SimConfig,
) (SimResult, error) {
	if len(holdings) == 0 {
		return SimResult{}, fmt.Errorf("no holdings provided")
	}
	if benchData == nil || len(benchData.Timestamps) == 0 {
		return SimResult{}, fmt.Errorf("no benchmark data provided")
	}
	if !cfg.From.IsZero() && !cfg.To.IsZero() && cfg.From.After(cfg.To) {
		return SimResult{}, fmt.Errorf("from date is after to date")
	}

	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		ist = time.FixedZone("IST", 5*3600+30*60)
	}

	fromStr := cfg.From.Format("2006-01-02")
	toStr := cfg.To.Format("2006-01-02")
	if cfg.From.IsZero() {
		fromStr = "0001-01-01"
	}
	if cfg.To.IsZero() {
		toStr = "9999-12-31"
	}

	// Build date → price maps for each ticker
	type priceByDate = map[string]float64
	tickerPrices := make(map[string]priceByDate, len(holdings))
	for _, h := range holdings {
		pd, ok := priceData[h.Ticker]
		if !ok || len(pd.Timestamps) == 0 {
			return SimResult{}, fmt.Errorf("no price data for %s", h.Ticker)
		}
		pm := make(priceByDate, len(pd.Timestamps))
		for i, ts := range pd.Timestamps {
			d := time.Unix(ts, 0).In(ist).Format("2006-01-02")
			pm[d] = pd.Closes[i]
		}
		tickerPrices[h.Ticker] = pm
	}

	benchPrices := make(priceByDate, len(benchData.Timestamps))
	for i, ts := range benchData.Timestamps {
		d := time.Unix(ts, 0).In(ist).Format("2006-01-02")
		benchPrices[d] = benchData.Closes[i]
	}

	// Find trading days in [from, to] where ALL tickers and the benchmark have data
	var tradingDays []string
	for dateStr := range benchPrices {
		if dateStr < fromStr || dateStr > toStr {
			continue
		}
		allPresent := true
		for _, h := range holdings {
			if _, ok := tickerPrices[h.Ticker][dateStr]; !ok {
				allPresent = false
				break
			}
		}
		if allPresent {
			tradingDays = append(tradingDays, dateStr)
		}
	}
	sort.Strings(tradingDays)

	if len(tradingDays) < 2 {
		return SimResult{}, fmt.Errorf("only %d common trading days found in the date range — need at least 2", len(tradingDays))
	}

	// Initialize: buy at first day close with slippage
	firstDay := tradingDays[0]
	shares := make(map[string]float64, len(holdings))
	cash := cfg.InitialCapital

	for _, h := range holdings {
		price := tickerPrices[h.Ticker][firstDay]
		if price <= 0 {
			continue
		}
		allocated := cfg.InitialCapital * h.Weight
		effectivePrice := price * (1 + cfg.SlippagePct)
		shares[h.Ticker] = allocated / effectivePrice
		cash -= allocated
	}

	// Benchmark: track as NAV units from first day
	benchInitPrice := benchPrices[firstDay]
	benchUnits := cfg.InitialCapital / benchInitPrice

	targetWeights := make(map[string]float64, len(holdings))
	for _, h := range holdings {
		targetWeights[h.Ticker] = h.Weight
	}

	snapshots := make([]DailySnapshot, 0, len(tradingDays))
	rebalanceCount := 0
	var lastRebalanceDate time.Time

	for _, dayStr := range tradingDays {
		day, _ := time.ParseInLocation("2006-01-02", dayStr, ist)

		// Current portfolio value
		portVal := cash
		for _, h := range holdings {
			portVal += shares[h.Ticker] * tickerPrices[h.Ticker][dayStr]
		}
		benchVal := benchUnits * benchPrices[dayStr]

		snapshots = append(snapshots, DailySnapshot{
			Date:           day,
			PortfolioValue: portVal,
			BenchmarkValue: benchVal,
		})

		// Determine if this day triggers a rebalance
		needsRebalance := false
		switch cfg.Rebalance {
		case FreqMonthly:
			needsRebalance = lastRebalanceDate.IsZero() ||
				day.Month() != lastRebalanceDate.Month() ||
				day.Year() != lastRebalanceDate.Year()
		case FreqQuarterly:
			quarter := func(t time.Time) int { return (int(t.Month()) - 1) / 3 }
			needsRebalance = lastRebalanceDate.IsZero() ||
				quarter(day) != quarter(lastRebalanceDate) ||
				day.Year() != lastRebalanceDate.Year()
		case FreqDrift:
			if portVal > 0 {
				threshold := cfg.DriftThreshold
				if threshold <= 0 {
					threshold = 0.05
				}
				for _, h := range holdings {
					if targetWeights[h.Ticker] > 0 {
						actual := (shares[h.Ticker] * tickerPrices[h.Ticker][dayStr]) / portVal
						if absf(actual-targetWeights[h.Ticker]) > threshold {
							needsRebalance = true
							break
						}
					}
				}
			}
		}

		if needsRebalance && dayStr != firstDay {
			closePrices := make(map[string]float64, len(holdings))
			for _, h := range holdings {
				closePrices[h.Ticker] = tickerPrices[h.Ticker][dayStr]
			}
			rebalance(shares, &cash, holdings, closePrices, targetWeights, portVal, cfg.SlippagePct)
			lastRebalanceDate = day
			rebalanceCount++
		} else if dayStr == firstDay {
			lastRebalanceDate = day
		}
	}

	if len(snapshots) == 0 {
		return SimResult{}, fmt.Errorf("no snapshots generated")
	}

	portValues := make([]float64, len(snapshots))
	benchValues := make([]float64, len(snapshots))
	for i, s := range snapshots {
		portValues[i] = s.PortfolioValue
		benchValues[i] = s.BenchmarkValue
	}

	initial := portValues[0]
	final := portValues[len(portValues)-1]
	bInitial := benchValues[0]
	bFinal := benchValues[len(benchValues)-1]

	first, _ := time.ParseInLocation("2006-01-02", tradingDays[0], ist)
	last, _ := time.ParseInLocation("2006-01-02", tradingDays[len(tradingDays)-1], ist)
	calDays := int(last.Sub(first).Hours()/24) + 1

	// TotalReturn is measured from the original invested capital (includes slippage impact).
	totalRet := (final - cfg.InitialCapital) / cfg.InitialCapital
	benchTotalRet := (bFinal - bInitial) / bInitial
	cagr := CalcCAGR(initial, final, calDays)
	benchCAGR := CalcCAGR(bInitial, bFinal, calDays)

	return SimResult{
		Snapshots:       snapshots,
		TotalReturn:     totalRet,
		BenchmarkReturn: benchTotalRet,
		CAGR:            cagr,
		BenchmarkCAGR:   benchCAGR,
		MaxDrawdown:     CalcMaxDrawdown(portValues),
		SharpeRatio:     CalcSharpe(portValues),
		SortinoRatio:    CalcSortino(portValues),
		CalmarRatio:     CalcCalmar(cagr, CalcMaxDrawdown(portValues)),
		Alpha:           CalcAlpha(cagr, benchCAGR, CalcBeta(portValues, benchValues)),
		Beta:            CalcBeta(portValues, benchValues),
		TradingDays:     len(tradingDays),
		RebalanceCount:  rebalanceCount,
	}, nil
}

// rebalance adjusts shares to hit target weights at current prices with slippage.
// Sells are executed first (generating cash), then buys.
func rebalance(
	shares map[string]float64,
	cash *float64,
	holdings []Holding,
	prices, targetWeights map[string]float64,
	portVal, slippage float64,
) {
	// Sell over-weight positions
	for _, h := range holdings {
		target := portVal * targetWeights[h.Ticker]
		current := shares[h.Ticker] * prices[h.Ticker]
		if current > target && prices[h.Ticker] > 0 {
			excessShares := (current - target) / prices[h.Ticker]
			shares[h.Ticker] -= excessShares
			*cash += excessShares * prices[h.Ticker] * (1 - slippage)
		}
	}
	// Buy under-weight positions
	for _, h := range holdings {
		target := portVal * targetWeights[h.Ticker]
		current := shares[h.Ticker] * prices[h.Ticker]
		if current < target && prices[h.Ticker] > 0 {
			neededCash := target - current
			if neededCash > *cash {
				neededCash = *cash
			}
			sharesToBuy := neededCash / (prices[h.Ticker] * (1 + slippage))
			shares[h.Ticker] += sharesToBuy
			*cash -= sharesToBuy * prices[h.Ticker] * (1 + slippage)
		}
	}
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
