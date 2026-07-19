package optimizer

import (
	"math"

	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// OptimizeMultiFactor computes weights based on multi-factor scores including fundamental metrics
func OptimizeMultiFactor(
	tickers []string,
	priceHistory map[string][]float64,
	benchmarkPrices []float64,
	fundamentals map[string]yfinance.Fundamentals,
	w MFSWeights,
) map[string]float64 {
	weights := make(map[string]float64)
	nTickers := len(tickers)
	if nTickers == 0 {
		return weights
	}

	// Calculate returns for benchmark (e.g. Nifty 50)
	benchReturns := CalculateDailyReturns(benchmarkPrices)
	benchMean := CalculateMean(benchReturns)
	var benchVar float64
	if len(benchReturns) >= 2 {
		benchVar = CalculateCovariance(benchReturns, benchReturns)
	}
	if benchVar <= 0.0000001 {
		benchVar = 0.0001 // fallback variance
	}

	// Calculate averages for imputation of missing/zero values
	avg := computeAverages(tickers, fundamentals)

	// Raw factors for each ticker
	totalReturns := make(map[string]float64)
	volatilities := make(map[string]float64)
	sharpes := make(map[string]float64)
	sortinos := make(map[string]float64)
	betas := make(map[string]float64)
	alphas := make(map[string]float64)
	treynors := make(map[string]float64)
	ulcers := make(map[string]float64)

	pegs := make(map[string]float64)
	roes := make(map[string]float64)
	fwdPEs := make(map[string]float64)
	opMargins := make(map[string]float64)
	pbs := make(map[string]float64)
	netDebtEbitdas := make(map[string]float64)
	marketCaps := make(map[string]float64)
	insidersPercents := make(map[string]float64)

	// Collect raw values and find min/max for scaling
	minRet, maxRet := math.MaxFloat64, -math.MaxFloat64
	minVol, maxVol := math.MaxFloat64, -math.MaxFloat64
	minSharpe, maxSharpe := math.MaxFloat64, -math.MaxFloat64
	minSortino, maxSortino := math.MaxFloat64, -math.MaxFloat64
	minBeta, maxBeta := math.MaxFloat64, -math.MaxFloat64
	minAlpha, maxAlpha := math.MaxFloat64, -math.MaxFloat64
	minTreynor, maxTreynor := math.MaxFloat64, -math.MaxFloat64
	minUlcer, maxUlcer := math.MaxFloat64, -math.MaxFloat64

	minPEG, maxPEG := math.MaxFloat64, -math.MaxFloat64
	minROE, maxROE := math.MaxFloat64, -math.MaxFloat64
	minFwdPE, maxFwdPE := math.MaxFloat64, -math.MaxFloat64
	minOpMargins, maxOpMargins := math.MaxFloat64, -math.MaxFloat64
	minPB, maxPB := math.MaxFloat64, -math.MaxFloat64
	minNetDebtEbitda, maxNetDebtEbitda := math.MaxFloat64, -math.MaxFloat64
	minMarketCap, maxMarketCap := math.MaxFloat64, -math.MaxFloat64
	minInsidersPercent, maxInsidersPercent := math.MaxFloat64, -math.MaxFloat64

	for _, ticker := range tickers {
		prices := priceHistory[ticker]
		var totRet float64
		var vol float64
		var sharpe float64
		var sortino float64
		var beta float64 = 1.0 // market standard
		var alpha float64
		var treynor float64
		var ulcer float64

		if len(prices) >= 2 {
			totRet = CalculateTotalReturn(prices)
			returns := CalculateDailyReturns(prices)
			meanRet := CalculateMean(returns)
			vol = CalculateVolatility(returns)
			downsideDev := CalculateDownsideDeviation(returns, 0.0)
			ulcer = CalculateUlcerIndex(prices)

			// Sharpe Ratio (daily return / daily volatility)
			if vol > 0.00001 {
				sharpe = meanRet / vol
			}
			// Sortino Ratio (daily return / daily downside deviation)
			if downsideDev > 0.00001 {
				sortino = meanRet / downsideDev
			}

			// Covariance with benchmark returns
			if len(returns) == len(benchReturns) && len(returns) >= 2 {
				cov := CalculateCovariance(returns, benchReturns)
				beta = cov / benchVar
				alpha = meanRet - beta*benchMean
			} else if len(returns) > 0 && len(benchReturns) > 0 {
				// Align lengths if they mismatch slightly due to holidays
				minLen := len(returns)
				if len(benchReturns) < minLen {
					minLen = len(benchReturns)
				}
				cov := CalculateCovariance(returns[len(returns)-minLen:], benchReturns[len(benchReturns)-minLen:])
				beta = cov / benchVar
				alpha = meanRet - beta*benchMean
			}

			// Treynor Ratio (daily mean return / beta)
			if math.Abs(beta) > 0.0001 {
				treynor = meanRet / beta
			}
		}

		// Safeguard values
		if vol <= 0.00001 {
			vol = 0.05
		}

		totalReturns[ticker] = totRet
		volatilities[ticker] = vol
		sharpes[ticker] = sharpe
		sortinos[ticker] = sortino
		betas[ticker] = beta
		alphas[ticker] = alpha
		treynors[ticker] = treynor
		ulcers[ticker] = ulcer

		// Min / Max updates for market statistics
		if totRet < minRet { minRet = totRet }
		if totRet > maxRet { maxRet = totRet }
		if vol < minVol { minVol = vol }
		if vol > maxVol { maxVol = vol }
		if sharpe < minSharpe { minSharpe = sharpe }
		if sharpe > maxSharpe { maxSharpe = sharpe }
		if sortino < minSortino { minSortino = sortino }
		if sortino > maxSortino { maxSortino = sortino }
		if beta < minBeta { minBeta = beta }
		if beta > maxBeta { maxBeta = beta }
		if alpha < minAlpha { minAlpha = alpha }
		if alpha > maxAlpha { maxAlpha = alpha }
		if treynor < minTreynor { minTreynor = treynor }
		if treynor > maxTreynor { maxTreynor = treynor }
		if ulcer < minUlcer { minUlcer = ulcer }
		if ulcer > maxUlcer { maxUlcer = ulcer }

		// Fundamental metrics extraction and imputation
		f, hasF := fundamentals[ticker]
		peg := f.PEGRatio
		if !hasF || peg == 0 {
			peg = avg.peg
		}
		roe := f.ROE
		if !hasF || roe == 0 {
			roe = avg.roe
		}
		fwdPE := f.ForwardPE
		if !hasF || fwdPE == 0 {
			fwdPE = avg.fwdPE
		}
		opMargin := f.OperatingMargins
		if !hasF || opMargin == 0 {
			opMargin = avg.opMargins
		}
		pb := f.PBRatio
		if !hasF || pb == 0 {
			pb = avg.pb
		}
		nde := f.NetDebtEBITDA
		if !hasF || nde == 0 {
			nde = avg.netDebtEbitda
		}
		mc := f.MarketCap
		if !hasF || mc == 0 {
			mc = avg.marketCap
		}
		ip := f.InsidersPercent
		if !hasF || ip == 0 {
			ip = avg.insidersPercent
		}

		pegs[ticker] = peg
		roes[ticker] = roe
		fwdPEs[ticker] = fwdPE
		opMargins[ticker] = opMargin
		pbs[ticker] = pb
		netDebtEbitdas[ticker] = nde
		marketCaps[ticker] = mc
		insidersPercents[ticker] = ip

		// Min / Max updates for fundamental statistics
		if peg < minPEG { minPEG = peg }
		if peg > maxPEG { maxPEG = peg }
		if roe < minROE { minROE = roe }
		if roe > maxROE { maxROE = roe }
		if fwdPE < minFwdPE { minFwdPE = fwdPE }
		if fwdPE > maxFwdPE { maxFwdPE = fwdPE }
		if opMargin < minOpMargins { minOpMargins = opMargin }
		if opMargin > maxOpMargins { maxOpMargins = opMargin }
		if pb < minPB { minPB = pb }
		if pb > maxPB { maxPB = pb }
		if nde < minNetDebtEbitda { minNetDebtEbitda = nde }
		if nde > maxNetDebtEbitda { maxNetDebtEbitda = nde }
		if mc < minMarketCap { minMarketCap = mc }
		if mc > maxMarketCap { maxMarketCap = mc }
		if ip < minInsidersPercent { minInsidersPercent = ip }
		if ip > maxInsidersPercent { maxInsidersPercent = ip }
	}

	// Helper for scaling: returns score between 0 and 1
	scaleMinMax := func(val, minVal, maxVal float64, invert bool) float64 {
		if math.Abs(maxVal-minVal) < 0.000001 {
			return 0.5 // neutral score
		}
		score := (val - minVal) / (maxVal - minVal)
		if invert {
			score = 1.0 - score
		}
		return score
	}

	// Weights for each factor
	wSharpe   := w.Sharpe
	wSortino  := w.Sortino
	wReturn   := w.Return
	wAlpha    := w.Alpha
	wVol      := w.Volatility
	wBeta     := w.Beta
	wTreynor  := w.Treynor
	wUlcer    := w.Ulcer

	// Calculate overall score for each ticker
	scores := make(map[string]float64)
	var sumScore float64

	for _, ticker := range tickers {
		sSharpe := scaleMinMax(sharpes[ticker], minSharpe, maxSharpe, false)
		sSortino := scaleMinMax(sortinos[ticker], minSortino, maxSortino, false)
		sReturn := scaleMinMax(totalReturns[ticker], minRet, maxRet, false)
		sAlpha := scaleMinMax(alphas[ticker], minAlpha, maxAlpha, false)
		sVol := scaleMinMax(volatilities[ticker], minVol, maxVol, true) // lower volatility is better
		sBeta := scaleMinMax(betas[ticker], minBeta, maxBeta, true) // lower beta is better
		sTreynor := scaleMinMax(treynors[ticker], minTreynor, maxTreynor, false)
		sUlcer := scaleMinMax(ulcers[ticker], minUlcer, maxUlcer, true) // lower ulcer index is better (less drawdowns)

		sPEG := scaleMinMax(pegs[ticker], minPEG, maxPEG, true) // lower PEG is better
		sROE := scaleMinMax(roes[ticker], minROE, maxROE, false) // higher ROE is better
		sFwdPE := scaleMinMax(fwdPEs[ticker], minFwdPE, maxFwdPE, true) // lower Forward P/E is better
		sOpMargins := scaleMinMax(opMargins[ticker], minOpMargins, maxOpMargins, false) // higher margins are better
		sPB := scaleMinMax(pbs[ticker], minPB, maxPB, true) // lower P/B is better
		sNetDebtEbitda := scaleMinMax(netDebtEbitdas[ticker], minNetDebtEbitda, maxNetDebtEbitda, true) // lower debt is better
		sMarketCap := scaleMinMax(marketCaps[ticker], minMarketCap, maxMarketCap, true) // lower market cap is better (for multibagger size room)
		sInsidersPercent := scaleMinMax(insidersPercents[ticker], minInsidersPercent, maxInsidersPercent, false) // higher insider ownership is better

		// Combined score
		combinedScore := wSharpe*sSharpe + wSortino*sSortino + wReturn*sReturn + wAlpha*sAlpha + wVol*sVol + wBeta*sBeta + wTreynor*sTreynor + wUlcer*sUlcer +
			w.PEGRatio*sPEG + w.ROE*sROE + w.ForwardPE*sFwdPE + w.OperatingMargins*sOpMargins + w.PBRatio*sPB + w.NetDebtEBITDA*sNetDebtEbitda +
			w.MarketCap*sMarketCap + w.InsidersPercent*sInsidersPercent
		
		// Add baseline floor
		if combinedScore < 0.01 {
			combinedScore = 0.01
		}
		scores[ticker] = combinedScore
		sumScore += combinedScore
	}

	// Normalize scores
	for _, ticker := range tickers {
		if sumScore > 0 {
			weights[ticker] = scores[ticker] / sumScore
		} else {
			weights[ticker] = 1.0 / float64(nTickers)
		}
	}

	// Enforce 25% sector weight cap if w.MarketCap > 0 (multibagger flag)
	if w.MarketCap > 0 {
		EnforceSectorCaps(tickers, weights, fundamentals, 0.25)
	}

	return weights
}
