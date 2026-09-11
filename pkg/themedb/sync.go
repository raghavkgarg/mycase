package themedb

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SyncThemeOptions holds inputs for syncing or backfilling theme history.
type SyncThemeOptions struct {
	ThemeName     string
	GoldenCSVPath string
	ProposalsDir  string
	Keyword       string
	AccountID     string
}

type dateProposal struct {
	dateStr   string
	date      time.Time
	filePath  string
	positions map[string]float64
}

// CleanTicker strips exchange prefixes (e.g. "NSE:") and series suffixes.
func CleanTicker(ticker string) string {
	t := strings.TrimSpace(ticker)
	if idx := strings.Index(t, ":"); idx != -1 {
		t = t[idx+1:]
	}
	t = strings.TrimSuffix(t, "-BE")
	t = strings.TrimSuffix(t, "-SM")
	return strings.ToUpper(strings.TrimSpace(t))
}

// loadCSVPositions reads ticker -> weight from a CSV file.
func loadCSVPositions(path string) (map[string]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("csv %s has insufficient rows", path)
	}

	tickerCol := -1
	weightCol := -1
	for i, h := range records[0] {
		lower := strings.ToLower(strings.TrimSpace(h))
		if lower == "ticker" || lower == "symbol" {
			tickerCol = i
		} else if lower == "weight" {
			weightCol = i
		}
	}
	if tickerCol == -1 {
		tickerCol = 0
	}

	res := make(map[string]float64)
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) <= tickerCol {
			continue
		}
		sym := CleanTicker(row[tickerCol])
		if sym == "" {
			continue
		}
		weight := 0.0
		if weightCol != -1 && len(row) > weightCol {
			if w, err := strconv.ParseFloat(strings.TrimSpace(row[weightCol]), 64); err == nil {
				weight = w
			}
		}
		res[sym] = weight
	}
	return res, nil
}

// SyncThemeFromProposals scans proposal files and current golden copy to construct
// sequential rebalance versions (v1, v2, ... vN) and writes them to theme_rebalances and theme_history.
func (d *DB) SyncThemeFromProposals(ctx context.Context, opts SyncThemeOptions) error {
	if opts.ThemeName == "" {
		return fmt.Errorf("theme name required")
	}
	if opts.ProposalsDir == "" {
		opts.ProposalsDir = "data/candidates/proposals"
	}
	if opts.Keyword == "" {
		opts.Keyword = strings.ToLower(opts.ThemeName)
	}

	// 1. Discover all proposal files matching the theme keyword
	entries, err := os.ReadDir(opts.ProposalsDir)
	if err != nil {
		return fmt.Errorf("reading proposals dir: %w", err)
	}

	// Map date string (YYYYMMDD) -> selected file (preferring _optim.csv)
	dateFiles := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".csv") {
			continue
		}
		nameLower := strings.ToLower(e.Name())
		if !strings.Contains(nameLower, opts.Keyword) {
			continue
		}

		parts := strings.Split(e.Name(), "_")
		if len(parts) < 2 {
			continue
		}
		dateStr := parts[0]
		if len(dateStr) != 8 {
			continue
		}
		if _, err := time.Parse("20060102", dateStr); err != nil {
			continue
		}

		existing, ok := dateFiles[dateStr]
		isOptim := strings.Contains(nameLower, "_optim.csv")
		if !ok || isOptim || !strings.Contains(strings.ToLower(existing), "_optim.csv") {
			dateFiles[dateStr] = filepath.Join(opts.ProposalsDir, e.Name())
		}
	}

	var dates []string
	for d := range dateFiles {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	if len(dates) == 0 && opts.GoldenCSVPath == "" {
		return fmt.Errorf("no proposal or golden copy files found for theme %s", opts.ThemeName)
	}

	// 2. Load proposal checkpoints chronologically
	var checkpoints []dateProposal
	for _, dStr := range dates {
		fPath := dateFiles[dStr]
		positions, err := loadCSVPositions(fPath)
		if err != nil {
			continue
		}
		t, _ := time.Parse("20060102", dStr)
		checkpoints = append(checkpoints, dateProposal{
			dateStr:   dStr,
			date:      t,
			filePath:  fPath,
			positions: positions,
		})
	}

	// 3. If Golden copy exists, append it as the final current checkpoint
	if opts.GoldenCSVPath != "" {
		if gPositions, err := loadCSVPositions(opts.GoldenCSVPath); err == nil && len(gPositions) > 0 {
			now := time.Now()
			dateStr := now.Format("20060102")
			checkpoints = append(checkpoints, dateProposal{
				dateStr:   dateStr,
				date:      now,
				filePath:  opts.GoldenCSVPath,
				positions: gPositions,
			})
		}
	}

	if len(checkpoints) == 0 {
		return fmt.Errorf("no valid positions found to sync for %s", opts.ThemeName)
	}

	// Filter checkpoints so we only record rebalances when constituents/weights actually change
	type cleanStep struct {
		date      time.Time
		positions map[string]float64
		notes     string
	}
	var steps []cleanStep

	for i, cp := range checkpoints {
		if i == 0 {
			steps = append(steps, cleanStep{
				date:      cp.date,
				positions: cp.positions,
				notes:     fmt.Sprintf("Inception checkpoint (%s)", cp.dateStr),
			})
			continue
		}

		prev := steps[len(steps)-1].positions
		curr := cp.positions

		// Check if different from prev
		isDiff := false
		if len(prev) != len(curr) {
			isDiff = true
		} else {
			for s, w := range curr {
				if prevW, ok := prev[s]; !ok || prevW != w {
					isDiff = true
					break
				}
			}
		}

		if isDiff {
			steps = append(steps, cleanStep{
				date:      cp.date,
				positions: cp.positions,
				notes:     fmt.Sprintf("Rebalance checkpoint (%s)", cp.dateStr),
			})
		}
	}

	// 4. Record sequential versions into DuckDB
	prevPositions := make(map[string]float64)

	for versionIdx, step := range steps {
		version := versionIdx + 1

		var items []ThemeHistoryItem
		currPositions := step.positions

		// Track all symbols present in previous or current
		allSymbols := make(map[string]bool)
		for s := range prevPositions {
			allSymbols[s] = true
		}
		for s := range currPositions {
			allSymbols[s] = true
		}

		turnover := 0.0

		for s := range allSymbols {
			pW := prevPositions[s]
			cW := currPositions[s]

			action := "UNCHANGED"
			if pW <= 0 && cW > 0 {
				action = "NEW_ENTRY"
				turnover += cW
			} else if pW > 0 && cW <= 0 {
				action = "EXITED"
				turnover += pW
			} else if pW > 0 && cW > 0 && pW != cW {
				action = "REWEIGHT"
				if cW > pW {
					turnover += (cW - pW)
				}
			} else if pW <= 0 && cW <= 0 {
				// Symbol was exited in earlier step and remains 0.0
				continue
			}

			items = append(items, ThemeHistoryItem{
				ThemeName:       opts.ThemeName,
				Version:         version,
				Symbol:          s,
				Action:          action,
				TargetWeight:    cW,
				PrevWeight:      pW,
				ExecutionStatus: "FILLED",
			})
		}

		rebalance := ThemeRebalance{
			ThemeName:     opts.ThemeName,
			Version:       version,
			EffectiveDate: step.date,
			CreatedAt:     step.date,
			Status:        "COMMITTED",
			TurnoverPct:   turnover * 100.0,
			Notes:         step.notes,
		}

		if err := d.RecordRebalance(ctx, rebalance, items); err != nil {
			return fmt.Errorf("recording version %d: %w", version, err)
		}

		// Update prevPositions for next version
		prevPositions = make(map[string]float64)
		for s, w := range currPositions {
			if w > 0 {
				prevPositions[s] = w
			}
		}
	}

	return nil
}
