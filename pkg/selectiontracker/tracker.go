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
	InitialCount        int
	SafetyReasons       map[string]string  // ticker -> reason
	ScoreThresholdDrops map[string]string  // ticker -> reason
	RawScores           map[string]float64 // ticker -> raw score
	EffectiveScores     map[string]float64 // ticker -> effective score (raw * regime)
	RawRanks            map[string]int     // ticker -> 1-based rank
	SectorCapDrops      map[string]string  // ticker -> explanation
	HysteresisDrops     map[string]string  // ticker -> explanation
	SelectedReasons     map[string]string  // ticker -> explanation
	AdditionDrivers     map[string]string  // ticker -> positive driver summary
	ResultDates         map[string]string  // ticker -> "24-04-26 ->  25-06-26"
	RegimeMultiplier    float64
}

// New initializes and returns a new Tracker instance.
func New() *Tracker {
	return &Tracker{
		SafetyReasons:       make(map[string]string),
		ScoreThresholdDrops: make(map[string]string),
		RawScores:           make(map[string]float64),
		EffectiveScores:     make(map[string]float64),
		RawRanks:            make(map[string]int),
		SectorCapDrops:      make(map[string]string),
		HysteresisDrops:     make(map[string]string),
		SelectedReasons:     make(map[string]string),
		AdditionDrivers:     make(map[string]string),
		ResultDates:         make(map[string]string),
		RegimeMultiplier:    1.0,
	}
}

// RecordSafetyDrop saves a hard filter rejection reason.
func (t *Tracker) RecordSafetyDrop(ticker, reason string) {
	t.SafetyReasons[ticker] = reason
}

// RecordScoreThresholdDrop saves a regime score cutoff rejection reason.
func (t *Tracker) RecordScoreThresholdDrop(ticker, reason string) {
	t.ScoreThresholdDrops[ticker] = reason
}

// RecordRawScore saves the raw score and rank for a ticker that passed safety filters.
func (t *Tracker) RecordRawScore(ticker string, score float64, rank int) {
	t.RawScores[ticker] = score
	t.EffectiveScores[ticker] = score * t.RegimeMultiplier
	t.RawRanks[ticker] = rank
}

// RecordScore saves both raw and effective score for a ticker.
func (t *Tracker) RecordScore(ticker string, rawScore, effectiveScore float64, rank int) {
	t.RawScores[ticker] = rawScore
	t.EffectiveScores[ticker] = effectiveScore
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

// SelectionFunnel structurally models and validates exact constituent conservation across funnel stages.
type SelectionFunnel struct {
	InitialPool     int      `json:"initial_pool"`
	Stage1Survivors []string `json:"stage1_survivors"`
	RegimeRejected  []string `json:"regime_rejected"`
	SectorCapped    []string `json:"sector_capped"`
	RankLimited     []string `json:"rank_limited"`
	FinalSelected   []string `json:"final_selected"`
}

// Validate asserts that every Stage-1 survivor is strictly accounted for.
func (f SelectionFunnel) Validate() error {
	totalAccounted := len(f.RegimeRejected) + len(f.SectorCapped) + len(f.RankLimited) + len(f.FinalSelected)
	if totalAccounted != len(f.Stage1Survivors) {
		return fmt.Errorf("funnel conservation mismatch: %d accounted (RegimeRejected:%d + SectorCapped:%d + RankLimited:%d + FinalSelected:%d) vs %d Stage-1 survivors",
			totalAccounted, len(f.RegimeRejected), len(f.SectorCapped), len(f.RankLimited), len(f.FinalSelected), len(f.Stage1Survivors))
	}
	return nil
}

// BuildFunnel constructs and validates the selection funnel from tracker state.
func (t *Tracker) BuildFunnel() (SelectionFunnel, error) {
	var survivors []string
	for sym := range t.RawScores {
		survivors = append(survivors, sym)
	}

	var regimeRej, sectorCap, rankLim, finalSel []string
	for sym := range t.ScoreThresholdDrops {
		regimeRej = append(regimeRej, sym)
	}
	for sym := range t.SectorCapDrops {
		sectorCap = append(sectorCap, sym)
	}
	for sym := range t.HysteresisDrops {
		rankLim = append(rankLim, sym)
	}
	for sym := range t.SelectedReasons {
		finalSel = append(finalSel, sym)
	}

	f := SelectionFunnel{
		InitialPool:     t.InitialCount,
		Stage1Survivors: survivors,
		RegimeRejected:  regimeRej,
		SectorCapped:    sectorCap,
		RankLimited:     rankLim,
		FinalSelected:   finalSel,
	}
	return f, f.Validate()
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
	idx := strings.Index(s, startStr)
	if idx == -1 {
		return ""
	}
	sub := s[idx+len(startStr):]
	endIdx := strings.Index(sub, endStr)
	if endIdx == -1 {
		return strings.TrimSpace(sub)
	}
	return strings.TrimSpace(sub[:endIdx])
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

func formatValOrDelta(prevVal, currVal, unit string) string {
	if currVal == "" {
		return ""
	}
	if prevVal == "" || prevVal == currVal {
		return currVal + unit
	}
	return prevVal + unit + " -> " + currVal + unit
}

func formatDriverDelta(prevStr, currStr string) string {
	if currStr == "" || prevStr == "" || prevStr == currStr {
		return ""
	}
	p := parseDriverString(prevStr)
	c := parseDriverString(currStr)

	if c.isMulti && p.isMulti {
		if p.ttmGrowth == c.ttmGrowth && p.cagr3y == c.cagr3y && p.roce == c.roce && p.instStake == c.instStake {
			return ""
		}
		ttmStr := formatValOrDelta(p.ttmGrowth, c.ttmGrowth, "%")
		cagrStr := formatValOrDelta(p.cagr3y, c.cagr3y, "%")
		roceStr := formatValOrDelta(p.roce, c.roce, "%")
		instStr := formatValOrDelta(p.instStake, c.instStake, "%")
		return fmt.Sprintf("TTM Growth: %s (3Y: %s), ROCE: %s, Inst Stake: %s", ttmStr, cagrStr, roceStr, instStr)
	}

	if c.isValue && p.isValue {
		if p.forwardPE == c.forwardPE && p.fcfYield == c.fcfYield && p.instStake == c.instStake {
			return ""
		}
		peStr := formatValOrDelta(p.forwardPE, c.forwardPE, "")
		fcfStr := formatValOrDelta(p.fcfYield, c.fcfYield, "%")
		instStr := formatValOrDelta(p.instStake, c.instStake, "%")
		return fmt.Sprintf("Forward PE: %s, FCF Yield: %s, Inst Stake: %s", peStr, fcfStr, instStr)
	}

	return ""
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
							dIdx := strings.Index(reasonCol, "Drivers: ")
							if dIdx != -1 {
								prevDriversMap[ticker] = strings.TrimSpace(reasonCol[dIdx+len("Drivers: "):])
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
	scoreCutoffCount := len(t.ScoreThresholdDrops)
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
	writeLine("Initial pool size:                     %d constituents\n", t.InitialCount)
	writeLine("Passed Stage 1 Safety/Hard Filters:    %d stocks\n", passedSafetyCount)
	writeLine("Eliminated by Stage 1 Safety Filters:  %d stocks\n", safetyCount)
	if scoreCutoffCount > 0 {
		writeLine("Eliminated by Regime Score Cutoff:     %d stocks\n", scoreCutoffCount)
	}
	writeLine("Eliminated by Sector Caps:             %d stocks\n", sectorCapCount)
	writeLine("Eliminated by Rank Limits:             %d stocks\n", hysteresisCount)
	writeLine("Final Selected Stocks:                 %d stocks\n\n", selectedCount)

	// 1. Selected Stocks Section
	writeLine("===================================================================================================================================\n")
	writeLine("                                               SELECTED STOCKS\n")
	writeLine("===================================================================================================================================\n")
	if len(t.SelectedReasons) == 0 {
		writeLine("No stocks were selected.\n")
	} else {
		type selectedRow struct {
			ticker         string
			rawScore       float64
			effectiveScore float64
			rank           int
			reason         string
		}
		var sRows []selectedRow
		for ticker, reason := range t.SelectedReasons {
			rawS := t.RawScores[ticker]
			effS := t.EffectiveScores[ticker]
			if effS == 0 && rawS > 0 && t.RegimeMultiplier > 0 {
				effS = rawS * t.RegimeMultiplier
			}
			sRows = append(sRows, selectedRow{
				ticker:         ticker,
				rawScore:       rawS,
				effectiveScore: effS,
				rank:           t.RawRanks[ticker],
				reason:         reason,
			})
		}
		sort.Slice(sRows, func(i, j int) bool {
			return sRows[i].rank < sRows[j].rank
		})

		writeLine("%-16s | %-20s | %-9s | %-9s | %-8s | %-14s | %-21s | %s\n", "Ticker", "Sector", "Raw Score", "Eff Score", "Raw Rank", "Weight Decided", "Result Prev -> Coming", "Selection Reason")
		writeLine("-------------------------------------------------------------------------------------------------------------------------------------------------------------------\n")
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

			writeLine("%-16s | %-20s | %9.1f | %9.1f | %-8d | %-14s | %-21s | %s\n", r.ticker, sec, r.rawScore, r.effectiveScore, r.rank, weightStr, resDates, reasonStr)
		}
	}
	writeLine("\n")

	// 2. Removed Active Holdings (Exits) Section
	writeLine("=========================================================================================================\n")
	writeLine("                               REMOVED ACTIVE HOLDINGS (EXITS)\n")
	writeLine("=========================================================================================================\n")
	var removedRows []struct {
		ticker         string
		rawScore       float64
		effectiveScore float64
		rank           int
		reason         string
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

			rawScore := 0.0
			effScore := 0.0
			rank := 999
			if s, ok := t.RawScores[ticker]; ok {
				rawScore = s
			}
			if s, ok := t.EffectiveScores[ticker]; ok {
				effScore = s
			} else if rawScore > 0 && t.RegimeMultiplier > 0 {
				effScore = rawScore * t.RegimeMultiplier
			}
			if r, ok := t.RawRanks[ticker]; ok {
				rank = r
			}

			removedRows = append(removedRows, struct {
				ticker         string
				rawScore       float64
				effectiveScore float64
				rank           int
				reason         string
			}{ticker, rawScore, effScore, rank, reason})
		}
	}

	if len(removedRows) == 0 {
		writeLine("No existing holdings were removed.\n")
	} else {
		sort.Slice(removedRows, func(i, j int) bool {
			return removedRows[i].ticker < removedRows[j].ticker
		})
		writeLine("%-16s | %-20s | %-9s | %-9s | %-8s | %s\n", "Ticker", "Sector", "Raw Score", "Eff Score", "Raw Rank", "Exit Reason")
		writeLine("---------------------------------------------------------------------------------------------------------\n")
		for _, r := range removedRows {
			rankStr := fmt.Sprintf("%d", r.rank)
			if r.rank == 999 {
				rankStr = "N/A"
			}
			rawScoreStr := fmt.Sprintf("%9.1f", r.rawScore)
			effScoreStr := fmt.Sprintf("%9.1f", r.effectiveScore)
			if r.rank == 999 {
				rawScoreStr = "      N/A"
				effScoreStr = "      N/A"
			}
			sec := sectors[r.ticker]
			if sec == "" {
				sec = "Unknown"
			}
			writeLine("%-16s | %-20s | %s | %s | %-8s | %s\n", r.ticker, sec, rawScoreStr, effScoreStr, rankStr, r.reason)
		}
	}
	writeLine("\n")

	// 3. Rejected Candidates (Not in previous holdings, but failed caps or rankings)
	writeLine("=========================================================================================================\n")
	writeLine("                                  REJECTED NEW CANDIDATES\n")
	writeLine("=========================================================================================================\n")
	var rejectedCandidates []struct {
		ticker         string
		rawScore       float64
		effectiveScore float64
		rank           int
		reason         string
	}

	for ticker, reason := range t.SectorCapDrops {
		if _, ok := existingHoldings[ticker]; !ok {
			rawS := t.RawScores[ticker]
			effS := t.EffectiveScores[ticker]
			if effS == 0 && rawS > 0 && t.RegimeMultiplier > 0 {
				effS = rawS * t.RegimeMultiplier
			}
			rejectedCandidates = append(rejectedCandidates, struct {
				ticker         string
				rawScore       float64
				effectiveScore float64
				rank           int
				reason         string
			}{ticker, rawS, effS, t.RawRanks[ticker], reason})
		}
	}
	for ticker, reason := range t.ScoreThresholdDrops {
		if _, ok := existingHoldings[ticker]; !ok {
			rawS := t.RawScores[ticker]
			effS := t.EffectiveScores[ticker]
			if effS == 0 && rawS > 0 && t.RegimeMultiplier > 0 {
				effS = rawS * t.RegimeMultiplier
			}
			rejectedCandidates = append(rejectedCandidates, struct {
				ticker         string
				rawScore       float64
				effectiveScore float64
				rank           int
				reason         string
			}{ticker, rawS, effS, t.RawRanks[ticker], reason})
		}
	}
	for ticker, reason := range t.HysteresisDrops {
		if _, ok := existingHoldings[ticker]; !ok {
			rawS := t.RawScores[ticker]
			effS := t.EffectiveScores[ticker]
			if effS == 0 && rawS > 0 && t.RegimeMultiplier > 0 {
				effS = rawS * t.RegimeMultiplier
			}
			rejectedCandidates = append(rejectedCandidates, struct {
				ticker         string
				rawScore       float64
				effectiveScore float64
				rank           int
				reason         string
			}{ticker, rawS, effS, t.RawRanks[ticker], reason})
		}
	}

	if len(rejectedCandidates) == 0 {
		writeLine("No new candidates were rejected.\n")
	} else {
		sort.Slice(rejectedCandidates, func(i, j int) bool {
			return rejectedCandidates[i].rank < rejectedCandidates[j].rank
		})
		writeLine("%-16s | %-20s | %-9s | %-9s | %-8s | %s\n", "Ticker", "Sector", "Raw Score", "Eff Score", "Raw Rank", "Rejection Reason")
		writeLine("---------------------------------------------------------------------------------------------------------\n")
		for _, r := range rejectedCandidates {
			sec := sectors[r.ticker]
			if sec == "" {
				sec = "Unknown"
			}
			writeLine("%-16s | %-20s | %9.1f | %9.1f | %-8d | %s\n", r.ticker, sec, r.rawScore, r.effectiveScore, r.rank, r.reason)
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
