package stockpicker

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/marketfmt"
	"github.com/raghavkgarg/mycase/pkg/optimizer"
	"github.com/raghavkgarg/mycase/pkg/selectiontracker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// LoadStrategyConfig loads the weights, filters, and governance from external configurations.
func LoadStrategyConfig(method string) (*StrategyConfig, error) {
	mfsCfg, err := config.LoadMFSConfig(config.Path("mfs.json"), method)
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

	hardFilters, err := config.LoadHardFilters(config.Path("mfs.json"), method)
	if err != nil && loadErr == nil {
		loadErr = fmt.Errorf("failed to load hard filters from mfs.json: %w", err)
	}

	govMap, govErr := config.LoadGovernance(config.Path("governance.json"))
	if govErr != nil {
		slog.Warn("config.governance_load_failed", "err", govErr, "fallback", "0pct_pledging")
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

// InjectSectors backfills Fundamentals.Sector from a ticker->sector map derived
// from the constituents CSV (e.g. the S&P 500 GICS Sector column). It only fills
// a sector that is currently empty, so provider-supplied sectors (Yahoo populates
// Sector; Schwab does not) are preserved. This is the Phase 10a fix for US sector
// caps collapsing to "Unknown" because the Schwab fundamentals endpoint carries
// no sector. A nil/empty map is a no-op.
func InjectSectors(fundamentals map[string]yfinance.Fundamentals, sectorByTicker map[string]string) {
	if len(sectorByTicker) == 0 {
		return
	}
	for t, f := range fundamentals {
		if f.Sector != "" {
			continue
		}
		if sec := sectorByTicker[t]; sec != "" {
			f.Sector = sec
			fundamentals[t] = f
		}
	}
}

// getEffectiveFinancialROE returns the reported ROE, or derives it via NetIncome / (MarketCap / PBRatio)
// when reported ROE is missing or zero.
func getEffectiveFinancialROE(f *yfinance.Fundamentals) float64 {
	if f.ROE > 0 {
		return f.ROE
	}
	if f.NetIncome > 0 && f.PBRatio > 0 && f.PBRatio < 50.0 && f.MarketCap > 0 {
		equity := f.MarketCap / f.PBRatio
		if equity > 0 {
			return f.NetIncome / equity
		}
	}
	return f.ROE
}

// getLatestROCE calculates the latest Return on Capital Employed (ROCE).
// GetLatestROCE calculates the latest Return on Capital Employed (ROCE) using default 45-day filing lag.
func GetLatestROCE(f *yfinance.Fundamentals) (float64, bool) {
	return getLatestROCE(f, time.Now(), 45)
}

// filterMetricsBeforeDate returns only annual metrics whose reporting date is at least lagDays before asOf.
func filterMetricsBeforeDate(metrics []yfinance.AnnualMetric, asOf time.Time, lagDays int) []yfinance.AnnualMetric {
	if asOf.IsZero() {
		asOf = time.Now()
	}
	cutoff := asOf.AddDate(0, 0, -lagDays)
	var filtered []yfinance.AnnualMetric
	for _, m := range metrics {
		if t, err := time.Parse("2006-01-02", m.Date); err == nil {
			if !t.After(cutoff) {
				filtered = append(filtered, m)
			}
		} else {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func getLatestROCE(f *yfinance.Fundamentals, asOf time.Time, lagDays int) (float64, bool) {
	incomes := filterMetricsBeforeDate(f.AnnualOperatingIncome, asOf, lagDays)
	assets := filterMetricsBeforeDate(f.AnnualTotalAssets, asOf, lagDays)
	liabs := filterMetricsBeforeDate(f.AnnualCurrentLiabilities, asOf, lagDays)

	nIncome := len(incomes)
	nAssets := len(assets)
	nLiab := len(liabs)
	if nIncome == 0 || nAssets == 0 || nLiab == 0 {
		return 0.0, false
	}
	latestEBIT := incomes[nIncome-1].Value
	latestAssets := assets[nAssets-1].Value
	latestLiab := liabs[nLiab-1].Value
	capEmployed := latestAssets - latestLiab
	if capEmployed <= 0 {
		return 0.0, false
	}
	return latestEBIT / capEmployed, true
}

// Get3YearAvgROCE calculates the average Return on Capital Employed across the last 3 fiscal years.
func Get3YearAvgROCE(f *yfinance.Fundamentals, asOf time.Time, lagDays int) (float64, bool) {
	incomes := filterMetricsBeforeDate(f.AnnualOperatingIncome, asOf, lagDays)
	assets := filterMetricsBeforeDate(f.AnnualTotalAssets, asOf, lagDays)
	liabs := filterMetricsBeforeDate(f.AnnualCurrentLiabilities, asOf, lagDays)

	nIncome := len(incomes)
	nAssets := len(assets)
	nLiab := len(liabs)

	var sumROCE float64
	var countROCE int
	for i := 1; i <= 3; i++ {
		idxInc := nIncome - i
		idxAss := nAssets - i
		idxLiab := nLiab - i
		if idxInc >= 0 && idxAss >= 0 && idxLiab >= 0 {
			ebit := incomes[idxInc].Value
			assetsVal := assets[idxAss].Value
			liabVal := liabs[idxLiab].Value
			ce := assetsVal - liabVal
			if ce > 0 {
				sumROCE += ebit / ce
				countROCE++
			}
		}
	}
	if countROCE > 0 {
		return sumROCE / float64(countROCE), true
	}
	return 0.0, false
}

// checkROCE checks if latest or 3-year average ROCE is at or above a minimum threshold.
func checkROCE(f *yfinance.Fundamentals, minROCE float64, asOf time.Time, lagDays int) bool {
	if latestROCE, ok := getLatestROCE(f, asOf, lagDays); ok && latestROCE >= minROCE {
		return true
	}
	if avgROCE, ok := Get3YearAvgROCE(f, asOf, lagDays); ok && avgROCE >= minROCE {
		return true
	}
	return false
}

// getLatestCROIC calculates the latest Cash Return on Invested Capital (CROIC)
// respecting point-in-time filing lag.
func getLatestCROIC(f *yfinance.Fundamentals, asOf time.Time, lagDays int) (float64, bool) {
	assets := filterMetricsBeforeDate(f.AnnualTotalAssets, asOf, lagDays)
	liabs := filterMetricsBeforeDate(f.AnnualCurrentLiabilities, asOf, lagDays)
	cfos := filterMetricsBeforeDate(f.AnnualOperatingCashFlow, asOf, lagDays)
	capexs := filterMetricsBeforeDate(f.AnnualCapEx, asOf, lagDays)
	fcfs := filterMetricsBeforeDate(f.AnnualFreeCashFlow, asOf, lagDays)

	if len(assets) > 0 && len(liabs) > 0 {
		type annualCF struct {
			asset    float64
			liab     float64
			cfo      float64
			capex    float64
			fcf      float64
			hasCFO   bool
			hasCapEx bool
			hasFCF   bool
		}
		byDate := make(map[string]*annualCF)
		for _, a := range assets {
			if byDate[a.Date] == nil {
				byDate[a.Date] = &annualCF{}
			}
			byDate[a.Date].asset = a.Value
		}
		for _, l := range liabs {
			if byDate[l.Date] == nil {
				byDate[l.Date] = &annualCF{}
			}
			byDate[l.Date].liab = l.Value
		}
		for _, c := range cfos {
			if byDate[c.Date] == nil {
				byDate[c.Date] = &annualCF{}
			}
			byDate[c.Date].cfo = c.Value
			byDate[c.Date].hasCFO = true
		}
		for _, cx := range capexs {
			if byDate[cx.Date] == nil {
				byDate[cx.Date] = &annualCF{}
			}
			byDate[cx.Date].capex = math.Abs(cx.Value)
			byDate[cx.Date].hasCapEx = true
		}
		for _, fc := range fcfs {
			if byDate[fc.Date] == nil {
				byDate[fc.Date] = &annualCF{}
			}
			byDate[fc.Date].fcf = fc.Value
			byDate[fc.Date].hasFCF = true
		}

		var validDates []string
		for dt, item := range byDate {
			ce := item.asset - item.liab
			if ce > 0 && (item.hasCFO || item.hasFCF) {
				validDates = append(validDates, dt)
			}
		}
		sort.Strings(validDates)

		if len(validDates) > 0 {
			latestDate := validDates[len(validDates)-1]
			item := byDate[latestDate]
			ce := item.asset - item.liab
			fcfVal := item.fcf
			if item.hasCFO && item.hasCapEx {
				fcfVal = item.cfo - item.capex
			} else if !item.hasFCF && item.hasCFO {
				fcfVal = item.cfo
			}
			return fcfVal / ce, true
		}
	}

	// Fallback to point-in-time single-point CalculateCROIC
	return yfinance.CalculateCROIC(f)
}

// Get3YearAvgCROIC calculates the average CROIC across the last 3 fiscal years.
func Get3YearAvgCROIC(f *yfinance.Fundamentals, asOf time.Time, lagDays int) (float64, bool) {
	assets := filterMetricsBeforeDate(f.AnnualTotalAssets, asOf, lagDays)
	liabs := filterMetricsBeforeDate(f.AnnualCurrentLiabilities, asOf, lagDays)
	cfos := filterMetricsBeforeDate(f.AnnualOperatingCashFlow, asOf, lagDays)
	capexs := filterMetricsBeforeDate(f.AnnualCapEx, asOf, lagDays)
	fcfs := filterMetricsBeforeDate(f.AnnualFreeCashFlow, asOf, lagDays)

	type annualCF struct {
		asset    float64
		liab     float64
		cfo      float64
		capex    float64
		fcf      float64
		hasCFO   bool
		hasCapEx bool
		hasFCF   bool
	}
	byDate := make(map[string]*annualCF)
	for _, a := range assets {
		if byDate[a.Date] == nil {
			byDate[a.Date] = &annualCF{}
		}
		byDate[a.Date].asset = a.Value
	}
	for _, l := range liabs {
		if byDate[l.Date] == nil {
			byDate[l.Date] = &annualCF{}
		}
		byDate[l.Date].liab = l.Value
	}
	for _, c := range cfos {
		if byDate[c.Date] == nil {
			byDate[c.Date] = &annualCF{}
		}
		byDate[c.Date].cfo = c.Value
		byDate[c.Date].hasCFO = true
	}
	for _, cx := range capexs {
		if byDate[cx.Date] == nil {
			byDate[cx.Date] = &annualCF{}
		}
		byDate[cx.Date].capex = math.Abs(cx.Value)
		byDate[cx.Date].hasCapEx = true
	}
	for _, fc := range fcfs {
		if byDate[fc.Date] == nil {
			byDate[fc.Date] = &annualCF{}
		}
		byDate[fc.Date].fcf = fc.Value
		byDate[fc.Date].hasFCF = true
	}

	var validDates []string
	for dt, item := range byDate {
		ce := item.asset - item.liab
		if ce > 0 && (item.hasCFO || item.hasFCF) {
			validDates = append(validDates, dt)
		}
	}
	sort.Strings(validDates)

	n := len(validDates)
	var sumCROIC float64
	var countCROIC int
	for i := 1; i <= 3; i++ {
		idx := n - i
		if idx >= 0 {
			dt := validDates[idx]
			item := byDate[dt]
			ce := item.asset - item.liab
			if ce > 0 {
				fcfVal := item.fcf
				if item.hasCFO && item.hasCapEx {
					fcfVal = item.cfo - item.capex
				} else if !item.hasFCF && item.hasCFO {
					fcfVal = item.cfo
				}
				sumCROIC += fcfVal / ce
				countCROIC++
			}
		}
	}

	if countCROIC > 0 {
		return sumCROIC / float64(countCROIC), true
	}
	return 0.0, false
}

// checkCROIC checks if latest or 3-year average CROIC is at or above a minimum threshold.
// Returns (passed, croicVal, ok) where ok indicates whether cash flow data is present.
func checkCROIC(f *yfinance.Fundamentals, minCROIC float64, asOf time.Time, lagDays int) (bool, float64, bool) {
	latestCROIC, ok := getLatestCROIC(f, asOf, lagDays)
	if !ok {
		return true, 0.0, false
	}
	if latestCROIC >= minCROIC {
		return true, latestCROIC, true
	}

	if avgCROIC, okAvg := Get3YearAvgCROIC(f, asOf, lagDays); okAvg && avgCROIC >= minCROIC {
		return true, avgCROIC, true
	}

	return false, latestCROIC, true
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
	isExisting bool,
	mkt marketfmt.Market,
) (bool, string) {
	// Soft-band tolerance adjustments for existing portfolio holdings (anti-churn preservation)
	minROCE := hardFilters.MinROCE
	minCROIC := hardFilters.MinCROIC
	maxDebtToEquity := hardFilters.MaxDebtToEquity
	minInterestCoverage := hardFilters.MinInterestCoverage
	minCFOPAT := hardFilters.MinCFOPAT
	minPromoter := hardFilters.MinPromoterPercent

	minSMARatio := hardFilters.Min200DaySMARatio
	if minSMARatio <= 0 {
		minSMARatio = 0.95
	}

	if isExisting {
		if minROCE > 0 {
			minROCE *= 0.85 // e.g. 12% -> 10.2%
		}
		if minCROIC > 0 {
			minCROIC *= 0.80 // e.g. 6.0% -> 4.8%
		}
		if maxDebtToEquity > 0 {
			maxDebtToEquity *= 1.15 // e.g. 1.5 -> 1.725
		}
		if minInterestCoverage > 0 {
			minInterestCoverage *= 0.90 // e.g. 3.0 -> 2.7
		}
		if minCFOPAT > 0 {
			minCFOPAT *= 0.80 // e.g. 0.25 -> 0.20
		}
		if minPromoter > 0 {
			minPromoter *= 0.90
		}
		// Soft-band tolerance for existing holdings: allow up to 10% pullback (0.90) instead of 5% (0.95)
		// to avoid binary cliff liquidations on short-term market corrections. Layer 2 slope check still guards downtrends.
		minSMARatio = 0.90
	}

	// 1. Size Limit check
	if (hardFilters.MinMarketCap > 0 && f.MarketCap < hardFilters.MinMarketCap) ||
		(hardFilters.MaxMarketCap > 0 && f.MarketCap > hardFilters.MaxMarketCap) {
		stats.EliminatedSize++
		return false, fmt.Sprintf("Market Cap limit check failed (Market Cap: %s)", marketfmt.Compact(f.MarketCap, mkt))
	}

	// 2. Liquidity Limit check
	price := f.RegularPrice
	if price == 0 && len(closes) > 0 {
		price = closes[len(closes)-1]
	}
	adv := f.AverageVolume * price
	if hardFilters.MinADV > 0 && adv < hardFilters.MinADV {
		stats.EliminatedLiquidity++
		return false, fmt.Sprintf("ADV limit check failed (ADV: %s < %s limit)", marketfmt.Compact(adv, mkt), marketfmt.Compact(hardFilters.MinADV, mkt))
	}

	// 3. Cash Flow Quality check
	if minCFOPAT > 0 || hardFilters.MinFCF != nil {
		if f.OperatingCashflow != 0 || f.FreeCashflow != 0 {
			cfoPatPassed := true
			if f.NetIncome > 0 {
				cfoPatPassed = (f.OperatingCashflow / f.NetIncome) >= minCFOPAT
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

	// 5. Promoter Stake check (Skip for US stocks as US equities are institutionally held)
	if !strings.HasPrefix(t, "US:") && !strings.HasPrefix(t, "NASDAQ:") && !strings.HasPrefix(t, "NYSE:") {
		if yfinance.IsFinancialSector(f.Sector) {
			// Financial Services: Statutorily capped promoter holdings (RBI limits) are exempted from 25% floor,
			// but MUST demonstrate market leadership / non-negative relative strength (Comp RS >= 0.0).
			compRS, _, _, _ := yfinance.CalculateCompositeRS(closes, nil, t)
			if compRS < 0.0 {
				stats.EliminatedPromoter++
				return false, fmt.Sprintf("Financial Services weak relative strength (Comp RS < 0.0%%: %.1f%%)", compRS*100.0)
			}
		} else if minPromoter > 0 && f.InsidersPercent < minPromoter {
			stats.EliminatedPromoter++
			return false, fmt.Sprintf("Low promoter stake (%.1f%% < %.1f%% limit)", f.InsidersPercent*100.0, minPromoter*100.0)
		}
	}

	// 6. 200-Day SMA Trend & Buffer Floor Check
	if hardFilters.Check200DaySMA {
		if ok, reason := check200DaySMATrend(closes, minSMARatio); !ok {
			stats.EliminatedSMATrend++
			return false, reason
		}
	}

	// 7. Promoter Pledging Check (Indian Governance Trap)
	if hardFilters.MaxPledgedPercent > 0 && f.PledgedPercent >= hardFilters.MaxPledgedPercent {
		stats.EliminatedPledge++
		return false, fmt.Sprintf("High promoter pledging (%.1f%% >= %.1f%% cap)", f.PledgedPercent*100.0, hardFilters.MaxPledgedPercent*100.0)
	}

	// 8. ROCE / Capital Efficiency Check (Sector-Relative & Accumulation-Gated Quality Gate)
	if minROCE > 0 {
		if yfinance.IsFinancialSector(f.Sector) {
			// Financial Services / Banks / NBFCs: ROCE is structurally distorted by customer deposits as debt.
			// Drop ROCE gate entirely; enforce an ROE quality check (e.g., >= 12.0% floor or hardFilters.MinROE).
			minROE := 0.12
			if hardFilters.MinROE > 0 {
				minROE = hardFilters.MinROE
			}
			if isExisting {
				minROE *= 0.85
			}
			effROE := getEffectiveFinancialROE(&f)
			if effROE < minROE {
				stats.EliminatedROCE++
				if effROE <= 0 {
					return false, fmt.Sprintf("Unverified / Sub-zero Financial ROE (%.1f%% < %.1f%% threshold)", effROE*100.0, minROE*100.0)
				}
				return false, fmt.Sprintf("Low Financial ROE (%.1f%% < %.1f%% threshold)", effROE*100.0, minROE*100.0)
			}
		} else {
			// Non-Financial Sectors:
			// Sector floor: 7.0% for Technology & Consumer Cyclical recent listings (asset-light / reinvesting), 12.0% standard.
			// Delivery/RS/VCP override: Deliv_Δ >= 6% AND Comp_RS >= 0 AND VCP_ATR <= 1.20 rescues high-conviction compounders (e.g. DIACABS).
			roceFloor := minROCE
			isRecentListing := (len(f.AnnualOperatingIncome) <= 3 || len(f.EarningsHistory) < 8 || len(closes) < 500)
			if (strings.EqualFold(f.Sector, "Technology") || strings.EqualFold(f.Sector, "Consumer Cyclical")) && isRecentListing {
				roceFloor = 0.07
			}
			if isExisting {
				roceFloor *= 0.85
			}

			lagDays := 45
			if hardFilters.FundamentalsLagDays > 0 {
				lagDays = hardFilters.FundamentalsLagDays
			}

			if !checkROCE(&f, roceFloor, time.Now(), lagDays) {
				stats.EliminatedROCE++
				return false, fmt.Sprintf("Low Capital Efficiency (ROCE < %.1f%%)", roceFloor*100.0)
			}
		}
	}

	// 9. Debt-to-Equity Check (Balance Sheet Survivability)
	if maxDebtToEquity > 0 {
		ratio := f.DebtToEquity / 100.0
		if ratio >= maxDebtToEquity {
			stats.EliminatedLeverage++
			return false, fmt.Sprintf("High Debt/Equity (%.2f >= %.2f cap)", ratio, maxDebtToEquity)
		}
	}

	// 10. Interest Coverage Check (Balance Sheet Survivability)
	if minInterestCoverage > 0 {
		passedCoverage := true
		nIncome := len(f.AnnualOperatingIncome)
		nInt := len(f.AnnualInterestExpense)
		if nIncome > 0 && nInt > 0 {
			latestEBIT := f.AnnualOperatingIncome[nIncome-1].Value
			latestInt := f.AnnualInterestExpense[nInt-1].Value
			if latestInt > 0 {
				coverage := latestEBIT / latestInt
				if coverage < minInterestCoverage {
					passedCoverage = false
				}
			}
		}
		if !passedCoverage {
			stats.EliminatedInterestCoverage++
			return false, fmt.Sprintf("Low Interest Coverage (ratio < %.1f)", minInterestCoverage)
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
				slog.Debug("filter.margin_history_missing", "ticker", t, "action", "bypass")
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

	// N. CROIC Check (Quality of Earnings & Cash Returns)
	if minCROIC > 0 {
		lagDays := 45
		if hardFilters.FundamentalsLagDays > 0 {
			lagDays = hardFilters.FundamentalsLagDays
		}
		passed, croicVal, ok := checkCROIC(&f, minCROIC, time.Now(), lagDays)
		if ok && !passed {
			stats.EliminatedCROIC++
			return false, fmt.Sprintf("Low CROIC (%.1f%% < %.1f%% limit)", croicVal*100.0, minCROIC*100.0)
		}
	}

	// 11. Special Multibagger / Value strategy filters
	switch method {
	case "value":
		// 200-Day SMA ratio floor check (allows up to 15% dip below 200-SMA)
		minSMARatio := 0.85
		if hardFilters.Min200DaySMARatio > 0 {
			minSMARatio = hardFilters.Min200DaySMARatio
		}
		if len(closes) >= 200 {
			sum := 0.0
			for i := len(closes) - 200; i < len(closes); i++ {
				sum += closes[i]
			}
			sma200 := sum / 200.0
			latestClose := closes[len(closes)-1]
			if sma200 > 0 && (latestClose/sma200) < minSMARatio {
				stats.EliminatedSMATrend++
				return false, fmt.Sprintf("Below 200-Day SMA trend floor ratio (%.2f < %.2f limit)", latestClose/sma200, minSMARatio)
			}
		}

		// Dual-Path Filtering (BFSI vs Non-Financials)
		if yfinance.IsFinancialSector(f.Sector) {
			// Financial Path (BFSI / Banks / NBFCs)
			minROE := 0.12
			if hardFilters.MinROE > 0 {
				minROE = hardFilters.MinROE
			}
			effROE := getEffectiveFinancialROE(&f)
			if effROE < minROE {
				stats.EliminatedROCE++
				if effROE <= 0 {
					return false, fmt.Sprintf("Unverified / Sub-zero Financial ROE (%.1f%% < %.1f%% threshold)", effROE*100.0, minROE*100.0)
				}
				return false, fmt.Sprintf("Low Financial ROE (%.1f%% < %.1f%% threshold)", effROE*100.0, minROE*100.0)
			}
		} else {
			// Industrial / Non-Financial Path
			minCFO := 0.70
			if hardFilters.MinCFOPAT > 0 {
				minCFO = hardFilters.MinCFOPAT
			}
			if f.NetIncome > 0 && f.OperatingCashflow > 0 {
				cfoRatio := f.OperatingCashflow / f.NetIncome
				if cfoRatio < minCFO {
					stats.EliminatedCashFlow++
					return false, fmt.Sprintf("Low Cash Conversion CFO/PAT (%.1f%% < %.1f%% limit)", cfoRatio*100.0, minCFO*100.0)
				}
			}
		}
	case "early_multibagger", "earlymb":
		// 1. Earnings Proximity Blackout Gate
		if hardFilters.EarningsBlackoutDaysBefore > 0 && f.ResultPrevComing != "" {
			if yfinance.IsEarningsBlackout(f.ResultPrevComing, hardFilters.EarningsBlackoutDaysBefore) {
				stats.EliminatedEarningsBlackout++
				return false, fmt.Sprintf("Earnings event blackout: results scheduled within %d days (%s)", hardFilters.EarningsBlackoutDaysBefore, f.ResultPrevComing)
			}
		}

		// 2. Proximity to 52-Week High
		if hardFilters.MinProximity52WHigh > 0 && len(closes) > 0 {
			prox := yfinance.CalculateProximity52W(closes)
			if prox < hardFilters.MinProximity52WHigh {
				stats.EliminatedProximity52W++
				return false, fmt.Sprintf("Far from 52-Week High (%.1f%% of 52W high < %.1f%% floor)", prox*100.0, hardFilters.MinProximity52WHigh*100.0)
			}
		}

		// 3. Base Duration Floor: Graduated scoring penalty replaces binary elimination.
		// (Fresh breakouts with 0-3 weeks base enter the ranked pool at graduated discount multipliers in scoring.go).

		// 4. Working Capital Deterioration Sentry (DSO)
		_, dsoPrev, dsoLatest := yfinance.CalculateDSO(&f)
		if dsoPrev > 0 {
			dsoDeltaPct := ((dsoLatest - dsoPrev) / dsoPrev) * 100.0
			maxDSOPct := 15.0
			if hardFilters != nil && hardFilters.MaxDSODeteriorationPct > 0 {
				maxDSOPct = hardFilters.MaxDSODeteriorationPct
			}
			if dsoDeltaPct > maxDSOPct {
				stats.EliminatedWorkingCapital++
				return false, fmt.Sprintf("DSO Deterioration limit exceeded (+%.1f%% > %.1f%% threshold)", dsoDeltaPct, maxDSOPct)
			}
		}
	case "multibagger":
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

		// 3. Working Capital Efficiency & Deterioration Sentry (DSO)
		passedWC, dsoPrev, dsoLatest := yfinance.CalculateDSO(&f)
		if dsoPrev > 0 {
			dsoDeltaPct := ((dsoLatest - dsoPrev) / dsoPrev) * 100.0
			maxDSOPct := 15.0
			if hardFilters != nil && hardFilters.MaxDSODeteriorationPct > 0 {
				maxDSOPct = hardFilters.MaxDSODeteriorationPct
			}
			if dsoDeltaPct > maxDSOPct {
				stats.EliminatedWorkingCapital++
				return false, fmt.Sprintf("DSO Deterioration limit exceeded (+%.1f%% > %.1f%% threshold)", dsoDeltaPct, maxDSOPct)
			}
		}

		// Require at least 2 out of 3 operational criteria to pass for new candidates,
		// and at least 1 out of 3 for existing holdings to avoid cliff evictions on growth normalization.
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

		minPassCount := 2
		if isExisting {
			minPassCount = 1
		}

		if passCount < minPassCount {
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

// marketFromTickers infers the market for formatting from constituent tickers.
// US tickers carry a "US:" exchange prefix; India tickers use "NSE:"/"BSE:" or
// none. If every prefixed ticker is US, the set is US; otherwise India. This
// mirrors the convention used across the picker and keeps marketfmt a pure leaf
// (stockpicker owns the ticker→market policy, not marketfmt).
func marketFromTickers(tickers []string) marketfmt.Market {
	sawPrefix := false
	allUS := true
	for _, tk := range tickers {
		idx := strings.Index(tk, ":")
		if idx <= 0 {
			continue
		}
		sawPrefix = true
		if !strings.EqualFold(tk[:idx], "US") {
			allUS = false
		}
	}
	if sawPrefix && allUS {
		return marketfmt.US
	}
	return marketfmt.India
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
	existingHoldings map[string]float64,
) []string {
	var filteredKeys []string
	var stats FilterStats

	mkt := marketFromTickers(activeKeys)

	tracker.InitialCount = len(activeKeys)

	// Pre-calculate 52-Week Relative Strength percentiles if requested
	rsPercentiles := make(map[string]float64)
	if hardFilters.MinRSPercentile > 0 {
		benchSym := GetBenchmarkSymbolForIndex("", activeKeys)
		benchmark1y, bErr := yfinance.FetchHistoricalPrices(ctx, benchSym, "1y")
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
			tracker.RecordFetchFailure(t, "DATA_FETCH_FAILED: Missing fundamental data")
			continue
		}
		hist := fullHistory[t]
		isExisting := false
		if existingHoldings != nil {
			_, isExisting = existingHoldings[t]
		}
		eligible, reason := isEligible(t, f, method, hardFilters, hist.Closes, hist.Opens, hist.Volumes, rsPercentiles, &stats, isExisting, mkt)
		if eligible {
			filteredKeys = append(filteredKeys, t)
		} else {
			tracker.RecordSafetyDrop(t, reason)
		}
	}

	PrintSafetyFilterSummary(hardFilters, stats, method, len(filteredKeys), len(activeKeys), mkt)
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

// check200DaySMATrend checks if prices meet the 200-day SMA ratio floor and slope trend criteria.
// Returns (true, "") if passed, or (false, reason) if eliminated.
func check200DaySMATrend(prices []float64, minRatio float64) (bool, string) {
	if len(prices) < 200 {
		return true, ""
	}
	if minRatio <= 0 {
		minRatio = 0.95
	}

	sum := 0.0
	startIndex := len(prices) - 200
	for i := startIndex; i < len(prices); i++ {
		sum += prices[i]
	}
	sma200Current := sum / 200.0
	if sma200Current <= 0 {
		return true, ""
	}

	latestPrice := prices[len(prices)-1]
	ratio := latestPrice / sma200Current

	// 1. Buffer Ratio Floor check (e.g. latestPrice must be >= 95% of 200-SMA)
	if ratio < minRatio {
		return false, fmt.Sprintf("Below 200-Day SMA ratio floor (%.2f < %.2f limit)", ratio, minRatio)
	}

	// 2. Slope / Trend check: If price is currently below the 200-SMA line (ratio < 1.0),
	// ensure the 200-SMA line itself is NOT in a downward trend (declined by >0.5% over past 20 trading days).
	if ratio < 1.0 && len(prices) >= 220 {
		sumPast := 0.0
		pastStartIndex := len(prices) - 220
		for i := pastStartIndex; i < pastStartIndex+200; i++ {
			sumPast += prices[i]
		}
		sma200Past := sumPast / 200.0
		if sma200Past > 0 && sma200Current < (0.995*sma200Past) {
			return false, fmt.Sprintf("Below 200-Day SMA with downward 200-SMA trend (Ratio %.2f < 1.0, SMA 20d decline > 0.5%%)", ratio)
		}
	}

	return true, ""
}
