package backtest

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// PillarStats contains statistical summary of Spearman Rank IC over multiple periods.
type PillarStats struct {
	MeanIC     float64 `json:"mean_ic"`
	StdIC      float64 `json:"std_ic"`
	IR         float64 `json:"information_ratio"`
	TStat      float64 `json:"t_stat"`
	PositivePct float64 `json:"positive_pct"`
	SampleCount int     `json:"sample_count"`
}

// PeriodICResult stores cross-sectional Information Coefficients for a single evaluation date.
type PeriodICResult struct {
	Date                string  `json:"date"`
	SurvivorCount       int     `json:"survivor_count"`
	RegimeMultiplier    float64 `json:"regime_multiplier"`
	ICCompositeRS       float64 `json:"ic_composite_rs"`
	ICVCPTightness      float64 `json:"ic_vcp_tightness"`
	ICWinsorizedRVOL    float64 `json:"ic_winsorized_rvol"`
	ICDecayedPP         float64 `json:"ic_decayed_pp"`
	ICDeliveryDelta     float64 `json:"ic_delivery_delta"`
	ICRawComposite      float64 `json:"ic_raw_composite"`
	ICEffectiveComposite float64 `json:"ic_effective_composite"`
}

// EmpiricalBounds contains P5 and P95 derived from the training window.
type EmpiricalBounds struct {
	CompositeRS   [2]float64 `json:"composite_rs"`
	VCPRatio      [2]float64 `json:"vcp_ratio"`
	WinsorizedRVOL [2]float64 `json:"winsorized_rvol"`
	DecayedPP     [2]float64 `json:"decayed_pp"`
	DeliveryDelta [2]float64 `json:"delivery_delta"`
}

// CalibrationSummary is the full output of an empirical IC calibration run.
type CalibrationSummary struct {
	TotalPeriods       int                    `json:"total_periods"`
	TrainPeriodCount   int                    `json:"train_period_count"`
	TestPeriodCount    int                    `json:"test_period_count"`
	TrainStats         map[string]PillarStats `json:"train_stats"`
	TestStats          map[string]PillarStats `json:"test_stats"`
	CalibratedBounds   EmpiricalBounds        `json:"calibrated_bounds"`
	RecommendedWeights map[string]float64     `json:"recommended_weights"`
	TrainResults       []PeriodICResult       `json:"train_results"`
	TestResults        []PeriodICResult       `json:"test_results"`
}

// RankValues converts a slice of floats into fractional/average ranks (1-based).
func RankValues(values []float64) []float64 {
	n := len(values)
	if n == 0 {
		return nil
	}

	type valIdx struct {
		val float64
		idx int
	}
	items := make([]valIdx, n)
	for i, v := range values {
		items[i] = valIdx{val: v, idx: i}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].val < items[j].val
	})

	ranks := make([]float64, n)
	i := 0
	for i < n {
		j := i
		for j < n-1 && items[j].val == items[j+1].val {
			j++
		}
		// Average rank for ties
		avgRank := float64(i+j+2) / 2.0
		for k := i; k <= j; k++ {
			ranks[items[k].idx] = avgRank
		}
		i = j + 1
	}

	return ranks
}

// SpearmanRankCorrelation calculates the Spearman rank correlation between two vectors.
func SpearmanRankCorrelation(x, y []float64) float64 {
	n := len(x)
	if n < 3 || len(y) != n {
		return 0.0
	}

	rx := RankValues(x)
	ry := RankValues(y)

	var meanX, meanY float64
	for i := 0; i < n; i++ {
		meanX += rx[i]
		meanY += ry[i]
	}
	meanX /= float64(n)
	meanY /= float64(n)

	var num, denX, denY float64
	for i := 0; i < n; i++ {
		dx := rx[i] - meanX
		dy := ry[i] - meanY
		num += dx * dy
		denX += dx * dx
		denY += dy * dy
	}

	den := math.Sqrt(denX * denY)
	if den <= 0 {
		return 0.0
	}

	corr := num / den
	if math.IsNaN(corr) {
		return 0.0
	}
	return corr
}

// ComputePillarStats computes aggregate statistics from a slice of IC observations.
func ComputePillarStats(ics []float64) PillarStats {
	n := len(ics)
	if n == 0 {
		return PillarStats{}
	}

	var sum float64
	posCount := 0
	for _, v := range ics {
		sum += v
		if v > 0 {
			posCount++
		}
	}
	mean := sum / float64(n)

	var sumSq float64
	for _, v := range ics {
		diff := v - mean
		sumSq += diff * diff
	}
	std := 0.0
	if n > 1 {
		std = math.Sqrt(sumSq / float64(n-1))
	}

	ir := 0.0
	if std > 0 {
		ir = mean / std
	}
	tStat := ir * math.Sqrt(float64(n))
	posPct := (float64(posCount) / float64(n)) * 100.0

	return PillarStats{
		MeanIC:      mean,
		StdIC:       std,
		IR:          ir,
		TStat:       tStat,
		PositivePct: posPct,
		SampleCount: n,
	}
}

// Percentile calculates the p-th percentile (0.0 to 1.0) of a sorted float slice.
func Percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0.0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1.0 {
		return sorted[n-1]
	}
	idx := p * float64(n-1)
	low := int(math.Floor(idx))
	high := int(math.Ceil(idx))
	if low == high {
		return sorted[low]
	}
	weight := idx - float64(low)
	return (1.0-weight)*sorted[low] + weight*sorted[high]
}

// RunEarlyMBCalibration executes the rolling Spearman Rank IC evaluation and empirical bound calibration.
func RunEarlyMBCalibration(
	ctx context.Context,
	tickers []string,
	priceData map[string]*yfinance.HistoricalData,
	benchData *yfinance.HistoricalData,
	fundamentals map[string]yfinance.Fundamentals,
	hardFilters *config.HardFilters,
	stepDays int,
	forwardDays int,
	trainRatio float64,
) (*CalibrationSummary, error) {
	if len(tickers) == 0 {
		return nil, fmt.Errorf("no tickers provided")
	}
	if benchData == nil || len(benchData.Closes) < 50 {
		return nil, fmt.Errorf("insufficient benchmark data")
	}
	if stepDays <= 0 {
		stepDays = 21 // Monthly stepping
	}
	if forwardDays <= 0 {
		forwardDays = 21 // 21-Day Forward Return
	}
	if trainRatio <= 0 || trainRatio >= 1.0 {
		trainRatio = 0.70 // 70% In-Sample Train / 30% Out-of-Sample Test
	}

	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		ist = time.FixedZone("IST", 5*3600+30*60)
	}

	// 1. Map timestamps to trading day indices on benchmark
	benchTimestamps := benchData.Timestamps
	nBench := len(benchTimestamps)
	if nBench < 200 {
		return nil, fmt.Errorf("insufficient benchmark history: need at least 200 trading days, got %d", nBench)
	}

	// 2. Identify evaluation dates (stepping by stepDays from index 100 up to nBench - forwardDays - 1)
	var evalIndices []int
	for i := 100; i < nBench-forwardDays; i += stepDays {
		evalIndices = append(evalIndices, i)
	}

	if len(evalIndices) < 5 {
		return nil, fmt.Errorf("not enough evaluation periods found (%d periods); expand date range", len(evalIndices))
	}

	var allResults []PeriodICResult
	var trainCompRS, trainVCP, trainRVOL, trainPP, trainDeliv []float64

	nTrain := int(math.Floor(float64(len(evalIndices)) * trainRatio))
	if nTrain < 3 {
		nTrain = len(evalIndices) / 2
	}

	for periodIdx, bIdx := range evalIndices {
		evalTS := benchTimestamps[bIdx]
		evalDateStr := time.Unix(evalTS, 0).In(ist).Format("2006-01-02")
		isTraining := periodIdx < nTrain

		// Slice benchmark data up to evalTS
		var benchClosesUpTo []float64
		for k := 0; k <= bIdx; k++ {
			benchClosesUpTo = append(benchClosesUpTo, benchData.Closes[k])
		}
		rRegime := yfinance.CalculateSmoothedBenchmarkRegime(benchClosesUpTo, 50, 0.20)

		// Slice each stock's OHLCV strictly up to evalTS (Zero Lookahead)
		type survivorData struct {
			ticker       string
			compRS       float64
			vcpRatio     float64
			rvolZ        float64
			decayedPP    float64
			delivDelta   float64
			rawScore     float64
			effScore     float64
			forwardReturn float64
		}
		var survivors []survivorData

		for _, t := range tickers {
			pd, ok := priceData[t]
			if !ok || len(pd.Timestamps) == 0 {
				continue
			}

			// Find index in stock's series corresponding to evalTS
			stockIdx := -1
			for s := len(pd.Timestamps) - 1; s >= 0; s-- {
				if pd.Timestamps[s] <= evalTS {
					stockIdx = s
					break
				}
			}
			if stockIdx < 60 {
				continue // Need at least 60 trading days of history
			}

			// Forward return calculation: (Close[stockIdx + forwardDays] - Close[stockIdx]) / Close[stockIdx]
			fwdIdx := stockIdx + forwardDays
			if fwdIdx >= len(pd.Closes) {
				continue // Out of bounds for forward return
			}
			pNow := pd.Closes[stockIdx]
			pFwd := pd.Closes[fwdIdx]
			if pNow <= 0 {
				continue
			}
			fwdRet := (pFwd - pNow) / pNow

			// Slice historical prices strictly up to stockIdx
			closesSlice := pd.Closes[:stockIdx+1]
			opensSlice := pd.Opens[:stockIdx+1]
			volsSlice := pd.Volumes[:stockIdx+1]

			// Stage 1 Gating
			f := fundamentals[t]
			// Trend health floor (>= 0.95 * 200 DMA)
			if len(closesSlice) >= 200 {
				smaSum := 0.0
				for k := len(closesSlice) - 200; k < len(closesSlice); k++ {
					smaSum += closesSlice[k]
				}
				sma200 := smaSum / 200.0
				if pNow < 0.95*sma200 {
					continue
				}
			}

			// 52W Proximity floor (>= 0.85)
			prox := yfinance.CalculateProximity52W(closesSlice)
			if prox < 0.85 {
				continue
			}

			// Base Duration floor (>= 4 weeks)
			weeks, _ := yfinance.CalculateBaseDurationWeeks(closesSlice, 0.85)
			if weeks < 4 {
				continue
			}

			// Compute Stage 2 Pillar Metrics
			compRS, _, _, _ := yfinance.CalculateCompositeRS(closesSlice, benchClosesUpTo)
			vcpRatio, _ := yfinance.CalculateVCPTightness(closesSlice, opensSlice)
			rvolZ := yfinance.CalculateWinsorizedRVOLZScore(volsSlice, 5, 50, 4.0)
			decayedPP, _ := yfinance.CalculateDecayedPocketPivot(closesSlice, opensSlice, volsSlice, 10, 0.25)
			delivDelta := (f.DeliveryPct / 100.0) - 0.35

			// Invariant scoring
			clamp := func(val, minV, maxV float64) float64 {
				if val < minV {
					return minV
				}
				if val > maxV {
					return maxV
				}
				return val
			}
			p1 := 25.0 * clamp((compRS-(-0.30))/(0.70-(-0.30)), 0.0, 1.0)
			p2 := 25.0 * clamp((0.75-vcpRatio)/(0.75-0.25), 0.0, 1.0)
			p3a := 12.5 * clamp((rvolZ-0.0)/(3.0-0.0), 0.0, 1.0)
			p3b := 12.5 * clamp((decayedPP-0.0)/(12.0-0.0), 0.0, 1.0)
			p3 := p3a + p3b
			p4 := 25.0 * clamp((delivDelta-(-0.10))/(0.30-(-0.10)), 0.0, 1.0)
			raw := p1 + p2 + p3 + p4
			eff := raw * rRegime

			survivors = append(survivors, survivorData{
				ticker:        t,
				compRS:        compRS,
				vcpRatio:      vcpRatio,
				rvolZ:         rvolZ,
				decayedPP:     decayedPP,
				delivDelta:    delivDelta,
				rawScore:      raw,
				effScore:      eff,
				forwardReturn: fwdRet,
			})

			if isTraining {
				trainCompRS = append(trainCompRS, compRS)
				trainVCP = append(trainVCP, vcpRatio)
				trainRVOL = append(trainRVOL, rvolZ)
				trainPP = append(trainPP, decayedPP)
				trainDeliv = append(trainDeliv, delivDelta)
			}
		}

		if len(survivors) < 5 {
			continue // Need at least 5 survivors for meaningful cross-sectional rank correlation
		}

		// Extract vectors for Spearman correlation
		var fwdReturns, vecCompRS, vecVCP, vecRVOL, vecPP, vecDeliv, vecRaw, vecEff []float64
		for _, s := range survivors {
			fwdReturns = append(fwdReturns, s.forwardReturn)
			vecCompRS = append(vecCompRS, s.compRS)
			vecVCP = append(vecVCP, -s.vcpRatio) // Lower VCP is better, so negate for correlation with return
			vecRVOL = append(vecRVOL, s.rvolZ)
			vecPP = append(vecPP, s.decayedPP)
			vecDeliv = append(vecDeliv, s.delivDelta)
			vecRaw = append(vecRaw, s.rawScore)
			vecEff = append(vecEff, s.effScore)
		}

		res := PeriodICResult{
			Date:                 evalDateStr,
			SurvivorCount:        len(survivors),
			RegimeMultiplier:     rRegime,
			ICCompositeRS:        SpearmanRankCorrelation(vecCompRS, fwdReturns),
			ICVCPTightness:       SpearmanRankCorrelation(vecVCP, fwdReturns),
			ICWinsorizedRVOL:     SpearmanRankCorrelation(vecRVOL, fwdReturns),
			ICDecayedPP:          SpearmanRankCorrelation(vecPP, fwdReturns),
			ICDeliveryDelta:      SpearmanRankCorrelation(vecDeliv, fwdReturns),
			ICRawComposite:       SpearmanRankCorrelation(vecRaw, fwdReturns),
			ICEffectiveComposite: SpearmanRankCorrelation(vecEff, fwdReturns),
		}

		allResults = append(allResults, res)
	}

	if len(allResults) == 0 {
		return nil, fmt.Errorf("no calibration periods completed")
	}

	// 3. Split results into Train and Test
	actualTrainCount := int(math.Floor(float64(len(allResults)) * trainRatio))
	if actualTrainCount < 1 {
		actualTrainCount = len(allResults)
	}
	trainResults := allResults[:actualTrainCount]
	testResults := allResults[actualTrainCount:]

	// 4. Derive empirical P5 and P95 bounds exclusively from Training Window
	sort.Float64s(trainCompRS)
	sort.Float64s(trainVCP)
	sort.Float64s(trainRVOL)
	sort.Float64s(trainPP)
	sort.Float64s(trainDeliv)

	calibBounds := EmpiricalBounds{
		CompositeRS:   [2]float64{Percentile(trainCompRS, 0.05), Percentile(trainCompRS, 0.95)},
		VCPRatio:      [2]float64{Percentile(trainVCP, 0.05), Percentile(trainVCP, 0.95)},
		WinsorizedRVOL: [2]float64{Percentile(trainRVOL, 0.05), Percentile(trainRVOL, 0.95)},
		DecayedPP:     [2]float64{Percentile(trainPP, 0.05), Percentile(trainPP, 0.95)},
		DeliveryDelta: [2]float64{Percentile(trainDeliv, 0.05), Percentile(trainDeliv, 0.95)},
	}

	// 5. Aggregate Training & Testing Statistics
	calcStatsMap := func(results []PeriodICResult) map[string]PillarStats {
		var icRS, icVCP, icRVOL, icPP, icDeliv, icRaw, icEff []float64
		for _, r := range results {
			icRS = append(icRS, r.ICCompositeRS)
			icVCP = append(icVCP, r.ICVCPTightness)
			icRVOL = append(icRVOL, r.ICWinsorizedRVOL)
			icPP = append(icPP, r.ICDecayedPP)
			icDeliv = append(icDeliv, r.ICDeliveryDelta)
			icRaw = append(icRaw, r.ICRawComposite)
			icEff = append(icEff, r.ICEffectiveComposite)
		}
		return map[string]PillarStats{
			"composite_rs":       ComputePillarStats(icRS),
			"vcp_tightness":      ComputePillarStats(icVCP),
			"winsorized_rvol":    ComputePillarStats(icRVOL),
			"decayed_pp":         ComputePillarStats(icPP),
			"delivery_delta":     ComputePillarStats(icDeliv),
			"raw_composite":      ComputePillarStats(icRaw),
			"effective_composite": ComputePillarStats(icEff),
		}
	}

	trainStats := calcStatsMap(trainResults)
	var testStats map[string]PillarStats
	if len(testResults) > 0 {
		testStats = calcStatsMap(testResults)
	}

	// 6. Calculate Recommended IR-based Weights from Training Period
	irRS := math.Max(0, trainStats["composite_rs"].IR)
	irVCP := math.Max(0, trainStats["vcp_tightness"].IR)
	irVol := math.Max(0, (trainStats["winsorized_rvol"].IR+trainStats["decayed_pp"].IR)/2.0)
	irDeliv := math.Max(0, trainStats["delivery_delta"].IR)
	sumIR := irRS + irVCP + irVol + irDeliv

	recWeights := make(map[string]float64)
	if sumIR > 0 {
		recWeights["idiosyncratic_rs"] = (irRS / sumIR) * 100.0
		recWeights["vcp_tightness"] = (irVCP / sumIR) * 100.0
		recWeights["volume_footprint"] = (irVol / sumIR) * 100.0
		recWeights["delivery_delta"] = (irDeliv / sumIR) * 100.0
	} else {
		// Equal weight prior fallback
		recWeights["idiosyncratic_rs"] = 25.0
		recWeights["vcp_tightness"] = 25.0
		recWeights["volume_footprint"] = 25.0
		recWeights["delivery_delta"] = 25.0
	}

	return &CalibrationSummary{
		TotalPeriods:       len(allResults),
		TrainPeriodCount:   len(trainResults),
		TestPeriodCount:    len(testResults),
		TrainStats:         trainStats,
		TestStats:          testStats,
		CalibratedBounds:   calibBounds,
		RecommendedWeights: recWeights,
		TrainResults:       trainResults,
		TestResults:        testResults,
	}, nil
}
