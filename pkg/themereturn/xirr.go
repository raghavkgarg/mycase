package themereturn

import (
	"errors"
	"math"
	"sort"
)

const (
	maxIterations = 150
	tolerance     = 1e-7
	daysInYear    = 365.25
)

// CalculateXIRR calculates the annualized Money-Weighted Return (Internal Rate of Return)
// for an arbitrary sequence of dated cash flows.
func CalculateXIRR(cashFlows []DatedCashFlow) (float64, error) {
	if len(cashFlows) < 2 {
		return 0.0, errors.New("at least two cash flows required for XIRR")
	}

	// Check that we have at least one positive and one negative cash flow
	hasPos, hasNeg := false, false
	for _, cf := range cashFlows {
		if cf.Amount > 0 {
			hasPos = true
		} else if cf.Amount < 0 {
			hasNeg = true
		}
	}
	if !hasPos || !hasNeg {
		return 0.0, errors.New("cash flows must contain both positive and negative values")
	}

	// Sort chronologically
	flows := make([]DatedCashFlow, len(cashFlows))
	copy(flows, cashFlows)
	sort.Slice(flows, func(i, j int) bool {
		return flows[i].Date.Before(flows[j].Date)
	})

	t0 := flows[0].Date

	npv := func(rate float64) float64 {
		val := 0.0
		for _, cf := range flows {
			dt := cf.Date.Sub(t0).Hours() / 24.0 / daysInYear
			val += cf.Amount / math.Pow(1.0+rate, dt)
		}
		return val
	}

	dnpv := func(rate float64) float64 {
		val := 0.0
		for _, cf := range flows {
			dt := cf.Date.Sub(t0).Hours() / 24.0 / daysInYear
			val += -dt * cf.Amount / math.Pow(1.0+rate, dt+1.0)
		}
		return val
	}

	// Initial guess
	rate := 0.10

	// Newton-Raphson iteration
	for i := 0; i < maxIterations; i++ {
		f := npv(rate)
		df := dnpv(rate)

		if math.Abs(df) < 1e-12 {
			break
		}

		newRate := rate - f/df
		if math.Abs(newRate-rate) < tolerance {
			return newRate, nil
		}
		rate = newRate
		if rate <= -0.999 {
			rate = -0.999
			break
		}
	}

	// Bisection Fallback if Newton-Raphson did not converge
	low := -0.999
	high := 50.0 // up to 5000% p.a.
	fLow := npv(low)
	fHigh := npv(high)

	if fLow*fHigh > 0 {
		// Cannot bracket root directly in standard range
		return rate, nil
	}

	for i := 0; i < 100; i++ {
		mid := (low + high) / 2.0
		fMid := npv(mid)
		if math.Abs(fMid) < tolerance || (high-low)/2.0 < tolerance {
			return mid, nil
		}
		if fLow*fMid < 0 {
			high = mid
			fHigh = fMid
		} else {
			low = mid
			fLow = fMid
		}
	}

	return (low + high) / 2.0, nil
}
