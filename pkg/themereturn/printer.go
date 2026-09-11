package themereturn

import (
	"fmt"
	"math"
	"strings"
	"text/tabwriter"
)

// formatRupees formats a monetary amount with currency symbol and optional sign.
func formatRupees(val float64, withSign bool) string {
	sign := ""
	if withSign {
		if val > 0 {
			sign = "+"
		} else if val < 0 {
			sign = "-"
		}
	}
	return fmt.Sprintf("%s₹%.2f", sign, math.Abs(val))
}

// RenderThemeReport prints the full audited report to the terminal.
func RenderThemeReport(r *ThemeReturnReport, opts ThemeReturnOptions) string {
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString("=======================================================================================================================\n")
	title := fmt.Sprintf("AUDITED THEME PERFORMANCE & RETURN INTELLIGENCE: %s", strings.ToUpper(r.ThemeName))
	pad := max((119-len(title))/2, 0)
	sb.WriteString(strings.Repeat(" ", pad))
	sb.WriteString(title)
	sb.WriteString("\n")
	sb.WriteString("=======================================================================================================================\n")

	// Inception & Account Metadata
	sb.WriteString(fmt.Sprintf("• Demat Account:        %s (portfolio.db)\n", r.AccountID))
	sb.WriteString(fmt.Sprintf("• Earliest Inception:   %s (%d Calendar Days)\n", r.EarliestPurchase.Format("2006-01-02"), r.HoldingDays))
	sb.WriteString(fmt.Sprintf("• Evaluation Date:      %s\n", r.EvaluationDate.Format("2006-01-02 15:04:05 IST")))
	sb.WriteString("-----------------------------------------------------------------------------------------------------------------------\n")

	// Triple Return Framework Rollup
	sb.WriteString("🎯 TRIPLE RETURN FRAMEWORK (ACTIVE HOLDINGS):\n")
	sb.WriteString(fmt.Sprintf("  ├─ Money-Weighted Return (MWR / XIRR):   %+6.2f%% p.a.  🟢 (Exact Dated Cash Flow Compounding)\n", r.ActiveMWR))
	sb.WriteString(fmt.Sprintf("  ├─ Time-Weighted Return (TWR / Dietz):   %+6.2f%% p.a.  (Cumulative: %+5.2f%%)\n", r.ActiveTWRAnn, r.ActiveTWR))
	sb.WriteString(fmt.Sprintf("  └─ Holding Period Return (HPR):          %+6.2f%%        (Annualized: %+6.2f%% p.a.)\n", r.ActiveHPR, r.ActiveHPRAnn))
	sb.WriteString("-----------------------------------------------------------------------------------------------------------------------\n")

	// Capital & Wealth Creation Rollup
	sb.WriteString("💰 CAPITAL DEPLOYMENT & WEALTH CREATED:\n")
	sb.WriteString(fmt.Sprintf("  ├─ Current Invested Capital:             %s\n", formatRupees(r.ActiveInvestedValue, false)))
	sb.WriteString(fmt.Sprintf("  ├─ Current Market Valuation:             %s\n", formatRupees(r.ActiveCurrentValue, false)))
	sb.WriteString(fmt.Sprintf("  ├─ Net Unrealized Capital Gain:          %s (%+5.2f%%)\n", formatRupees(r.ActiveUnrealizedPnL, true), r.ActiveUnrealizedPct))
	sb.WriteString(fmt.Sprintf("  ├─ Corporate Cash Dividends Earned:      %s\n", formatRupees(r.ActiveDividends, true)))
	sb.WriteString(fmt.Sprintf("  └─ Total Wealth Created (Active):        %s\n", formatRupees(r.ActiveTotalWealth, true)))
	sb.WriteString("-----------------------------------------------------------------------------------------------------------------------\n")

	// Lifecycle Rebalancing Rollup
	if len(r.ExitedPositions) > 0 {
		sb.WriteString(fmt.Sprintf("🔄 FULL THEME LIFECYCLE (ACTIVE + %d EXITED REBALANCED POSITIONS):\n", len(r.ExitedPositions)))
		sb.WriteString(fmt.Sprintf("  ├─ Gross Capital Deployed (Total Buys):  %s\n", formatRupees(r.LifecycleGrossBuys, false)))
		sb.WriteString(fmt.Sprintf("  ├─ Capital Realized / Recycled (Sells):  %s\n", formatRupees(r.LifecycleGrossSells, false)))
		sb.WriteString(fmt.Sprintf("  ├─ Net Capital Outlay:                   %s\n", formatRupees(r.LifecycleNetOutlay, false)))
		sb.WriteString(fmt.Sprintf("  ├─ Realized Capital Gains (Closed Lots): %s\n", formatRupees(r.LifecycleRealizedGain, true)))
		sb.WriteString(fmt.Sprintf("  ├─ Total Dividends (Active + Exited):    %s\n", formatRupees(r.LifecycleDividends, true)))
		sb.WriteString(fmt.Sprintf("  ├─ Grand Total Wealth Created:           %s\n", formatRupees(r.LifecycleTotalWealth, true)))
		sb.WriteString(fmt.Sprintf("  ├─ Full Lifecycle MWR (XIRR):            %+6.2f%% p.a. 🟢\n", r.LifecycleMWR))
		sb.WriteString(fmt.Sprintf("  └─ Full Lifecycle HPR (on Gross Buys):   %+6.2f%%        (Annualized: %+6.2f%% p.a.)\n", r.LifecycleHPR, r.LifecycleHPRAnn))
		sb.WriteString("-----------------------------------------------------------------------------------------------------------------------\n")
	}

	// Benchmark Analytics
	if r.BenchmarkName != "" && r.BenchmarkStart > 0 {
		status := "OUTPERFORMING"
		if r.BenchmarkAlpha < 0 {
			status = "LAGGING"
		}
		sb.WriteString("⚖️  BENCHMARK & ALPHA COMPARISON:\n")
		sb.WriteString(fmt.Sprintf("  ├─ Benchmark Index:                      %s\n", r.BenchmarkName))
		sb.WriteString(fmt.Sprintf("  ├─ Benchmark Performance (%s -> %s): %+6.2f%%\n", r.EarliestPurchase.Format("02-Jan"), r.EvaluationDate.Format("02-Jan"), r.BenchmarkTWR))
		sb.WriteString(fmt.Sprintf("  └─ Active Theme Alpha:                   %+6.2f%%  🟢 [%s]\n", r.BenchmarkAlpha, status))
		sb.WriteString("=======================================================================================================================\n")
	}

	// Active Core Holdings Matrix
	if opts.Detail && len(r.ActivePositions) > 0 {
		sb.WriteString("\n📋 ACTIVE HOLDINGS PERFORMANCE MATRIX:\n")
		tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "Symbol\tQty\tAvg Cost\tLTP\tInvested\tCurrent Val\tPnL (₹)\tPnL %\tDividends\tHPR %\tXIRR (p.a.)\tDays\tTranches")
		fmt.Fprintln(tw, "------\t---\t--------\t---\t--------\t-----------\t-------\t-----\t---------\t-----\t-----------\t----\t--------")

		for _, p := range r.ActivePositions {
			xirrStr := "N/A (<14d)"
			if p.HasMWR {
				xirrStr = fmt.Sprintf("%+6.1f%%", p.MWR)
			}

			// Format tranches
			var trancheStrs []string
			for _, tr := range p.Tranches {
				trancheStrs = append(trancheStrs, fmt.Sprintf("%d@%.1f(%s)", tr.Quantity, tr.Price, tr.TradeDate.Format("01-02")))
			}
			tranchesSummary := strings.Join(trancheStrs, ", ")

			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t%s\t%+5.2f%%\t%s\t%+5.2f%%\t%s\t%d\t%s\n",
				p.Symbol, p.Quantity,
				formatRupees(p.AvgBuyPrice, false),
				formatRupees(p.CurrentPrice, false),
				formatRupees(p.InvestedValue, false),
				formatRupees(p.CurrentValue, false),
				formatRupees(p.UnrealizedPnL, true),
				p.UnrealizedPnLPct,
				formatRupees(p.Dividends, false),
				p.HPR, xirrStr, p.HoldingDays, tranchesSummary,
			)
		}
		tw.Flush()
		sb.WriteString("-----------------------------------------------------------------------------------------------------------------------\n")
	}

	// Exited Positions Matrix
	if opts.Detail && len(r.ExitedPositions) > 0 {
		sb.WriteString("\n🔄 EXITED REBALANCED POSITIONS LEDGER:\n")
		tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "Exited Symbol\tQty Sold\tBuy Outflow\tSell Inflow\tRealized Gain\tGain %\tDividends\tTotal Profit\tFirst Buy\tExit Date\tDays")
		fmt.Fprintln(tw, "-------------\t--------\t-----------\t-----------\t-------------\t------\t---------\t------------\t---------\t---------\t----")

		for _, ep := range r.ExitedPositions {
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%+5.2f%%\t%s\t%s\t%s\t%s\t%d\n",
				ep.Symbol, ep.QuantitySold,
				formatRupees(ep.BuyOutflow, false),
				formatRupees(ep.SellInflow, false),
				formatRupees(ep.RealizedGain, true),
				ep.GainPct,
				formatRupees(ep.Dividends, false),
				formatRupees(ep.TotalProfit, true),
				ep.FirstBuyDate.Format("2006-01-02"), ep.ExitDate.Format("2006-01-02"), ep.HoldingDays,
			)
		}
		tw.Flush()
		sb.WriteString("=======================================================================================================================\n")
	}

	return sb.String()
}

// RenderBanner generates a concise 4-line summary for embedding into holdings snapshot.
func RenderBanner(r *ThemeReturnReport) string {
	var sb strings.Builder
	sb.WriteString("-----------------------------------------------------------------------------------------------------------------------\n")
	sb.WriteString(fmt.Sprintf("🎯 AUDITED RETURN INTELLIGENCE (via portfolio.db | %d Days Holding Span):\n", r.HoldingDays))
	sb.WriteString(fmt.Sprintf("• Money-Weighted Return (MWR / XIRR):  %+6.2f%% p.a. 🟢\n", r.ActiveMWR))
	sb.WriteString(fmt.Sprintf("• Time-Weighted Return (TWR / Dietz):  %+6.2f%% p.a. (Cumulative: %+5.2f%%)\n", r.ActiveTWRAnn, r.ActiveTWR))
	sb.WriteString(fmt.Sprintf("• Total Wealth Created (Active):       %s (Unrealized: %s | Dividends: %s)\n",
		formatRupees(r.ActiveTotalWealth, true),
		formatRupees(r.ActiveUnrealizedPnL, true),
		formatRupees(r.ActiveDividends, true),
	))
	if len(r.ExitedPositions) > 0 {
		sb.WriteString(fmt.Sprintf("• Full Lifecycle Compounding (XIRR):   %+6.2f%% p.a. (Incl. %s Realized Rebalancing Gains across %d Exits)\n",
			r.LifecycleMWR, formatRupees(r.LifecycleRealizedGain, true), len(r.ExitedPositions)))
	}
	if r.BenchmarkName != "" && r.BenchmarkStart > 0 {
		sb.WriteString(fmt.Sprintf("• Benchmark Alpha (%s):          %+6.2f%% 🟢 [OUTPERFORMING]\n", r.BenchmarkName, r.BenchmarkAlpha))
	}
	return sb.String()
}
