package golden

import (
	"fmt"
	"io"
	"strings"
)

const (
	ansiReset     = "\033[0m"
	ansiBold      = "\033[1m"
	ansiBoldRed   = "\033[1;31m"
	ansiBoldGreen = "\033[1;32m"
	ansiBoldCyan  = "\033[1;36m"
	ansiBoldYel   = "\033[1;33m"
	ansiBoldMag   = "\033[1;35m"
	ansiDim       = "\033[2m"
)

func colorizeUpside(upsidePtr *float64, width int) string {
	if upsidePtr == nil {
		padLen := width - 1
		if padLen < 0 {
			padLen = 0
		}
		return strings.Repeat(" ", padLen) + "-"
	}
	val := *upsidePtr
	str := fmt.Sprintf("%+6.1f%%", val)
	padLen := width - len(str)
	if padLen < 0 {
		padLen = 0
	}
	padded := strings.Repeat(" ", padLen) + str

	if val > 0 {
		return ansiBoldGreen + padded + ansiReset
	} else if val < 0 {
		return ansiBoldRed + padded + ansiReset
	}
	return padded
}

func shortSector(sector string) string {
	switch sector {
	case "Consumer Defensive":
		return "Cons Defens"
	case "Consumer Cyclical":
		return "Cons Cycl"
	case "Financial Services":
		return "Financial"
	case "Basic Materials":
		return "Basic Mat"
	case "Communication Services":
		return "Communicat"
	default:
		return sector
	}
}

func truncateStr(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-1] + "…"
	}
	return s
}

// RenderReport outputs the Golden Triangle analysis to the provided writer.
func RenderReport(w io.Writer, r *GoldenReport) {
	div := strings.Repeat("=", 120)
	subDiv := strings.Repeat("-", 120)

	fmt.Fprintln(w, div)
	fmt.Fprintf(w, "%s      GOLDEN TRIANGLE MULTI-STRATEGY CONVERGENCE AUDIT (DuckDB PIT)      %s\n", ansiBoldCyan, ansiReset)
	fmt.Fprintf(w, "%s            Convergence: Early Multibagger + Multibagger + Predictive Fair Price%s\n", ansiDim, ansiReset)
	fmt.Fprintln(w, div)
	fmt.Fprintf(w, "As-Of Date:     %s\n", r.AsOfDate.Format("2006-01-02"))
	fmt.Fprintf(w, "Universe:       %s (Evaluated: %d active constituents)\n", r.IndexName, r.Census.TotalEvaluated)
	fmt.Fprintf(w, "Cohort Upside:  Median %s\n", colorizeUpside(&r.Census.CohortMedianUpside, 7))
	fmt.Fprintln(w, subDiv)

	// --- 1. REGIME CENSUS ---
	fmt.Fprintf(w, "\n%s--- 1. GOLDEN TRIANGLE REGIME CENSUS ---%s\n", ansiBold, ansiReset)
	fmt.Fprintf(w, "  • %sGolden Core%s (Full Trinity: Quality + Launchpad + Cushion) : %s%d stocks%s\n",
		ansiBoldGreen, ansiReset, ansiBoldGreen, r.Census.GoldenCoreCount, ansiReset)
	fmt.Fprintf(w, "  • %sHigh-Velocity%s (Momentum Leader + Multiple Overhang)       : %s%d stocks%s\n",
		ansiBoldCyan, ansiReset, ansiBoldCyan, r.Census.HighVelocityCount, ansiReset)
	fmt.Fprintf(w, "  • %sValue Breakouts%s (Deep Value + Emerging Accumulation)     : %s%d stocks%s\n",
		ansiBoldYel, ansiReset, ansiBoldYel, r.Census.ValueBreakoutCount, ansiReset)
	fmt.Fprintf(w, "  • %sCompounders on Sale%s (Quality + Deep Value, Awaiting Base) : %s%d stocks%s\n",
		ansiBoldMag, ansiReset, ansiBoldMag, r.Census.CompounderSaleCount, ansiReset)
	fmt.Fprintf(w, "  • General Cohort Monitor                                       : %d stocks\n", r.Census.MonitorCount)

	renderTable := func(title string, list []GoldenConstituent, emptyMsg string) {
		fmt.Fprintf(w, "\n%s--- %s ---%s\n", ansiBold, title, ansiReset)
		if len(list) == 0 {
			fmt.Fprintf(w, "  %s%s%s\n", ansiDim, emptyMsg, ansiReset)
			return
		}

		headerFmt := "  %-15s | %-13s | %7s | %8s | %5s | %7s | %8s | %10s | %8s | %6s | %-32s\n"
		rowFmt := "  %-15s | %-13s | %7s | %8s | %5s | %7s | %8s | %10s | %s | %6.1f | %-32s\n"

		fmt.Fprintf(w, headerFmt, "Ticker", "Sector", "MB (Q)", "EMB (V)", "VCP", "Deliv", "CMP", "Fair Price", "Upside", "Golden", "Tactical Action")
		fmt.Fprintln(w, "  "+strings.Repeat("-", 135))

		for _, c := range list {
			mbStr := "-"
			if c.MBScore != nil {
				mbStr = fmt.Sprintf("%.1f", *c.MBScore)
			}
			embStr := "-"
			if c.EMBScore != nil {
				embStr = fmt.Sprintf("%.1f", *c.EMBScore)
			}
			vcpStr := "-"
			if c.VCPRatio != nil {
				vcpStr = fmt.Sprintf("%.2f", *c.VCPRatio)
			}
			delivStr := "-"
			if c.DeliveryDelta != nil {
				delivStr = fmt.Sprintf("%+5.1f%%", *c.DeliveryDelta*100)
			}
			cmpStr := "-"
			if c.CMP != nil {
				cmpStr = fmt.Sprintf("₹%.1f", *c.CMP)
			}
			fpStr := "-"
			if c.FairPrice != nil {
				fpStr = fmt.Sprintf("₹%.1f", *c.FairPrice)
			}

			coloredUpside := colorizeUpside(c.UpsidePct, 8)
			action := truncateStr(c.DiagnosticAction, 32)

			fmt.Fprintf(w, rowFmt,
				c.Ticker,
				truncateStr(shortSector(c.Sector), 13),
				mbStr,
				embStr,
				vcpStr,
				delivStr,
				cmpStr,
				fpStr,
				coloredUpside,
				c.CompositeScore,
				action,
			)
		}
	}

	// --- 2. THE GOLDEN CORE ---
	renderTable(
		"2. THE GOLDEN CORE: THE FULL TRINITY (QUALITY + TIMING + VALUATION)",
		r.GoldenCore,
		"No stocks currently qualify for full 3-star Trinity conviction (requires MB Quality + EMB Launchpad + Upside >= -20%).",
	)

	// --- 3. HIGH-VELOCITY MOMENTUM LEADERS ---
	limitHV := r.HighVelocity
	if len(limitHV) > 15 {
		limitHV = limitHV[:15]
	}
	renderTable(
		"3. HIGH-VELOCITY MOMENTUM LEADERS (EMB LAUNCHPAD ACTIVE | MULTIPLE OVERHANG WARNING)",
		limitHV,
		"No active high-velocity momentum leaders on launchpad.",
	)

	// --- 4. ASYMMETRIC VALUE BREAKOUTS ---
	limitVB := r.ValueBreakouts
	if len(limitVB) > 10 {
		limitVB = limitVB[:10]
	}
	renderTable(
		"4. ASYMMETRIC VALUE BREAKOUTS (DEEP VALUE + EMERGING ACCUMULATION)",
		limitVB,
		"No deep-value turnaround breakouts currently detected.",
	)

	// --- 5. VALUATION CONTRACTION RISK ALERTS ---
	limitOA := r.OverhangAlerts
	if len(limitOA) > 10 {
		limitOA = limitOA[:10]
	}
	renderTable(
		"5. VALUATION BUBBLE & CONTRACTION RISK ALERTS (UPSIDE <= -60.0%)",
		limitOA,
		"No severe valuation overhang alerts detected.",
	)

	fmt.Fprintln(w, "\n"+div)
	fmt.Fprintf(w, "%sGolden Triangle Guideline:%s\n", ansiBold, ansiReset)
	fmt.Fprintln(w, "  1. Prioritize MAX sizing on 'THE GOLDEN CORE' (cushion protects capital; momentum compounds).")
	fmt.Fprintln(w, "  2. Trade 'HIGH-VELOCITY' strictly with tight trailing stops (multiple contraction risk is real).")
	fmt.Fprintln(w, "  3. Never hold stocks in 'VALUATION CONTRACTION RISK' without trailing stop-loss protection.")
	fmt.Fprintln(w, div)
}
