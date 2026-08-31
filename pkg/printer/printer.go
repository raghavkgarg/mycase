// Package printer builds the domain-specific financial reports (holdings
// snapshot, basket transaction preview) by composing the generic presentation
// primitives in pkg/render.
//
// Layering: cmd / executor → printer → render. printer owns the mapping from
// domain types (broker.Holding, basket weights) to report structure; render
// owns the actual table/section/currency rendering. printer contains no
// hand-rolled padding or table code — that all lives in render.
package printer

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/market"
	"github.com/raghavkgarg/mycase/pkg/optimizer"
	"github.com/raghavkgarg/mycase/pkg/render"
)

const rupee = "₹"

// ThemeGroup holds categorized holdings and configuration for a theme group.
type ThemeGroup struct {
	Name         string
	Prefix       string
	CSVPath      string
	TargetWeight float64
	Tickers      map[string]bool
	Holdings     []broker.Holding
}

// PrintPreviewTable builds the basket transaction-preview report, prints it to
// stdout, and returns the same content as a string (for reuse by the caller).
func PrintPreviewTable(
	basketKeys []string,
	basket map[string]float64,
	quoteData map[string]float64,
	currentHoldings map[string]int,
	finalQuantities []int,
) string {
	var currentTotalValue, finalTotalValue float64
	for i, inst := range basketKeys {
		symbol := lastSegment(inst)
		ltp := quoteData[inst]
		currentTotalValue += float64(currentHoldings[symbol]) * ltp
		finalTotalValue += float64(finalQuantities[i]) * ltp
	}

	var sb strings.Builder
	r := render.New(&sb)
	fmt.Fprintln(&sb)
	r.Section("PORTFOLIO SNAPSHOT")

	var totalBuys, totalSells float64
	rows := make([][]string, 0, len(basketKeys))
	for i, inst := range basketKeys {
		symbol := lastSegment(inst)
		ltp := quoteData[inst]
		limitPrice := market.CalculateBufferedLimitPrice(ltp)
		currentQty := currentHoldings[symbol]
		finalQty := finalQuantities[i]
		qtyChange := finalQty - currentQty

		action := "HOLD"
		switch {
		case qtyChange > 0:
			action = "BUY"
			totalBuys += float64(qtyChange) * limitPrice
		case qtyChange < 0:
			action = "SELL"
			totalSells += math.Abs(float64(qtyChange)) * limitPrice
		}

		var currentWt, finalWt float64
		if currentTotalValue > 0 {
			currentWt = (float64(currentQty) * ltp) / currentTotalValue
		}
		if finalTotalValue > 0 {
			finalWt = (float64(finalQty) * ltp) / finalTotalValue
		}

		txnCost := math.Abs(float64(qtyChange)) * limitPrice
		txnCostStr := rupee + "0.00"
		if qtyChange > 0 {
			txnCostStr = render.Currency(txnCost, rupee)
		} else if qtyChange < 0 {
			txnCostStr = "-" + render.Currency(txnCost, rupee)
		}

		rows = append(rows, []string{
			symbol, action,
			fmt.Sprintf("%d", qtyChange), fmt.Sprintf("%d", currentQty), fmt.Sprintf("%d", finalQty),
			fmt.Sprintf("%.1f%%", currentWt*100), fmt.Sprintf("%.1f%%", finalWt*100), fmt.Sprintf("%.1f%%", basket[inst]*100),
			render.Currency(limitPrice, rupee), txnCostStr,
		})
	}

	r.Table(render.TableOpts{
		Headers: []string{"Symbol", "Action", "Qty Change", "Current Qty", "Final Qty", "Current Wt", "Final Wt", "Target Wt", "Limit Price", "Total Transaction Cost"},
		Rows:    rows,
		Align: []render.Alignment{
			render.AlignLeft, render.AlignLeft, render.AlignRight, render.AlignRight, render.AlignRight,
			render.AlignRight, render.AlignRight, render.AlignRight, render.AlignRight, render.AlignRight,
		},
		Border: render.BorderPipe,
	})

	netCashFlow := totalBuys - totalSells
	var netStr string
	if netCashFlow >= 0 {
		netStr = render.Currency(netCashFlow, rupee) + " (Additional cash needed)"
	} else {
		netStr = "-" + render.Currency(math.Abs(netCashFlow), rupee) + " (Cash inflow / Refund to ledger)"
	}
	minTotalTxnCost := optimizer.CalculateMinimumRequiredOutflow(basketKeys, basket, quoteData, currentHoldings)

	render.KV(&sb, []render.KVPair{
		{Key: "Estimated Total Outflow (Sum of Buys)", Value: render.Currency(totalBuys, rupee)},
		{Key: "Estimated Total Inflow (Sum of Sells)", Value: render.Currency(totalSells, rupee)},
		{Key: "Net Cash Flow (Buys - Sells)", Value: netStr},
		{Key: "Minimum Amount required to Match Proposed Weight", Value: render.Currency(minTotalTxnCost, rupee)},
	})

	out := sb.String()
	fmt.Print(out)
	return out
}

// RenderHoldingsSnapshot builds the full holdings report as a string.
func RenderHoldingsSnapshot(
	rawHoldings []broker.Holding,
	groups []ThemeGroup,
	uncategorized []broker.Holding,
) string {
	var totalCurrent float64
	for _, h := range rawHoldings {
		totalCurrent += currentValue(h)
	}

	var sb strings.Builder
	r := render.New(&sb)
	fmt.Fprintln(&sb)

	renderThemeAllocationSummary(r, groups, uncategorized, totalCurrent)
	for _, g := range groups {
		title := fmt.Sprintf("%s HOLDINGS SNAPSHOT", strings.ToUpper(g.Name))
		renderHoldingSection(r, title, g.Prefix, g.Holdings, totalCurrent)
	}
	renderHoldingSection(r, "UNCATEGORIZED HOLDINGS SNAPSHOT", "Uncategorized", uncategorized, totalCurrent)
	renderHoldingSection(r, "OVERALL HOLDING SNAPSHOT", "Total", rawHoldings, 0)
	renderDiscrepancies(r, rawHoldings, groups, uncategorized)

	return sb.String()
}

func renderHoldingSection(r render.Renderer, title, labelPrefix string, holdings []broker.Holding, totalCurrentAll float64) {
	if len(holdings) == 0 {
		return
	}
	w := r.Writer()

	slices.SortFunc(holdings, func(a, b broker.Holding) int {
		return cmp.Compare(a.PnLPct, b.PnLPct)
	})

	r.Banner(title)

	var invested, current, pnl float64
	for _, h := range holdings {
		totalQty := h.Quantity + h.T1Quantity + h.T2Quantity
		invested += float64(totalQty) * h.AveragePrice
		current += currentValue(h)
		pnl += h.PnL
	}

	rows := make([][]string, 0, len(holdings))
	for _, h := range holdings {
		totalQty := h.Quantity + h.T1Quantity + h.T2Quantity
		curVal := float64(totalQty) * h.LastPrice
		var weightPct float64
		if current > 0 {
			weightPct = (curVal / current) * 100.0
		}
		rows = append(rows, []string{
			holdingDisplaySymbol(h),
			h.Exchange,
			fmt.Sprintf("%d", totalQty),
			render.Currency(h.AveragePrice, rupee),
			render.Currency(h.LastPrice, rupee),
			render.Currency(curVal, rupee),
			fmt.Sprintf("%.2f%%", weightPct),
			render.PnL(h.PnL, rupee),
			render.PnLPct(h.PnLPct),
		})
	}
	r.Table(render.TableOpts{
		Headers: []string{"Symbol", "Exchange", "Quantity", "Avg Price", "LTP", "Current Value", "Weight", "PnL", "PnL %"},
		Rows:    rows,
		Align: []render.Alignment{
			render.AlignLeft, render.AlignLeft, render.AlignRight, render.AlignRight,
			render.AlignRight, render.AlignRight, render.AlignRight, render.AlignRight, render.AlignRight,
		},
		Border: render.BorderPipe,
	})

	var pnlPct float64
	if invested > 0 {
		pnlPct = (pnl / invested) * 100.0
	}
	groupWeight := ""
	if totalCurrentAll > 0 {
		groupWeight = fmt.Sprintf(" (%.2f%% of Total Portfolio)", (current/totalCurrentAll)*100.0)
	}
	render.KV(w, []render.KVPair{
		{Key: labelPrefix + " Invested Value", Value: render.Currency(invested, rupee)},
		{Key: labelPrefix + " Current Value", Value: render.Currency(current, rupee) + groupWeight},
		{Key: labelPrefix + " Portfolio PnL", Value: fmt.Sprintf("%s (%s)", render.PnL(pnl, rupee), render.PnLPct(pnlPct))},
	})
	fmt.Fprintln(w)
}

func renderThemeAllocationSummary(r render.Renderer, groups []ThemeGroup, uncategorized []broker.Holding, totalCurrent float64) {
	r.Banner("THEME TARGET VS ACTUAL WEIGHT ALLOCATION SUMMARY")

	var totalInvested, totalCurrentAll, totalPnL, totalTargetWt float64
	rows := make([][]string, 0, len(groups)+1)

	for _, g := range groups {
		var invested, current, pnl float64
		for _, h := range g.Holdings {
			totalQty := h.Quantity + h.T1Quantity + h.T2Quantity
			invested += float64(totalQty) * h.AveragePrice
			current += currentValue(h)
			pnl += h.PnL
		}
		if len(g.Holdings) == 0 && g.TargetWeight == 0 {
			continue
		}
		var pnlPct float64
		if invested > 0 {
			pnlPct = (pnl / invested) * 100.0
		}
		var actualWt float64
		if totalCurrent > 0 {
			actualWt = (current / totalCurrent) * 100.0
		}
		targetWt := g.TargetWeight * 100.0
		cleanName := strings.TrimSpace(strings.TrimPrefix(g.Name, "Theme"))

		totalInvested += invested
		totalCurrentAll += current
		totalPnL += pnl
		totalTargetWt += targetWt

		rows = append(rows, themeRow(cleanName, invested, current, pnl, pnlPct, actualWt, targetWt, actualWt-targetWt))
	}

	if len(uncategorized) > 0 {
		var invested, current, pnl float64
		for _, h := range uncategorized {
			totalQty := h.Quantity + h.T1Quantity + h.T2Quantity
			invested += float64(totalQty) * h.AveragePrice
			current += currentValue(h)
			pnl += h.PnL
		}
		var pnlPct float64
		if invested > 0 {
			pnlPct = (pnl / invested) * 100.0
		}
		var actualWt float64
		if totalCurrent > 0 {
			actualWt = (current / totalCurrent) * 100.0
		}
		totalInvested += invested
		totalCurrentAll += current
		totalPnL += pnl
		rows = append(rows, themeRow("Uncategorized", invested, current, pnl, pnlPct, actualWt, 0.0, actualWt))
	}

	var totalPnLPct float64
	if totalInvested > 0 {
		totalPnLPct = (totalPnL / totalInvested) * 100.0
	}
	var totalActualWt float64
	if totalCurrent > 0 {
		totalActualWt = (totalCurrentAll / totalCurrent) * 100.0
	}
	footer := themeRow("Total", totalInvested, totalCurrentAll, totalPnL, totalPnLPct, totalActualWt, totalTargetWt, totalActualWt-totalTargetWt)

	r.Table(render.TableOpts{
		Headers: []string{"Theme", "Invested Value", "Current Value", "PnL", "PnL %", "Actual Wt", "Target Wt", "Drift"},
		Rows:    rows,
		Footer:  footer,
		Align: []render.Alignment{
			render.AlignLeft, render.AlignRight, render.AlignRight, render.AlignRight,
			render.AlignRight, render.AlignRight, render.AlignRight, render.AlignRight,
		},
		Border: render.BorderPipe,
	})
	fmt.Fprintln(r.Writer())
}

func themeRow(name string, invested, current, pnl, pnlPct, actualWt, targetWt, drift float64) []string {
	return []string{
		name,
		render.Currency(invested, rupee),
		render.Currency(current, rupee),
		render.PnL(pnl, rupee),
		render.PnLPct(pnlPct),
		fmt.Sprintf("%.2f%%", actualWt),
		fmt.Sprintf("%.2f%%", targetWt),
		render.PnLPct(drift),
	}
}

func renderDiscrepancies(r render.Renderer, rawHoldings []broker.Holding, groups []ThemeGroup, uncategorized []broker.Holding) {
	w := r.Writer()
	r.Banner("DISCREPANCIES & VERIFICATION")

	anyDiscrepancy := len(uncategorized) > 0
	for _, g := range groups {
		if len(findMissingTickers(g.Tickers, rawHoldings)) > 0 {
			anyDiscrepancy = true
		}
	}

	if !anyDiscrepancy {
		fmt.Fprintln(w, "✓ All holdings are correctly categorized, and all group tickers are present in holdings.")
		return
	}
	if len(uncategorized) > 0 {
		syms := make([]string, 0, len(uncategorized))
		for _, h := range uncategorized {
			syms = append(syms, h.TradingSymbol)
		}
		fmt.Fprintf(w, "⚠ Holdings not categorized in any group: %s\n", strings.Join(syms, ", "))
	}
	for _, g := range groups {
		missing := findMissingTickers(g.Tickers, rawHoldings)
		if len(missing) > 0 {
			fmt.Fprintf(w, "⚠ Tickers in %s not present in holdings: %s\n", g.CSVPath, strings.Join(missing, ", "))
		}
	}
}

func findMissingTickers(tickers map[string]bool, holdings []broker.Holding) []string {
	holdingSymbols := make(map[string]bool)
	for _, h := range holdings {
		holdingSymbols[h.TradingSymbol] = true
	}
	keys := make([]string, 0, len(tickers))
	for t := range tickers {
		keys = append(keys, t)
	}
	slices.Sort(keys)

	var missing []string
	for _, t := range keys {
		if !holdingSymbols[lastSegment(t)] {
			missing = append(missing, t)
		}
	}
	return missing
}

func holdingDisplaySymbol(h broker.Holding) string {
	if h.Quantity != 0 {
		return h.TradingSymbol
	}
	switch {
	case h.T1Quantity > 0 && h.T2Quantity > 0:
		return h.TradingSymbol + "(T+1/2)"
	case h.T1Quantity > 0:
		return h.TradingSymbol + "(T+1)"
	case h.T2Quantity > 0:
		return h.TradingSymbol + "(T+2)"
	default:
		return h.TradingSymbol
	}
}

func currentValue(h broker.Holding) float64 {
	totalQty := h.Quantity + h.T1Quantity + h.T2Quantity
	return float64(totalQty) * h.LastPrice
}

func lastSegment(inst string) string {
	parts := strings.Split(inst, ":")
	return parts[len(parts)-1]
}
