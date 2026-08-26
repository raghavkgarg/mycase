package csvloader

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CombineMultipleCSVs merges the unique tickers from multiple source CSV files into one output CSV.
func CombineMultipleCSVs(paths []string, outFile string) error {
	tickers := make(map[string]bool)

	readTickers := func(path string) error {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		r := csv.NewReader(f)
		records, err := r.ReadAll()
		if err != nil {
			return err
		}

		if len(records) < 2 {
			return nil
		}

		tickerIdx := -1
		for i, h := range records[0] {
			if strings.ToLower(strings.TrimSpace(h)) == "ticker" {
				tickerIdx = i
				break
			}
		}

		if tickerIdx == -1 {
			return fmt.Errorf("ticker column not found in %s", path)
		}

		for _, row := range records[1:] {
			if len(row) > tickerIdx {
				t := strings.TrimSpace(row[tickerIdx])
				if t != "" {
					tickers[t] = true
				}
			}
		}
		return nil
	}

	for _, path := range paths {
		if err := readTickers(path); err != nil {
			return fmt.Errorf("error reading %s: %w", path, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(outFile), 0755); err != nil {
		return err
	}

	outF, err := os.Create(outFile)
	if err != nil {
		return err
	}
	defer outF.Close()

	w := csv.NewWriter(outF)
	if err := w.Write([]string{"ticker"}); err != nil {
		return err
	}

	for t := range tickers {
		if err := w.Write([]string{t}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// MergeGoldenCopy merges the new candidates into the golden copy, preserving existing tickers
// that are not in the new set at 0.0000 weight.
func MergeGoldenCopy(src, dst string) error {
	existingTickers := make(map[string]bool)
	dstFile, err := os.Open(dst)
	if err == nil {
		r := csv.NewReader(dstFile)
		records, err := r.ReadAll()
		dstFile.Close()
		if err == nil && len(records) > 1 {
			tickerIdx := -1
			for i, h := range records[0] {
				if strings.ToLower(strings.TrimSpace(h)) == "ticker" {
					tickerIdx = i
					break
				}
			}
			if tickerIdx != -1 {
				for _, row := range records[1:] {
					if len(row) > tickerIdx {
						t := strings.TrimSpace(row[tickerIdx])
						if t != "" {
							existingTickers[t] = true
						}
					}
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("error opening destination file: %w", err)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("error opening source file: %w", err)
	}
	defer srcFile.Close()

	srcReader := csv.NewReader(srcFile)
	srcRecords, err := srcReader.ReadAll()
	if err != nil {
		return fmt.Errorf("error reading source file: %w", err)
	}

	if len(srcRecords) < 1 {
		return fmt.Errorf("source file is empty")
	}

	tickerIdx := -1
	weightIdx := -1
	for i, h := range srcRecords[0] {
		hLower := strings.ToLower(strings.TrimSpace(h))
		switch hLower {
		case "ticker":
			tickerIdx = i
		case "weight":
			weightIdx = i
		}
	}

	if tickerIdx == -1 || weightIdx == -1 {
		return fmt.Errorf("required columns (ticker, weight) not found in source file")
	}

	newTickers := make(map[string]bool)
	type csvRow struct {
		ticker string
		weight string
	}
	var outputRows []csvRow

	for _, row := range srcRecords[1:] {
		if len(row) > tickerIdx && len(row) > weightIdx {
			t := strings.TrimSpace(row[tickerIdx])
			w := strings.TrimSpace(row[weightIdx])
			if t != "" {
				outputRows = append(outputRows, csvRow{ticker: t, weight: w})
				newTickers[t] = true
			}
		}
	}

	for t := range existingTickers {
		if !newTickers[t] {
			outputRows = append(outputRows, csvRow{ticker: t, weight: "0.0000"})
		}
	}

	outF, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("error creating destination file: %w", err)
	}
	defer outF.Close()

	w := csv.NewWriter(outF)
	if err := w.Write([]string{"ticker", "weight"}); err != nil {
		return err
	}

	for _, row := range outputRows {
		if err := w.Write([]string{row.ticker, row.weight}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// ReadCSVWeights reads ticker weights from a CSV file.
func ReadCSVWeights(path string) (map[string]float64, error) {
	weights := make(map[string]float64)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return weights, nil
		}
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) < 2 {
		return weights, nil
	}

	tickerIdx := -1
	weightIdx := -1
	for i, h := range records[0] {
		hLower := strings.ToLower(strings.TrimSpace(h))
		switch hLower {
		case "ticker":
			tickerIdx = i
		case "weight":
			weightIdx = i
		}
	}

	if tickerIdx == -1 || weightIdx == -1 {
		return nil, fmt.Errorf("required columns (ticker, weight) not found in CSV %s", path)
	}

	for _, row := range records[1:] {
		if len(row) > tickerIdx && len(row) > weightIdx {
			t := strings.TrimSpace(row[tickerIdx])
			wStr := strings.TrimSpace(row[weightIdx])
			if t != "" {
				w, err := strconv.ParseFloat(wStr, 64)
				if err == nil {
					weights[t] += w
				}
			}
		}
	}

	for t, w := range weights {
		if w <= 0.00001 {
			delete(weights, t)
		}
	}
	return weights, nil
}

type tickerRankScore struct {
	rank  int
	score float64
}

// parseSelectionReport parses a selection reasons report file and returns a map of ticker to rank/score.
func parseSelectionReport(filePath string) map[string]tickerRankScore {
	res := make(map[string]tickerRankScore)
	f, err := os.Open(filePath)
	if err != nil {
		return res
	}
	defer f.Close()

	r := csv.NewReader(f)
	_ = r
	// Read line by line
	data, err := io.ReadAll(f)
	if err != nil {
		return res
	}

	lines := strings.Split(string(data), "\n")
	inSelected := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "SELECTED STOCKS") {
			inSelected = true
			continue
		}
		if inSelected && strings.Contains(trimmed, "REMOVED ACTIVE HOLDINGS") {
			inSelected = false
			break
		}
		if inSelected && strings.Contains(line, "|") {
			parts := strings.Split(line, "|")
			if len(parts) >= 4 {
				ticker := strings.TrimSpace(parts[0])
				if ticker == "Ticker" || strings.HasPrefix(ticker, "---") || ticker == "" {
					continue
				}
				scoreStr := strings.TrimSpace(parts[2])
				rankStr := strings.TrimSpace(parts[3])

				scoreVal, sErr := strconv.ParseFloat(scoreStr, 64)
				rankVal, rErr := strconv.Atoi(rankStr)

				if sErr == nil && rErr == nil {
					res[ticker] = tickerRankScore{rank: rankVal, score: scoreVal}
				}
			}
		}
	}
	return res
}

// PrintComparisonReport compares a source candidate CSV with the destination golden copy CSV,
// prints the report to stdout and saves a copy in the report/ directory.
func PrintComparisonReport(src, dst, strategy string) {
	prevWeights, err := ReadCSVWeights(dst)
	if err != nil {
		fmt.Printf("Warning: Could not read golden copy for comparison: %v\n", err)
		return
	}
	newWeights, err := ReadCSVWeights(src)
	if err != nil {
		fmt.Printf("Warning: Could not read new candidates for comparison: %v\n", err)
		return
	}

	goldenBase := GetUniverseName(dst)
	reportDir := filepath.Join("report", fmt.Sprintf("%s_%s", goldenBase, strategy), "executions")
	dateStr := time.Now().Format("20060102")
	todayReportPath := filepath.Join(reportDir, fmt.Sprintf("%s_01_selection_reasons.txt", dateStr))

	currRanksScores := parseSelectionReport(todayReportPath)

	// Locate previous selection report
	var prevReportPath string
	if entries, err := os.ReadDir(reportDir); err == nil {
		var reportFiles []string
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), "_01_selection_reasons.txt") {
				reportFiles = append(reportFiles, filepath.Join(reportDir, entry.Name()))
			}
		}
		sort.Strings(reportFiles)
		for _, reportFile := range slices.Backward(reportFiles) {
			if reportFile != todayReportPath {
				prevReportPath = reportFile
				break
			}
		}
	}

	prevRanksScores := make(map[string]tickerRankScore)
	if prevReportPath != "" {
		prevRanksScores = parseSelectionReport(prevReportPath)
	}

	// Calculate counts
	var goldenActiveCount, newActiveCount int
	var newAdditions, removed, increased, reduced, noChange int

	allTickers := make(map[string]bool)
	for t, w := range prevWeights {
		if w > 0 {
			goldenActiveCount++
			allTickers[t] = true
		}
	}
	for t, w := range newWeights {
		if w > 0 {
			newActiveCount++
			allTickers[t] = true
		}
	}

	var tickersList []string
	for t := range allTickers {
		tickersList = append(tickersList, t)
	}
	sort.Strings(tickersList)

	var prevSum, newSum float64
	for _, w := range prevWeights {
		if w > 0 {
			prevSum += w
		}
	}
	for _, w := range newWeights {
		if w > 0 {
			newSum += w
		}
	}
	var avgPrev, avgNew float64
	if goldenActiveCount > 0 {
		avgPrev = (prevSum / float64(goldenActiveCount)) * 100
	}
	if newActiveCount > 0 {
		avgNew = (newSum / float64(newActiveCount)) * 100
	}

	type rowInfo struct {
		ticker string
		prevW  float64
		newW   float64
		action string
	}
	var rows []rowInfo

	for _, t := range tickersList {
		prevW := prevWeights[t]
		newW := newWeights[t]

		pInfo, hasPrev := prevRanksScores[t]
		cInfo, hasCurr := currRanksScores[t]

		var action string
		if prevW > 0 && newW == 0 {
			if hasPrev {
				action = fmt.Sprintf("Remove Action (Prev Rank #%d)", pInfo.rank)
			} else {
				action = "Remove Action"
			}
			removed++
		} else if prevW == 0 && newW > 0 {
			if hasCurr {
				action = fmt.Sprintf("New Addition (Rank #%d, Score %.1f)", cInfo.rank, cInfo.score)
			} else {
				action = "New Addition"
			}
			newAdditions++
		} else if prevW > 0 && newW > 0 {
			if newW > prevW {
				if hasPrev && hasCurr {
					action = fmt.Sprintf("Increased weight (Rank #%d -> #%d, Score %.1f -> %.1f)", pInfo.rank, cInfo.rank, pInfo.score, cInfo.score)
				} else {
					action = "Increased weight"
				}
				increased++
			} else if newW < prevW {
				if hasPrev && hasCurr {
					action = fmt.Sprintf("Reduced weight (Rank #%d -> #%d, Score %.1f -> %.1f)", pInfo.rank, cInfo.rank, pInfo.score, cInfo.score)
				} else {
					action = "Reduced weight"
				}
				reduced++
			} else {
				action = "No Change"
				noChange++
			}
		} else {
			continue
		}

		rows = append(rows, rowInfo{
			ticker: t,
			prevW:  prevW,
			newW:   newW,
			action: action,
		})
	}

	// Prepare multi-writer to stdout and report file
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		fmt.Printf("Warning: Could not create report directory: %v\n", err)
	}

	reportFileName := filepath.Join(reportDir, fmt.Sprintf("%s_02_comparison.txt", dateStr))

	fOut, err := os.Create(reportFileName)
	var fileWriter io.Writer
	if err != nil {
		fmt.Printf("Warning: Could not create report file %s: %v\n", reportFileName, err)
	} else {
		defer fOut.Close()
		fileWriter = fOut
	}

	stdoutWriter := os.Stdout
	writeLine := func(colorCode, format string, args ...any) {
		if colorCode != "" {
			fmt.Fprint(stdoutWriter, colorCode)
			fmt.Fprintf(stdoutWriter, format, args...)
			fmt.Fprint(stdoutWriter, "\033[0m")
		} else {
			fmt.Fprintf(stdoutWriter, format, args...)
		}
		if fileWriter != nil {
			fmt.Fprintf(fileWriter, format, args...)
		}
	}

	writeLine("", "\n----------------- PORTFOLIO COMPARISON REPORT -----------------\n")
	writeLine("", "Golden copy had %d active scripts with average weight: %.2f%%\n", goldenActiveCount, avgPrev)
	writeLine("", "New candidate set has %d active scripts with average weight: %.2f%%\n", newActiveCount, avgNew)

	// Highlight total additions and removals if present
	summaryColor := ""
	if newAdditions > 0 || removed > 0 {
		summaryColor = "\033[1;33m" // Yellow warning/attention
	}
	writeLine(summaryColor, "Changes summary: %d New Additions, %d Removals, %d Increased, %d Reduced, %d No Change\n\n",
		newAdditions, removed, increased, reduced, noChange)

	writeLine("", "%-20s | %-15s | %-10s | %s\n", "Symbol", "Previous Weight", "New Weight", "Action & Rank Rationale")
	writeLine("", "%s\n", strings.Repeat("-", 100))

	for _, r := range rows {
		prevStr := fmt.Sprintf("%.2f%%", r.prevW*100)
		if r.prevW == 0 {
			prevStr = "0.00%"
		}
		newStr := fmt.Sprintf("%.2f%%", r.newW*100)
		if r.newW == 0 {
			newStr = "0.00%"
		}

		colorCode := ""
		if strings.HasPrefix(r.action, "New Addition") {
			colorCode = "\033[1;32m" // Bold Green
		} else if strings.HasPrefix(r.action, "Remove Action") {
			colorCode = "\033[1;31m" // Bold Red
		} else if strings.HasPrefix(r.action, "Increased weight") {
			colorCode = "\033[1;36m" // Bold Cyan
		} else if strings.HasPrefix(r.action, "Reduced weight") {
			colorCode = "\033[1;33m" // Bold Yellow
		}

		writeLine(colorCode, "%-20s | %15s | %10s | %s\n", r.ticker, prevStr, newStr, r.action)
	}
	writeLine("", "----------------------------------------------------------------------------------------------------\n")

	if err == nil {
		fmt.Printf("Comparison report successfully saved to %s\n\n", reportFileName)
	}
}
