package stockpicker

import (
	"encoding/csv"
	"fmt"
	"maps"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/optimizer"
	"github.com/raghavkgarg/mycase/pkg/selectiontracker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// ScoreMultibagger computes a 100-point relative scoring matrix for multibagger candidates.
func ScoreMultibagger(
	activeKeys []string,
	fundamentals map[string]yfinance.Fundamentals,
	fullHistory map[string]*yfinance.HistoricalData,
	hardFilters *config.HardFilters,
) map[string]float64 {
	fmt.Printf("Calculating 100-Point Multibagger Relative Scoring Matrix for %d candidates...\n", len(activeKeys))

	// Fetch 1-year benchmark prices for Relative Strength calculation
	benchmark1y, bErr := yfinance.FetchHistoricalPrices("^NSEI", "1y")
	bench1yReturn := 0.0
	if bErr == nil && len(benchmark1y) >= 2 {
		bench1yReturn = (benchmark1y[len(benchmark1y)-1] - benchmark1y[0]) / benchmark1y[0]
	} else {
		fmt.Printf("Warning: Failed to fetch 1y benchmark data: %v. Using 0%% benchmark return.\n", bErr)
	}

	// Raw indicators for normalization
	revAccGaps := make(map[string]float64)
	atExpansions := make(map[string]float64)
	pegs := make(map[string]float64)
	roces := make(map[string]float64)
	vIntensities := make(map[string]float64)
	rs52ws := make(map[string]float64)

	minRevAcc, maxRevAcc := math.MaxFloat64, -math.MaxFloat64
	minAtExp, maxAtExp := math.MaxFloat64, -math.MaxFloat64
	minPeg, maxPeg := math.MaxFloat64, -math.MaxFloat64
	minRoce, maxRoce := math.MaxFloat64, -math.MaxFloat64
	minVInt, maxVInt := math.MaxFloat64, -math.MaxFloat64
	minRs52, maxRs52 := math.MaxFloat64, -math.MaxFloat64

	for _, t := range activeKeys {
		f := fundamentals[t]
		hist := fullHistory[t]

		// Revenue Acceleration Gap
		_, ttmGrowth, cagr3y := yfinance.CalculateSalesGrowth(&f)
		revAccGaps[t] = ttmGrowth - cagr3y
		if revAccGaps[t] < minRevAcc {
			minRevAcc = revAccGaps[t]
		}
		if revAccGaps[t] > maxRevAcc {
			maxRevAcc = revAccGaps[t]
		}

		// Asset Turnover Expansion
		var maxCapExMultiplier float64
		if hardFilters != nil {
			maxCapExMultiplier = hardFilters.MaxCapExYoYMultiplier
		}
		_, atPrev, atLatest, _, _ := yfinance.CalculateAssetTurnoverCapEx(&f, maxCapExMultiplier)
		atExp := 0.0
		if atPrev > 0 {
			atExp = (atLatest - atPrev) / atPrev
		}
		atExpansions[t] = atExp
		if atExp < minAtExp {
			minAtExp = atExp
		}
		if atExp > maxAtExp {
			maxAtExp = atExp
		}

		// PEG Ratio (cap at pegFloor, excluding negative values)
		pegFloor := 0.1
		if hardFilters != nil && hardFilters.PEGFloor > 0 {
			pegFloor = hardFilters.PEGFloor
		}
		pegVal := f.PEGRatio
		if pegVal < pegFloor {
			pegVal = pegFloor
		}
		pegs[t] = pegVal
		if pegVal < minPeg {
			minPeg = pegVal
		}
		if pegVal > maxPeg {
			maxPeg = pegVal
		}

		// ROCE (latest year)
		roceVal, _ := getLatestROCE(&f)
		roces[t] = roceVal
		if roceVal < minRoce {
			minRoce = roceVal
		}
		if roceVal > maxRoce {
			maxRoce = roceVal
		}

		// Breakout Volume Intensity
		volMult := yfinance.GetVolumeBreakoutMultiplier(hist.Closes, hist.Opens, hist.Volumes, hardFilters.VolumeBreakoutLookbackDays)
		vIntensities[t] = volMult
		if volMult < minVInt {
			minVInt = volMult
		}
		if volMult > maxVInt {
			maxVInt = volMult
		}

		// 52-Week Relative Strength
		stock1yReturn := 0.0
		if len(hist.Closes) >= 2 {
			stock1yReturn = (hist.Closes[len(hist.Closes)-1] - hist.Closes[0]) / hist.Closes[0]
		}
		rs52 := stock1yReturn - bench1yReturn
		rs52ws[t] = rs52
		if rs52 < minRs52 {
			minRs52 = rs52
		}
		if rs52 > maxRs52 {
			maxRs52 = rs52
		}
	}

	scores := make(map[string]float64)
	for _, t := range activeKeys {
		wRevAcc := 20.0
		wAssetTurnover := 20.0
		wPEG := 15.0
		wROCE := 15.0
		wVolumeBreakout := 15.0
		wRelativeStrength := 15.0

		if hardFilters != nil {
			if hardFilters.ScoreWeightRevAcc > 0 {
				wRevAcc = hardFilters.ScoreWeightRevAcc
			}
			if hardFilters.ScoreWeightAssetTurnover > 0 {
				wAssetTurnover = hardFilters.ScoreWeightAssetTurnover
			}
			if hardFilters.ScoreWeightPEG > 0 {
				wPEG = hardFilters.ScoreWeightPEG
			}
			if hardFilters.ScoreWeightROCE > 0 {
				wROCE = hardFilters.ScoreWeightROCE
			}
			if hardFilters.ScoreWeightVolumeBreakout > 0 {
				wVolumeBreakout = hardFilters.ScoreWeightVolumeBreakout
			}
			if hardFilters.ScoreWeightRelativeStrength > 0 {
				wRelativeStrength = hardFilters.ScoreWeightRelativeStrength
			}
		}

		p1_revAcc := normalizeValue(revAccGaps[t], minRevAcc, maxRevAcc, wRevAcc, true)
		p1_atExp := normalizeValue(atExpansions[t], minAtExp, maxAtExp, wAssetTurnover, true)
		p2_peg := normalizeValue(pegs[t], minPeg, maxPeg, wPEG, false)
		p2_roce := normalizeValue(roces[t], minRoce, maxRoce, wROCE, true)
		p3_vInt := normalizeValue(vIntensities[t], minVInt, maxVInt, wVolumeBreakout, true)
		p3_rs52 := normalizeValue(rs52ws[t], minRs52, maxRs52, wRelativeStrength, true)

		scores[t] = p1_revAcc + p1_atExp + p2_peg + p2_roce + p3_vInt + p3_rs52
	}

	// Sort activeKeys descending by total score, with lower market cap tie-breaker
	sort.Slice(activeKeys, func(i, j int) bool {
		scoreI := scores[activeKeys[i]]
		scoreJ := scores[activeKeys[j]]
		if math.Abs(scoreI-scoreJ) < 1e-9 {
			return fundamentals[activeKeys[i]].MarketCap < fundamentals[activeKeys[j]].MarketCap
		}
		return scoreI > scoreJ
	})

	return scores
}

// normalizeValue normalizes a raw factor value to points based on strategy guidelines.
func normalizeValue(val, minVal, maxVal, maxPoints float64, higherIsBetter bool) float64 {
	if maxVal == minVal {
		return maxPoints
	}
	var score float64
	if higherIsBetter {
		score = ((val - minVal) / (maxVal - minVal)) * maxPoints
	} else {
		score = ((maxVal - val) / (maxVal - minVal)) * maxPoints
	}
	if score < 0 {
		return 0
	}
	if score > maxPoints {
		return maxPoints
	}
	return score
}

// SelectTopNMultibagger filters top N constituents applying sector caps and hysteresis buffer.
func SelectTopNMultibagger(
	activeKeys []string,
	scores map[string]float64,
	fundamentals map[string]yfinance.Fundamentals,
	hardFilters *config.HardFilters,
	topN int,
	existingHoldings map[string]float64,
	hysteresisBuffer int,
	tracker *selectiontracker.Tracker,
) []string {
	maxPerSector := hardFilters.MaxStocksPerSector
	if maxPerSector <= 0 {
		maxPerSector = 3
	}
	fmt.Printf("Applying Sector Caps (max %d stocks per sector)...\n", maxPerSector)

	// 1. Filter all activeKeys by sector caps to get valid candidates in ranked order
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
		if sectorCounts[sec] >= maxPerSector {
			tracker.RecordSectorCapDrop(t, sec, sectorTopTickers[sec])
			continue
		}
		sectorCounts[sec]++
		sectorTopTickers[sec] = append(sectorTopTickers[sec], t)
		sectorCapCandidates = append(sectorCapCandidates, t)
	}

	// 2. Apply hysteresis buffer selection
	bufferLimit := topN + hysteresisBuffer
	fmt.Printf("Applying Hysteresis Buffer Zone (Top %d target, existing kept up to rank %d)...\n", topN, bufferLimit)
	return ApplyHysteresisSelection(sectorCapCandidates, existingHoldings, topN, bufferLimit, tracker)
}

// NormalizeMultibaggerWeights normalizes weights proportionally to scores and enforces sector caps.
func NormalizeMultibaggerWeights(
	selectedKeys []string,
	scores map[string]float64,
	fundamentals map[string]yfinance.Fundamentals,
	hardFilters *config.HardFilters,
	existingHoldings map[string]float64,
	rebalanceTolerance float64,
) map[string]float64 {
	fmt.Printf("Normalizing weights for selected top %d stocks...\n", len(selectedKeys))
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
	capVal := hardFilters.MaxSectorWeightCap
	if capVal <= 0 {
		capVal = 0.25
	}
	optimizer.EnforceSectorCaps(selectedKeys, finalWeights, fundamentals, capVal)

	// Apply rebalancing band/tolerance
	finalWeights = ApplyRebalancingBand(selectedKeys, finalWeights, existingHoldings, rebalanceTolerance)

	return finalWeights
}

// SelectTopNStandard scores, ranks, and slices standard constituents.
func SelectTopNStandard(
	activeKeys []string,
	slicedPriceHistory map[string][]float64,
	benchmarkPrices []float64,
	fundamentals map[string]yfinance.Fundamentals,
	optWeights optimizer.MFSWeights,
	topN int,
	existingHoldings map[string]float64,
	hysteresisBuffer int,
	tracker *selectiontracker.Tracker,
) []string {
	fmt.Printf("Scoring and ranking constituents...\n")
	allWeights := optimizer.OptimizeMultiFactor(activeKeys, slicedPriceHistory, benchmarkPrices, fundamentals, optWeights)

	// Sort all tickers by raw score weights descending
	sortedKeys := make([]string, len(activeKeys))
	copy(sortedKeys, activeKeys)
	sort.Slice(sortedKeys, func(i, j int) bool {
		return allWeights[sortedKeys[i]] > allWeights[sortedKeys[j]]
	})

	for rankIdx, t := range sortedKeys {
		rank := rankIdx + 1
		tracker.RecordRawScore(t, allWeights[t], rank)
	}

	bufferLimit := topN + hysteresisBuffer
	fmt.Printf("Applying Hysteresis Buffer Zone (Top %d target, existing kept up to rank %d)...\n", topN, bufferLimit)
	return ApplyHysteresisSelection(sortedKeys, existingHoldings, topN, bufferLimit, tracker)
}

// NormalizeStandardWeights optimizes multi-factor weights for standard strategy selection.
func NormalizeStandardWeights(
	selectedKeys []string,
	slicedPriceHistory map[string][]float64,
	benchmarkPrices []float64,
	fundamentals map[string]yfinance.Fundamentals,
	optWeights optimizer.MFSWeights,
	existingHoldings map[string]float64,
	rebalanceTolerance float64,
) map[string]float64 {
	fmt.Printf("Normalizing weights for selected top %d stocks...\n", len(selectedKeys))
	finalWeights := optimizer.OptimizeMultiFactor(selectedKeys, slicedPriceHistory, benchmarkPrices, fundamentals, optWeights)

	// Apply rebalancing band/tolerance
	finalWeights = ApplyRebalancingBand(selectedKeys, finalWeights, existingHoldings, rebalanceTolerance)

	return finalWeights
}

// LoadGoldenWeights reads the golden copy CSV and returns a map of ticker to weight.
func LoadGoldenWeights(path string) map[string]float64 {
	weights := make(map[string]float64)
	if path == "" {
		return weights
	}
	file, err := os.Open(path)
	if err != nil {
		return weights
	}
	defer file.Close()

	r := csv.NewReader(file)
	records, err := r.ReadAll()
	if err != nil || len(records) < 2 {
		return weights
	}

	tickerIdx := -1
	weightIdx := -1
	for i, h := range records[0] {
		hLower := strings.ToLower(strings.TrimSpace(h))
		if hLower == "ticker" {
			tickerIdx = i
		} else if hLower == "weight" {
			weightIdx = i
		}
	}

	if tickerIdx == -1 || weightIdx == -1 {
		return weights
	}

	for _, row := range records[1:] {
		if len(row) > tickerIdx && len(row) > weightIdx {
			t := strings.TrimSpace(row[tickerIdx])
			wStr := strings.TrimSpace(row[weightIdx])
			if wVal, err := strconv.ParseFloat(wStr, 64); err == nil && t != "" {
				weights[t] += wVal
			}
		}
	}

	for t, w := range weights {
		if w <= 0.00001 {
			delete(weights, t)
		}
	}
	return weights
}

// ApplyHysteresisSelection selects the top N constituents using a rank buffer.
func ApplyHysteresisSelection(
	sortedKeys []string,
	existingHoldings map[string]float64,
	topN int,
	bufferLimit int,
	tracker *selectiontracker.Tracker,
) []string {
	if len(sortedKeys) <= topN {
		for rankIdx, ticker := range sortedKeys {
			rank := rankIdx + 1
			_, isExisting := existingHoldings[ticker]
			tracker.RecordSelected(ticker, rank, topN, isExisting)
		}
		return sortedKeys
	}

	// 1. Identify which existing holdings are within the buffer limit (rank <= 25)
	existingInBuffer := make(map[string]bool)
	for rankIdx, ticker := range sortedKeys {
		rank := rankIdx + 1
		if _, ok := existingHoldings[ticker]; ok {
			if rank <= bufferLimit {
				existingInBuffer[ticker] = true
			}
		}
	}

	// 2. Build the selected set
	var selected []string
	for rankIdx, ticker := range sortedKeys {
		rank := rankIdx + 1
		if len(selected) >= topN {
			break
		}
		if existingInBuffer[ticker] {
			selected = append(selected, ticker)
			tracker.RecordSelected(ticker, rank, bufferLimit, true)
		}
	}

	// Fill remaining slots with the highest-ranked new candidates
	for rankIdx, ticker := range sortedKeys {
		rank := rankIdx + 1
		if len(selected) >= topN {
			break
		}
		alreadySelected := slices.Contains(selected, ticker)
		if alreadySelected {
			continue
		}
		selected = append(selected, ticker)
		_, isExisting := existingHoldings[ticker]
		tracker.RecordSelected(ticker, rank, topN, isExisting)
	}

	// Record rejections for candidates not selected
	selectedMap := make(map[string]bool)
	for _, t := range selected {
		selectedMap[t] = true
	}
	for rankIdx, ticker := range sortedKeys {
		rank := rankIdx + 1
		if !selectedMap[ticker] {
			_, isExisting := existingHoldings[ticker]
			tracker.RecordHysteresisDrop(ticker, rank, topN, bufferLimit, isExisting)
		}
	}

	// Sort selection to maintain the original ranked order (by score)
	tickerIndices := make(map[string]int)
	for idx, ticker := range sortedKeys {
		tickerIndices[ticker] = idx
	}
	sort.Slice(selected, func(i, j int) bool {
		return tickerIndices[selected[i]] < tickerIndices[selected[j]]
	})

	return selected
}

// ApplyRebalancingBand adjusts target weights to preserve existing weights if the difference is < tolerance.
func ApplyRebalancingBand(
	selectedKeys []string,
	targetWeights map[string]float64,
	existingHoldings map[string]float64,
	rebalanceTolerance float64,
) map[string]float64 {
	if len(existingHoldings) == 0 {
		return targetWeights
	}

	lockedWeights := make(map[string]float64)
	var sumLocked float64
	var sumNonLockedTarget float64
	nonLockedKeys := []string{}

	// Convert percentage (e.g. 0.10) to decimal (0.0010)
	toleranceLimit := rebalanceTolerance * 0.01

	for _, ticker := range selectedKeys {
		targetW := targetWeights[ticker]
		if oldW, ok := existingHoldings[ticker]; ok {
			if math.Abs(targetW-oldW) < toleranceLimit {
				lockedWeights[ticker] = oldW
				sumLocked += oldW
				continue
			}
		}
		nonLockedKeys = append(nonLockedKeys, ticker)
		sumNonLockedTarget += targetW
	}

	if len(lockedWeights) == 0 {
		return targetWeights
	}

	if sumNonLockedTarget <= 0 {
		// All selected stocks are within the tolerance band.
		// Retain their existing weights, normalized to sum to exactly 1.0.
		var sum float64
		for _, w := range lockedWeights {
			sum += w
		}
		if sum > 0 {
			normalized := make(map[string]float64)
			for ticker, w := range lockedWeights {
				normalized[ticker] = w / sum
			}
			return normalized
		}
		return targetWeights
	}

	if sumLocked >= 1.0 {
		return targetWeights
	}

	finalWeights := make(map[string]float64)
	maps.Copy(finalWeights, lockedWeights)
	remainingWeight := 1.0 - sumLocked
	for _, ticker := range nonLockedKeys {
		finalWeights[ticker] = targetWeights[ticker] * (remainingWeight / sumNonLockedTarget)
	}

	return finalWeights
}
