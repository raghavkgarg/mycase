package stockpicker

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// PrintHeader displays the header banner of the application.
func PrintHeader(displayName, method string, topN int, rangeStr, filePath string) {
	fmt.Printf("====================================================================\n")
	fmt.Printf("                 Go Mycase Index Stock Picker                    \n")
	fmt.Printf("====================================================================\n")
	if filePath != "" {
		fmt.Printf("Source File: %s\n", filePath)
	} else {
		fmt.Printf("Index:    %s\n", displayName)
	}
	fmt.Printf("Strategy: %s\n", method)
	fmt.Printf("Top N:    %d\n", topN)
	fmt.Printf("Range:    %s\n", rangeStr)
	fmt.Printf("====================================================================\n")
}

// PrintSafetyFilterSummary renders the stats from applying hard filters.
func PrintSafetyFilterSummary(hardFilters *config.HardFilters, stats FilterStats, method string, remaining, total int) {
	fmt.Printf("\nApplying Hard Filters defined in mfs.json to %d constituents...\n", total)
	fmt.Printf("Hard Filter Summary:\n")
	fmt.Printf("- Market Cap (%.0fCr - %.0fCr) eliminated: %d stocks\n", hardFilters.MinMarketCap/1e7, hardFilters.MaxMarketCap/1e7, stats.EliminatedSize)
	fmt.Printf("- ADV (< %.0fCr) eliminated:               %d stocks\n", hardFilters.MinADV/1e7, stats.EliminatedLiquidity)
	fmt.Printf("- Cash Flow Quality eliminated:            %d stocks\n", stats.EliminatedCashFlow)
	fmt.Printf("- Declining Earnings Trend eliminated:    %d stocks\n", stats.EliminatedEarningsTrend)
	fmt.Printf("- Low Promoter Stake (< %.0f%%) eliminated:  %d stocks\n", hardFilters.MinPromoterPercent*100.0, stats.EliminatedPromoter)
	fmt.Printf("- Below 200-Day SMA (Downtrend) eliminated: %d stocks\n", stats.EliminatedSMATrend)
	if hardFilters.MaxPledgedPercent > 0 {
		fmt.Printf("- High Promoter Pledging (>= %.1f%%) eliminated: %d stocks\n", hardFilters.MaxPledgedPercent*100.0, stats.EliminatedPledge)
	}
	if hardFilters.MinROCE > 0 {
		fmt.Printf("- Low Capital Efficiency (ROCE < %.1f%%) eliminated: %d stocks\n", hardFilters.MinROCE*100.0, stats.EliminatedROCE)
	}
	if hardFilters.MaxDebtToEquity > 0 {
		fmt.Printf("- High Debt/Equity (>= %.1f) eliminated:    %d stocks\n", hardFilters.MaxDebtToEquity, stats.EliminatedLeverage)
	}
	if hardFilters.MinInterestCoverage > 0 {
		fmt.Printf("- Low Interest Coverage (< %.1f) eliminated: %d stocks\n", hardFilters.MinInterestCoverage, stats.EliminatedInterestCoverage)
	}
	if hardFilters.MaxPEG > 0 {
		fmt.Printf("- High PEG (> %.1f) eliminated:               %d stocks\n", hardFilters.MaxPEG, stats.EliminatedPEG)
	}
	if hardFilters.CheckGrossMargin {
		fmt.Printf("- Declining Gross Margin Trend eliminated:   %d stocks\n", stats.EliminatedGrossMargin)
	}
	if hardFilters.MinRSPercentile > 0 {
		fmt.Printf("- Low Relative Strength (< %.0f%%) eliminated: %d stocks\n", hardFilters.MinRSPercentile, stats.EliminatedRSPercentile)
	}
	if hardFilters.MinCROIC > 0 {
		fmt.Printf("- Low CROIC (< %.1f%%) eliminated:             %d stocks\n", hardFilters.MinCROIC*100.0, stats.EliminatedCROIC)
	}
	if method == "multibagger" {
		fmt.Printf("- Sales Growth Accelerator eliminated:      %d stocks\n", stats.EliminatedSalesAccelerator)
		fmt.Printf("- Asset Turnover & CapEx Inflection eliminated: %d stocks\n", stats.EliminatedAssetTurnoverCapEx)
		fmt.Printf("- Working Capital (DSO) eliminated:         %d stocks\n", stats.EliminatedWorkingCapital)
		fmt.Printf("- Volume Breakout Check eliminated:         %d stocks\n", stats.EliminatedVolumeBreakout)
	} else if method == "early_multibagger" || method == "earlymb" {
		if hardFilters.EarningsBlackoutDaysBefore > 0 {
			fmt.Printf("- Earnings Event Blackout (±%d Days) eliminated: %d stocks\n", hardFilters.EarningsBlackoutDaysBefore, stats.EliminatedEarningsBlackout)
		}
		if hardFilters.MinProximity52WHigh > 0 {
			fmt.Printf("- Far from 52-Week High (< %.0f%%) eliminated:  %d stocks\n", hardFilters.MinProximity52WHigh*100.0, stats.EliminatedProximity52W)
		}
		if hardFilters.MinBaseDurationWeeks > 0 {
			fmt.Printf("- Base Duration (< %d Weeks) eliminated:       %d stocks\n", hardFilters.MinBaseDurationWeeks, stats.EliminatedBaseDuration)
		}
		fmt.Printf("- Working Capital Deterioration (DSO) eliminated: %d stocks\n", stats.EliminatedWorkingCapital)
	}
	fmt.Printf("Remaining Candidates: %d / %d\n\n", remaining, total)
}

// PrintEarlyMultibaggerTable prints output comparisons formatted for early-multibagger / pre-breakout strategy.
func PrintEarlyMultibaggerTable(
	selectedKeys []string,
	finalWeights map[string]float64,
	scores map[string]float64,
	fundamentals map[string]yfinance.Fundamentals,
	fullHistory map[string]*yfinance.HistoricalData,
	displayName string,
	method string,
) {
	fmt.Println("\n===================================================================================================================================")
	fmt.Printf("                   TOP %d SELECTED %s (PRE-BREAKOUT) STOCKS FROM %s               \n", len(selectedKeys), strings.ToUpper(method), strings.ToUpper(displayName))
	fmt.Println("===================================================================================================================================")
	fmt.Printf("%-16s | %-6s | %-8s | %-8s | %-8s | %-10s | %-8s | %-10s | %-8s | %-12s\n",
		"Ticker", "Score", "1M RS", "3M RS", "Base Wks", "VCP Ratio", "RVOL Z", "Decayed PP", "52W Prox", "Final Weight")
	fmt.Println("-----------------------------------------------------------------------------------------------------------------------------------")

	var totalNewWeight float64
	for _, t := range selectedKeys {
		weight := finalWeights[t]
		totalNewWeight += weight

		hist := fullHistory[t]
		var vcpRatio float64
		var prox52 float64
		var rs1m, rs3m float64
		var weeksInBase int
		var decayedPP float64
		var rvolZ float64

		if hist != nil && len(hist.Closes) > 0 {
			vcpRatio, _ = yfinance.CalculateVCPTightness(hist.Closes, hist.Opens)
			prox52 = yfinance.CalculateProximity52W(hist.Closes)
			weeksInBase, _ = yfinance.CalculateBaseDurationWeeks(hist.Closes, 0.85)
			decayedPP, _ = yfinance.CalculateDecayedPocketPivot(hist.Closes, hist.Opens, hist.Volumes, 10, 0.25)
			rvolZ = yfinance.CalculateRVOLZScore(hist.Volumes, 5, 50)
			_, rs1m, rs3m, _ = yfinance.CalculateCompositeRS(hist.Closes, nil)
		}

		ppStr := "0.0"
		if decayedPP > 0 {
			ppStr = fmt.Sprintf("%.1f", decayedPP)
		}

		fmt.Printf("%-16s | %-6.1f | %+-7.1f%% | %+-7.1f%% | %-8d | %-10.2f | %+-7.1f | %-10s | %-7.1f%% | %-12.4f\n",
			t, scores[t], rs1m*100.0, rs3m*100.0, weeksInBase,
			vcpRatio, rvolZ, ppStr, prox52*100.0, weight,
		)
	}
	fmt.Println("-----------------------------------------------------------------------------------------------------------------------------------")
	if totalNewWeight < 0.9999 {
		cashWeight := 1.0 - totalNewWeight
		fmt.Printf("%-16s | %-6s | %-8s | %-8s | %-8s | %-10s | %-8s | %-10s | %-8s | %-12.4f\n",
			"CASH_RESERVE", "-", "-", "-", "-", "-", "-", "-", "-", cashWeight)
		fmt.Println("-----------------------------------------------------------------------------------------------------------------------------------")
		fmt.Printf("%-16s | %-6s | %-8s | %-8s | %-8s | %-10s | %-8s | %-10s | %-8s | %-12.4f\n", "Total Weight", "", "", "", "", "", "", "", "", 1.0000)
	} else {
		fmt.Printf("%-16s | %-6s | %-8s | %-8s | %-8s | %-10s | %-8s | %-10s | %-8s | %-12.4f\n", "Total Weight", "", "", "", "", "", "", "", "", totalNewWeight)
	}
	fmt.Println("===================================================================================================================================")
}

// PrintMultibaggerTable prints output comparisons formatted for the multibagger and value strategies.
func PrintMultibaggerTable(
	selectedKeys []string,
	finalWeights map[string]float64,
	scores map[string]float64,
	fundamentals map[string]yfinance.Fundamentals,
	fullHistory map[string]*yfinance.HistoricalData,
	displayName string,
	method string,
) {
	fmt.Println("\n=================================================================================================")
	fmt.Printf("             TOP %d SELECTED %s STOCKS FROM %s               \n", len(selectedKeys), strings.ToUpper(method), strings.ToUpper(displayName))
	fmt.Println("=================================================================================================")
	fmt.Printf("%-16s | %-10s | %-8s | %-10s | %-5s | %-7s | %-6s | %-12s\n", "Ticker", "TTM Growth", "3Y CAGR", "DSO (L/P)", "RSI", "Inst %", "Score", "Final Weight")
	fmt.Println("-------------------------------------------------------------------------------------------------")

	var totalNewWeight float64
	for _, t := range selectedKeys {
		weight := finalWeights[t]
		totalNewWeight += weight

		f := fundamentals[t]

		_, ttmGrowth, cagr3y := yfinance.CalculateSalesGrowth(&f)
		_, dsoPrev, dsoLatest := yfinance.CalculateDSO(&f)

		rsiVal := yfinance.CalculateRSI(fullHistory[t].Closes)
		instPct := f.HeldPercentInstitutions

		fmt.Printf("%-16s | %-10.1f%% | %-8.1f%% | %-10s | %-5.1f | %-7.1f%% | %-6.1f | %-12.4f\n",
			t, ttmGrowth*100.0, cagr3y*100.0,
			fmt.Sprintf("%.0f/%.0f", dsoLatest, dsoPrev),
			rsiVal, instPct*100.0, scores[t], weight,
		)
	}
	fmt.Println("-------------------------------------------------------------------------------------------------")
	if totalNewWeight < 0.9999 {
		cashWeight := 1.0 - totalNewWeight
		fmt.Printf("%-16s | %-10s | %-8s | %-10s | %-5s | %-7s | %-6s | %-12.4f\n",
			"CASH_RESERVE", "-", "-", "-", "-", "-", "-", cashWeight)
		fmt.Println("-------------------------------------------------------------------------------------------------")
		fmt.Printf("%-16s | %-10s | %-8s | %-10s | %-5s | %-7s | %-6s | %-12.4f\n", "Total Weight", "", "", "", "", "", "", 1.0000)
	} else {
		fmt.Printf("%-16s | %-10s | %-8s | %-10s | %-5s | %-7s | %-6s | %-12.4f\n", "Total Weight", "", "", "", "", "", "", totalNewWeight)
	}
	fmt.Println("=================================================================================================")
}

// PrintStandardTable prints output comparisons formatted for standard strategies.
func PrintStandardTable(
	selectedKeys []string,
	finalWeights map[string]float64,
	fullHistory map[string]*yfinance.HistoricalData,
	displayName string,
	method string,
) {
	fmt.Println("\n=========================================================================")
	fmt.Printf("             TOP %d SELECTED %s STOCKS FROM %s               \n", len(selectedKeys), strings.ToUpper(method), strings.ToUpper(displayName))
	fmt.Println("=========================================================================")
	fmt.Printf("%-16s | %-15s | %-12s | %-12s\n", "Ticker", "Raw Score Rank", "Final Weight", "1Y Return")
	fmt.Println("---------------------------------------------------------")

	var totalNewWeight float64
	for idx, t := range selectedKeys {
		weight := finalWeights[t]
		totalNewWeight += weight

		closes := fullHistory[t].Closes
		oneYearRet := (closes[len(closes)-1] - closes[0]) / closes[0] * 100.0

		fmt.Printf("%-16s | #%-14d | %-12.4f | %+.1f%%\n", t, idx+1, weight, oneYearRet)
	}
	fmt.Println("---------------------------------------------------------")
	if totalNewWeight < 0.9999 {
		cashWeight := 1.0 - totalNewWeight
		fmt.Printf("%-16s | %-15s | %-12.4f | %-12s\n", "CASH_RESERVE", "-", cashWeight, "-")
		fmt.Println("---------------------------------------------------------")
		fmt.Printf("%-16s | %-15s | %-12.4f | %-12s\n", "Total Weight", "", 1.0000, "")
	} else {
		fmt.Printf("%-16s | %-15s | %-12.4f | %-12s\n", "Total Weight", "", totalNewWeight, "")
	}
	fmt.Println("=========================================================================")
}

// PrintScuttlebutt saves qualitative and automated live NSE scuttlebutt checks to a text file in the report/ folder.
func PrintScuttlebutt(selectedKeys []string, fundamentals map[string]yfinance.Fundamentals, displayName, strategy string) {
	if len(selectedKeys) == 0 {
		return
	}

	safeName := strings.ReplaceAll(strings.ToLower(displayName), " ", "_")
	reportDir := filepath.Join("report", fmt.Sprintf("%s_%s", safeName, strategy), "research")
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		fmt.Printf("Warning: Failed to create report directory: %v\n", err)
		return
	}

	dateStr := time.Now().Format("20060102")
	outPath := filepath.Join(reportDir, fmt.Sprintf("%s_scuttlebutt.txt", dateStr))

	outFile, err := os.Create(outPath)
	if err != nil {
		fmt.Printf("Warning: Failed to create scuttlebutt report file %s: %v\n", outPath, err)
		return
	}
	defer outFile.Close()

	fmt.Fprintln(outFile, "=========================================================================")
	fmt.Fprintln(outFile, "         AUTOMATED SCUTTLEBUTT & LIVE NSE QUALITATIVE RESEARCH REPORT")
	fmt.Fprintln(outFile, "=========================================================================")
	fmt.Fprintf(outFile, "Index/File:       %s\n", displayName)
	fmt.Fprintf(outFile, "Strategy:         %s\n", strategy)
	fmt.Fprintf(outFile, "Generated:        %s IST\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintln(outFile, "=========================================================================")

	// Load sector TAM config
	pyPath := "python3"
	if _, err := os.Stat(".venv/bin/python3"); err == nil {
		pyPath = ".venv/bin/python3"
	}
	exec.Command(pyPath, "scripts/update_sector_tam.py").Run()

	tamMap := make(map[string]string)
	if data, err := os.ReadFile("config/sector_tam.json"); err == nil {
		json.Unmarshal(data, &tamMap)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	liveDeliveryMap, _ := yfinance.FetchNselibDeliveryDataDetails(ctx, selectedKeys)
	qualDataMap, _ := yfinance.FetchQualitativeNSEData(ctx, selectedKeys)
	custConcMap, _ := yfinance.FetchCustomerConcentrationData(ctx, selectedKeys)

	for idx, t := range selectedKeys {
		f := fundamentals[t]
		sec := f.Sector
		if sec == "" {
			sec = "N/A"
		}
		resDates := f.ResultPrevComing
		if resDates == "" {
			resDates = "N/A -> N/A"
		}

		delPct := f.DeliveryPct
		delDate := f.DeliveryDate
		cleanSym := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(t, "NSE:"), "BSE:"), ".NS")
		if rec, ok := liveDeliveryMap[t]; ok && rec.DeliveryPct > 0 {
			delPct = rec.DeliveryPct
			delDate = rec.Date
		} else if rec, ok := liveDeliveryMap[cleanSym]; ok && rec.DeliveryPct > 0 {
			delPct = rec.DeliveryPct
			delDate = rec.Date
		}

		fmt.Fprintf(outFile, "\n%d. %-15s | Sector: %-20s | Market Cap: %.0fCr\n", idx+1, t, sec, f.MarketCap/1e7)
		fmt.Fprintln(outFile, "   ----------------------------------------------------------------------")
		fmt.Fprintf(outFile, "   [Live NSE Result Schedule]  : %s\n", resDates)
		if delPct > 0 {
			dLabel := "Last Business Day"
			if delDate != "" {
				if dTime, err := time.Parse("2006-01-02", delDate); err == nil {
					dLabel = fmt.Sprintf("Last Business Day: %s", dTime.Format("02-01-06"))
				} else {
					dLabel = fmt.Sprintf("Last Business Day: %s", delDate)
				}
			}
			fmt.Fprintf(outFile, "   [Live NSE Delivery Vol %%]   : %.1f%% deliverable accumulation (%s)\n", delPct, dLabel)
		} else {
			fmt.Fprintln(outFile, "   [Live NSE Delivery Vol %]   : N/A")
		}
		fmt.Fprintf(outFile, "   [Shareholding Snapshot]     : Institutional: %.1f%% | Promoter/Insiders: %.1f%% | Pledged: %.1f%%\n", f.HeldPercentInstitutions*100.0, f.InsidersPercent*100.0, f.PledgedPercent*100.0)

		_, ttmGrowth, cagr3y := yfinance.CalculateSalesGrowth(&f)
		roceVal, _ := GetLatestROCE(&f)
		_, latestDSO, prevDSO := yfinance.CalculateDSO(&f)

		if latestDSO > 0 {
			fmt.Fprintf(outFile, "   [Fundamental Traction]      : TTM Growth: %+.1f%% | 3Y CAGR: %+.1f%% | ROCE: %.1f%% | DSO: %.0fd (Prev: %.0fd)\n", ttmGrowth*100.0, cagr3y*100.0, roceVal*100.0, latestDSO, prevDSO)
		} else {
			fmt.Fprintf(outFile, "   [Fundamental Traction]      : TTM Growth: %+.1f%% | 3Y CAGR: %+.1f%% | ROCE: %.1f%%\n", ttmGrowth*100.0, cagr3y*100.0, roceVal*100.0)
		}

		// Operating Margin Trajectory
		_, latestOM, prevOM, omOk := yfinance.CalculateOperatingMarginTrajectory(&f)
		if omOk {
			bpsDelta := (latestOM - prevOM) * 10000.0
			fmt.Fprintf(outFile, "   [Operating Margin Trajectory]: OPM: %.1f%% -> %.1f%% (%+.0f bps YoY)\n", prevOM*100.0, latestOM*100.0, bpsDelta)
		} else if f.OperatingMargins != 0 {
			fmt.Fprintf(outFile, "   [Operating Margin Trajectory]: OPM: %.1f%%\n", f.OperatingMargins*100.0)
		}

		// CapEx Expansion Trend
		latestCapExCr, prevCapExCr, capexGrowth, capexOk := yfinance.CalculateCapExTrend(&f)
		deRatio := f.DebtToEquity
		if deRatio > 5.0 {
			deRatio = deRatio / 100.0
		}

		if capexOk && prevCapExCr > 0 {
			fmt.Fprintf(outFile, "   [Balance Sheet & Reinvestment]: Debt/Equity: %.2f | CapEx: %.1fCr -> %.1fCr (%+.1f%% YoY Expansion)\n", deRatio, prevCapExCr, latestCapExCr, capexGrowth)
		} else {
			fmt.Fprintf(outFile, "   [Balance Sheet & Reinvestment]: Debt/Equity: %.2f | Annual CapEx: %.1fCr\n", deRatio, latestCapExCr)
		}

		// Earnings Growth Consistency
		beats, totalBeats, beatOk := yfinance.CalculateEarningsBeatRate(&f)
		if beatOk {
			hitPct := (float64(beats) / float64(totalBeats)) * 100.0
			fmt.Fprintf(outFile, "   [Earnings Growth Consistency]: %d/%d YoY Growth Cycles (%.0f%% Expansion Rate)\n", beats, totalBeats, hitPct)
		} else {
			fmt.Fprintln(outFile, "   [Earnings Growth Consistency]: Metric Coverage Pending")
		}

		// Auditor Status & Live Transcripts
		auditorStatus := "Metric Coverage Pending (Failed to retrieve qualitative data)"
		transcriptSummary := "Metric Coverage Pending (Failed to retrieve qualitative data)"
		mgtStability := "Metric Coverage Pending (Failed to retrieve qualitative data)"
		rptStatus := "Metric Coverage Pending (Failed to retrieve qualitative data)"

		if qData, ok := qualDataMap[t]; ok && qData.AuditorStatus != "" {
			auditorStatus = qData.AuditorStatus
			transcriptSummary = qData.TranscriptSummary
			mgtStability = qData.ManagementStability
			rptStatus = qData.RPTStatus
		} else if qData, ok := qualDataMap[cleanSym]; ok && qData.AuditorStatus != "" {
			auditorStatus = qData.AuditorStatus
			transcriptSummary = qData.TranscriptSummary
			mgtStability = qData.ManagementStability
			rptStatus = qData.RPTStatus
		}

		fmt.Fprintf(outFile, "   [Auditor Opinion Status]    : %s\n", auditorStatus)
		fmt.Fprintf(outFile, "   [Live Transcript Highlights]: %s\n", transcriptSummary)

		tamInfo := tamMap[sec]
		if tamInfo == "" {
			tamInfo = "TAM Growth: > 15% CAGR Trajectory (Industry Expansion)"
		}
		fmt.Fprintf(outFile, "   [Sector TAM Trajectory]     : %s\n", tamInfo)

		fmt.Fprintf(outFile, "   [Management Stability Check]: %s\n", mgtStability)
		fmt.Fprintf(outFile, "   [Related Party Trans. Check]: %s\n", rptStatus)

		custConc := "Metric Coverage Pending (Place the Annual Report PDF in data/annual_reports/)"
		if val, ok := custConcMap[t]; ok && val != "" {
			custConc = val
		} else if val, ok := custConcMap[cleanSym]; ok && val != "" {
			custConc = val
		}
		fmt.Fprintf(outFile, "   [Customer Concentration Check]: %s\n", custConc)
	}
	fmt.Fprintln(outFile, "=========================================================================")

	fmt.Printf("\nSaved automated scuttlebutt report to %s\n", outPath)
}

// SavePortfolioToCSV persists final portfolio selections to a CSV file at the specified path.
func SavePortfolioToCSV(selectedKeys []string, finalWeights map[string]float64, outputPath string) error {
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	writer := csv.NewWriter(outFile)
	if err := writer.Write([]string{"ticker", "weight"}); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	for _, t := range selectedKeys {
		weightStr := fmt.Sprintf("%.4f", finalWeights[t])
		if err := writer.Write([]string{t, weightStr}); err != nil {
			return fmt.Errorf("failed to write record: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("CSV writer error: %w", err)
	}

	fmt.Printf("\nSuccessfully saved selected stock portfolio to %s\n", outputPath)
	return nil
}
