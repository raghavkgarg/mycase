package stockpicker

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/selectiontracker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// ScoreFairPrice evaluates and ranks candidates using the 100-point fair price matrix.
func ScoreFairPrice(
	ctx context.Context,
	activeKeys []string,
	fundamentals map[string]yfinance.Fundamentals,
	fairPrices map[string]*yfinance.FairPriceResult,
	hardFilters *config.HardFilters,
) map[string]float64 {
	slog.InfoContext(ctx, "score.fairprice_start", "candidates", len(activeKeys))
	scores := make(map[string]float64)

	// Collect metrics for normalization
	upsides := make(map[string]float64)
	roces := make(map[string]float64)
	cfos := make(map[string]float64)
	des := make(map[string]float64)
	revCAGRs := make(map[string]float64)

	minUpside, maxUpside := math.MaxFloat64, -math.MaxFloat64
	minROCE, maxROCE := math.MaxFloat64, -math.MaxFloat64
	minCFO, maxCFO := math.MaxFloat64, -math.MaxFloat64
	minDE, maxDE := math.MaxFloat64, -math.MaxFloat64
	minRev, maxRev := math.MaxFloat64, -math.MaxFloat64

	for _, t := range activeKeys {
		fp := fairPrices[t]
		if fp == nil || fp.ValidModelCount < 2 {
			continue
		}
		f := fundamentals[t]

		upsides[t] = fp.UpsidePct
		if fp.UpsidePct < minUpside {
			minUpside = fp.UpsidePct
		}
		if fp.UpsidePct > maxUpside {
			maxUpside = fp.UpsidePct
		}

		var roceVal float64
		if f.Sector == "Financial Services" {
			if f.ROE > 0 {
				roceVal = f.ROE
			} else if f.ReturnOnAssets > 0 {
				roceVal = f.ReturnOnAssets * 10.0
			}
		} else {
			var ok bool
			roceVal, ok = GetLatestROCE(&f)
			if !ok || roceVal <= 0 {
				roceVal = f.ROE
			}
		}
		roces[t] = roceVal
		if roceVal < minROCE {
			minROCE = roceVal
		}
		if roceVal > maxROCE {
			maxROCE = roceVal
		}

		cfoPat := 0.0
		if f.NetIncome > 0 && f.OperatingCashflow > 0 {
			cfoPat = f.OperatingCashflow / f.NetIncome
		}
		cfos[t] = cfoPat
		if cfoPat < minCFO {
			minCFO = cfoPat
		}
		if cfoPat > maxCFO {
			maxCFO = cfoPat
		}

		de := f.DebtToEquity
		if de > 5.0 {
			de /= 100.0
		}
		des[t] = de
		if de < minDE {
			minDE = de
		}
		if de > maxDE {
			maxDE = de
		}

		_, ttmGrowth, cagr3y := yfinance.CalculateSalesGrowth(&f)
		revGrowth := cagr3y
		if revGrowth <= 0 {
			revGrowth = ttmGrowth
		}
		revCAGRs[t] = revGrowth
		if revGrowth < minRev {
			minRev = revGrowth
		}
		if revGrowth > maxRev {
			maxRev = revGrowth
		}
	}

	wUpside := 25.0
	wMOS := 15.0
	wAgreement := 10.0
	wConvergence := 10.0
	wROCE := 10.0
	wCFOPAT := 8.0
	wDebtSafety := 7.0
	wRevCAGR := 8.0
	wEarningsAccel := 7.0

	if hardFilters != nil {
		if hardFilters.ScoreWeightUpside > 0 {
			wUpside = hardFilters.ScoreWeightUpside
		}
		if hardFilters.ScoreWeightMOSBand > 0 {
			wMOS = hardFilters.ScoreWeightMOSBand
		}
		if hardFilters.ScoreWeightAgreement > 0 {
			wAgreement = hardFilters.ScoreWeightAgreement
		}
		if hardFilters.ScoreWeightConvergence > 0 {
			wConvergence = hardFilters.ScoreWeightConvergence
		}
		if hardFilters.ScoreWeightROCE > 0 {
			wROCE = hardFilters.ScoreWeightROCE
		}
		if hardFilters.ScoreWeightCFOPAT > 0 {
			wCFOPAT = hardFilters.ScoreWeightCFOPAT
		}
		if hardFilters.ScoreWeightDebtSafety > 0 {
			wDebtSafety = hardFilters.ScoreWeightDebtSafety
		}
		if hardFilters.ScoreWeightRevenueCAGR > 0 {
			wRevCAGR = hardFilters.ScoreWeightRevenueCAGR
		}
		if hardFilters.ScoreWeightEarningsAccel > 0 {
			wEarningsAccel = hardFilters.ScoreWeightEarningsAccel
		}
	}

	for _, t := range activeKeys {
		fp := fairPrices[t]
		if fp == nil || fp.ValidModelCount < 2 {
			scores[t] = 0.0
			continue
		}
		f := fundamentals[t]

		// Pillar I: Valuation Discount (40 pts)
		upsideScore := normalizeValue(upsides[t], minUpside, maxUpside, wUpside, true)
		mosScore := 0.0
		cmp := fp.CMP
		if cmp <= fp.PessimisticFairPrice {
			mosScore = wMOS
		} else if cmp <= fp.EnsembleFairPrice {
			mosScore = wMOS * (2.0 / 3.0)
		} else if cmp <= fp.OptimisticFairPrice {
			mosScore = wMOS * (1.0 / 3.0)
		}

		// Pillar II: Model Consensus (20 pts)
		agreeCount := 0
		if fp.DCFFairPrice != nil && *fp.DCFFairPrice > cmp {
			agreeCount++
		}
		if fp.EPVFairPrice != nil && *fp.EPVFairPrice > cmp {
			agreeCount++
		}
		if fp.GrahamNumber != nil && *fp.GrahamNumber > cmp {
			agreeCount++
		}
		if fp.RelativeFairPrice != nil && *fp.RelativeFairPrice > cmp {
			agreeCount++
		}
		if fp.PEGFairPrice != nil && *fp.PEGFairPrice > cmp {
			agreeCount++
		}

		agreeScore := (float64(agreeCount) / 5.0) * wAgreement
		convScore := wConvergence * math.Max(0.0, 1.0-(fp.ModelCV/0.40))

		// Pillar III: Quality Anchor (25 pts)
		roceScore := normalizeValue(roces[t], minROCE, maxROCE, wROCE, true)
		cfoScore := normalizeValue(cfos[t], minCFO, maxCFO, wCFOPAT, true)
		deScore := normalizeValue(des[t], minDE, maxDE, wDebtSafety, false)

		// Pillar IV: Growth Kicker (15 pts)
		revScore := normalizeValue(revCAGRs[t], minRev, maxRev, wRevCAGR, true)
		earnScore := normalizeValue(math.Max(0.0, f.OperatingMargins), 0.0, 0.40, wEarningsAccel, true)

		totalScore := upsideScore + mosScore + agreeScore + convScore + roceScore + cfoScore + deScore + revScore + earnScore
		scores[t] = totalScore
	}

	return scores
}

// SelectTopNFairPriceWithCooldown selects top N constituents applying sector caps, hysteresis, and cooldown.
func SelectTopNFairPriceWithCooldown(
	activeKeys []string,
	scores map[string]float64,
	fairPrices map[string]*yfinance.FairPriceResult,
	fundamentals map[string]yfinance.Fundamentals,
	hardFilters *config.HardFilters,
	topN int,
	existingHoldings map[string]float64,
	hysteresisBuffer int,
	tracker *selectiontracker.Tracker,
	recentExits map[string]time.Time,
	cooldownDays int,
	bypassRank int,
	smartHysteresis ...SmartHysteresisConfig,
) []string {
	maxPerSector := hardFilters.MaxStocksPerSector
	if maxPerSector <= 0 {
		maxPerSector = 3
	}
	slog.Info("select.fairprice_sector_caps", "max_per_sector", maxPerSector)

	smartCfg := resolveSmartHysteresis(scores, fundamentals, smartHysteresis)

	// Sort active keys by Score descending; tie-breaker: higher Upside %
	sortedKeys := make([]string, len(activeKeys))
	copy(sortedKeys, activeKeys)
	sort.Slice(sortedKeys, func(i, j int) bool {
		si, sj := scores[sortedKeys[i]], scores[sortedKeys[j]]
		if math.Abs(si-sj) > 1e-4 {
			return si > sj
		}
		// Tie breaker: higher upside %
		ui, uj := 0.0, 0.0
		if fpi := fairPrices[sortedKeys[i]]; fpi != nil {
			ui = fpi.UpsidePct
		}
		if fpj := fairPrices[sortedKeys[j]]; fpj != nil {
			uj = fpj.UpsidePct
		}
		return ui > uj
	})

	var sectorCapCandidates []string
	sectorCounts := make(map[string]int)
	sectorTopTickers := make(map[string][]string)

	for rankIdx, t := range sortedKeys {
		rank := rankIdx + 1
		tracker.RecordRawScore(t, scores[t], rank)

		f := fundamentals[t]
		sec := f.Sector
		if sec == "" {
			sec = "Unclassified"
		}

		effCap := maxPerSector
		if hardFilters.SectorMaxStocks != nil {
			if oCap, ok := hardFilters.SectorMaxStocks[sec]; ok && oCap > 0 {
				effCap = oCap
			}
		}

		if sectorCounts[sec] < effCap {
			sectorCapCandidates = append(sectorCapCandidates, t)
			sectorCounts[sec]++
			sectorTopTickers[sec] = append(sectorTopTickers[sec], t)

			fp := fairPrices[t]
			upside := 0.0
			verdict := "N/A"
			fpVal := 0.0
			if fp != nil {
				upside = fp.UpsidePct
				verdict = fp.Verdict
				fpVal = fp.EnsembleFairPrice
			}
			returnMetric := "ROCE"
			returnVal, _ := GetLatestROCE(&f)
			if f.Sector == "Financial Services" {
				returnMetric = "ROE"
				if f.ROE > 0 {
					returnVal = f.ROE
				} else if f.ReturnOnAssets > 0 {
					returnMetric = "ROA"
					returnVal = f.ReturnOnAssets
				}
			} else if returnVal <= 0 {
				returnVal = f.ROE
			}
			driverStr := fmt.Sprintf("Fair Price: ₹%.1f (Upside: %+.1f%%, %s), %s: %.1f%%, Score: %.1f",
				fpVal, upside, verdict, returnMetric, returnVal*100.0, scores[t])
			tracker.RecordAdditionDriver(t, driverStr)
		} else {
			tracker.RecordSectorCapDrop(t, sec, sectorTopTickers[sec])
		}
	}

	bufferLimit := topN + hysteresisBuffer
	return ApplyHysteresisSelectionSmart(sectorCapCandidates, existingHoldings, topN, bufferLimit, tracker, recentExits, cooldownDays, bypassRank, smartCfg)
}

// NormalizeFairPriceWeights normalizes weights proportionally to scores and enforces sector and stock caps.
func NormalizeFairPriceWeights(
	selectedKeys []string,
	scores map[string]float64,
	fundamentals map[string]yfinance.Fundamentals,
	hardFilters *config.HardFilters,
	existingHoldings map[string]float64,
	rebalanceTolerance float64,
) map[string]float64 {
	slog.Info("normalize.fairprice_weights", "selected", len(selectedKeys))
	finalWeights := make(map[string]float64)
	if len(selectedKeys) == 0 {
		return finalWeights
	}

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

	stockCapVal := hardFilters.MaxStockWeightCap
	if stockCapVal <= 0 {
		stockCapVal = 0.08 // 8% default single stock cap
	}

	sectorCapVal := hardFilters.MaxSectorWeightCap
	if sectorCapVal <= 0 {
		sectorCapVal = 0.25 // 25% default sector cap
	}

	// Iteratively cap single stock weights
	for iter := 0; iter < 10; iter++ {
		excess := 0.0
		uncappedSum := 0.0
		for _, t := range selectedKeys {
			if finalWeights[t] > stockCapVal {
				excess += finalWeights[t] - stockCapVal
				finalWeights[t] = stockCapVal
			} else {
				uncappedSum += finalWeights[t]
			}
		}
		if excess <= 1e-6 || uncappedSum <= 0 {
			break
		}
		for _, t := range selectedKeys {
			if finalWeights[t] < stockCapVal {
				finalWeights[t] += excess * (finalWeights[t] / uncappedSum)
			}
		}
	}

	// Enforce Sector Cap
	sectorWeights := make(map[string]float64)
	for _, t := range selectedKeys {
		sec := fundamentals[t].Sector
		if sec == "" {
			sec = "Unclassified"
		}
		sectorWeights[sec] += finalWeights[t]
	}

	for sec, w := range sectorWeights {
		if w > sectorCapVal {
			scale := sectorCapVal / w
			for _, t := range selectedKeys {
				s := fundamentals[t].Sector
				if s == "" {
					s = "Unclassified"
				}
				if s == sec {
					finalWeights[t] *= scale
				}
			}
		}
	}

	// Hysteresis preservation: preserve incumbent weights if delta within tolerance
	if rebalanceTolerance > 0 && len(existingHoldings) > 0 {
		for _, t := range selectedKeys {
			if oldW, ok := existingHoldings[t]; ok && oldW > 0 {
				newW := finalWeights[t]
				if math.Abs(newW-oldW) <= rebalanceTolerance {
					finalWeights[t] = oldW
				}
			}
		}
	}

	return finalWeights
}
