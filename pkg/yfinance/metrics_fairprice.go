package yfinance

import (
	"fmt"
	"math"
	"sort"
)

// FairPriceResult encapsulates the complete intrinsic valuation breakdown for a stock.
type FairPriceResult struct {
	// Individual Model Fair Prices (nil indicates incomputable)
	DCFFairPrice      *float64 `json:"dcf_fair_price,omitempty"`
	EPVFairPrice      *float64 `json:"epv_fair_price,omitempty"`
	GrahamNumber      *float64 `json:"graham_number,omitempty"`
	RelativeFairPrice *float64 `json:"relative_fair_price,omitempty"`
	PEGFairPrice      *float64 `json:"peg_fair_price,omitempty"`

	// Ensemble Aggregates
	RawEnsembleFairPrice float64 `json:"raw_ensemble_fair_price"` // Unshrunk raw weighted ensemble price
	EnsembleFairPrice    float64 `json:"ensemble_fair_price"`     // Bayesian confidence-shrunk fair price
	CMP                  float64 `json:"cmp"`
	UpsidePct         float64 `json:"upside_pct"`
	Verdict           string  `json:"verdict"` // DEEPLY_UNDERVALUED, UNDERVALUED, FAIRLY_VALUED, OVERVALUED, DEEPLY_OVERVALUED
	ValidModelCount   int     `json:"valid_model_count"`
	ModelCV           float64 `json:"model_cv"`
	ConfidenceScore   float64 `json:"confidence_score"` // 0.0 to 100.0%

	// Margin of Safety Band
	PessimisticFairPrice float64 `json:"pessimistic_fair_price"` // Base * 0.80
	OptimisticFairPrice  float64 `json:"optimistic_fair_price"`  // Base * 1.15

	// Diagnostic Metrics
	WACCUsed          float64 `json:"wacc_used"`
	SustainableGrowth float64 `json:"sustainable_growth"`
	Clamped           bool    `json:"clamped"`
}

// SectorMedians stores cohort-level multiple medians for relative valuation.
type SectorMedians struct {
	PE       float64
	PB       float64
	EVEBITDA float64
}

// SectorWACC returns the sector-adjusted cost of capital.
func SectorWACC(sector string) float64 {
	switch sector {
	case "Consumer Defensive", "Healthcare", "Utilities":
		return 0.100
	case "Industrials", "Basic Materials", "Consumer Cyclical":
		return 0.110
	case "Technology", "Communication Services":
		return 0.120
	case "Energy", "Real Estate":
		return 0.130
	case "Financial Services":
		return 0.115
	default:
		return 0.115
	}
}

// CalculateDCFFairPrice implements the 2-Stage Discounted Cash Flow per share.
func CalculateDCFFairPrice(f *Fundamentals, wacc, terminalGrowth float64) (*float64, bool) {
	if f == nil || f.RegularPrice <= 0 || f.MarketCap <= 0 {
		return nil, false
	}
	shares := f.MarketCap / f.RegularPrice
	if shares <= 0 {
		return nil, false
	}

	if wacc <= 0 {
		wacc = SectorWACC(f.Sector)
	}
	if terminalGrowth <= 0 {
		terminalGrowth = 0.050 // 5.0% long-term GDP growth for India
	}

	// Banking and financial institutions have non-standard cash flows where
	// changes in deposits and loan disbursements dominate operating cash flows.
	// Standard corporate FCF-based DCF is structurally inapplicable; bypass to
	// allow weights to adaptively redistribute to EPV, Graham Number, and P/B & P/E comps.
	if f.Sector == "Financial Services" {
		return nil, false
	}

	nOp := len(f.AnnualOperatingIncome)
	// A company with negative Net Income AND negative Operating Income cannot have
	// positive recurring FCF for a standard growth DCF.
	if f.NetIncome <= 0 && nOp > 0 && f.AnnualOperatingIncome[nOp-1].Value <= 0 {
		return nil, false
	}

	// 1. Determine FCF0 using waterfall
	var fcf0 float64
	nFCF := len(f.AnnualFreeCashFlow)
	if nFCF > 0 && f.AnnualFreeCashFlow[nFCF-1].Value > 0 {
		fcf0 = f.AnnualFreeCashFlow[nFCF-1].Value
	} else if nFCF >= 2 {
		// 3-year or multi-year average FCF
		sum := 0.0
		count := 0
		for i := max(0, nFCF-3); i < nFCF; i++ {
			if f.AnnualFreeCashFlow[i].Value > 0 {
				sum += f.AnnualFreeCashFlow[i].Value
				count++
			}
		}
		if count > 0 {
			fcf0 = sum / float64(count)
		}
	}

	// Tier 3 proxy: Normalized Operating Cash Flow - Maintenance CapEx (60% of CapEx)
	nOCF := len(f.AnnualOperatingCashFlow)
	if fcf0 <= 0 && nOCF > 0 && f.AnnualOperatingCashFlow[nOCF-1].Value > 0 {
		capex := 0.0
		nCapEx := len(f.AnnualCapEx)
		if nCapEx > 0 {
			capex = math.Abs(f.AnnualCapEx[nCapEx-1].Value)
		}
		fcf0 = f.AnnualOperatingCashFlow[nOCF-1].Value - (capex * 0.60)
	}

	// If still <= 0, try scalar fields (only if company has positive Net Income or Operating Income)
	if fcf0 <= 0 && f.FreeCashflow > 0 && (f.NetIncome > 0 || (nOp > 0 && f.AnnualOperatingIncome[nOp-1].Value > 0)) {
		fcf0 = f.FreeCashflow
	}
	if fcf0 <= 0 && f.OperatingCashflow > 0 && f.NetIncome > 0 {
		fcf0 = f.OperatingCashflow * 0.50
	}

	// Sanity cap: Sustainable recurring FCF cannot indefinitely exceed normalized operating earnings
	// or net income (prevents one-off asset sales, HAM project concessions, or working-capital spikes from blowing up DCF).
	if f.NetIncome > 0 && fcf0 > 2.0*f.NetIncome {
		fcf0 = 2.0 * f.NetIncome
	}
	if nOp > 0 && f.AnnualOperatingIncome[nOp-1].Value > 0 && fcf0 > 1.8*f.AnnualOperatingIncome[nOp-1].Value {
		fcf0 = 1.8 * f.AnnualOperatingIncome[nOp-1].Value
	}

	if fcf0 <= 0 {
		return nil, false
	}

	// 2. Estimate ROCE to anchor maximum growth
	roce := estimateROCE(f)
	maxGrowth := 0.18
	if roce >= 0.22 {
		maxGrowth = 0.25
	} else if roce < 0.10 {
		maxGrowth = 0.06
	}

	// Historical growth rate from 3-year revenue
	g := 0.08 // conservative fallback
	nRev := len(f.AnnualRevenue)
	if nRev >= 3 && f.AnnualRevenue[nRev-3].Value > 0 && f.AnnualRevenue[nRev-1].Value > 0 {
		revCAGR := math.Pow(f.AnnualRevenue[nRev-1].Value/f.AnnualRevenue[nRev-3].Value, 1.0/2.0) - 1.0
		if revCAGR > 0 {
			g = revCAGR
		}
	} else if _, ttmGrowth, cagr3y := CalculateSalesGrowth(f); cagr3y > 0 || ttmGrowth > 0 {
		if cagr3y > 0 {
			g = cagr3y
		} else {
			g = ttmGrowth
		}
	}

	if g > maxGrowth {
		g = maxGrowth
	}
	if g < 0.04 {
		g = 0.04
	}

	// 3. Stage 1 PV (5 years)
	pvStage1 := 0.0
	fcfCurrent := fcf0
	for t := 1; t <= 5; t++ {
		fcfCurrent *= (1.0 + g)
		pvStage1 += fcfCurrent / math.Pow(1.0+wacc, float64(t))
	}

	// 4. Terminal Value PV
	if wacc <= terminalGrowth {
		return nil, false
	}
	terminalValue := (fcfCurrent * (1.0 + terminalGrowth)) / (wacc - terminalGrowth)
	pvTerminal := terminalValue / math.Pow(1.0+wacc, 5.0)

	// 5. Enterprise to Equity Value
	ev := pvStage1 + pvTerminal
	cash := 0.0
	if f.OperatingCashflow > 0 {
		cash = f.OperatingCashflow * 0.50
	}
	equityVal := ev - f.TotalDebt + cash
	if equityVal <= 0 {
		return nil, false
	}

	fairPrice := equityVal / shares
	if fairPrice <= 0 || math.IsNaN(fairPrice) || math.IsInf(fairPrice, 0) {
		return nil, false
	}

	return &fairPrice, true
}

// CalculateEPVPerShare implements Bruce Greenwald's Earnings Power Value per share.
func CalculateEPVPerShare(f *Fundamentals, wacc float64) (*float64, bool) {
	if f == nil || f.RegularPrice <= 0 || f.MarketCap <= 0 {
		return nil, false
	}
	shares := f.MarketCap / f.RegularPrice
	if shares <= 0 {
		return nil, false
	}

	if wacc <= 0 {
		wacc = SectorWACC(f.Sector)
	}

	// For Financial Services (Banks/NBFCs), debt is operational raw material (deposits/borrowings).
	// Net Income directly reflects earnings power attributable to equity holders.
	if f.Sector == "Financial Services" {
		if f.NetIncome <= 0 {
			return nil, false
		}
		equityEPV := f.NetIncome / wacc
		fairPrice := equityEPV / shares
		if fairPrice <= 0 || math.IsNaN(fairPrice) || math.IsInf(fairPrice, 0) {
			return nil, false
		}
		return &fairPrice, true
	}

	epvEnterprise, _, ok := CalculateEPV(f, wacc)
	if !ok || epvEnterprise <= 0 {
		return nil, false
	}

	cash := 0.0
	if f.OperatingCashflow > 0 {
		cash = f.OperatingCashflow * 0.50
	}

	equityEPV := epvEnterprise - f.TotalDebt + cash
	if equityEPV <= 0 {
		return nil, false
	}

	fairPrice := equityEPV / shares
	if fairPrice <= 0 || math.IsNaN(fairPrice) || math.IsInf(fairPrice, 0) {
		return nil, false
	}

	return &fairPrice, true
}

// CalculateGrahamNumber computes Benjamin Graham's safety floor value.
// normalizedEPS calculates the normalized earnings per share using Graham & Dodd 3-year normalization.
// If the latest Net Income is a severe outlier relative to 3-year historical operating earnings (e.g. one-off
// milestone receipts, asset sales, or biopharma out-licensing following multiple years of losses), EPS is
// anchored to the 3-year average operating earnings power.
func normalizedEPS(f *Fundamentals, shares float64) float64 {
	if f == nil || shares <= 0 {
		return 0.0
	}
	latestEPS := f.NetIncome / shares
	if latestEPS <= 0 {
		return 0.0
	}
	if f.Sector == "Financial Services" {
		return latestEPS
	}

	nOp := len(f.AnnualOperatingIncome)
	if nOp < 2 {
		return latestEPS
	}

	sumEBIT := 0.0
	count := 0
	for i := max(0, nOp-3); i < nOp; i++ {
		sumEBIT += f.AnnualOperatingIncome[i].Value
		count++
	}
	if count == 0 {
		return latestEPS
	}
	avgEBIT := sumEBIT / float64(count)

	if avgEBIT <= 0 {
		return latestEPS * 0.25
	}

	normEPS := (avgEBIT * 0.70) / shares
	if latestEPS > normEPS*2.0 {
		return 0.70*normEPS + 0.30*latestEPS
	}

	return latestEPS
}

// CalculateGrahamNumber computes the conservative Graham Number per share.
func CalculateGrahamNumber(f *Fundamentals) (*float64, bool) {
	if f == nil || f.RegularPrice <= 0 || f.MarketCap <= 0 || f.PBRatio <= 0 || f.NetIncome <= 0 {
		return nil, false
	}
	shares := f.MarketCap / f.RegularPrice
	if shares <= 0 {
		return nil, false
	}

	eps := normalizedEPS(f, shares)
	bvps := f.RegularPrice / f.PBRatio
	if eps <= 0 || bvps <= 0 {
		return nil, false
	}

	graham := math.Sqrt(22.5 * eps * bvps)
	if graham <= 0 || math.IsNaN(graham) || math.IsInf(graham, 0) {
		return nil, false
	}

	return &graham, true
}

// CalculatePEGFairPrice computes the quality-scaled PEG fair value.
func CalculatePEGFairPrice(f *Fundamentals) (*float64, bool) {
	if f == nil || f.RegularPrice <= 0 || f.MarketCap <= 0 || f.NetIncome <= 0 {
		return nil, false
	}
	shares := f.MarketCap / f.RegularPrice
	if shares <= 0 {
		return nil, false
	}

	eps := normalizedEPS(f, shares)
	if eps <= 0 {
		return nil, false
	}

	// Sustainable Growth Rate: blend 3Y sales CAGR (60%) and earnings growth (40%)
	gSales := 0.10
	if _, ttmGrowth, cagr3y := CalculateSalesGrowth(f); cagr3y > 0 {
		gSales = cagr3y
	} else if ttmGrowth > 0 {
		gSales = ttmGrowth
	}

	gEarn := 0.10
	if earnGrowth := CalculateEarningsGrowth(f); earnGrowth > 0 {
		gEarn = earnGrowth
	} else {
		gEarn = gSales
	}

	g := 0.60*gSales + 0.40*gEarn
	if g < 0.05 {
		g = 0.05
	}
	if g > 0.40 {
		g = 0.40
	}

	// Quality Multiplier
	roce := estimateROCE(f)
	deRatio := f.DebtToEquity
	if deRatio > 5.0 {
		deRatio /= 100.0
	}

	targetPEG := 1.00
	if roce >= 0.22 && deRatio < 0.5 {
		targetPEG = 1.25
	} else if roce < 0.12 || deRatio >= 1.2 {
		targetPEG = 0.80
	}

	fairPE := targetPEG * (g * 100.0)
	fairPrice := fairPE * eps
	if fairPrice <= 0 || math.IsNaN(fairPrice) || math.IsInf(fairPrice, 0) {
		return nil, false
	}

	return &fairPrice, true
}

// ComputeCohortSectorMedians computes outlier-winsorized valuation medians by sector.
func ComputeCohortSectorMedians(activeKeys []string, fundamentals map[string]Fundamentals) map[string]SectorMedians {
	type sectorAccum struct {
		pes      []float64
		pbs      []float64
		evebitdas []float64
	}
	accum := make(map[string]*sectorAccum)
	var allPEs, allPBs, allEVEBITDAs []float64

	for _, t := range activeKeys {
		f := fundamentals[t]
		sec := f.Sector
		if sec == "" {
			sec = "Unclassified"
		}
		if accum[sec] == nil {
			accum[sec] = &sectorAccum{}
		}

		// Trailing PE: NetIncome > 0
		if f.NetIncome > 0 && f.MarketCap > 0 {
			pe := f.MarketCap / f.NetIncome
			if pe >= 3.0 && pe <= 120.0 {
				accum[sec].pes = append(accum[sec].pes, pe)
				allPEs = append(allPEs, pe)
			}
		}

		// PB Ratio: PBRatio > 0
		if f.PBRatio >= 0.3 && f.PBRatio <= 30.0 {
			accum[sec].pbs = append(accum[sec].pbs, f.PBRatio)
			allPBs = append(allPBs, f.PBRatio)
		}

		// EV/EBITDA (Skip for Financial Services where EV/EBITDA is structurally invalid)
		if sec != "Financial Services" {
			nOp := len(f.AnnualOperatingIncome)
			var ebitda float64
			if nOp > 0 && f.AnnualOperatingIncome[nOp-1].Value > 0 {
				ebitda = f.AnnualOperatingIncome[nOp-1].Value
			} else if f.OperatingCashflow > 0 && f.NetIncome > 0 {
				ebitda = f.OperatingCashflow
			}
			if ebitda > 0 && f.MarketCap > 0 {
				ev := f.MarketCap + f.TotalDebt
				evEbitda := ev / ebitda
				if evEbitda >= 2.0 && evEbitda <= 60.0 {
					accum[sec].evebitdas = append(accum[sec].evebitdas, evEbitda)
					allEVEBITDAs = append(allEVEBITDAs, evEbitda)
				}
			}
		}
	}

	// Calculate cohort-wide market medians for fallbacks
	marketMedianPE := median(allPEs)
	if marketMedianPE <= 0 {
		marketMedianPE = 22.0
	}
	marketMedianPB := median(allPBs)
	if marketMedianPB <= 0 {
		marketMedianPB = 3.2
	}
	marketMedianEV := median(allEVEBITDAs)
	if marketMedianEV <= 0 {
		marketMedianEV = 14.0
	}

	medians := make(map[string]SectorMedians)
	for sec, acc := range accum {
		medPE := median(acc.pes)
		if len(acc.pes) < 3 || medPE <= 0 {
			medPE = marketMedianPE
		}

		medPB := median(acc.pbs)
		if len(acc.pbs) < 3 || medPB <= 0 {
			medPB = marketMedianPB
		}

		medEV := median(acc.evebitdas)
		if len(acc.evebitdas) < 3 || medEV <= 0 {
			medEV = marketMedianEV
		}

		medians[sec] = SectorMedians{
			PE:       medPE,
			PB:       medPB,
			EVEBITDA: medEV,
		}
	}

	// Always ensure default fallback exists
	if _, ok := medians["Unclassified"]; !ok {
		medians["Unclassified"] = SectorMedians{
			PE:       marketMedianPE,
			PB:       marketMedianPB,
			EVEBITDA: marketMedianEV,
		}
	}

	return medians
}

// ComputeRelativeValuation computes fair price using cohort sector medians.
func ComputeRelativeValuation(f *Fundamentals, medians SectorMedians) (*float64, bool) {
	if f == nil || f.RegularPrice <= 0 || f.MarketCap <= 0 {
		return nil, false
	}
	shares := f.MarketCap / f.RegularPrice
	if shares <= 0 {
		return nil, false
	}

	var candidates []float64

	// 1. Fair PE Price (anchored by Graham & Dodd normalized EPS)
	if f.NetIncome > 0 && medians.PE > 0 {
		eps := normalizedEPS(f, shares)
		if eps > 0 {
			candidates = append(candidates, eps*medians.PE)
		}
	}

	// 2. Fair PB Price
	if f.PBRatio > 0 && medians.PB > 0 {
		bvps := f.RegularPrice / f.PBRatio
		if bvps > 0 {
			candidates = append(candidates, bvps*medians.PB)
		}
	}

	// 3. Fair EV/EBITDA Price (Skip for Financial Services where debt/deposits distort EV)
	if f.Sector != "Financial Services" {
		nOp := len(f.AnnualOperatingIncome)
		var ebitda float64
		if nOp > 0 && f.AnnualOperatingIncome[nOp-1].Value > 0 {
			ebitda = f.AnnualOperatingIncome[nOp-1].Value
		} else if f.OperatingCashflow > 0 && f.NetIncome > 0 {
			ebitda = f.OperatingCashflow
		}
		if ebitda > 0 && medians.EVEBITDA > 0 {
			cash := 0.0
			if f.OperatingCashflow > 0 {
				cash = f.OperatingCashflow * 0.50
			}
			targetEV := medians.EVEBITDA * ebitda
			targetEquity := targetEV - f.TotalDebt + cash
			if targetEquity > 0 {
				candidates = append(candidates, targetEquity/shares)
			}
		}
	}

	if len(candidates) == 0 {
		return nil, false
	}

	sum := 0.0
	for _, c := range candidates {
		sum += c
	}
	fairPrice := sum / float64(len(candidates))
	if fairPrice <= 0 || math.IsNaN(fairPrice) || math.IsInf(fairPrice, 0) {
		return nil, false
	}

	return &fairPrice, true
}

// CalculateFairPrice computes the full 5-model ensemble fair price for a stock.
func CalculateFairPrice(f *Fundamentals, medians SectorMedians) (*FairPriceResult, error) {
	if f == nil {
		return nil, fmt.Errorf("fundamentals cannot be nil")
	}
	if f.RegularPrice <= 0 {
		return nil, fmt.Errorf("invalid regular price <= 0")
	}

	cmp := f.RegularPrice
	wacc := SectorWACC(f.Sector)
	terminalGrowth := 0.050

	// 1. Calculate the 5 models
	dcfPrice, _ := CalculateDCFFairPrice(f, wacc, terminalGrowth)
	epvPrice, _ := CalculateEPVPerShare(f, wacc)
	grahamPrice, _ := CalculateGrahamNumber(f)
	relPrice, _ := ComputeRelativeValuation(f, medians)
	pegPrice, _ := CalculatePEGFairPrice(f)

	type modelVal struct {
		name       string
		price      *float64
		baseWeight float64
	}

	models := []modelVal{
		{"dcf", dcfPrice, 0.30},
		{"epv", epvPrice, 0.25},
		{"graham", grahamPrice, 0.15},
		{"relative", relPrice, 0.15},
		{"peg", pegPrice, 0.15},
	}

	var validPrices []float64
	totalValidBaseWeight := 0.0
	validCount := 0

	for _, m := range models {
		if m.price != nil && *m.price > 0 && !math.IsNaN(*m.price) && !math.IsInf(*m.price, 0) {
			validPrices = append(validPrices, *m.price)
			totalValidBaseWeight += m.baseWeight
			validCount++
		}
	}

	if validCount < 2 || totalValidBaseWeight <= 0 {
		return nil, fmt.Errorf("insufficient valid models (%d valid, minimum 2 required)", validCount)
	}

	// 2. Adaptive Weight Redistribution
	rawEnsembleFairPrice := 0.0
	for _, m := range models {
		if m.price != nil && *m.price > 0 && !math.IsNaN(*m.price) && !math.IsInf(*m.price, 0) {
			effWeight := m.baseWeight / totalValidBaseWeight
			rawEnsembleFairPrice += effWeight * (*m.price)
		}
	}

	// 3. Model Convergence (CV)
	meanVal := 0.0
	for _, p := range validPrices {
		meanVal += p
	}
	meanVal /= float64(validCount)

	sumSq := 0.0
	for _, p := range validPrices {
		sumSq += (p - meanVal) * (p - meanVal)
	}
	stdDev := math.Sqrt(sumSq / float64(validCount))
	cv := 0.0
	if meanVal > 0 {
		cv = stdDev / meanVal
	}

	// 4. Valuation Confidence Score (0 - 100%)
	completenessScore := 1.0
	if len(f.AnnualRevenue) < 3 {
		completenessScore = 0.6
	}
	confScore := (float64(validCount)/5.0)*50.0 + (math.Max(0.0, 1.0-cv))*30.0 + (completenessScore * 20.0)
	if confScore > 100.0 {
		confScore = 100.0
	}
	if confScore < 20.0 {
		confScore = 20.0
	}

	// 5. Bayesian Shrinkage towards Market Prior (CMP)
	// Shrink the spread (Raw - CMP) proportional to ensemble confidence.
	// High-confidence ensembles (5 models, low CV) retain ~90-95% of their spread.
	// Low-confidence ensembles (2 models, high CV) shrink heavily towards CMP.
	confWeight := confScore / 100.0
	ensembleFairPrice := cmp + (rawEnsembleFairPrice-cmp)*confWeight

	// 6. Boundary Sanity Clamping [0.20 * CMP, 4.00 * CMP]
	clamped := false
	lowerBound := 0.20 * cmp
	upperBound := 4.00 * cmp

	if ensembleFairPrice < lowerBound {
		ensembleFairPrice = lowerBound
		clamped = true
	} else if ensembleFairPrice > upperBound {
		ensembleFairPrice = upperBound
		clamped = true
	}

	// 7. MOS Bands & Verdict
	pessimisticFP := ensembleFairPrice * 0.80
	optimisticFP := ensembleFairPrice * 1.15
	upsidePct := ((ensembleFairPrice - cmp) / cmp) * 100.0

	var verdict string
	if upsidePct >= 30.0 {
		verdict = "DEEPLY_UNDERVALUED"
	} else if upsidePct >= 12.0 {
		verdict = "UNDERVALUED"
	} else if upsidePct >= -10.0 {
		verdict = "FAIRLY_VALUED"
	} else if upsidePct >= -25.0 {
		verdict = "OVERVALUED"
	} else {
		verdict = "DEEPLY_OVERVALUED"
	}

	return &FairPriceResult{
		DCFFairPrice:         dcfPrice,
		EPVFairPrice:         epvPrice,
		GrahamNumber:         grahamPrice,
		RelativeFairPrice:    relPrice,
		PEGFairPrice:         pegPrice,
		RawEnsembleFairPrice: rawEnsembleFairPrice,
		EnsembleFairPrice:    ensembleFairPrice,
		CMP:                  cmp,
		UpsidePct:            upsidePct,
		Verdict:              verdict,
		ValidModelCount:      validCount,
		ModelCV:              cv,
		ConfidenceScore:      confScore,
		PessimisticFairPrice: pessimisticFP,
		OptimisticFairPrice:  optimisticFP,
		WACCUsed:             wacc,
		Clamped:              clamped,
	}, nil
}

// estimateROCE provides an internal helper to retrieve latest ROCE without circular imports.
func estimateROCE(f *Fundamentals) float64 {
	if f == nil {
		return 0.12
	}
	if f.Sector == "Financial Services" {
		if f.ROE > 0 {
			return f.ROE
		}
		if f.ReturnOnAssets > 0 {
			return f.ReturnOnAssets * 10.0 // ROA 1.5% -> ~15% ROE proxy
		}
		if f.NetIncome > 0 && f.PBRatio > 0 && f.MarketCap > 0 {
			bookVal := f.MarketCap / f.PBRatio
			if bookVal > 0 {
				return f.NetIncome / bookVal
			}
		}
		return 0.12
	}

	nIncome := len(f.AnnualOperatingIncome)
	nAssets := len(f.AnnualTotalAssets)
	nLiab := len(f.AnnualCurrentLiabilities)

	if nIncome > 0 && nAssets > 0 && nLiab > 0 {
		ebit := f.AnnualOperatingIncome[nIncome-1].Value
		ce := f.AnnualTotalAssets[nAssets-1].Value - f.AnnualCurrentLiabilities[nLiab-1].Value
		if ce > 0 && ebit > 0 {
			return ebit / ce
		}
	}

	if f.ROE > 0 {
		return f.ROE
	}
	return 0.12 // standard default
}

// median returns the median of a slice of float64 numbers.
func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0.0
	}
	cp := make([]float64, len(vals))
	copy(cp, vals)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2.0
}
