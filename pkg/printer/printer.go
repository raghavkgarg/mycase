package printer

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gkgarg24/mycase/pkg/market"
	"github.com/gkgarg24/mycase/pkg/optimizer"
	"github.com/gkgarg24/mycase/pkg/portfolio"
)

// PadString pads a string with spaces on the right to reach the target width in runes
func PadString(s string, width int) string {
	res := s
	for utf8.RuneCountInString(res) < width {
		res += " "
	}
	return res
}

// PadStringRight pads a string with spaces on the left to reach the target width in runes
func PadStringRight(s string, width int) string {
	res := s
	for utf8.RuneCountInString(res) < width {
		res = " " + res
	}
	return res
}

// PrintPreviewTable generates the visual text table representing the basket's state and transaction cost.
func PrintPreviewTable(
	basketKeys []string,
	basket map[string]float64,
	quoteData map[string]float64,
	currentHoldings map[string]int,
	finalQuantities []int,
) string {
	var currentTotalValue float64
	var finalTotalValue float64

	for i, inst := range basketKeys {
		parts := strings.Split(inst, ":")
		symbol := parts[len(parts)-1]
		ltp := quoteData[inst]

		currentQty := currentHoldings[symbol]
		finalQty := finalQuantities[i]

		currentTotalValue += float64(currentQty) * ltp
		finalTotalValue += float64(finalQty) * ltp
	}

	var sb strings.Builder
	sb.WriteString("\nPORTFOLIO SNAPSHOT:\n")
	sb.WriteString("---------------------------------------------------------------------------------------------------------------------------------------------\n")
	sb.WriteString("Symbol          | Action | Qty Change | Current Qty | Final Qty | Current Wt | Final Wt | Target Wt | Limit Price | Total Transaction Cost\n")
	sb.WriteString("---------------------------------------------------------------------------------------------------------------------------------------------\n")

	var totalTxnCost float64
	for i, inst := range basketKeys {
		parts := strings.Split(inst, ":")
		symbol := parts[len(parts)-1]
		ltp := quoteData[inst]

		limitPrice := market.CalculateBufferedLimitPrice(ltp)

		currentQty := currentHoldings[symbol]
		finalQty := finalQuantities[i]
		qtyChange := finalQty - currentQty

		action := "HOLD"
		if qtyChange > 0 {
			action = "BUY"
		} else if qtyChange < 0 {
			action = "SELL"
		}

		var currentWt float64
		if currentTotalValue > 0 {
			currentWt = (float64(currentQty) * ltp) / currentTotalValue
		}

		var finalWt float64
		if finalTotalValue > 0 {
			finalWt = (float64(finalQty) * ltp) / finalTotalValue
		}

		targetWeight := basket[inst]

		currentWtStr := fmt.Sprintf("%.1f%%", currentWt*100)
		finalWtStr := fmt.Sprintf("%.1f%%", finalWt*100)
		targetWtStr := fmt.Sprintf("%.1f%%", targetWeight*100)

		txnCost := math.Abs(float64(qtyChange)) * limitPrice
		if qtyChange > 0 {
			totalTxnCost += txnCost // keep for compatibility if needed
		}

		txnCostStr := "₹0.00"
		if qtyChange > 0 {
			txnCostStr = fmt.Sprintf("₹%.2f", txnCost)
		} else if qtyChange < 0 {
			txnCostStr = fmt.Sprintf("-₹%.2f", txnCost)
		}

		sb.WriteString(fmt.Sprintf("%s | %s | %s | %s | %s | %s | %s | %s | %s  | %s  \n",
			PadString(symbol, 14),
			PadString(action, 6),
			PadString(fmt.Sprintf("%d", qtyChange), 10),
			PadString(fmt.Sprintf("%d", currentQty), 11),
			PadString(fmt.Sprintf("%d", finalQty), 9),
			PadString(currentWtStr, 10),
			PadString(finalWtStr, 8),
			PadString(targetWtStr, 9),
			PadStringRight(fmt.Sprintf("₹%.2f", limitPrice), 10),
			PadStringRight(txnCostStr, 22),
		))
	}

	// Calculate total buys and sells for Net Cash Flow
	var totalBuys, totalSells float64
	for i, inst := range basketKeys {
		parts := strings.Split(inst, ":")
		symbol := parts[len(parts)-1]
		ltp := quoteData[inst]
		limitPrice := market.CalculateBufferedLimitPrice(ltp)
		currentQty := currentHoldings[symbol]
		finalQty := finalQuantities[i]
		qtyChange := finalQty - currentQty

		if qtyChange > 0 {
			totalBuys += float64(qtyChange) * limitPrice
		} else if qtyChange < 0 {
			totalSells += math.Abs(float64(qtyChange)) * limitPrice
		}
	}

	netCashFlow := totalBuys - totalSells
	var netCashFlowStr string
	if netCashFlow >= 0 {
		netCashFlowStr = fmt.Sprintf("₹%.2f (Additional cash needed)", netCashFlow)
	} else {
		netCashFlowStr = fmt.Sprintf("-₹%.2f (Cash inflow / Refund to ledger)", math.Abs(netCashFlow))
	}

	// Calculate Minimum Amount required to Match Proposed Weight using optimizer package
	minTotalTxnCost := optimizer.CalculateMinimumRequiredOutflow(basketKeys, basket, quoteData, currentHoldings)

	sb.WriteString("---------------------------------------------------------------------------------------------------------------------------------------------\n")
	sb.WriteString(fmt.Sprintf("Estimated Total Outflow (Sum of Buys): ₹%.2f\n", totalBuys))
	sb.WriteString(fmt.Sprintf("Estimated Total Inflow (Sum of Sells): ₹%.2f\n", totalSells))
	sb.WriteString(fmt.Sprintf("Net Cash Flow (Buys - Sells):          %s\n", netCashFlowStr))
	sb.WriteString(fmt.Sprintf("Minimum Amount required to Match Proposed Weight: ₹%.2f\n", minTotalTxnCost))
	sb.WriteString("---------------------------------------------------------------------------------------------------------------------------------------------\n")

	fmt.Print(sb.String())
	return sb.String()
}


// FormatPnL formats currency PnL with standard sign indicators
func FormatPnL(val float64) string {
	sign := ""
	if val < 0 {
		sign = "-"
	} else if val > 0 {
		sign = "+"
	}
	return fmt.Sprintf("%s₹%.2f", sign, math.Abs(val))
}

// FormatPnLPct formats percentage PnL with standard sign indicators
func FormatPnLPct(val float64) string {
	sign := ""
	if val < 0 {
		sign = "-"
	} else if val > 0 {
		sign = "+"
	}
	return fmt.Sprintf("%s%.2f%%", sign, math.Abs(val))
}

func renderSection(title string, labelPrefix string, holdings []portfolio.Holding, totalCurrentAll float64) string {
	if len(holdings) == 0 {
		return ""
	}
	// Sort by PnL% ascending
	sort.Sort(portfolio.ByPnLPct(holdings))

	header := "=======================================================================================================================\n"
	cols := "Symbol            | Exchange | Quantity | Avg Price  | LTP        | Current Value | Weight | PnL          | PnL %    \n"
	sep := "-----------------------------------------------------------------------------------------------------------------------\n"

	var sb strings.Builder
	sb.WriteString(header)
	
	// Dynamic centering of title
	titleLen := len(title)
	padding := (119 - titleLen) / 2
	if padding < 0 {
		padding = 0
	}
	centeredTitle := strings.Repeat(" ", padding) + title
	sb.WriteString(centeredTitle + strings.Repeat(" ", int(math.Max(0, 119-float64(len(centeredTitle))))) + "\n")
	
	sb.WriteString(header + cols + sep)

	var invested, current, pnl float64
	for _, h := range holdings {
		totalQty := h.Quantity + h.T1Quantity + h.T2Quantity
		invested += float64(totalQty) * h.AveragePrice
		current += float64(totalQty) * h.LastPrice
		pnl += h.PnL
	}

	for _, h := range holdings {
		displaySym := h.TradingSymbol
		totalQty := h.Quantity + h.T1Quantity + h.T2Quantity
		if h.Quantity == 0 {
			if h.T1Quantity > 0 && h.T2Quantity > 0 {
				displaySym = h.TradingSymbol + "(T+1/2)"
			} else if h.T1Quantity > 0 {
				displaySym = h.TradingSymbol + "(T+1)"
			} else if h.T2Quantity > 0 {
				displaySym = h.TradingSymbol + "(T+2)"
			}
		}

		currentVal := float64(totalQty) * h.LastPrice
		var weightPct float64
		if current > 0 {
			weightPct = (currentVal / current) * 100.0
		}

		sb.WriteString(renderHoldingRow(displaySym, h.Exchange, totalQty, h.AveragePrice, h.LastPrice, currentVal, weightPct, h.PnL, h.PnLPct))
	}

	var pnlPct float64
	if invested > 0 {
		pnlPct = (pnl / invested) * 100.0
	}

	groupWeightStr := ""
	if totalCurrentAll > 0 {
		groupWeightPct := (current / totalCurrentAll) * 100.0
		groupWeightStr = fmt.Sprintf(" (%.2f%% of Total Portfolio)", groupWeightPct)
	}

	sb.WriteString(sep)
	sb.WriteString(fmt.Sprintf("%s  ₹%.2f\n", PadString(labelPrefix+" Invested Value:", 28), invested))
	sb.WriteString(fmt.Sprintf("%s  ₹%.2f%s\n", PadString(labelPrefix+" Current Value:", 28), current, groupWeightStr))
	sb.WriteString(fmt.Sprintf("%s  %s (%s)\n", PadString(labelPrefix+" Portfolio PnL:", 28), FormatPnL(pnl), FormatPnLPct(pnlPct)))
	sb.WriteString(header + "\n")

	return sb.String()
}

func findMissingTickers(tickers map[string]bool, holdings []portfolio.Holding) []string {
	holdingSymbols := make(map[string]bool)
	for _, h := range holdings {
		holdingSymbols[h.TradingSymbol] = true
	}
	var missing []string
	var keys []string
	for t := range tickers {
		keys = append(keys, t)
	}
	sort.Strings(keys)

	for _, t := range keys {
		parts := strings.Split(t, ":")
		symbol := parts[len(parts)-1]
		if !holdingSymbols[symbol] {
			missing = append(missing, t)
		}
	}
	return missing
}

// ThemeGroup holds categorized holdings and configuration for a specific theme group
type ThemeGroup struct {
	Name     string
	Prefix   string
	CSVPath  string
	Tickers  map[string]bool
	Holdings []portfolio.Holding
}

// RenderHoldingsSnapshot formats and constructs the holdings layout snapshot.
func RenderHoldingsSnapshot(
	rawHoldings []portfolio.Holding,
	groups []ThemeGroup,
	uncategorizedHoldings []portfolio.Holding,
) string {
	// Calculate totalCurrent for weights
	var totalCurrent float64
	for _, h := range rawHoldings {
		totalQty := h.Quantity + h.T1Quantity + h.T2Quantity
		totalCurrent += float64(totalQty) * h.LastPrice
	}

	var sb strings.Builder
	sb.WriteString("\n")

	// Render configured groups
	for _, g := range groups {
		title := fmt.Sprintf("%s HOLDINGS SNAPSHOT", strings.ToUpper(g.Name))
		sb.WriteString(renderSection(title, g.Prefix, g.Holdings, totalCurrent))
	}

	// Render Uncategorized if any
	sb.WriteString(renderSection("UNCATEGORIZED HOLDINGS SNAPSHOT", "Uncategorized", uncategorizedHoldings, totalCurrent))

	// Render Overall (Total)
	sb.WriteString(renderSection("OVERALL HOLDING SNAPSHOT", "Total", rawHoldings, 0))

	// Render Verification
	sb.WriteString("=======================================================================================================================\n")
	sb.WriteString("                                            DISCREPANCIES & VERIFICATION                                               \n")
	sb.WriteString("=======================================================================================================================\n")

	anyDiscrepancy := len(uncategorizedHoldings) > 0
	for _, g := range groups {
		missing := findMissingTickers(g.Tickers, rawHoldings)
		if len(missing) > 0 {
			anyDiscrepancy = true
		}
	}

	if !anyDiscrepancy {
		sb.WriteString("✓ All holdings are correctly categorized, and all group tickers are present in holdings.\n")
	} else {
		if len(uncategorizedHoldings) > 0 {
			var uncSymbols []string
			for _, h := range uncategorizedHoldings {
				uncSymbols = append(uncSymbols, h.TradingSymbol)
			}
			sb.WriteString(fmt.Sprintf("⚠ Holdings not categorized in any group: %s\n", strings.Join(uncSymbols, ", ")))
		}
		for _, g := range groups {
			missing := findMissingTickers(g.Tickers, rawHoldings)
			if len(missing) > 0 {
				sb.WriteString(fmt.Sprintf("⚠ Tickers in %s not present in holdings: %s\n", g.CSVPath, strings.Join(missing, ", ")))
			}
		}
	}
	sb.WriteString("=======================================================================================================================\n")

	return sb.String()
}

func renderHoldingRow(sym, exchange string, qty int, avgP, ltp, currentVal, weightPct, pnl, pnlPct float64) string {
	return fmt.Sprintf("%s | %s | %s | %s | %s | %s | %s | %s | %s\n",
		PadString(sym, 17),
		PadString(exchange, 8),
		PadString(fmt.Sprintf("%d", qty), 8),
		PadStringRight(fmt.Sprintf("₹%.2f", avgP), 10),
		PadStringRight(fmt.Sprintf("₹%.2f", ltp), 10),
		PadStringRight(fmt.Sprintf("₹%.2f", currentVal), 13),
		PadStringRight(fmt.Sprintf("%.2f%%", weightPct), 6),
		PadStringRight(FormatPnL(pnl), 12),
		PadStringRight(FormatPnLPct(pnlPct), 8),
	)
}
