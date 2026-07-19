package optimizer

import (
	"math"
	"strings"

	"mycase/pkg/market"
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

	// Calculate baseline cost (initial 1 share for items not owned)
	baselineCost := 0.0
	for i, inst := range basketKeys {
		parts := strings.Split(inst, ":")
		symbol := parts[len(parts)-1]
		ltp := quoteData[inst]
		
		// limit_price in Mojo is buffer of 3% above LTP, rounded to 1 decimal place.
		limitPrice := market.CalculateBufferedLimitPrice(ltp)

		currentQty := currentHoldings[symbol]
		startQty := currentQty
		if currentQty == 0 {
			startQty = 1
			baselineCost += limitPrice
		}
		rawQuantities[i] = startQty
	}

	// Fallback if baseline cost overshoots budget
	if baselineCost > totalInvestment {
		for i, inst := range basketKeys {
			parts := strings.Split(inst, ":")
			symbol := parts[len(parts)-1]
			rawQuantities[i] = currentHoldings[symbol]
		}
	}

	// Greedy allocation loop
	for {
		bestIdx := -1
		maxEfficiency := -999.0

		// Current deviation
		currentTotalCost := 0.0
		for j, inst := range basketKeys {
			currentTotalCost += float64(rawQuantities[j]) * quoteData[inst]
		}

		currentDev := 0.0
		if currentTotalCost > 0 {
			for j, inst := range basketKeys {
				targetWeight := basket[inst]
				actWt := (float64(rawQuantities[j]) * quoteData[inst]) / currentTotalCost
				currentDev += math.Abs(actWt - targetWeight)
			}
		} else {
			currentDev = 999.0
		}

		// Evaluate candidate additions (adding 1 share to each asset in turn)
		for i, inst := range basketKeys {
			if basket[inst] <= 0.0 {
				continue // Skip assets that are marked to be removed/sold completely
			}
			candidateQtys := make([]int, n)
			copy(candidateQtys, rawQuantities)
			candidateQtys[i]++


			newSharesCost := 0.0
			newTotalLTPCost := 0.0
			targetLimitPrice := 0.0

			for j, instJ := range basketKeys {
				partsJ := strings.Split(instJ, ":")
				symbolJ := partsJ[len(partsJ)-1]
				ltpJ := quoteData[instJ]
				limitPriceJ := market.CalculateBufferedLimitPrice(ltpJ)

				qtyJ := candidateQtys[j]
				if j == i {
					targetLimitPrice = limitPriceJ
				}

				currentQtyJ := currentHoldings[symbolJ]
				addedQtyJ := qtyJ - currentQtyJ
				if addedQtyJ > 0 {
					newSharesCost += float64(addedQtyJ) * limitPriceJ
				}
				newTotalLTPCost += float64(qtyJ) * ltpJ
			}

			if newSharesCost <= totalInvestment {
				dev := 0.0
				for j, instJ := range basketKeys {
					targetWeight := basket[instJ]
					actWt := (float64(candidateQtys[j]) * quoteData[instJ]) / newTotalLTPCost
					dev += math.Abs(actWt - targetWeight)
				}
				devRed := currentDev - dev
				efficiency := devRed / targetLimitPrice
				if efficiency > maxEfficiency {
					maxEfficiency = efficiency
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

// CalculateMinimumRequiredOutflow calculates the minimum portfolio outflow required to match the proposed basket weights.
func CalculateMinimumRequiredOutflow(
	basketKeys []string,
	basket map[string]float64,
	quoteData map[string]float64,
	currentHoldings map[string]int,
) float64 {
	tMax := 0.0
	for _, inst := range basketKeys {
		ltp := quoteData[inst]
		targetWeight := basket[inst]

		if targetWeight <= 0.0 {
			continue
		}


		// The minimum quantity needed of any asset to exist in the basket is 1 share.
		// Since we are rebalancing, we can sell down existing holdings.
		minQty := 1

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
		// For minimum required portfolio size calculation, the minimum target qty is 1 share
		minRequired := 1
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



