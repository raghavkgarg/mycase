package stockpicker

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/csvloader"
)

// RecentExitInfo holds information about a recently exited holding.
type RecentExitInfo struct {
	Ticker   string
	ExitDate time.Time
	Reason   string
}

// LoadRecentExits scans portfolio backups and selection reports to identify tickers exited within cutoffDays.
func LoadRecentExits(goldenBase string, existingHoldings map[string]float64, cutoffDays int, asOf time.Time) map[string]time.Time {
	if asOf.IsZero() {
		asOf = time.Now()
	}
	if cutoffDays <= 0 {
		cutoffDays = 30
	}
	cutoff := asOf.AddDate(0, 0, -cutoffDays)
	recentExits := make(map[string]time.Time)

	// 1. Scan execution reports for explicit exits: report/<goldenBase>_*/executions/*_01_selection_reasons.txt
	reportMatches, err := filepath.Glob(filepath.Join("report", goldenBase+"_*", "executions", "*_01_selection_reasons.txt"))
	if err == nil {
		for _, rPath := range reportMatches {
			baseName := filepath.Base(rPath)
			// expected format: YYYYMMDD_01_selection_reasons.txt
			parts := strings.Split(baseName, "_")
			if len(parts) > 0 && len(parts[0]) == 8 {
				reportDate, pErr := time.Parse("20060102", parts[0])
				if pErr == nil && !reportDate.Before(cutoff) && !reportDate.After(asOf) {
					extractExitsFromReport(rPath, reportDate, recentExits)
				}
			}
		}
	}

	// 2. Scan backups: data/backups/<goldenBase>/bk_YYYYMMDD_HHMMSS.csv
	backupDir := filepath.Join("data", "backups", goldenBase)
	bkMatches, err := filepath.Glob(filepath.Join(backupDir, "bk_*.csv"))
	if err == nil && len(bkMatches) > 0 {
		type bkFile struct {
			path string
			date time.Time
		}
		var validBackups []bkFile
		for _, bPath := range bkMatches {
			bName := filepath.Base(bPath)
			// format bk_YYYYMMDD_HHMMSS.csv
			trimmed := strings.TrimPrefix(bName, "bk_")
			parts := strings.Split(trimmed, "_")
			if len(parts) > 0 && len(parts[0]) == 8 {
				bDate, pErr := time.Parse("20060102", parts[0])
				if pErr == nil && !bDate.Before(cutoff) && !bDate.After(asOf) {
					validBackups = append(validBackups, bkFile{path: bPath, date: bDate})
				}
			}
		}
		// Sort backups ascending by date
		sort.Slice(validBackups, func(i, j int) bool {
			return validBackups[i].date.Before(validBackups[j].date)
		})

		for _, b := range validBackups {
			_, tickers, lErr := csvloader.LoadBasketCSV(b.path)
			if lErr == nil {
				for _, t := range tickers {
					// If the ticker was in a backup within cutoffDays, but is NOT in current holdings
					if _, isCurrent := existingHoldings[t]; !isCurrent {
						// Record exit date if not already recorded or if this backup is more recent
						if prevDate, exists := recentExits[t]; !exists || b.date.After(prevDate) {
							recentExits[t] = b.date
						}
					}
				}
			}
		}
	}

	return recentExits
}

// extractExitsFromReport parses the REMOVED ACTIVE HOLDINGS section in selection reasons reports.
func extractExitsFromReport(path string, reportDate time.Time, exits map[string]time.Time) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	inExitsSection := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, "REMOVED ACTIVE HOLDINGS (EXITS)") {
			inExitsSection = true
			continue
		}
		if inExitsSection {
			// End of section header or separator
			if strings.Contains(line, "REJECTED NEW CANDIDATES") || strings.Contains(line, "SAFETY & FUNDAMENTAL ELIMINATIONS") {
				break
			}
			if strings.HasPrefix(line, "NSE:") || strings.HasPrefix(line, "BSE:") || strings.HasPrefix(line, "US:") {
				parts := strings.Split(line, "|")
				if len(parts) > 0 {
					ticker := strings.TrimSpace(parts[0])
					if prev, ok := exits[ticker]; !ok || reportDate.After(prev) {
						exits[ticker] = reportDate
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return
	}
}

// IsOnCooldown checks if a candidate ticker is in the cooldown window and whether it qualifies for a bypass.
func IsOnCooldown(ticker string, recentExits map[string]time.Time, rank int, bypassRank int, asOf time.Time, cooldownDays int) (bool, string) {
	if recentExits == nil {
		return false, ""
	}
	exitDate, exited := recentExits[ticker]
	if !exited {
		return false, ""
	}

	if asOf.IsZero() {
		asOf = time.Now()
	}
	if cooldownDays <= 0 {
		cooldownDays = 30
	}
	if bypassRank <= 0 {
		bypassRank = 5
	}

	daysSinceExit := int(asOf.Sub(exitDate).Hours() / 24)
	if daysSinceExit < 0 {
		daysSinceExit = 0
	}

	if daysSinceExit <= cooldownDays {
		if rank <= bypassRank {
			// Exceptional turnaround: bypasses cooldown
			return false, ""
		}
		return true, fmt.Sprintf("Exited on %s (%dd ago <= %dd cooldown window; Rank %d > %d bypass threshold)",
			exitDate.Format("2006-01-02"), daysSinceExit, cooldownDays, rank, bypassRank)
	}

	return false, ""
}
