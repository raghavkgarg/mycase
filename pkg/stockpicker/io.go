package stockpicker

import (
	"encoding/csv"
	"fmt"
	"os"
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
	}
	fmt.Printf("Remaining Candidates: %d / %d\n\n", remaining, total)
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
	fmt.Printf("%-16s | %-10s | %-8s | %-10s | %-5s | %-7s | %-6s | %-12.4f\n", "Total Weight", "", "", "", "", "", "", totalNewWeight)
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
	fmt.Printf("%-16s | %-15s | %-12.4f | %-12s\n", "Total Weight", "", totalNewWeight, "")
	fmt.Println("=========================================================================")
}

// PrintScuttlebutt saves manual scuttlebutt check prompts to a text file in the report/ folder.
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
	fmt.Fprintln(outFile, "          QUALITATIVE FILTER ('SCUTTLEBUTT' OVERLAY) INSTRUCTIONS")
	fmt.Fprintln(outFile, "=========================================================================")
	fmt.Fprintf(outFile, "Index/File: %s\n", displayName)
	fmt.Fprintln(outFile, "For the top selected candidates, perform the following manual checks:")
	for idx, t := range selectedKeys {
		f := fundamentals[t]
		fmt.Fprintf(outFile, "\n%d. %s\n", idx+1, t)
		fmt.Fprintln(outFile, "   [ ] Earnings Call Transcripts (Seeking Alpha / Company IR):")
		fmt.Fprintln(outFile, "       * Has management consistently hit their guidance over the last 4 quarters?")
		fmt.Fprintln(outFile, "       * What is their forecast for margin expansion next year?")
		fmt.Fprintln(outFile, "   [ ] Annual Report (10-K / MD&A section):")
		fmt.Fprintln(outFile, "       * Locate Management Discussion & Analysis.")
		fmt.Fprintln(outFile, "       * What is the Total Addressable Market (TAM) mentioned? Is it growing > 15%?")
		fmt.Fprintln(outFile, "       * Are there any mentions of strategic business pivots or new factory CapEx?")
		fmt.Fprintln(outFile, "   [ ] Shareholder Shareholding Trend:")
		fmt.Fprintf(outFile, "       * Check if institutional holdings (%.1f%%) have risen QoQ.\n", f.HeldPercentInstitutions*100.0)
	}
	fmt.Fprintln(outFile, "=========================================================================")

	fmt.Printf("\nSaved qualitative scuttlebutt checklist to %s\n", outPath)
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
