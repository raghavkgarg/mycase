package universe

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SnapshotDir is the directory where historical constituent CSV snapshots are saved.
const SnapshotDir = "data/universe_snapshots"

// SaveSnapshot saves a list of tickers for an index as of a specific date.
func SaveSnapshot(indexName string, date time.Time, tickers []string) error {
	if err := os.MkdirAll(SnapshotDir, 0755); err != nil {
		return fmt.Errorf("failed to create snapshot dir: %w", err)
	}

	dateStr := date.Format("20060102")
	cleanIndex := strings.ToLower(strings.TrimSpace(indexName))
	filePath := filepath.Join(SnapshotDir, fmt.Sprintf("%s_%s.csv", cleanIndex, dateStr))

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create snapshot file: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"Symbol"}); err != nil {
		return err
	}

	for _, t := range tickers {
		cleanSym := strings.TrimPrefix(t, "NSE:")
		if err := w.Write([]string{cleanSym}); err != nil {
			return err
		}
	}

	return nil
}

// GetConstituentsForDate finds the closest historical constituent snapshot on or before asOfDate.
// If no historical snapshot is found, it returns nil to indicate falling back to live/active constituents.
func GetConstituentsForDate(indexName string, asOfDate time.Time) ([]string, string, error) {
	if err := os.MkdirAll(SnapshotDir, 0755); err != nil {
		return nil, "", err
	}

	cleanIndex := strings.ToLower(strings.TrimSpace(indexName))
	entries, err := os.ReadDir(SnapshotDir)
	if err != nil {
		return nil, "", err
	}

	prefix := cleanIndex + "_"
	var matchedFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".csv") {
			matchedFiles = append(matchedFiles, name)
		}
	}

	if len(matchedFiles) == 0 {
		return nil, "", nil // No snapshots available, fallback to live
	}

	sort.Strings(matchedFiles)
	targetDateStr := asOfDate.Format("20060102")
	bestFile := ""

	for _, file := range matchedFiles {
		// Extract date portion: index_YYYYMMDD.csv
		parts := strings.TrimSuffix(strings.TrimPrefix(file, prefix), ".csv")
		if parts <= targetDateStr {
			bestFile = file
		} else {
			break
		}
	}

	if bestFile == "" {
		// Earliest snapshot is after asOfDate, use the earliest available
		bestFile = matchedFiles[0]
	}

	fullPath := filepath.Join(SnapshotDir, bestFile)
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open snapshot %s: %w", bestFile, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return nil, "", fmt.Errorf("failed to read snapshot %s: %w", bestFile, err)
	}

	var tickers []string
	for i, row := range records {
		if i == 0 || len(row) == 0 {
			continue
		}
		sym := strings.TrimSpace(row[0])
		if sym != "" {
			if !strings.HasPrefix(sym, "NSE:") {
				sym = "NSE:" + sym
			}
			tickers = append(tickers, sym)
		}
	}

	return tickers, bestFile, nil
}
