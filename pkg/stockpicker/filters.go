package stockpicker

import (
	"context"
	"fmt"
	"sort"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/optimizer"
	"github.com/raghavkgarg/mycase/pkg/selectiontracker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// LoadStrategyConfig loads the weights, filters, and governance from external configurations.
func LoadStrategyConfig(method string) (*StrategyConfig, error) {
	mfsCfg, err := config.LoadMFSConfig("config/mfs.json", method)
	var loadErr error
	if err != nil {
		loadErr = fmt.Errorf("failed to load config/mfs.json: %w", err)
	}

	optWeights := optimizer.MFSWeights{
		Sharpe:           mfsCfg.Sharpe,
		Sortino:          mfsCfg.Sortino,
		Return:           mfsCfg.Return,
		Alpha:            mfsCfg.Alpha,
		Volatility:       mfsCfg.Volatility,
		Beta:             mfsCfg.Beta,
		Treynor:          mfsCfg.Treynor,
		Ulcer:            mfsCfg.Ulcer,
		PEGRatio:         mfsCfg.PEGRatio,
		ROE:              mfsCfg.ROE,
		ForwardPE:        mfsCfg.ForwardPE,
		OperatingMargins: mfsCfg.OperatingMargins,
		PBRatio:          mfsCfg.PBRatio,
		NetDebtEBITDA:    mfsCfg.NetDebtEBITDA,
		MarketCap:        mfsCfg.MarketCap,
		InsidersPercent:  mfsCfg.InsidersPercent,
	}

	hardFilters, err := config.LoadHardFilters("config/mfs.json", method)
	if err != nil && loadErr == nil {
		loadErr = fmt.Errorf("failed to load hard filters from mfs.json: %w", err)
	}

	govMap, govErr := config.LoadGovernance("config/governance.json")
	if govErr != nil {
		fmt.Printf("Warning: Failed to load governance data from governance.json: %v. Using default 0%% pledging.\n", govErr)
		govMap = make(map[string]float64)
	}

	return &StrategyConfig{
		Weights:     optWeights,
		HardFilters: hardFilters,
		Governance:  govMap,
	}, loadErr
}

// InjectGovernance maps promoter pledged percentage values into the fundamentals structure.
func InjectGovernance(fundamentals map[string]yfinance.Fundamentals, govMap map[string]float64) {
	for t, f := range fundamentals {
		pledge := govMap[t] // defaults to 0.0 if not found
		f.PledgedPercent = pledge
		fundamentals[t] = f
	}
}

// getLatestROCE calculates the latest Return on Capital Employed (ROCE).
func getLatestROCE(f *yfinance.Fundamentals) (float64, bool) {
	nIncome := len(f.AnnualOperatingIncome)
	nAssets := len(f.AnnualTotalAssets)
	nLiab := len(f.AnnualCurrentLiabilities)
	if nIncome == 0 || nAssets == 0 || nLiab == 0 {
		return 0.0, false
	}
	latestEBIT := f.AnnualOperatingIncome[nIncome-1].Value
	latestAssets := f.AnnualTotalAssets[nAssets-1].Value
	latestLiab := f.AnnualCurrentLiabilities[nLiab-1].Value
	capEmployed := latestAssets - latestLiab
	if capEmployed <= 0 {
		return 0.0, false
	}
	return latestEBIT / capEmployed, true
}

// checkROCE checks if latest or 3-year average ROCE is at or above a minimum threshold.
func checkROCE(f *yfinance.Fundamentals, minROCE float64) bool {
	if latestROCE, ok := getLatestROCE(f); ok && latestROCE >= minROCE {
		return true
	}
	nIncome := len(f.AnnualOperatingIncome)
	nAssets := len(f.AnnualTotalAssets)
	nLiab := len(f.AnnualCurrentLiabilities)

	var sumROCE float64
	var countROCE int
	for i := 1; i <= 3; i++ {
		idxInc := nIncome - i
		idxAss := nAssets - i
		idxLiab := nLiab - i
		if idxInc >= 0 && idxAss >= 0 && idxLiab >= 0 {
			ebit := f.AnnualOperatingIncome[idxInc].Value
			assets := f.AnnualTotalAssets[idxAss].Value
			liab := f.AnnualCurrentLiabilities[idxLiab].Value
			ce := assets - liab
			if ce > 0 {
				sumROCE += ebit / ce
				countROCE++
			}
		}
	}
	if countROCE > 0 && (sumROCE/float64(countROCE)) >= minROCE {
		return true
	}
	return false
}

// isEligible checks if a ticker constituent passes all safety and fundamental filters.
func isEligible(
	t string,
	f yfinance.Fundamentals,
	method string,
	hardFilters *config.HardFilters,
	closes []float64,
	opens []float64,
	volumes []float64,
	rsPercentiles map[string]float64,
	stats *FilterStats,
) (bool, string) {
	// 1. Size Limit check
	if (hardFilters.MinMarketCap > 0 && f.MarketCap < hardFilters.MinMarketCap) ||
		(hardFilters.MaxMarketCap > 0 && f.MarketCap > hardFilters.MaxMarketCap) {
		stats.EliminatedSize++
		return false, fmt.Sprintf("Market Cap limit check failed (Market Cap: %.0fCr)", f.MarketCap/1e7)
	}

	// 2. Liquidity Limit check
	price := f.RegularPrice
	if price == 0 && len(closes) > 0 {
		price = closes[len(closes)-1]
	}
	adv := f.AverageVolume * price
	if hardFilters.MinADV > 0 && adv < hardFilters.MinADV {
		stats.EliminatedLiquidity++
		return false, fmt.Sprintf("ADV limit check failed (ADV: %.2fCr < %.2fCr limit)", adv/1e7, hardFilters.MinADV/1e7)
	}

	// 3. Cash Flow Quality check
	if hardFilters.MinCFOPAT > 0 || hardFilters.MinFCF != nil {
		if f.OperatingCashflow != 0 || f.FreeCashflow != 0 {
			cfoPatPassed := true
			if f.NetIncome > 0 {
				cfoPatPassed = (f.OperatingCashflow / f.NetIncome) >= hardFilters.MinCFOPAT
			} else {
				cfoPatPassed = false
			}
			fcfFailed := false
			if hardFilters.MinFCF != nil && f.FreeCashflow <= *hardFilters.MinFCF {
				fcfFailed = true
			}
			if f.OperatingCashflow <= 0 || fcfFailed || !cfoPatPassed {
				stats.EliminatedCashFlow++
				return false, "Cash Flow Quality check failed (Operating/Free Cash Flow <= 0, or CFO/PAT ratio low)"
			}
		}
	}

	// 4. Earnings Growth Trend check
	if hardFilters.CheckEarningsTrend {
		if len(f.EarningsHistory) < 2 || f.EarningsHistory[len(f.EarningsHistory)-1].Earnings <= f.EarningsHistory[len(f.EarningsHistory)-2].Earnings {
			stats.EliminatedEarningsTrend++
			return false, "Declining earnings trend (recent earnings fell or no history)"
		}
	}

	// 5. Promoter Stake check
	if hardFilters.MinPromoterPercent > 0 && f.InsidersPercent < hardFilters.MinPromoterPercent {
		stats.EliminatedPromoter++
		return false, fmt.Sprintf("Low promoter stake (%.1f%% < %.1f%% limit)", f.InsidersPercent*100.0, hardFilters.MinPromoterPercent*100.0)
	}

	// 6. 200-Day SMA Trend Check
	if hardFilters.Check200DaySMA && !isAbove200DaySMA(closes) {
		stats.EliminatedSMATrend++
		return false, "Below 200-Day SMA (Downtrend)"
	}

	// 7. Promoter Pledging Check (Indian Governance Trap)
	if hardFilters.MaxPledgedPercent > 0 && f.PledgedPercent >= hardFilters.MaxPledgedPercent {
		stats.EliminatedPledge++
		return false, fmt.Sprintf("High promoter pledging (%.1f%% >= %.1f%% cap)", f.PledgedPercent*100.0, hardFilters.MaxPledgedPercent*100.0)
	}

	// 8. ROCE Capital Efficiency Check
	if hardFilters.MinROCE > 0 {
		if !checkROCE(&f, hardFilters.MinROCE) {
			stats.EliminatedROCE++
			return false, fmt.Sprintf("Low Capital Efficiency (ROCE < %.1f%%)", hardFilters.MinROCE*100.0)
		}
	}

	// 9. Debt-to-Equity Check (Balance Sheet Survivability)
	if hardFilters.MaxDebtToEquity > 0 {
		ratio := f.DebtToEquity / 100.0
		if ratio >= hardFilters.MaxDebtToEquity {
			stats.EliminatedLeverage++
			return false, fmt.Sprintf("High Debt/Equity (%.2f >= %.2f cap)", ratio, hardFilters.MaxDebtToEquity)
		}
	}

	// 10. Interest Coverage Check (Balance Sheet Survivability)
	if hardFilters.MinInterestCoverage > 0 {
		passedCoverage := true
		nIncome := len(f.AnnualOperatingIncome)
		nInt := len(f.AnnualInterestExpense)
		if nIncome > 0 && nInt > 0 {
			latestEBIT := f.AnnualOperatingIncome[nIncome-1].Value
			latestInt := f.AnnualInterestExpense[nInt-1].Value
			if latestInt > 0 {
				coverage := latestEBIT / latestInt
				if coverage < hardFilters.MinInterestCoverage {
					passedCoverage = false
				}
			}
		}
		if !passedCoverage {
			stats.EliminatedInterestCoverage++
			return false, fmt.Sprintf("Low Interest Coverage (ratio < %.1f)", hardFilters.MinInterestCoverage)
		}
	}

	// K. PEG Ratio Check
	if hardFilters.MaxPEG > 0 {
		pegVal := f.PEGRatio
		// Calculate fallback trailing PEG if Yahoo Finance PEG is missing (0 or 99)
		if pegVal == 0 || pegVal == 99.0 {
			cagr := yfinance.CalculateEarningsGrowth(&f)
			if cagr > 0 {
				pe := f.ForwardPE
				if pe > 0 && pe != 999.0 {
					calculatedPeg := pe / (cagr * 100.0)
					if calculatedPeg > 0 {
						pegVal = calculatedPeg
					}
				}
			}
		}
		if pegVal > 0 && pegVal >= hardFilters.MaxPEG {
			stats.EliminatedPEG++
			return false, fmt.Sprintf("High PEG ratio (PEG: %.2f >= %.2f limit)", pegVal, hardFilters.MaxPEG)
		}
	}

	// L. Gross Margin Trajectory Check
	if hardFilters.CheckGrossMargin {
		passedGM, latestGM, prevGM, okGM := yfinance.CalculateGrossMarginTrajectory(&f)
		if okGM {
			if !passedGM {
				stats.EliminatedGrossMargin++
				return false, fmt.Sprintf("Declining Gross Margin (latest: %.1f%% < prev: %.1f%%)", latestGM*100.0, prevGM*100.0)
			}
		} else {
			// Fallback to Operating Margin
			passedOM, latestOM, prevOM, okOM := yfinance.CalculateOperatingMarginTrajectory(&f)
			if okOM {
				if !passedOM {
					stats.EliminatedGrossMargin++
					return false, fmt.Sprintf("Declining Operating Margin fallback (latest: %.1f%% < prev: %.1f%%)", latestOM*100.0, prevOM*100.0)
				}
			} else {
				fmt.Printf("Warning: Missing both Gross Margin and Operating Margin history for %s. Bypassing check.\n", t)
			}
		}
	}

	// M. Relative Strength Percentile Check
	if hardFilters.MinRSPercentile > 0 && rsPercentiles != nil {
		pct, ok := rsPercentiles[t]
		if ok && pct < hardFilters.MinRSPercentile {
			stats.EliminatedRSPercentile++
			return false, fmt.Sprintf("Low 52-Week Relative Strength percentile (%.1f percentile < %.1f threshold)", pct, hardFilters.MinRSPercentile)
		}
	}

	// N. CROIC Check
	if hardFilters.MinCROIC > 0 {
		croic, ok := yfinance.CalculateCROIC(&f)
		if ok {
			if croic < hardFilters.MinCROIC {
				stats.EliminatedCROIC++
				return false, fmt.Sprintf("Low CROIC (%.1f%% < %.1f%% limit)", croic*100.0, hardFilters.MinCROIC*100.0)
			}
		}
	}

	// 11. Special Multibagger strategy filters
	if method == "multibagger" {
		// 1. Sales Growth Accelerator (TTM vs 3Y CAGR)
		passedSales, _, _ := yfinance.CalculateSalesGrowth(&f)

		// 2. Asset Turnover & CapEx Inflection
		_, atPrev, atLatest, pctCapExChange, _ := yfinance.CalculateAssetTurnoverCapEx(&f, hardFilters.MaxCapExYoYMultiplier)
		maxCapExPct := (hardFilters.MaxCapExYoYMultiplier - 1.0) * 100.0
		passedCapEx := pctCapExChange <= maxCapExPct
		if !passedCapEx {
			stats.EliminatedAssetTurnoverCapEx++
			return false, fmt.Sprintf("CapEx YoY reinvestment growth limit exceeded (reinvest multiplier: %.2f)", hardFilters.MaxCapExYoYMultiplier)
		}
		passedAT := atLatest > atPrev

		// 3. Working Capital Efficiency (DSO)
		passedWC, _, _ := yfinance.CalculateDSO(&f)

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

		if passCount < 2 {
			stats.EliminatedSalesAccelerator++
			return false, fmt.Sprintf("Operational Criteria failed: only %d/3 criteria met (Sales Acceleration: %t, Asset Turnover expansion: %t, DSO improvement: %t)", passCount, passedSales, passedAT, passedWC)
		}

		// RSI & Stage Analysis (Volume Breakouts)
		if !yfinance.CheckVolumeBreakout(closes, opens, volumes, hardFilters.VolumeBreakoutLookbackDays, hardFilters.VolumeBreakoutMultiplier) {
			stats.EliminatedVolumeBreakout++
			return false, "Volume Breakout check failed (no volume expansion or breakout detected)"
		}
	}

	return true, ""
}

// ApplySafetyFilters filters out companies based on safety/fundamental thresholds.
func ApplySafetyFilters(
	ctx context.Context,
	activeKeys []string,
	method string,
	hardFilters *config.HardFilters,
	fundamentals map[string]yfinance.Fundamentals,
	fullHistory map[string]*yfinance.HistoricalData,
	tracker *selectiontracker.Tracker,
) []string {
	var filteredKeys []string
	var stats FilterStats

	tracker.InitialCount = len(activeKeys)

	// Pre-calculate 52-Week Relative Strength percentiles if requested
	rsPercentiles := make(map[string]float64)
	if hardFilters.MinRSPercentile > 0 {
		benchmark1y, bErr := yfinance.FetchHistoricalPrices(ctx, "^NSEI", "1y")
		bench1yReturn := 0.0
		if bErr == nil && len(benchmark1y) >= 2 {
			bench1yReturn = (benchmark1y[len(benchmark1y)-1] - benchmark1y[0]) / benchmark1y[0]
		}

		type rsVal struct {
			ticker string
			rs     float64
		}
		var rsList []rsVal
		for _, t := range activeKeys {
			hist, ok := fullHistory[t]
			if !ok || len(hist.Closes) < 2 {
				continue
			}
			stock1yReturn := (hist.Closes[len(hist.Closes)-1] - hist.Closes[0]) / hist.Closes[0]
			rs := stock1yReturn - bench1yReturn
			rsList = append(rsList, rsVal{ticker: t, rs: rs})
		}

		sort.Slice(rsList, func(i, j int) bool {
			return rsList[i].rs < rsList[j].rs
		})

		totalRS := len(rsList)
		for i, item := range rsList {
			pct := 100.0
			if totalRS > 1 {
				pct = (float64(i) / float64(totalRS-1)) * 100.0
			}
			rsPercentiles[item.ticker] = pct
		}
	}

	for _, t := range activeKeys {
		f, ok := fundamentals[t]
		if !ok {
			tracker.RecordSafetyDrop(t, "Missing fundamental data")
			continue
		}
		hist := fullHistory[t]
		eligible, reason := isEligible(t, f, method, hardFilters, hist.Closes, hist.Opens, hist.Volumes, rsPercentiles, &stats)
		if eligible {
			filteredKeys = append(filteredKeys, t)
		} else {
			tracker.RecordSafetyDrop(t, reason)
		}
	}

	PrintSafetyFilterSummary(hardFilters, stats, method, len(filteredKeys), len(activeKeys))
	return filteredKeys
}

// isAbove200DaySMA calculates whether the latest closing price is above the 200-day simple moving average.
func isAbove200DaySMA(prices []float64) bool {
	if len(prices) < 200 {
		return true // not enough data to check, bypass safety filter
	}
	sum := 0.0
	startIndex := len(prices) - 200
	for i := startIndex; i < len(prices); i++ {
		sum += prices[i]
	}
	sma200 := sum / 200.0
	latestPrice := prices[len(prices)-1]
	return latestPrice >= sma200
}
