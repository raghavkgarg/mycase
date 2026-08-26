package selectiontracker

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Tracker records the lifecycle of tickers during the selection process.
type Tracker struct {
	InitialCount    int
	SafetyReasons   map[string]string  // ticker -> reason
	RawScores       map[string]float64 // ticker -> score
	RawRanks        map[string]int     // ticker -> 1-based rank
	SectorCapDrops  map[string]string  // ticker -> explanation
	HysteresisDrops map[string]string  // ticker -> explanation
	SelectedReasons map[string]string  // ticker -> explanation
	AdditionDrivers map[string]string  // ticker -> positive driver summary
	ResultDates     map[string]string  // ticker -> "24-04-26 ->  25-06-26"
}

// New initializes and returns a new Tracker instance.
func New() *Tracker {
	return &Tracker{
		SafetyReasons:   make(map[string]string),
		RawScores:       make(map[string]float64),
		RawRanks:        make(map[string]int),
		SectorCapDrops:  make(map[string]string),
		HysteresisDrops: make(map[string]string),
		SelectedReasons: make(map[string]string),
		AdditionDrivers: make(map[string]string),
		ResultDates:     make(map[string]string),
	}
}

// RecordSafetyDrop saves a hard filter rejection reason.
func (t *Tracker) RecordSafetyDrop(ticker, reason string) {
	t.SafetyReasons[ticker] = reason
}

// RecordRawScore saves the score and rank for a ticker that passed safety filters.
func (t *Tracker) RecordRawScore(ticker string, score float64, rank int) {
	t.RawScores[ticker] = score
	t.RawRanks[ticker] = rank
}

// RecordSectorCapDrop logs that a ticker was dropped because of sector caps.
func (t *Tracker) RecordSectorCapDrop(ticker, sector string, higherTickers []string) {
	fillers := strings.Join(higherTickers, ", ")
	t.SectorCapDrops[ticker] = fmt.Sprintf("Sector cap for '%s' exceeded (3/3 slots filled by %s)", sector, fillers)
}

// RecordHysteresisDrop logs rejection during top N and hysteresis evaluation.
func (t *Tracker) RecordHysteresisDrop(ticker string, rank, topN, bufferLimit int, isExisting bool) {
	if isExisting {
		t.HysteresisDrops[ticker] = fmt.Sprintf("Removed: Rank %d fell below hysteresis buffer limit (%d)", rank, bufferLimit)
	} else {
		if rank <= topN {
			t.HysteresisDrops[ticker] = "Not added: portfolio full (slots filled by existing holdings retained via hysteresis)"
		} else {
			t.HysteresisDrops[ticker] = fmt.Sprintf("Not added: Rank %d fell below selection cutoff (Top %d)", rank, topN)
		}
	}
}

// RecordAdditionDriver logs key positive metric drivers for new additions or selected stocks.
func (t *Tracker) RecordAdditionDriver(ticker, driverSummary string) {
	t.AdditionDrivers[ticker] = driverSummary
}

// RecordSelected logs that a ticker was selected and specifies why.
func (t *Tracker) RecordSelected(ticker string, rank, limit int, isExisting bool) {
	if isExisting {
		t.SelectedReasons[ticker] = fmt.Sprintf("Retained via hysteresis (Rank %d <= %d)", rank, limit)
	} else {
		t.SelectedReasons[ticker] = fmt.Sprintf("New addition (Rank %d)", rank)
	}
}

type parsedDriverMetrics struct {
	ttmGrowth string
	cagr3y    string
	roce      string
	instStake string
	forwardPE string
	fcfYield  string
	isMulti   bool
	isValue   bool
}

func extractMetricBetween(s, startStr, endStr string) string {
	_, after, ok := strings.Cut(s, startStr)
	if !ok {
		return ""
	}
	sub := after
	before0, _, ok0 := strings.Cut(sub, endStr)
	if !ok0 {
		return strings.TrimSpace(sub)
	}
	return strings.TrimSpace(before0)
}

func parseDriverString(s string) parsedDriverMetrics {
	var m parsedDriverMetrics
	if strings.Contains(s, "TTM Growth:") {
		m.isMulti = true
		m.ttmGrowth = extractMetricBetween(s, "TTM Growth: ", "%")
		m.cagr3y = extractMetricBetween(s, "(3Y: ", "%")
		m.roce = extractMetricBetween(s, "ROCE: ", "%")
		m.instStake = extractMetricBetween(s, "Inst Stake: ", "%")
	} else if strings.Contains(s, "Forward PE:") {
		m.isValue = true
		m.forwardPE = extractMetricBetween(s, "Forward PE: ", ",")
		m.fcfYield = extractMetricBetween(s, "FCF Yield: ", "%")
		m.instStake = extractMetricBetween(s, "Inst Stake: ", "%")
	}
	return m
}

func formatDriverDelta(prevStr, currStr string) string {
	if currStr == "" {
		return ""
	}
	if prevStr == "" {
		prevStr = currStr
	}
	p := parseDriverString(prevStr)
	c := parseDriverString(currStr)

	if c.isMulti && p.isMulti && p.ttmGrowth != "" {
		ttmStr := p.ttmGrowth + "% to " + c.ttmGrowth + "%"
		cagrStr := p.cagr3y + "% to " + c.cagr3y + "%"
		roceStr := p.roce + "% to " + c.roce + "%"
		instStr := p.instStake + "% to " + c.instStake + "%"
		return fmt.Sprintf("TTM Growth: %s (3Y: %s), ROCE: %s, Inst Stake: %s", ttmStr, cagrStr, roceStr, instStr)
	}

	if c.isValue && p.isValue && p.forwardPE != "" {
		peStr := p.forwardPE + " to " + c.forwardPE
		fcfStr := p.fcfYield + "% to " + c.fcfYield + "%"
		instStr := p.instStake + "% to " + c.instStake + "%"
		return fmt.Sprintf("Forward PE: %s, FCF Yield: %s, Inst Stake: %s", peStr, fcfStr, instStr)
	}

	return currStr
}

// RecordResultDates logs the quarterly result dates (prev -> coming) for a ticker.
func (t *Tracker) RecordResultDates(ticker, dates string) {
	t.ResultDates[ticker] = dates
}

// SaveReport generates a structured selection reasons report in the report/ folder.
func (t *Tracker) SaveReport(displayName, method string, existingHoldings map[string]float64, sectors map[string]string, weights map[string]float64, resultDates map[string]string) error {
	safeName := strings.ReplaceAll(strings.ToLower(displayName), " ", "_")
	reportDir := filepath.Join("report", fmt.Sprintf("%s_%s", safeName, method), "executions")
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		return fmt.Errorf("failed to create report directory: %w", err)
	}

	dateStr := time.Now().Format("20060102")
	outPath := filepath.Join(reportDir, fmt.Sprintf("%s_01_selection_reasons.txt", dateStr))

	// Locate previous report drivers
	prevDriversMap := make(map[string]string)

	if entries, err := os.ReadDir(reportDir); err == nil {
		var reportFiles []string
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), "_01_selection_reasons.txt") {
				filePath := filepath.Join(reportDir, entry.Name())
				reportFiles = append(reportFiles, filePath)
			}
		}
		sort.Strings(reportFiles)
		if len(reportFiles) > 0 {
			lastReport := reportFiles[len(reportFiles)-1]
			if data, err := os.ReadFile(lastReport); err == nil {
				lines := strings.Split(string(data), "\n")
				inSelected := false
				for _, l := range lines {
					tr := strings.TrimSpace(l)
					if strings.Contains(tr, "SELECTED STOCKS") {
						inSelected = true
						continue
					}
					if inSelected && strings.Contains(tr, "REMOVED ACTIVE HOLDINGS") {
						inSelected = false
						break
					}
					if inSelected && strings.Contains(l, "|") {
						parts := strings.Split(l, "|")
						if len(parts) >= 6 {
							ticker := strings.TrimSpace(parts[0])
							if ticker == "Ticker" || strings.HasPrefix(ticker, "---") || ticker == "" {
								continue
							}
							reasonCol := strings.TrimSpace(parts[len(parts)-1])
							_, after, ok := strings.Cut(reasonCol, "Drivers: ")
							if ok {
								prevDriversMap[ticker] = strings.TrimSpace(after)
							}
						}
					}
				}
			}
		}
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("failed to create explanation file: %w", err)
	}
	defer f.Close()

	// Compute summary statistics
	selectedCount := len(t.SelectedReasons)
	safetyCount := len(t.SafetyReasons)
	sectorCapCount := len(t.SectorCapDrops)
	hysteresisCount := len(t.HysteresisDrops)
	passedSafetyCount := t.InitialCount - safetyCount

	var writers []io.Writer = []io.Writer{f, os.Stdout}
	multiW := io.MultiWriter(writers...)

	writeLine := func(format string, args ...any) {
		fmt.Fprintf(multiW, format, args...)
	}

	writeLine("\n====================================================================\n")
	writeLine("             Stock Selection & Rejection Explanation Report\n")
	writeLine("====================================================================\n")
	writeLine("Index/File:       %s\n", displayName)
	writeLine("Strategy Preset:  %s\n", method)
	writeLine("Generated:        %s\n", time.Now().Format("2006-01-02 15:04:05 MST"))
	writeLine("====================================================================\n\n")

	writeLine("--- SUMMARY ---\n")
	writeLine("Initial pool size:              %d constituents\n", t.InitialCount)
	writeLine("Passed Safety/Hard Filters:     %d stocks\n", passedSafetyCount)
	writeLine("Eliminated by Safety Filters:   %d stocks\n", safetyCount)
	writeLine("Eliminated by Sector Caps:      %d stocks\n", sectorCapCount)
	writeLine("Eliminated by Rank Limits:      %d stocks\n", hysteresisCount)
	writeLine("Final Selected Stocks:          %d stocks\n\n", selectedCount)

	// 1. Selected Stocks Section
	writeLine("=============================================================================================================\n")
	writeLine("                                               SELECTED STOCKS\n")
	writeLine("=============================================================================================================\n")
	if len(t.SelectedReasons) == 0 {
		writeLine("No stocks were selected.\n")
	} else {
		type selectedRow struct {
			ticker string
			score  float64
			rank   int
			reason string
		}
		var sRows []selectedRow
		for ticker, reason := range t.SelectedReasons {
			sRows = append(sRows, selectedRow{
				ticker: ticker,
				score:  t.RawScores[ticker],
				rank:   t.RawRanks[ticker],
				reason: reason,
			})
		}
		sort.Slice(sRows, func(i, j int) bool {
			return sRows[i].rank < sRows[j].rank
		})

		writeLine("%-16s | %-20s | %-6s | %-8s | %-14s | %-21s | %s\n", "Ticker", "Sector", "Score", "Raw Rank", "Weight Decided", "Result Prev -> Coming", "Selection Reason")
		writeLine("---------------------------------------------------------------------------------------------------------------------------------------------\n")
		for _, r := range sRows {
			sec := sectors[r.ticker]
			if sec == "" {
				sec = "Unknown"
			}
			weightVal := weights[r.ticker]
			weightStr := fmt.Sprintf("%.2f%%", weightVal*100.0)
			if weightVal == 0 {
				weightStr = "0.00%"
			}

			resDates := t.ResultDates[r.ticker]
			if resDates == "" && resultDates != nil {
				resDates = resultDates[r.ticker]
			}
			if resDates == "" {
				resDates = "N/A -> N/A"
			}

			prevW, isExisting := existingHoldings[r.ticker]
			newW := weightVal
			drivers := t.AdditionDrivers[r.ticker]
			prevDrivers := prevDriversMap[r.ticker]

			driverDelta := formatDriverDelta(prevDrivers, drivers)

			var reasonStr string
			if !isExisting || prevW <= 0.00001 {
				if drivers != "" {
					reasonStr = fmt.Sprintf("New addition (Rank %d) | Drivers: %s", r.rank, drivers)
				} else {
					reasonStr = fmt.Sprintf("New addition (Rank %d)", r.rank)
				}
			} else if newW > prevW+0.0001 {
				if driverDelta != "" {
					reasonStr = fmt.Sprintf("Increased weight (%.2f%% -> %.2f%%) | Drivers: %s", prevW*100.0, newW*100.0, driverDelta)
				} else {
					reasonStr = fmt.Sprintf("Increased weight (%.2f%% -> %.2f%%)", prevW*100.0, newW*100.0)
				}
			} else if newW < prevW-0.0001 {
				if driverDelta != "" {
					reasonStr = fmt.Sprintf("Reduced weight (%.2f%% -> %.2f%%) | Drivers: %s", prevW*100.0, newW*100.0, driverDelta)
				} else {
					reasonStr = fmt.Sprintf("Reduced weight (%.2f%% -> %.2f%%)", prevW*100.0, newW*100.0)
				}
			} else {
				reasonStr = fmt.Sprintf("No Change (Retained Rank %d <= 25)", r.rank)
			}

			writeLine("%-16s | %-20s | %5.1f  | %-8d | %-14s | %-21s | %s\n", r.ticker, sec, r.score, r.rank, weightStr, resDates, reasonStr)
		}
	}
	writeLine("\n")

	// 2. Removed Active Holdings (Exits) Section
	writeLine("=============================================================================================\n")
	writeLine("                               REMOVED ACTIVE HOLDINGS (EXITS)\n")
	writeLine("=============================================================================================\n")
	var removedRows []struct {
		ticker string
		score  float64
		rank   int
		reason string
	}

	for ticker := range existingHoldings {
		if _, selected := t.SelectedReasons[ticker]; !selected {
			reason := "Missing from index dataset or fetch error"
			if r, ok := t.SafetyReasons[ticker]; ok {
				reason = fmt.Sprintf("Failed safety filter: %s", r)
			} else if r, ok := t.SectorCapDrops[ticker]; ok {
				reason = fmt.Sprintf("Dropped by sector cap: %s", r)
			} else if r, ok := t.HysteresisDrops[ticker]; ok {
				reason = r
			}

			score := 0.0
			rank := 999
			if s, ok := t.RawScores[ticker]; ok {
				score = s
			}
			if r, ok := t.RawRanks[ticker]; ok {
				rank = r
			}

			removedRows = append(removedRows, struct {
				ticker string
				score  float64
				rank   int
				reason string
			}{ticker, score, rank, reason})
		}
	}

	if len(removedRows) == 0 {
		writeLine("No existing holdings were removed.\n")
	} else {
		sort.Slice(removedRows, func(i, j int) bool {
			return removedRows[i].ticker < removedRows[j].ticker
		})
		writeLine("%-16s | %-20s | %-6s | %-8s | %s\n", "Ticker", "Sector", "Score", "Raw Rank", "Exit Reason")
		writeLine("---------------------------------------------------------------------------------------------\n")
		for _, r := range removedRows {
			rankStr := fmt.Sprintf("%d", r.rank)
			if r.rank == 999 {
				rankStr = "N/A"
			}
			scoreStr := fmt.Sprintf("%5.1f", r.score)
			if r.rank == 999 {
				scoreStr = "N/A"
			}
			sec := sectors[r.ticker]
			if sec == "" {
				sec = "Unknown"
			}
			writeLine("%-16s | %-20s | %-6s | %-8s | %s\n", r.ticker, sec, scoreStr, rankStr, r.reason)
		}
	}
	writeLine("\n")

	// 3. Rejected Candidates (Not in previous holdings, but failed caps or rankings)
	writeLine("=============================================================================================\n")
	writeLine("                                  REJECTED NEW CANDIDATES\n")
	writeLine("=============================================================================================\n")
	var rejectedCandidates []struct {
		ticker string
		score  float64
		rank   int
		reason string
	}

	for ticker, reason := range t.SectorCapDrops {
		if _, ok := existingHoldings[ticker]; !ok {
			rejectedCandidates = append(rejectedCandidates, struct {
				ticker string
				score  float64
				rank   int
				reason string
			}{ticker, t.RawScores[ticker], t.RawRanks[ticker], reason})
		}
	}
	for ticker, reason := range t.HysteresisDrops {
		if _, ok := existingHoldings[ticker]; !ok {
			rejectedCandidates = append(rejectedCandidates, struct {
				ticker string
				score  float64
				rank   int
				reason string
			}{ticker, t.RawScores[ticker], t.RawRanks[ticker], reason})
		}
	}

	if len(rejectedCandidates) == 0 {
		writeLine("No new candidates were rejected.\n")
	} else {
		sort.Slice(rejectedCandidates, func(i, j int) bool {
			return rejectedCandidates[i].rank < rejectedCandidates[j].rank
		})
		writeLine("%-16s | %-20s | %-6s | %-8s | %s\n", "Ticker", "Sector", "Score", "Raw Rank", "Rejection Reason")
		writeLine("---------------------------------------------------------------------------------------------\n")
		for _, r := range rejectedCandidates {
			sec := sectors[r.ticker]
			if sec == "" {
				sec = "Unknown"
			}
			writeLine("%-16s | %-20s | %5.1f  | %-8d | %s\n", r.ticker, sec, r.score, r.rank, r.reason)
		}
	}
	writeLine("\n")

	// 4. Safety & Fundamental Exclusions
	f.WriteString("=============================================================================================\n")
	f.WriteString("                               SAFETY & FUNDAMENTAL ELIMINATIONS\n")
	f.WriteString("=============================================================================================\n")
	if len(t.SafetyReasons) == 0 {
		f.WriteString("No stocks were eliminated by safety/hard filters.\n")
	} else {
		var safetyList []string
		for ticker := range t.SafetyReasons {
			safetyList = append(safetyList, ticker)
		}
		sort.Strings(safetyList)

		fmt.Fprintf(f, "%-16s | %s\n", "Ticker", "Rejection Reason")
		fmt.Fprintf(f, "---------------------------------------------------------------------------------------------\n")
		for _, ticker := range safetyList {
			fmt.Fprintf(f, "%-16s | %s\n", ticker, t.SafetyReasons[ticker])
		}
	}
	f.WriteString("\n")
	writeLine("Selection explanation report successfully saved to %s\n\n", outPath)

	return nil
}
