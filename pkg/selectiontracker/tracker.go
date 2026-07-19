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
			t.HysteresisDrops[ticker] = fmt.Sprintf("Not added: portfolio full (slots filled by existing holdings retained via hysteresis)")
		} else {
			t.HysteresisDrops[ticker] = fmt.Sprintf("Not added: Rank %d fell below selection cutoff (Top %d)", rank, topN)
		}
	}
}

// RecordSelected logs that a ticker was selected and specifies why.
func (t *Tracker) RecordSelected(ticker string, rank, limit int, isExisting bool) {
	if isExisting {
		t.SelectedReasons[ticker] = fmt.Sprintf("Retained via hysteresis (Rank %d <= %d)", rank, limit)
	} else {
		t.SelectedReasons[ticker] = fmt.Sprintf("New addition (Rank %d)", rank)
	}
}

// SaveReport generates a structured selection reasons report in the report/ folder.
func (t *Tracker) SaveReport(displayName, method string, existingHoldings map[string]float64, sectors map[string]string, weights map[string]float64) error {
	safeName := strings.ReplaceAll(strings.ToLower(displayName), " ", "_")
	reportDir := filepath.Join("report", fmt.Sprintf("%s_%s", safeName, method), "executions")
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		return fmt.Errorf("failed to create report directory: %w", err)
	}

	dateStr := time.Now().Format("20060102")
	outPath := filepath.Join(reportDir, fmt.Sprintf("%s_01_selection_reasons.txt", dateStr))

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

	writeLine := func(format string, args ...interface{}) {
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

		writeLine("%-16s | %-20s | %-6s | %-8s | %-14s | %s\n", "Ticker", "Sector", "Score", "Raw Rank", "Weight Decided", "Selection Reason")
		writeLine("-------------------------------------------------------------------------------------------------------------\n")
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
			writeLine("%-16s | %-20s | %5.1f  | %-8d | %-14s | %s\n", r.ticker, sec, r.score, r.rank, weightStr, r.reason)
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
			reason := "Dropped due to unknown reason"
			if r, ok := t.SafetyReasons[ticker]; ok {
				reason = fmt.Sprintf("Failed safety filters: %s", r)
			} else if r, ok := t.SectorCapDrops[ticker]; ok {
				reason = fmt.Sprintf("Dropped by Sector Cap: %s", r)
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
