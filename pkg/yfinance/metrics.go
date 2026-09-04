package yfinance

import (
	"math"
	"sort"
	"strings"
	"time"
)

// CalculateRSI calculates the 14-day Relative Strength Index
func CalculateRSI(closes []float64) float64 {
	if len(closes) < 15 {
		return 50.0 // neutral default
	}

	deltas := make([]float64, len(closes)-1)
	for i := 0; i < len(closes)-1; i++ {
		deltas[i] = closes[i+1] - closes[i]
	}

	avgGain := 0.0
	avgLoss := 0.0
	limit := min(len(deltas), 14)
	for i := range limit {
		if deltas[i] > 0 {
			avgGain += deltas[i]
		} else {
			avgLoss += -deltas[i]
		}
	}
	avgGain /= float64(limit)
	avgLoss /= float64(limit)

	for i := limit; i < len(deltas); i++ {
		gain := 0.0
		loss := 0.0
		if deltas[i] > 0 {
			gain = deltas[i]
		} else {
			loss = -deltas[i]
		}
		avgGain = (avgGain*13.0 + gain) / 14.0
		avgLoss = (avgLoss*13.0 + loss) / 14.0
	}

	if avgLoss == 0 {
		return 100.0
	}
	rs := avgGain / avgLoss
	return 100.0 - (100.0 / (1.0 + rs))
}

// IsFinancialSector checks if a sector belongs to Banks, NBFCs, Insurance, or Financial Services.
func IsFinancialSector(sector string) bool {
	s := sector
	return s == "Financial Services" || s == "Financials" || s == "Banking" || s == "Banks" || s == "Insurance"
}

// CalculateEPV calculates Bruce Greenwald's Earnings Power Value (EPV) and Margin of Safety (MOS).
func CalculateEPV(f *Fundamentals, wacc float64) (epv float64, mos float64, ok bool) {
	if wacc <= 0 {
		wacc = 0.105 // 10.5% default WACC for Indian Large-Caps
	}

	nOp := len(f.AnnualOperatingIncome)
	var avgEBIT float64
	if nOp > 0 {
		sum := 0.0
		count := 0
		for i := max(0, nOp-3); i < nOp; i++ {
			if f.AnnualOperatingIncome[i].Value > 0 {
				sum += f.AnnualOperatingIncome[i].Value
				count++
			}
		}
		if count > 0 {
			avgEBIT = sum / float64(count)
		}
	}

	if avgEBIT <= 0 && f.NetIncome > 0 {
		avgEBIT = f.NetIncome / 0.70 // Proxy EBIT from Net Income
	}

	if avgEBIT <= 0 {
		return 0, 0, false
	}

	nopat := avgEBIT * 0.70 // 30% tax rate assumption
	adjustedCashEarnings := nopat

	epv = adjustedCashEarnings / wacc
	if epv <= 0 {
		return 0, 0, false
	}

	// Calculate Enterprise Value (EV) = MarketCap + TotalDebt - OperatingCashflow (cash proxy)
	ev := f.MarketCap + f.TotalDebt
	if ev <= 0 {
		ev = f.MarketCap
	}

	mos = (1.0 - (ev / epv)) * 100.0
	return epv, mos, true
}

// CalculateShillerYield calculates Shiller CAPE Yield based on 3-year average EPS relative to stock price.
func CalculateShillerYield(f *Fundamentals) (float64, bool) {
	if f.RegularPrice <= 0 {
		return 0, false
	}

	nHist := len(f.EarningsHistory)
	if nHist == 0 {
		if f.NetIncome > 0 && f.MarketCap > 0 {
			return f.NetIncome / f.MarketCap, true
		}
		return 0, false
	}

	sumEarnings := 0.0
	count := 0
	for i := max(0, nHist-3); i < nHist; i++ {
		sumEarnings += f.EarningsHistory[i].Earnings
		count++
	}

	if count == 0 || sumEarnings <= 0 {
		return 0, false
	}

	avgEarnings := sumEarnings / float64(count)
	if f.MarketCap > 0 {
		return avgEarnings / f.MarketCap, true
	}
	return 0, false
}

// CalculateShareholderYield computes total cash returned to shareholders via dividends and cash yield.
func CalculateShareholderYield(f *Fundamentals) float64 {
	if f.MarketCap <= 0 {
		return 0.0
	}
	yield := 0.0
	if f.OperatingCashflow > 0 {
		yield += (f.FreeCashflow / f.MarketCap) * 100.0
	}
	return yield
}

// CheckVolumeBreakout checks for a green-day volume breakout in a given rolling lookback window and multiplier
func CheckVolumeBreakout(closes, opens, volumes []float64, lookback int, multiplier float64) bool {
	n := len(closes)
	if n < 2 {
		return false
	}

	if lookback <= 0 {
		lookback = 60
	}
	if multiplier <= 0 {
		multiplier = 2.0
	}

	if n < lookback {
		lookback = n
	}
	startIndex := n - lookback

	redVolSum := 0.0
	redCount := 0
	allVolSum := 0.0

	for i := startIndex; i < n; i++ {
		allVolSum += volumes[i]
		isRed := false
		if i < len(opens) && opens[i] > 0 {
			isRed = closes[i] < opens[i]
		} else if i > 0 {
			isRed = closes[i] < closes[i-1]
		}
		if isRed {
			redVolSum += volumes[i]
			redCount++
		}
	}

	avgRedVol := 0.0
	if redCount > 0 {
		avgRedVol = redVolSum / float64(redCount)
	} else {
		avgRedVol = allVolSum / float64(lookback)
	}

	for i := startIndex; i < n; i++ {
		isGreen := false
		if i < len(opens) && opens[i] > 0 {
			isGreen = closes[i] >= opens[i]
		} else if i > 0 {
			isGreen = closes[i] >= closes[i-1]
		}
		if isGreen && avgRedVol > 0 && volumes[i] >= multiplier*avgRedVol {
			return true
		}
	}

	return false
}

// CalculateSalesGrowth checks the Sales Growth Accelerator rule
func CalculateSalesGrowth(f *Fundamentals) (bool, float64, float64) {
	sort.Slice(f.AnnualRevenue, func(i, j int) bool {
		return f.AnnualRevenue[i].Date < f.AnnualRevenue[j].Date
	})

	nRev := len(f.AnnualRevenue)
	if nRev < 3 {
		return false, 0.0, 0.0
	}

	revLatest := f.AnnualRevenue[nRev-1].Value
	revOldest := f.AnnualRevenue[0].Value
	yearsDiff := float64(nRev - 1)
	cagr3y := 0.0
	if revOldest > 0 {
		cagr3y = math.Pow(revLatest/revOldest, 1.0/yearsDiff) - 1.0
	}

	baseRev := revLatest
	if nRev >= 2 {
		pctDiff := math.Abs(f.TTMRevenue-revLatest) / (revLatest + 1e-10)
		if pctDiff < 0.02 {
			baseRev = f.AnnualRevenue[nRev-2].Value
		}
	}
	ttmGrowth := 0.0
	if f.TTMRevenue > 0 && baseRev > 0 {
		ratio := f.TTMRevenue / baseRev
		if ratio < 0.10 && nRev >= 2 {
			// If TTM is 10x+ smaller due to Yahoo Finance API unit scale discrepancy,
			// fallback to latest annual YoY growth
			prevAnnual := f.AnnualRevenue[nRev-1].Value
			prev2Annual := f.AnnualRevenue[nRev-2].Value
			if prev2Annual > 0 {
				ttmGrowth = (prevAnnual / prev2Annual) - 1.0
			}
		} else {
			ttmGrowth = ratio - 1.0
		}
	}

	return ttmGrowth > cagr3y, ttmGrowth, cagr3y
}

// CalculateAssetTurnoverCapEx checks Asset Turnover and CapEx Inflection rule
func CalculateAssetTurnoverCapEx(f *Fundamentals, maxCapExYoYMultiplier float64) (bool, float64, float64, float64, float64) {
	sort.Slice(f.AnnualNetPPE, func(i, j int) bool {
		return f.AnnualNetPPE[i].Date < f.AnnualNetPPE[j].Date
	})
	sort.Slice(f.AnnualCapEx, func(i, j int) bool {
		return f.AnnualCapEx[i].Date < f.AnnualCapEx[j].Date
	})

	if maxCapExYoYMultiplier <= 0 {
		maxCapExYoYMultiplier = 1.15
	}

	type ppeCap struct {
		rev   float64
		ppe   float64
		capex float64
	}
	dateMap := make(map[string]*ppeCap)
	for _, r := range f.AnnualRevenue {
		dateMap[r.Date] = &ppeCap{rev: r.Value}
	}
	for _, p := range f.AnnualNetPPE {
		if val, ok := dateMap[p.Date]; ok {
			val.ppe = p.Value
		}
	}
	for _, c := range f.AnnualCapEx {
		if val, ok := dateMap[c.Date]; ok {
			val.capex = c.Value
		}
	}

	var commonDates []string
	for d, val := range dateMap {
		if val.rev > 0 && val.ppe > 0 && val.capex != 0 {
			commonDates = append(commonDates, d)
		}
	}
	sort.Strings(commonDates)
	if len(commonDates) < 2 {
		return false, 0.0, 0.0, 0.0, 0.0
	}

	latestDate := commonDates[len(commonDates)-1]
	prevDate := commonDates[len(commonDates)-2]

	valLatest := dateMap[latestDate]
	valPrev := dateMap[prevDate]

	atLatest := valLatest.rev / valLatest.ppe
	atPrev := valPrev.rev / valPrev.ppe

	capexLatestAbs := math.Abs(valLatest.capex)
	capexPrevAbs := math.Abs(valPrev.capex)

	pctCapExChange := 0.0
	if capexPrevAbs > 0 {
		pctCapExChange = ((capexLatestAbs - capexPrevAbs) / capexPrevAbs) * 100.0
	}

	passed := atLatest > atPrev && (capexPrevAbs == 0 || capexLatestAbs <= capexPrevAbs*maxCapExYoYMultiplier)
	return passed, atPrev, atLatest, pctCapExChange, capexLatestAbs
}

// CalculateDSO checks the Working Capital DSO improvement rule
func CalculateDSO(f *Fundamentals) (bool, float64, float64) {
	sort.Slice(f.AnnualAccountsReceivable, func(i, j int) bool {
		return f.AnnualAccountsReceivable[i].Date < f.AnnualAccountsReceivable[j].Date
	})

	type arRev struct {
		rev float64
		ar  float64
	}
	arDateMap := make(map[string]*arRev)
	for _, r := range f.AnnualRevenue {
		arDateMap[r.Date] = &arRev{rev: r.Value}
	}
	for _, ar := range f.AnnualAccountsReceivable {
		if val, ok := arDateMap[ar.Date]; ok {
			val.ar = ar.Value
		}
	}

	var arCommonDates []string
	for d, val := range arDateMap {
		if val.rev > 0 && val.ar > 0 {
			arCommonDates = append(arCommonDates, d)
		}
	}
	sort.Strings(arCommonDates)
	if len(arCommonDates) < 2 {
		return false, 0.0, 0.0
	}

	latestArDate := arCommonDates[len(arCommonDates)-1]
	prevArDate := arCommonDates[len(arCommonDates)-2]

	dsoLatest := (arDateMap[latestArDate].ar / arDateMap[latestArDate].rev) * 365.0
	dsoPrev := (arDateMap[prevArDate].ar / arDateMap[prevArDate].rev) * 365.0

	dsoTarget := dsoPrev
	if len(arCommonDates) >= 3 {
		twoAgoArDate := arCommonDates[len(arCommonDates)-3]
		dso2Ago := (arDateMap[twoAgoArDate].ar / arDateMap[twoAgoArDate].rev) * 365.0
		dsoTarget = dso2Ago
	}

	passed := dsoLatest < dsoTarget || dsoLatest < dsoPrev
	return passed, dsoPrev, dsoLatest
}

// GetVolumeBreakoutMultiplier calculates the maximum volume multiplier of green days in a rolling lookback window compared to average red days volume.
func GetVolumeBreakoutMultiplier(closes, opens, volumes []float64, lookback int) float64 {
	n := len(closes)
	if n < 2 {
		return 0.0
	}

	if lookback <= 0 {
		lookback = 60
	}

	if n < lookback {
		lookback = n
	}
	startIndex := n - lookback

	redVolSum := 0.0
	redCount := 0
	allVolSum := 0.0

	for i := startIndex; i < n; i++ {
		allVolSum += volumes[i]
		isRed := false
		if i < len(opens) && opens[i] > 0 {
			isRed = closes[i] < opens[i]
		} else if i > 0 {
			isRed = closes[i] < closes[i-1]
		}
		if isRed {
			redVolSum += volumes[i]
			redCount++
		}
	}

	avgRedVol := 0.0
	if redCount > 0 {
		avgRedVol = redVolSum / float64(redCount)
	} else {
		avgRedVol = allVolSum / float64(lookback)
	}

	if avgRedVol == 0 {
		return 0.0
	}

	maxMult := 0.0
	for i := startIndex; i < n; i++ {
		isGreen := false
		if i < len(opens) && opens[i] > 0 {
			isGreen = closes[i] >= opens[i]
		} else if i > 0 {
			isGreen = closes[i] >= closes[i-1]
		}
		if isGreen {
			mult := volumes[i] / avgRedVol
			if mult > maxMult {
				maxMult = mult
			}
		}
	}

	return maxMult
}

// CalculateEarningsGrowth calculates 3-year CAGR of net income.
func CalculateEarningsGrowth(f *Fundamentals) float64 {
	if len(f.EarningsHistory) < 2 {
		return 0.0
	}
	// Copy and sort earnings history by year
	history := make([]AnnualFinancial, len(f.EarningsHistory))
	copy(history, f.EarningsHistory)
	sort.Slice(history, func(i, j int) bool {
		return history[i].Year < history[j].Year
	})
	n := len(history)
	latestEarnings := history[n-1].Earnings
	oldestEarnings := history[0].Earnings
	yearsDiff := float64(history[n-1].Year - history[0].Year)
	if yearsDiff <= 0 || oldestEarnings <= 0 || latestEarnings <= 0 {
		return 0.0
	}
	cagr := math.Pow(latestEarnings/oldestEarnings, 1.0/yearsDiff) - 1.0
	return cagr
}

// CalculateGrossMarginTrajectory evaluates gross profit margins YoY.
func CalculateGrossMarginTrajectory(f *Fundamentals) (bool, float64, float64, bool) {
	if len(f.AnnualGrossProfit) < 2 || len(f.AnnualRevenue) < 2 {
		return false, 0.0, 0.0, false
	}
	// Copy and sort
	gpList := make([]AnnualMetric, len(f.AnnualGrossProfit))
	copy(gpList, f.AnnualGrossProfit)
	sort.Slice(gpList, func(i, j int) bool {
		return gpList[i].Date < gpList[j].Date
	})

	revList := make([]AnnualMetric, len(f.AnnualRevenue))
	copy(revList, f.AnnualRevenue)
	sort.Slice(revList, func(i, j int) bool {
		return revList[i].Date < revList[j].Date
	})

	type revGP struct {
		rev float64
		gp  float64
	}
	dateMap := make(map[string]*revGP)
	for _, r := range revList {
		dateMap[r.Date] = &revGP{rev: r.Value}
	}
	for _, g := range gpList {
		if val, ok := dateMap[g.Date]; ok {
			val.gp = g.Value
		}
	}

	var commonDates []string
	for d, val := range dateMap {
		if val.rev > 0 && val.gp > 0 {
			commonDates = append(commonDates, d)
		}
	}
	sort.Strings(commonDates)

	n := len(commonDates)
	if n < 2 {
		return false, 0.0, 0.0, false
	}

	latestDate := commonDates[n-1]
	prevDate := commonDates[n-2]

	latestGM := dateMap[latestDate].gp / dateMap[latestDate].rev
	prevGM := dateMap[prevDate].gp / dateMap[prevDate].rev

	// Stable/expanding latest gross margin is >= previous gross margin - 0.5% tolerance
	isStableOrExpanding := latestGM >= (prevGM - 0.005)

	return isStableOrExpanding, latestGM, prevGM, true
}

// CalculateOperatingMarginTrajectory evaluates operating margins YoY.
func CalculateOperatingMarginTrajectory(f *Fundamentals) (bool, float64, float64, bool) {
	if len(f.AnnualOperatingIncome) < 2 || len(f.AnnualRevenue) < 2 {
		return false, 0.0, 0.0, false
	}
	// Copy and sort
	opList := make([]AnnualMetric, len(f.AnnualOperatingIncome))
	copy(opList, f.AnnualOperatingIncome)
	sort.Slice(opList, func(i, j int) bool {
		return opList[i].Date < opList[j].Date
	})

	revList := make([]AnnualMetric, len(f.AnnualRevenue))
	copy(revList, f.AnnualRevenue)
	sort.Slice(revList, func(i, j int) bool {
		return revList[i].Date < revList[j].Date
	})

	type revOp struct {
		rev float64
		op  float64
	}
	dateMap := make(map[string]*revOp)
	for _, r := range revList {
		dateMap[r.Date] = &revOp{rev: r.Value}
	}
	for _, o := range opList {
		if val, ok := dateMap[o.Date]; ok {
			val.op = o.Value
		}
	}

	var commonDates []string
	for d, val := range dateMap {
		if val.rev > 0 && val.op != 0 {
			commonDates = append(commonDates, d)
		}
	}
	sort.Strings(commonDates)

	n := len(commonDates)
	if n < 2 {
		return false, 0.0, 0.0, false
	}

	latestDate := commonDates[n-1]
	prevDate := commonDates[n-2]

	latestOM := dateMap[latestDate].op / dateMap[latestDate].rev
	prevOM := dateMap[prevDate].op / dateMap[prevDate].rev

	isStableOrExpanding := latestOM >= (prevOM - 0.005)

	return isStableOrExpanding, latestOM, prevOM, true
}

// CalculateCROIC computes Cash Return on Invested Capital.
func CalculateCROIC(f *Fundamentals) (float64, bool) {
	// If both FCF and CFO are exactly 0.0, treat it as a data coverage gap
	if f.FreeCashflow == 0.0 && f.OperatingCashflow == 0.0 {
		return 0.0, false
	}

	totalEquity := 0.0
	if f.PBRatio > 0 {
		totalEquity = f.MarketCap / f.PBRatio
	} else {
		// Fallback if PBRatio is missing or <= 0
		return 0.0, false
	}

	investedCapital := totalEquity + f.TotalDebt
	if investedCapital <= 0 {
		return 0.0, false
	}

	// Yahoo Finance FreeCashflow can be extremely buggy/laggy or missing for international tickers.
	// If annual CapEx data is available, compute FCF as OperatingCashflow - CapEx.
	fcf := f.FreeCashflow
	if len(f.AnnualCapEx) > 0 {
		sort.Slice(f.AnnualCapEx, func(i, j int) bool {
			return f.AnnualCapEx[i].Date < f.AnnualCapEx[j].Date
		})
		latestCapEx := math.Abs(f.AnnualCapEx[len(f.AnnualCapEx)-1].Value)
		fcf = f.OperatingCashflow - latestCapEx
	}

	croic := fcf / investedCapital
	return croic, true
}

// CalculateCapExTrend calculates the YoY growth in annual CapEx.
func CalculateCapExTrend(f *Fundamentals) (latestCr float64, prevCr float64, pctGrowth float64, ok bool) {
	if len(f.AnnualCapEx) < 2 {
		if len(f.AnnualCapEx) == 1 {
			return math.Abs(f.AnnualCapEx[0].Value) / 1e7, 0.0, 0.0, true
		}
		return 0.0, 0.0, 0.0, false
	}
	sorted := make([]AnnualMetric, len(f.AnnualCapEx))
	copy(sorted, f.AnnualCapEx)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Date < sorted[j].Date
	})

	n := len(sorted)
	latest := math.Abs(sorted[n-1].Value)
	prev := math.Abs(sorted[n-2].Value)

	latestCr = latest / 1e7
	prevCr = prev / 1e7

	if prev > 0 {
		pctGrowth = ((latest - prev) / prev) * 100.0
	}
	return latestCr, prevCr, pctGrowth, true
}

// CalculateEarningsBeatRate calculates annual earnings expansion years out of history.
func CalculateEarningsBeatRate(f *Fundamentals) (beats int, total int, ok bool) {
	if len(f.EarningsHistory) < 2 {
		return 0, 0, false
	}
	for i := 1; i < len(f.EarningsHistory); i++ {
		total++
		if f.EarningsHistory[i].Earnings > f.EarningsHistory[i-1].Earnings {
			beats++
		}
	}
	if total == 0 {
		return 0, 0, false
	}
	return beats, total, true
}

// CalculateCompositeRS calculates multi-timeframe relative strength (1M, 3M, 12M) vs benchmark.
func CalculateCompositeRS(stockCloses, benchCloses []float64) (compositeRS, rs1m, rs3m, rs12m float64) {
	nStock := len(stockCloses)
	nBench := len(benchCloses)
	if nStock < 2 {
		return 0, 0, 0, 0
	}

	calcReturn := func(arr []float64, days int) float64 {
		n := len(arr)
		if n < 2 {
			return 0.0
		}
		if days >= n {
			days = n - 1
		}
		startVal := arr[n-1-days]
		endVal := arr[n-1]
		if startVal <= 0 {
			return 0.0
		}
		return (endVal - startVal) / startVal
	}

	stock1m := calcReturn(stockCloses, 21)
	stock3m := calcReturn(stockCloses, 63)
	stock12m := calcReturn(stockCloses, 252)

	bench1m, bench3m, bench12m := 0.0, 0.0, 0.0
	if nBench >= 2 {
		bench1m = calcReturn(benchCloses, 21)
		bench3m = calcReturn(benchCloses, 63)
		bench12m = calcReturn(benchCloses, 252)
	}

	rs1m = stock1m - bench1m
	rs3m = stock3m - bench3m
	rs12m = stock12m - bench12m

	// Weighted composite: 40% 1-Month, 30% 3-Month, 30% 12-Month
	compositeRS = (0.40 * rs1m) + (0.30 * rs3m) + (0.30 * rs12m)
	return compositeRS, rs1m, rs3m, rs12m
}

// CalculateVCPTightness calculates short-term ATR vs long-term ATR volatility contraction.
func CalculateVCPTightness(closes, opens []float64) (vcpRatio float64, isTight bool) {
	n := len(closes)
	if n < 15 {
		return 1.0, false
	}

	calcNormalizedTR := func(i int) float64 {
		c := closes[i]
		if c <= 0 {
			return 0.0
		}
		o := c
		if i < len(opens) && opens[i] > 0 {
			o = opens[i]
		}
		prevC := c
		if i > 0 {
			prevC = closes[i-1]
		}

		tr := math.Max(math.Abs(c-o), math.Max(math.Abs(c-prevC), math.Abs(o-prevC)))
		return tr / c
	}

	// 10-day short-term ATR
	lookback10 := 10
	if n < lookback10 {
		lookback10 = n
	}
	sumTR10 := 0.0
	for i := n - lookback10; i < n; i++ {
		sumTR10 += calcNormalizedTR(i)
	}
	atr10 := sumTR10 / float64(lookback10)

	// 60-day long-term ATR
	lookback60 := 60
	if n < lookback60 {
		lookback60 = n
	}
	sumTR60 := 0.0
	for i := n - lookback60; i < n; i++ {
		sumTR60 += calcNormalizedTR(i)
	}
	atr60 := sumTR60 / float64(lookback60)

	if atr60 <= 0 {
		return 1.0, false
	}

	vcpRatio = atr10 / atr60
	isTight = vcpRatio <= 0.75
	return vcpRatio, isTight
}

// CheckPocketPivot checks if any session in the past lookback days had a Pocket Pivot
// (an up-day where volume exceeded the largest down-day volume in the previous 10 sessions).
func CheckPocketPivot(closes, opens, volumes []float64, lookback int) (hasPocketPivot bool, daysAgo int, intensity float64) {
	n := len(closes)
	if n < 12 || len(volumes) < n {
		return false, -1, 0.0
	}

	if lookback <= 0 {
		lookback = 10
	}
	if lookback > n-11 {
		lookback = n - 11
	}

	for i := n - 1; i >= n-lookback; i-- {
		isGreen := false
		if i < len(opens) && opens[i] > 0 {
			isGreen = closes[i] >= opens[i]
		} else if i > 0 {
			isGreen = closes[i] >= closes[i-1]
		}

		if !isGreen {
			continue
		}

		// Find highest down-day volume in previous 10 trading sessions before i
		maxDownVol := 0.0
		for j := i - 1; j >= i-10 && j >= 0; j-- {
			isRed := false
			if j < len(opens) && opens[j] > 0 {
				isRed = closes[j] < opens[j]
			} else if j > 0 {
				isRed = closes[j] < closes[j-1]
			}
			if isRed && volumes[j] > maxDownVol {
				maxDownVol = volumes[j]
			}
		}

		if maxDownVol > 0 && volumes[i] > maxDownVol {
			return true, n - 1 - i, volumes[i] / maxDownVol
		}
	}

	return false, -1, 0.0
}

// CalculateProximity52W returns the ratio of the latest close to the 52-week (252 days) high.
func CalculateProximity52W(closes []float64) float64 {
	n := len(closes)
	if n == 0 {
		return 0.0
	}
	lookback := 252
	if n < lookback {
		lookback = n
	}

	maxPrice := 0.0
	for i := n - lookback; i < n; i++ {
		if closes[i] > maxPrice {
			maxPrice = closes[i]
		}
	}

	if maxPrice <= 0 {
		return 0.0
	}
	return closes[n-1] / maxPrice
}

// CalculateDecayedPocketPivot computes a recency-decayed Pocket Pivot accumulation score with capped intensity.
func CalculateDecayedPocketPivot(closes, opens, volumes []float64, lookback int, decayFactor float64) (score float64, count int) {
	n := len(closes)
	if n < 12 || len(volumes) < n {
		return 0.0, 0
	}

	if lookback <= 0 {
		lookback = 10
	}
	if lookback > n-11 {
		lookback = n - 11
	}
	if decayFactor <= 0 {
		decayFactor = 0.25
	}

	for i := n - 1; i >= n-lookback; i-- {
		isGreen := false
		if i < len(opens) && opens[i] > 0 {
			isGreen = closes[i] >= opens[i]
		} else if i > 0 {
			isGreen = closes[i] >= closes[i-1]
		}

		if !isGreen {
			continue
		}

		maxDownVol := 0.0
		for j := i - 1; j >= i-10 && j >= 0; j-- {
			isRed := false
			if j < len(opens) && opens[j] > 0 {
				isRed = closes[j] < opens[j]
			} else if j > 0 {
				isRed = closes[j] < closes[j-1]
			}
			if isRed && volumes[j] > maxDownVol {
				maxDownVol = volumes[j]
			}
		}

		if maxDownVol > 0 && volumes[i] > maxDownVol {
			daysAgo := float64(n - 1 - i)
			intensity := volumes[i] / maxDownVol
			if intensity > 3.0 {
				intensity = 3.0 // Cap daily intensity at 3.0
			}
			decayWeight := math.Exp(-decayFactor * daysAgo)
			score += intensity * decayWeight
			count++
		}
	}

	return score, count
}

// CalculateRVOLZScore computes Relative Volume Z-score comparing short window (5D) to long window (50D) with winsorization.
func CalculateRVOLZScore(volumes []float64, shortWindow, longWindow int) float64 {
	return CalculateWinsorizedRVOLZScore(volumes, shortWindow, longWindow, 4.0)
}

// CalculateWinsorizedRVOLZScore computes Relative Volume Z-score with daily volume winsorized at winsorizeMult * 20D average volume.
func CalculateWinsorizedRVOLZScore(volumes []float64, shortWindow, longWindow int, winsorizeMult float64) float64 {
	n := len(volumes)
	if n < 10 {
		return 0.0
	}

	if shortWindow <= 0 {
		shortWindow = 5
	}
	if longWindow <= 0 {
		longWindow = 50
	}
	if longWindow > n {
		longWindow = n
	}
	if shortWindow > longWindow {
		shortWindow = longWindow / 2
	}
	if winsorizeMult <= 0 {
		winsorizeMult = 4.0
	}

	// Calculate 20-day mean volume for winsorization cap
	vol20Lookback := 20
	if n < vol20Lookback {
		vol20Lookback = n
	}
	sum20 := 0.0
	for i := n - vol20Lookback; i < n; i++ {
		sum20 += volumes[i]
	}
	mean20 := sum20 / float64(vol20Lookback)
	volCap := winsorizeMult * mean20

	cappedVols := make([]float64, n)
	for i := 0; i < n; i++ {
		if volCap > 0 && volumes[i] > volCap {
			cappedVols[i] = volCap
		} else {
			cappedVols[i] = volumes[i]
		}
	}

	sumShort := 0.0
	for i := n - shortWindow; i < n; i++ {
		sumShort += cappedVols[i]
	}
	meanShort := sumShort / float64(shortWindow)

	sumLong := 0.0
	for i := n - longWindow; i < n; i++ {
		sumLong += cappedVols[i]
	}
	meanLong := sumLong / float64(longWindow)

	varianceLong := 0.0
	for i := n - longWindow; i < n; i++ {
		diff := cappedVols[i] - meanLong
		varianceLong += diff * diff
	}
	stdDevLong := math.Sqrt(varianceLong / float64(longWindow))

	if stdDevLong <= 0 {
		return 0.0
	}

	return (meanShort - meanLong) / stdDevLong
}

// CalculateBaseDurationWeeks counts consecutive weeks the stock stayed within the upper base zone (e.g. >= 85% of 52W high).
func CalculateBaseDurationWeeks(closes []float64, zoneFloorPct float64) (weeks int, inBase bool) {
	n := len(closes)
	if n < 10 {
		return 0, false
	}

	if zoneFloorPct <= 0 {
		zoneFloorPct = 0.85
	}

	lookback := 252
	if n < lookback {
		lookback = n
	}

	maxPrice := 0.0
	for i := n - lookback; i < n; i++ {
		if closes[i] > maxPrice {
			maxPrice = closes[i]
		}
	}

	if maxPrice <= 0 {
		return 0, false
	}

	floorPrice := zoneFloorPct * maxPrice
	consecutiveDays := 0
	for i := n - 1; i >= 0; i-- {
		if closes[i] >= floorPrice {
			consecutiveDays++
		} else {
			break
		}
	}

	weeks = consecutiveDays / 5
	inBase = weeks >= 4
	return weeks, inBase
}

// CalculateSmoothedBenchmarkRegime calculates the continuous Market Regime Multiplier R_regime in [minFloor, 1.0].
func CalculateSmoothedBenchmarkRegime(benchCloses []float64, period int, minFloor float64) (regimeScore float64) {
	n := len(benchCloses)
	if n < 20 {
		return 1.0 // fallback if insufficient benchmark history
	}

	if period <= 0 {
		period = 50
	}
	if period > n {
		period = n
	}
	if minFloor <= 0 {
		minFloor = 0.20
	}

	// 1. Calculate 50-Day SMA
	sumSMA := 0.0
	for i := n - period; i < n; i++ {
		sumSMA += benchCloses[i]
	}
	sma50 := sumSMA / float64(period)
	if sma50 <= 0 {
		return 1.0
	}

	// 2. Count sessions above 50-DMA over last 20 sessions
	sessionsAbove := 0
	evalWindow := 20
	if n < evalWindow {
		evalWindow = n
	}
	for i := n - evalWindow; i < n; i++ {
		// Calculate rolling SMA at index i if possible, or compare to current SMA
		if benchCloses[i] >= sma50 {
			sessionsAbove++
		}
	}
	persistenceRatio := float64(sessionsAbove) / float64(evalWindow)

	// 3. Price distance to 50-DMA
	latestClose := benchCloses[n-1]
	distPct := (latestClose - sma50) / sma50
	scaledDist := distPct / 0.10
	if scaledDist > 0.40 {
		scaledDist = 0.40
	}
	if scaledDist < -0.40 {
		scaledDist = -0.40
	}

	// 4. Combined R_regime formula
	rawR := 0.20 + (0.60 * persistenceRatio) + (0.50 * scaledDist)
	if rawR > 1.0 {
		rawR = 1.0
	}
	if rawR < minFloor {
		rawR = minFloor
	}

	return rawR
}

// CalculateBenchmarkRegime evaluates if benchmark index is in a bullish regime above its SMA period (e.g. 50-day).
func CalculateBenchmarkRegime(benchCloses []float64, period int) (isBullish bool, ratio float64) {
	n := len(benchCloses)
	if n < 10 {
		return true, 1.0 // fallback if insufficient benchmark history
	}

	if period <= 0 {
		period = 50
	}
	if period > n {
		period = n
	}

	sum := 0.0
	for i := n - period; i < n; i++ {
		sum += benchCloses[i]
	}
	sma := sum / float64(period)
	if sma <= 0 {
		return true, 1.0
	}

	latest := benchCloses[n-1]
	ratio = latest / sma
	// Allow slight buffer (e.g. >= 0.99)
	isBullish = ratio >= 0.99
	return isBullish, ratio
}

// IsEarningsBlackout returns true if scheduled quarterly results fall within blackoutDays from today.
func IsEarningsBlackout(resultComingDate string, blackoutDays int) bool {
	resultComingDate = strings.TrimSpace(resultComingDate)
	if resultComingDate == "" || resultComingDate == "N/A" || resultComingDate == "-" {
		return false
	}

	if blackoutDays <= 0 {
		blackoutDays = 5
	}

	layouts := []string{"02-01-06", "02-01-2006", "2006-01-02", "02/01/2006", "02/01/06"}
	var parsed time.Time
	var err error
	for _, l := range layouts {
		parsed, err = time.Parse(l, resultComingDate)
		if err == nil {
			break
		}
	}

	if err != nil {
		return false
	}

	now := time.Now()
	// Calculate difference in days
	daysUntil := int(parsed.Sub(now).Hours() / 24.0)
	if daysUntil >= -1 && daysUntil <= blackoutDays {
		return true
	}

	return false
}


