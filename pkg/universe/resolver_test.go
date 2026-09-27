package universe

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGetConstituentsForDate_NoMatch(t *testing.T) {
	// A nonexistent index should cleanly return nil tickers, empty file, and nil error
	tickers, file, err := GetConstituentsForDate("nonexistent_index_xyz_123", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tickers != nil || file != "" {
		t.Errorf("expected nil tickers and empty file, got tickers=%v, file=%q", tickers, file)
	}
}

func TestSaveAndGetConstituents(t *testing.T) {
	testIndex := "test_unit_index"
	testDate := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)
	expectedFile := fmt.Sprintf("%s_20260115.csv", testIndex)
	expectedPath := filepath.Join(SnapshotDir, expectedFile)

	defer func() {
		_ = os.Remove(expectedPath)
	}()

	sampleTickers := []string{"NSE:RELIANCE", "TCS", "INFY"}
	if err := SaveSnapshot(testIndex, testDate, sampleTickers); err != nil {
		t.Fatalf("SaveSnapshot failed: %v", err)
	}

	// Verify the file was created
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Fatalf("expected snapshot file %s to exist", expectedPath)
	}

	// Exact date match
	retrieved, file, err := GetConstituentsForDate(testIndex, testDate)
	if err != nil {
		t.Fatalf("GetConstituentsForDate failed: %v", err)
	}
	if file != expectedFile {
		t.Errorf("expected file %q, got %q", expectedFile, file)
	}
	if len(retrieved) != 3 {
		t.Fatalf("expected 3 tickers, got %d", len(retrieved))
	}
	// All should have NSE: prefix
	if retrieved[0] != "NSE:RELIANCE" || retrieved[1] != "NSE:TCS" || retrieved[2] != "NSE:INFY" {
		t.Errorf("tickers mismatch: %v", retrieved)
	}

	// Later asOfDate should find the earlier snapshot
	laterDate := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	retrievedLater, fileLater, err := GetConstituentsForDate(testIndex, laterDate)
	if err != nil {
		t.Fatalf("GetConstituentsForDate later failed: %v", err)
	}
	if fileLater != expectedFile {
		t.Errorf("expected file %q, got %q", expectedFile, fileLater)
	}
	if len(retrievedLater) != 3 {
		t.Errorf("expected 3 tickers, got %d", len(retrievedLater))
	}
}
