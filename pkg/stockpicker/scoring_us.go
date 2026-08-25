package stockpicker

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/selectiontracker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// ScoreUSQualityMomentum computes a 100-point relative scoring matrix for US quality-momentum candidates.
// Factors (per roadmap):
//   - ROIC (Return on Invested Capital): 20 pts — capital efficiency
//   - Free Cash Flow Yield (FCF/EV): 20 pts — cash generation vs enterprise value
//   - 12-month Momentum (skip last month): 15 pts — Jegadeesh-Titman momentum
//   - Earnings Quality (CFO/Net Income): 15 pts — accruals anomaly
//   - Shareholder Yield (div + buyback): 15 pts — total capital return
//   - Low Volatility: 15 pts — low-vol anomaly (lower vol = better)
func ScoreUSQualityMomentum(
	ctx context.Context,
	activeKeys []string,
	fundamentals map[string]yfinance.Fundamentals,
	fullHistory map[string]*yfinance.HistoricalData,
	hardFilters *config.HardFilters,
) map[string]float64 {
	fmt.Printf("Calculating 100-Point US Quality-Momentum Relative Scoring Matrix for %d candidates...\n", len(activeKeys))

	// Raw indicator maps for normalization
	roics := make(map[string]float64)
	fcfYields := make(map[string]float64)
	momentums := make(map[string]float64)
	earningsQualities := make(map[string]float64)
	shareholderYields := make(map[string]float64)
	volatilities := make(map[string]float64)

	minROIC, maxROIC := math.MaxFloat64, -math.MaxFloat64
	minFCFY, maxFCFY := math.MaxFloat64, -math.MaxFloat64
	minMom, maxMom := math.MaxFloat64, -math.MaxFloat64
	minEQ, maxEQ := math.MaxFloat64, -math.MaxFloat64
	minSY, maxSY := math.MaxFloat64, -math.MaxFloat64
	minVol, maxVol := math.MaxFloat64, -math.MaxFloat64

	for _, t := range activeKeys {
		f := fundamentals[t]
		hist := fullHistory[t]

		// 1. ROIC — approximate as NOPAT / Invested Capital
		// Best available: use operating income / (total assets - current liabilities) if annual data exists,
		// otherwise fall back to ROE (which incorporates leverage but still ranks well for large-cap)
		roic := computeROIC(&f)
		roics[t] = roic
		if roic < minROIC {
			minROIC = roic
		}
		if roic > maxROIC {
			maxROIC = roic
		}

		// 2. Free Cash Flow Yield = FCF / Market Cap (or FCF / EV if we had total debt, approximate with market cap)
		fcfY := 0.0
		if f.MarketCap > 0 && f.FreeCashflow > 0 {
			fcfY = f.FreeCashflow / f.MarketCap
		}
		fcfYields[t] = fcfY
		if fcfY < minFCFY {
			minFCFY = fcfY
		}
		if fcfY > maxFCFY {
			maxFCFY = fcfY
		}

		// 3. 12-Month Momentum (skip last month) — Jegadeesh-Titman
		// Use 12 months of data, exclude the most recent ~21 trading days (1 month)
		mom := computeMomentumSkip1Mo(hist)
		momentums[t] = mom
		if mom < minMom {
			minMom = mom
		}
		if mom > maxMom {
			maxMom = mom
		}

		// 4. Earnings Quality = Operating Cash Flow / Net Income
		eq := 0.0
		if f.NetIncome > 0 && f.OperatingCashflow > 0 {
			eq = f.OperatingCashflow / f.NetIncome
		} else if f.NetIncome > 0 && f.FreeCashflow > 0 {
			// Fallback: use FCF as a looser proxy
			eq = f.FreeCashflow / f.NetIncome
		}
		// Cap at reasonable bounds to avoid outliers
		if eq > 5.0 {
			eq = 5.0
		}
		earningsQualities[t] = eq
		if eq < minEQ {
			minEQ = eq
		}
		if eq > maxEQ {
			maxEQ = eq
		}

		// 5. Shareholder Yield = Dividend Yield + Buyback Yield
		// Buyback yield approximated as (FCF - Dividends) / MarketCap when positive
		sy := computeShareholderYield(&f)
		shareholderYields[t] = sy
		if sy < minSY {
			minSY = sy
		}
		if sy > maxSY {
			maxSY = sy
		}

		// 6. Low Volatility = annualized standard deviation of daily returns
		vol := computeAnnualizedVol(hist)
		volatilities[t] = vol
		if vol < minVol {
			minVol = vol
		}
		if vol > maxVol {
			maxVol = vol
		}
	}

	// Score each ticker
	scores := make(map[string]float64)
	for _, t := range activeKeys {
		wROIC := 20.0
		wFCFY := 20.0
		wMom := 15.0
		wEQ := 15.0
		wSY := 15.0
		wVol := 15.0

		if hardFilters != nil {
			if hardFilters.ScoreWeightROIC > 0 {
				wROIC = hardFilters.ScoreWeightROIC
			}
			if hardFilters.ScoreWeightFCFYieldUS > 0 {
				wFCFY = hardFilters.ScoreWeightFCFYieldUS
			}
			if hardFilters.ScoreWeightMomentum12M > 0 {
				wMom = hardFilters.ScoreWeightMomentum12M
			}
			if hardFilters.ScoreWeightEarningsQuality > 0 {
				wEQ = hardFilters.ScoreWeightEarningsQuality
			}
			if hardFilters.ScoreWeightShareholderYieldUS > 0 {
				wSY = hardFilters.ScoreWeightShareholderYieldUS
			}
			if hardFilters.ScoreWeightLowVol > 0 {
				wVol = hardFilters.ScoreWeightLowVol
			}
		}

		pROIC := normalizeValue(roics[t], minROIC, maxROIC, wROIC, true)
		pFCFY := normalizeValue(fcfYields[t], minFCFY, maxFCFY, wFCFY, true)
		pMom := normalizeValue(momentums[t], minMom, maxMom, wMom, true)
		pEQ := normalizeValue(earningsQualities[t], minEQ, maxEQ, wEQ, true)
		pSY := normalizeValue(shareholderYields[t], minSY, maxSY, wSY, true)
		pVol := normalizeValue(volatilities[t], minVol, maxVol, wVol, false) // Lower vol = better

		scores[t] = pROIC + pFCFY + pMom + pEQ + pSY + pVol
	}

	// Sort activeKeys descending by total score, with higher FCF yield as tie-breaker
	sort.Slice(activeKeys, func(i, j int) bool {
		scoreI := scores[activeKeys[i]]
		scoreJ := scores[activeKeys[j]]
		if math.Abs(scoreI-scoreJ) < 1e-9 {
			return fcfYields[activeKeys[i]] > fcfYields[activeKeys[j]]
		}
		return scoreI > scoreJ
	})

	return scores
}

// SelectTopNUSQM selects top N US quality-momentum stocks with sector caps and hysteresis.
func SelectTopNUSQM(
	activeKeys []string,
	scores map[string]float64,
	fundamentals map[string]yfinance.Fundamentals,
	hardFilters *config.HardFilters,
	topN int,
	existingHoldings map[string]float64,
	hysteresisBuffer int,
	tracker *selectiontracker.Tracker,
) []string {
	maxPerSector := 4
	if hardFilters != nil && hardFilters.MaxStocksPerSector > 0 {
		maxPerSector = hardFilters.MaxStocksPerSector
	}
	fmt.Printf("Applying US Quality-Momentum Sector Caps (max %d stocks per sector)...\n", maxPerSector)

	var sectorCapCandidates []string
	sectorCounts := make(map[string]int)
	sectorTopTickers := make(map[string][]string)

	for rankIdx, t := range activeKeys {
		rank := rankIdx + 1
		tracker.RecordRawScore(t, scores[t], rank)

		f := fundamentals[t]
		sec := f.Sector
		if sec == "" {
			sec = "Unknown"
		}

		// Record drivers for transparency
		fcfY := 0.0
		if f.MarketCap > 0 && f.FreeCashflow > 0 {
			fcfY = (f.FreeCashflow / f.MarketCap) * 100.0
		}
		driverStr := fmt.Sprintf("ROE: %.1f%%, FCF Yield: %.1f%%, OpMargin: %.1f%%",
			f.ROE*100.0, fcfY, f.OperatingMargins*100.0)
		tracker.RecordAdditionDriver(t, driverStr)

		if sectorCounts[sec] >= maxPerSector {
			tracker.RecordSectorCapDrop(t, sec, sectorTopTickers[sec])
			continue
		}
		sectorCounts[sec]++
		sectorTopTickers[sec] = append(sectorTopTickers[sec], t)
		sectorCapCandidates = append(sectorCapCandidates, t)
	}

	bufferLimit := topN + hysteresisBuffer
	fmt.Printf("Applying Hysteresis Buffer Zone (Top %d target, existing kept up to rank %d)...\n", topN, bufferLimit)
	return ApplyHysteresisSelection(sectorCapCandidates, existingHoldings, topN, bufferLimit, tracker)
}

// NormalizeUSQMWeights normalizes weights proportionally to scores with stock & sector caps.
func NormalizeUSQMWeights(
	selectedKeys []string,
	scores map[string]float64,
	fundamentals map[string]yfinance.Fundamentals,
	hardFilters *config.HardFilters,
	existingHoldings map[string]float64,
	rebalanceTolerance float64,
) map[string]float64 {
	fmt.Printf("Normalizing weights for selected top %d US quality-momentum stocks...\n", len(selectedKeys))
	finalWeights := make(map[string]float64)
	var sumScore float64
	for _, t := range selectedKeys {
		sumScore += scores[t]
	}
	for _, t := range selectedKeys {
		if sumScore > 0 {
			finalWeights[t] = scores[t] / sumScore
		} else {
			finalWeights[t] = 1.0 / float64(len(selectedKeys))
		}
	}

	stockCapVal := 0.08
	sectorCapVal := 0.25
	if hardFilters != nil {
		if hardFilters.MaxStockWeightCap > 0 {
			stockCapVal = hardFilters.MaxStockWeightCap
		}
		if hardFilters.MaxSectorWeightCap > 0 {
			sectorCapVal = hardFilters.MaxSectorWeightCap
		}
	}

	NormalizeAndCapWeights(selectedKeys, finalWeights, fundamentals, stockCapVal, sectorCapVal)

	// Apply rebalancing band/tolerance
	finalWeights = ApplyRebalancingBand(selectedKeys, finalWeights, existingHoldings, rebalanceTolerance)

	return finalWeights
}

// ApplyUSHardFilters applies US-specific hard filters to eliminate ineligible stocks.
// US filters are simpler than India: market cap, liquidity (ADV), positive FCF.
// India-only filters (promoter stake, pledging, governance) are skipped.
func ApplyUSHardFilters(
	ctx context.Context,
	activeKeys []string,
	hardFilters *config.HardFilters,
	fundamentals map[string]yfinance.Fundamentals,
	tracker *selectiontracker.Tracker,
) []string {
	if hardFilters == nil {
		tracker.InitialCount = len(activeKeys)
		return activeKeys
	}

	tracker.InitialCount = len(activeKeys)
	var passed []string
	eliminated := 0

	for _, t := range activeKeys {
		f := fundamentals[t]

		// 1. Market Cap filter (min $10B)
		if hardFilters.MinMarketCap > 0 && f.MarketCap < hardFilters.MinMarketCap {
			tracker.RecordSafetyDrop(t, fmt.Sprintf("Market cap $%.0fM < $%.0fM min", f.MarketCap/1e6, hardFilters.MinMarketCap/1e6))
			eliminated++
			continue
		}

		// 2. Average Daily Volume filter (min $50M)
		if hardFilters.MinADV > 0 {
			adv := f.AverageVolume * f.RegularPrice
			if f.RegularPrice == 0 && f.MarketCap > 0 && f.AverageVolume > 0 {
				// Estimate price from market cap / shares (vol3m is in shares)
				// Just use raw volume as a proxy since we lack price in some cases
				adv = f.AverageVolume
			}
			if adv > 0 && adv < hardFilters.MinADV {
				tracker.RecordSafetyDrop(t, fmt.Sprintf("ADV $%.0fM < $%.0fM min", adv/1e6, hardFilters.MinADV/1e6))
				eliminated++
				continue
			}
		}

		// 3. Positive Free Cash Flow (hard requirement for quality)
		if hardFilters.MinFCF != nil && f.FreeCashflow <= *hardFilters.MinFCF {
			tracker.RecordSafetyDrop(t, fmt.Sprintf("FCF $%.0fM ≤ 0", f.FreeCashflow/1e6))
			eliminated++
			continue
		} else if hardFilters.MinFCF == nil && f.FreeCashflow <= 0 {
			// Default: positive FCF required for us_quality_momentum
			tracker.RecordSafetyDrop(t, fmt.Sprintf("FCF $%.0fM ≤ 0", f.FreeCashflow/1e6))
			eliminated++
			continue
		}

		passed = append(passed, t)
	}

	fmt.Printf("US Hard Filters: %d eliminated, %d candidates remaining.\n", eliminated, len(passed))
	return passed
}

// --- Helper functions for US scoring ---

// computeROIC approximates Return on Invested Capital.
// Uses operating income / (total assets - current liabilities) when annual data is available.
// Falls back to ROE as a proxy for large-cap US stocks.
func computeROIC(f *yfinance.Fundamentals) float64 {
	// Try to compute from annual financials
	nIncome := len(f.AnnualOperatingIncome)
	nAssets := len(f.AnnualTotalAssets)
	nLiab := len(f.AnnualCurrentLiabilities)

	if nIncome > 0 && nAssets > 0 && nLiab > 0 {
		latestEBIT := f.AnnualOperatingIncome[nIncome-1].Value
		latestAssets := f.AnnualTotalAssets[nAssets-1].Value
		latestLiab := f.AnnualCurrentLiabilities[nLiab-1].Value
		investedCapital := latestAssets - latestLiab
		if investedCapital > 0 {
			// Approximate NOPAT as EBIT * (1 - assumed 21% US corporate tax rate)
			nopat := latestEBIT * 0.79
			return nopat / investedCapital
		}
	}

	// Fallback: use ReturnOnAssets if available (better proxy than ROE for ROIC)
	if f.ReturnOnAssets > 0 {
		return f.ReturnOnAssets
	}

	// Last resort: use ROE (incorporates leverage, but still informative for ranking)
	return f.ROE
}

// computeMomentumSkip1Mo calculates 12-month price return excluding the most recent month.
// This is the Jegadeesh-Titman momentum factor (skip short-term reversal).
func computeMomentumSkip1Mo(hist *yfinance.HistoricalData) float64 {
	if hist == nil || len(hist.Closes) < 42 { // Need at least ~2 months of data
		return 0.0
	}

	n := len(hist.Closes)

	// Skip last ~21 trading days (1 month)
	skipDays := 21
	if n <= skipDays+1 {
		return 0.0
	}

	endIdx := n - skipDays - 1 // End of momentum window (excluding recent month)
	startIdx := 0              // Start of available history (ideally 12 months ago)

	startPrice := hist.Closes[startIdx]
	endPrice := hist.Closes[endIdx]

	if startPrice <= 0 {
		return 0.0
	}

	return (endPrice - startPrice) / startPrice
}

// computeShareholderYield calculates total shareholder yield (dividends + buybacks).
// For US stocks: dividend yield + net buyback yield (approximated from FCF surplus).
func computeShareholderYield(f *yfinance.Fundamentals) float64 {
	if f.MarketCap <= 0 {
		return 0.0
	}

	yield := 0.0

	// Dividend yield
	if f.DividendYield > 0 {
		yield += f.DividendYield
	}

	// Buyback yield proxy: (FCF - estimated dividends) / market cap, if positive
	// Estimated dividends = dividend yield * market cap
	if f.FreeCashflow > 0 {
		estimatedDividends := f.DividendYield * f.MarketCap
		netBuyback := f.FreeCashflow - estimatedDividends
		if netBuyback > 0 {
			yield += netBuyback / f.MarketCap
		}
	}

	return yield
}

// computeAnnualizedVol calculates annualized volatility from daily closing prices.
func computeAnnualizedVol(hist *yfinance.HistoricalData) float64 {
	if hist == nil || len(hist.Closes) < 20 {
		return 1.0 // High default volatility for missing data
	}

	// Compute daily log returns
	n := len(hist.Closes)
	returns := make([]float64, 0, n-1)
	for i := 1; i < n; i++ {
		if hist.Closes[i-1] > 0 && hist.Closes[i] > 0 {
			returns = append(returns, math.Log(hist.Closes[i]/hist.Closes[i-1]))
		}
	}

	if len(returns) < 10 {
		return 1.0
	}

	// Mean and standard deviation
	var sum float64
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(len(returns))

	var sumSq float64
	for _, r := range returns {
		diff := r - mean
		sumSq += diff * diff
	}
	stddev := math.Sqrt(sumSq / float64(len(returns)-1))

	// Annualize (252 trading days)
	return stddev * math.Sqrt(252.0)
}
