package backtest

import "math"

// indiaRiskFreeRate is the approximate 10-year G-sec yield used for Sharpe/Alpha.
const indiaRiskFreeRate = 0.06

// CalcCAGR computes the compound annual growth rate over calendar days.
func CalcCAGR(initial, final float64, days int) float64 {
	if initial <= 0 || days <= 0 || final <= 0 {
		return 0
	}
	years := float64(days) / 365.0
	return math.Pow(final/initial, 1/years) - 1
}

// CalcMaxDrawdown returns the maximum peak-to-trough decline as a negative fraction.
func CalcMaxDrawdown(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	peak := values[0]
	maxDD := 0.0
	for _, v := range values {
		if v > peak {
			peak = v
		}
		if peak > 0 {
			if dd := (v - peak) / peak; dd < maxDD {
				maxDD = dd
			}
		}
	}
	return maxDD
}

// CalcSharpe returns the annualized Sharpe ratio using indiaRiskFreeRate.
func CalcSharpe(navValues []float64) float64 {
	return CalcSharpeRF(navValues, indiaRiskFreeRate)
}

// CalcSharpeRF returns the annualized Sharpe ratio using the given annual
// risk-free rate (fraction, e.g. 0.045 = 4.5%).
func CalcSharpeRF(navValues []float64, riskFree float64) float64 {
	rets := dailyReturns(navValues)
	if len(rets) == 0 {
		return 0
	}
	dailyRF := riskFree / 252
	excess := make([]float64, len(rets))
	for i, r := range rets {
		excess[i] = r - dailyRF
	}
	m := mean(excess)
	sd := stddev(excess, m)
	if sd == 0 {
		return 0
	}
	return (m / sd) * math.Sqrt(252)
}

// CalcSortino returns the annualized Sortino ratio using indiaRiskFreeRate.
// Downside deviation uses all n returns in the denominator (standard Sortino formula).
func CalcSortino(navValues []float64) float64 {
	return CalcSortinoRF(navValues, indiaRiskFreeRate)
}

// CalcSortinoRF returns the annualized Sortino ratio using the given annual
// risk-free rate. Downside deviation uses all n returns in the denominator.
func CalcSortinoRF(navValues []float64, riskFree float64) float64 {
	rets := dailyReturns(navValues)
	if len(rets) == 0 {
		return 0
	}
	dailyRF := riskFree / 252
	m := mean(rets) - dailyRF

	var downsideVar float64
	for _, r := range rets {
		if ex := r - dailyRF; ex < 0 {
			downsideVar += ex * ex
		}
	}
	downsideVar /= float64(len(rets))
	downsideStd := math.Sqrt(downsideVar)
	if downsideStd == 0 {
		return 0
	}
	return (m / downsideStd) * math.Sqrt(252)
}

// CalcCalmar returns the Calmar ratio: CAGR / |maxDrawdown|.
func CalcCalmar(cagr, maxDrawdown float64) float64 {
	if maxDrawdown == 0 {
		return 0
	}
	return cagr / math.Abs(maxDrawdown)
}

// CalcBeta computes portfolio beta vs. a benchmark from their NAV series.
func CalcBeta(portValues, benchValues []float64) float64 {
	portRets := dailyReturns(portValues)
	benchRets := dailyReturns(benchValues)
	n := min(len(portRets), len(benchRets))
	if n < 2 {
		return 1
	}
	portRets = portRets[:n]
	benchRets = benchRets[:n]

	mp := mean(portRets)
	mb := mean(benchRets)

	var cov, varBench float64
	for i := range n {
		dp := portRets[i] - mp
		db := benchRets[i] - mb
		cov += dp * db
		varBench += db * db
	}
	if varBench == 0 {
		return 1
	}
	return cov / varBench
}

// CalcAlpha computes Jensen's alpha: portCAGR − (rf + β*(benchCAGR − rf)).
func CalcAlpha(portCAGR, benchCAGR, beta float64) float64 {
	return CalcAlphaRF(portCAGR, benchCAGR, beta, indiaRiskFreeRate)
}

// CalcAlphaRF computes Jensen's alpha with an explicit annual risk-free rate.
func CalcAlphaRF(portCAGR, benchCAGR, beta, riskFree float64) float64 {
	return portCAGR - (riskFree + beta*(benchCAGR-riskFree))
}

// dailyReturns computes day-over-day fractional returns from a NAV series.
func dailyReturns(values []float64) []float64 {
	if len(values) < 2 {
		return nil
	}
	ret := make([]float64, len(values)-1)
	for i := 1; i < len(values); i++ {
		if values[i-1] > 0 {
			ret[i-1] = (values[i] - values[i-1]) / values[i-1]
		}
	}
	return ret
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func stddev(xs []float64, m float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		d := x - m
		s += d * d
	}
	return math.Sqrt(s / float64(len(xs)-1))
}
