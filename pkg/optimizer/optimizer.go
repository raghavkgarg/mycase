package optimizer

import (
	"strings"

	"github.com/raghavkgarg/mycase/pkg/market"
)

// OptimizeFreshBuy runs the greedy allocation algorithm.
// - basketKeys: ordered list of tickers (e.g. "NSE:SWSOLAR")
// - basket: target weights (e.g. "NSE:SWSOLAR" -> 0.34)
// - quoteData: LTP for tickers (e.g. "NSE:SWSOLAR" -> 500.0)
// - currentHoldings: current quantities (e.g. "SWSOLAR" -> 10)
// - totalInvestment: investment amount in Rupees
// Returns the slice of final quantities matching the order of basketKeys.
func OptimizeFreshBuy(
	basketKeys []string,
	basket map[string]float64,
	quoteData map[string]float64,
	currentHoldings map[string]int,
	totalInvestment float64,
) []int {
	n := len(basketKeys)
	rawQuantities := make([]int, n)

	// Calculate exit sell proceeds (for stocks with target weight <= 0.0)
	exitSellProceeds := 0.0
	for i, inst := range basketKeys {
		targetWeight := basket[inst]
		parts := strings.Split(inst, ":")
		symbol := parts[len(parts)-1]
		currentQty := currentHoldings[symbol]
		ltp := quoteData[inst]

		if targetWeight <= 0.0 {
			// Stock is dropped/exited -> final target quantity is 0 (SELL all shares)
			rawQuantities[i] = 0
			if currentQty > 0 {
				exitSellProceeds += float64(currentQty) * ltp
			}
		} else {
			// Active stock -> start at current holdings
			rawQuantities[i] = currentQty
		}
	}

	// Add exit sell proceeds to total investment budget so freed capital is deployed into active targets
	effectiveInvestment := totalInvestment + exitSellProceeds

	// Calculate baseline cost (initial 1 share for items not owned among active stocks)
	baselineCost := 0.0
	for i, inst := range basketKeys {
		targetWeight := basket[inst]
		if targetWeight <= 0.0 {
			continue
		}

		parts := strings.Split(inst, ":")
		symbol := parts[len(parts)-1]
		ltp := quoteData[inst]
		limitPrice := market.CalculateBufferedLimitPrice(ltp)

		currentQty := currentHoldings[symbol]
		if currentQty == 0 {
			rawQuantities[i] = 1
			baselineCost += limitPrice
		}
	}

	// Fallback if baseline cost overshoots budget
	if baselineCost > effectiveInvestment {
		for i, inst := range basketKeys {
			targetWeight := basket[inst]
			parts := strings.Split(inst, ":")
			symbol := parts[len(parts)-1]
			if targetWeight <= 0.0 {
				rawQuantities[i] = 0
			} else {
				rawQuantities[i] = currentHoldings[symbol]
			}
		}
	}

	// Greedy allocation loop: allocate 1 share at a time to the asset that is currently least-funded relative to its target weight
	for {
		bestIdx := -1
		minAllocRatio := 999999999999999.0

		// Evaluate candidate additions (adding 1 share to each eligible asset in turn)
		for i, inst := range basketKeys {
			targetWeight := basket[inst]
			if targetWeight <= 0.0 {
				continue // Skip assets that are marked to be removed/sold completely
			}

			candidateQtys := make([]int, n)
			copy(candidateQtys, rawQuantities)
			candidateQtys[i]++

			newSharesCost := 0.0
			for j, instJ := range basketKeys {
				targetWeightJ := basket[instJ]
				if targetWeightJ <= 0.0 {
					continue
				}
				partsJ := strings.Split(instJ, ":")
				symbolJ := partsJ[len(partsJ)-1]
				ltpJ := quoteData[instJ]
				limitPriceJ := market.CalculateBufferedLimitPrice(ltpJ)

				currentQtyJ := currentHoldings[symbolJ]
				addedQtyJ := candidateQtys[j] - currentQtyJ
				if addedQtyJ > 0 {
					newSharesCost += float64(addedQtyJ) * limitPriceJ
				}
			}

			if newSharesCost <= effectiveInvestment {
				ltpI := quoteData[inst]
				// Allocation ratio: current allocated value divided by target weight
				allocRatio := (float64(rawQuantities[i]) * ltpI) / targetWeight
				if allocRatio < minAllocRatio {
					minAllocRatio = allocRatio
					bestIdx = i
				}
			}
		}

		if bestIdx != -1 {
			rawQuantities[bestIdx]++
		} else {
			break
		}
	}

	return rawQuantities
}

// CalculateMinimumRequiredOutflow calculates the minimum portfolio outflow required to match the proposed basket weights,
// taking into account existing holdings that are not being sold down.
func CalculateMinimumRequiredOutflow(
	basketKeys []string,
	basket map[string]float64,
	quoteData map[string]float64,
	currentHoldings map[string]int,
) float64 {
	tMax := 0.0
	for _, inst := range basketKeys {
		parts := strings.Split(inst, ":")
		symbol := parts[len(parts)-1]
		ltp := quoteData[inst]
		targetWeight := basket[inst]

		if targetWeight <= 0.0 {
			continue
		}

		currentQty := currentHoldings[symbol]
		minQty := max(currentQty, 1)

		tI := (float64(minQty) * ltp) / targetWeight
		if tI > tMax {
			tMax = tI
		}
	}

	var minTotalTxnCost float64
	for _, inst := range basketKeys {
		parts := strings.Split(inst, ":")
		symbol := parts[len(parts)-1]
		ltp := quoteData[inst]
		targetWeight := basket[inst]

		if targetWeight <= 0.0 {
			continue
		}

		limitPrice := market.CalculateBufferedLimitPrice(ltp)
		currentQty := currentHoldings[symbol]

		targetQty := int((tMax*targetWeight)/ltp + 0.5)
		minRequired := max(currentQty, 1)
		if targetQty < minRequired {
			targetQty = minRequired
		}

		buyQty := targetQty - currentQty
		if buyQty > 0 {
			minTotalTxnCost += float64(buyQty) * limitPrice
		}
	}

	return minTotalTxnCost
}
