package monitoring

import (
	"fmt"
	"sort"
	"time"
	"mycase/pkg/yfinance"
)

// GetCapStallSeverity calculates the Cap Stall Severity based on DSO Delta and TTM Growth.
func GetCapStallSeverity(ttmGrowth, cagr3y, dsoDelta float64) string {
	dsoDeltaPct := dsoDelta * 100.0
	// Check if DSO Delta <= 5% and TTM growth >= CAGR
	if dsoDeltaPct <= 5.0 && ttmGrowth >= cagr3y {
		return "None"
	}
	// Check if DSO Delta <= 10%
	if dsoDeltaPct <= 10.0 {
		return "Mild"
	}
	// Check if DSO Delta <= 20%
	if dsoDeltaPct <= 20.0 {
		return "Moderate"
	}
	return "Severe"
}

// RunSimulation performs the 4-Pillar monitoring backtest simulation.
func RunSimulation(
	portfolio []StockInfo,
	params PolicyParams,
	histData map[string]*yfinance.HistoricalData,
	benchData *yfinance.HistoricalData,
	fundamentals map[string]yfinance.Fundamentals,
	initialCapital float64,
) (SimulationResult, error) {
	result := SimulationResult{
		InitialValue: initialCapital,
		Verdicts:     make([]StockVerdict, 0),
	}

	if len(portfolio) == 0 {
		return result, fmt.Errorf("empty portfolio")
	}

	// 1. Determine simulation timeline
	// Find the minimum length of daily historical closes across all tickers
	minHistory := len(benchData.Closes)
	for _, s := range portfolio {
		h, ok := histData[s.Ticker]
		if ok && len(h.Closes) < minHistory {
			minHistory = len(h.Closes)
		}
	}

	// We need enough historical data for 200-day SMA.
	// Minimum total history should be > 200.
	if minHistory <= 200 {
		return result, fmt.Errorf("insufficient price history for simulation (need at least 200 days, got %d)", minHistory)
	}

	// Define simulation window: the last 1 year (typically 252 trading days)
	// or the remaining history after taking the first 200 days for SMA warmup.
	simDays := 252
	if minHistory-200 < simDays {
		simDays = minHistory - 200
	}

	if params.StartDate != "" {
		if tTarget, err := time.Parse("2006-01-02", params.StartDate); err == nil {
			istLoc, errLoc := time.LoadLocation("Asia/Kolkata")
			if errLoc != nil {
				istLoc = time.UTC
			}
			foundIdx := -1
			var minDiff int64 = 9999999999
			for j, ts := range benchData.Timestamps {
				tLocal := time.Unix(ts, 0).In(istLoc)
				diff := tTarget.Unix() - tLocal.Unix()
				if diff < 0 {
					diff = -diff
				}
				if diff < minDiff {
					minDiff = diff
					foundIdx = j
				}
			}
			if foundIdx >= 200 && foundIdx < len(benchData.Closes) {
				simDays = len(benchData.Closes) - foundIdx
			}
		}
	}

	// 2. Initialize positions
	// We allocate capital based on target weights
	cash := initialCapital
	shares := make(map[string]float64)
	activeStocks := make(map[string]bool)
	targetWeights := make(map[string]float64)
	consecutiveDaysBelowSMA := make(map[string]int)
	isHighAlert := make(map[string]bool)
	isExited := make(map[string]bool)
	consecutiveQuartersFailed := make(map[string]int)

	for _, s := range portfolio {
		h, ok := histData[s.Ticker]
		if !ok || len(h.Closes) < minHistory {
			continue
		}
		tickerStartIdx := len(h.Closes) - simDays
		initialPrice := h.Closes[tickerStartIdx]
		allocated := initialCapital * s.Weight
		shares[s.Ticker] = allocated / initialPrice
		cash -= allocated
		activeStocks[s.Ticker] = true
		targetWeights[s.Ticker] = s.Weight
	}

	var totalSells float64

	// Helper function to rebalance active stocks to relative target weights
	rebalanceToTargets := func(currentVal float64, closePrices map[string]float64) {
		// Calculate sum of target weights for active stocks
		var activeTargetSum float64
		for t := range activeStocks {
			activeTargetSum += targetWeights[t]
		}
		if activeTargetSum <= 0 {
			return
		}

		// Rebalance: sell excess, then buy deficits
		newShares := make(map[string]float64)
		newCash := currentVal

		// Sell phase
		for t := range activeStocks {
			targetW := targetWeights[t] / activeTargetSum
			targetValue := currentVal * targetW
			price := closePrices[t]

			currentShares := shares[t]
			currentValStock := currentShares * price

			if currentValStock > targetValue {
				diffValue := currentValStock - targetValue
				totalSells += diffValue
				shares[t] = targetValue / price
				newCash -= targetValue
			} else {
				// Don't modify shares yet, just track target
				newCash -= currentValStock
			}
			newShares[t] = shares[t]
		}

		// Buy phase
		for t := range activeStocks {
			targetW := targetWeights[t] / activeTargetSum
			targetValue := currentVal * targetW
			price := closePrices[t]

			currentShares := shares[t]
			currentValStock := currentShares * price

			if currentValStock <= targetValue {
				diffValue := targetValue - currentValStock
				shares[t] = targetValue / price
				newCash -= diffValue
			}
		}
		cash = newCash
	}

	// 3. Daily simulation loop
	for d := 0; d < simDays; d++ {
		// Get current day close prices & volumes
		closePrices := make(map[string]float64)
		currentVolumes := make(map[string]float64)
		for t := range activeStocks {
			h := histData[t]
			tIdx := len(h.Closes) - simDays + d
			closePrices[t] = h.Closes[tIdx]
			currentVolumes[t] = h.Volumes[tIdx]
		}

		// Calculate total portfolio value
		var portfolioVal float64
		for t := range activeStocks {
			portfolioVal += shares[t] * closePrices[t]
		}
		portfolioVal += cash

		// --- Pillar 4: Technical Trend (SMA 200) Check (Daily) ---
		for t := range activeStocks {
			h := histData[t]
			tIdx := len(h.Closes) - simDays + d
			// Calculate SMA 200
			smaSum := 0.0
			for i := tIdx - 199; i <= tIdx; i++ {
				smaSum += h.Closes[i]
			}
			sma200 := smaSum / 200.0

			// Calculate 20-day average volume for volume breakout comparison
			vol20Sum := 0.0
			for i := tIdx - 19; i <= tIdx; i++ {
				vol20Sum += h.Volumes[i]
			}
			vol20Avg := vol20Sum / 20.0

			closePrice := h.Closes[tIdx]
			if closePrice < sma200 {
				consecutiveDaysBelowSMA[t]++
				// Check rising volume constraint: volume on below-SMA days is above average
				if consecutiveDaysBelowSMA[t] > params.SMADays && h.Volumes[tIdx] > vol20Avg {
					isHighAlert[t] = true
				}
			} else {
				consecutiveDaysBelowSMA[t] = 0
				isHighAlert[t] = false
			}
		}

		// --- Pillar 3: Allocation Drift (Daily weight check) ---
		driftTriggered := false
		for t := range activeStocks {
			stockVal := shares[t] * closePrices[t]
			stockW := stockVal / portfolioVal
			if stockW > params.MaxWeightDrift {
				driftTriggered = true
				break
			}
		}

		if driftTriggered {
			rebalanceToTargets(portfolioVal, closePrices)
		}

		// --- Pillar 3: Semi-Annual Rebalancing (Every 6 Months / ~126 trading days) ---
		if d > 0 && d%126 == 0 {
			rebalanceToTargets(portfolioVal, closePrices)
		}

		// --- Pillar 1 & 2: Quarterly Reviews (Every 3 Months / ~63 trading days) ---
		if d > 0 && d%63 == 0 {
			var toExit []string
			for t := range activeStocks {
				f, exists := fundamentals[t]
				if !exists {
					continue
				}

				// Pillar 1: Sales Growth Accelerator (TTM vs 3Y CAGR)
				passedSales, _, _ := yfinance.CalculateSalesGrowth(&f)

				// Pillar 2: Working capital sentry (DSO check)
				_, dsoPrev, dsoLatest := yfinance.CalculateDSO(&f)
				dsoDelta := 0.0
				if dsoPrev > 0 {
					dsoDelta = (dsoLatest - dsoPrev) / dsoPrev
				}
				passedWC := dsoDelta <= params.DSODeteriorationThreshold

				// Pillar 3: Asset Turnover & CapEx Inflection
				_, atPrev, atLatest, pctCapExChange, _ := yfinance.CalculateAssetTurnoverCapEx(&f, params.MaxCapExYoYMultiplier)
				passedAT := atLatest > atPrev

				maxCapExPct := (params.MaxCapExYoYMultiplier - 1.0) * 100.0
				passedCapEx := pctCapExChange <= maxCapExPct

				// Require at least 2 out of 3 operational criteria to pass
				passCount := 0
				if passedSales {
					passCount++
				}
				if passedWC {
					passCount++
				}
				if passedAT {
					passCount++
				}

				// Exit if CapEx gate is violated OR if less than 2 out of 3 operational criteria pass consecutively
				if !passedCapEx {
					toExit = append(toExit, t)
				} else if passCount < 2 {
					consecutiveQuartersFailed[t]++
					if consecutiveQuartersFailed[t] >= params.ConsecutiveQuartersExit {
						toExit = append(toExit, t)
					}
				} else {
					consecutiveQuartersFailed[t] = 0
				}
			}

			// Exit positions
			if len(toExit) > 0 {
				for _, t := range toExit {
					// Sell entire position
					stockVal := shares[t] * closePrices[t]
					totalSells += stockVal
					cash += stockVal
					delete(shares, t)
					delete(activeStocks, t)
					isExited[t] = true
				}
				// Reallocate remaining cash proportionally to laggards / active stocks
				var newPortfolioVal float64
				for t := range activeStocks {
					newPortfolioVal += shares[t] * closePrices[t]
				}
				newPortfolioVal += cash
				rebalanceToTargets(newPortfolioVal, closePrices)
			}
		}
	}

	// 4. Final valuation
	finalClosePrices := make(map[string]float64)
	for _, s := range portfolio {
		h := histData[s.Ticker]
		finalClosePrices[s.Ticker] = h.Closes[len(h.Closes)-1]
	}

	var finalVal float64
	for t := range activeStocks {
		finalVal += shares[t] * finalClosePrices[t]
	}
	finalVal += cash

	result.FinalValue = finalVal
	result.PortfolioReturn = (finalVal - initialCapital) / initialCapital * 100.0

	// Benchmark return
	benchStartIdx := len(benchData.Closes) - simDays
	benchEndIdx := len(benchData.Closes) - 1
	benchStart := benchData.Closes[benchStartIdx]
	benchEnd := benchData.Closes[benchEndIdx]
	result.BenchmarkReturn = (benchEnd - benchStart) / benchStart * 100.0
	result.ExcessReturn = result.PortfolioReturn - result.BenchmarkReturn

	// Churn rate calculation
	result.ChurnRate = (totalSells / initialCapital) * 100.0
	if result.ChurnRate > 0 {
		result.AlphaEfficiency = result.ExcessReturn / result.ChurnRate
	} else {
		result.AlphaEfficiency = result.ExcessReturn
	}

	// Assemble verdicts
	for _, s := range portfolio {
		f := fundamentals[s.Ticker]
		_, ttmGrowth, cagr3y := yfinance.CalculateSalesGrowth(&f)
		_, dsoPrev, dsoLatest := yfinance.CalculateDSO(&f)
		dsoDelta := 0.0
		if dsoPrev > 0 {
			dsoDelta = (dsoLatest - dsoPrev) / dsoPrev
		}

		severity := GetCapStallSeverity(ttmGrowth, cagr3y, dsoDelta)

		verdict := "✅ KEEP HOLD"
		if isExited[s.Ticker] {
			verdict = "⚠️ AUTO EXIT"
		} else if isHighAlert[s.Ticker] {
			verdict = "👀 HIGH ALERT"
		}

		src := "Live"
		if s.IsMock {
			src = "Mock"
		}

		result.Verdicts = append(result.Verdicts, StockVerdict{
			Ticker:           s.Ticker,
			Sector:           f.Sector,
			CAGR3Y:           cagr3y * 100.0,
			TTMGrowth:        ttmGrowth * 100.0,
			DSODelta:         dsoDelta * 100.0,
			CapStallSeverity: severity,
			Verdict:          verdict,
			DataSource:       src,
		})
	}

	// Sort verdicts by Ticker name for consistent output
	sort.Slice(result.Verdicts, func(i, j int) bool {
		return result.Verdicts[i].Ticker < result.Verdicts[j].Ticker
	})

	return result, nil
}
