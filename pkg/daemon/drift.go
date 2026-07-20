package daemon

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/broker"
)

// DriftResult holds the output of a single drift calculation.
type DriftResult struct {
	DriftIndex    float64
	ActualWeights map[string]float64
	TargetWeights map[string]float64
	BasketKeys    []string // ordered as in the portfolio CSV
	TotalValue    float64
	CheckedAt     time.Time
}

// CalculateDrift computes ½ Σ|w_actual_i − w_target_i| for the given portfolio.
// basketKeys are instrument keys (e.g. "NSE:TCS") in CSV order.
// When there are no holdings the actual weights are all zero, yielding drift = 0.5.
// The formula is bounded by 1.0 (total variation distance); drift > 0.5 occurs when
// actual weight is concentrated in instruments that have low target weight.
func CalculateDrift(_ context.Context, b broker.Broker, targetWeights map[string]float64, basketKeys []string) (DriftResult, error) {
	quotes, err := b.GetQuotes(basketKeys)
	if err != nil {
		return DriftResult{}, fmt.Errorf("fetching quotes: %w", err)
	}

	holdings, err := b.GetHoldings()
	if err != nil {
		return DriftResult{}, fmt.Errorf("fetching holdings: %w", err)
	}

	heldQty := make(map[string]int, len(holdings))
	for _, h := range holdings {
		key := strings.ToUpper(h.Exchange) + ":" + h.TradingSymbol
		heldQty[key] += h.Quantity + h.T1Quantity + h.T2Quantity
	}

	values := make(map[string]float64, len(basketKeys))
	var totalValue float64
	for _, key := range basketKeys {
		v := float64(heldQty[key]) * quotes[key]
		values[key] = v
		totalValue += v
	}

	actualWeights := make(map[string]float64, len(basketKeys))
	if totalValue > 0 {
		for _, key := range basketKeys {
			actualWeights[key] = values[key] / totalValue
		}
	}

	var drift float64
	for _, key := range basketKeys {
		drift += math.Abs(actualWeights[key] - targetWeights[key])
	}
	drift *= 0.5

	return DriftResult{
		DriftIndex:    drift,
		ActualWeights: actualWeights,
		TargetWeights: targetWeights,
		BasketKeys:    basketKeys,
		TotalValue:    totalValue,
		CheckedAt:     time.Now(),
	}, nil
}
