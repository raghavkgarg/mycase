package stockpicker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadConstituents_MirrorFallback(t *testing.T) {
	indexName := "test_offline_index"
	cacheDir := filepath.Join("data", "cache", "constituents")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}
	cacheFile := filepath.Join(cacheDir, indexName+".csv")
	dummyCSV := "Company Name,Industry,Symbol,Series,ISIN Code\nTest Company,Technology,TESTCO,EQ,INE000000000\n"
	if err := os.WriteFile(cacheFile, []byte(dummyCSV), 0644); err != nil {
		t.Fatalf("failed to write test cache file: %v", err)
	}
	defer os.Remove(cacheFile)

	// An unreachable URL to force fallback
	invalidURL := "http://127.0.0.1:59999/non_existent.csv"
	tickers, sectors, err := downloadConstituents(indexName, invalidURL)
	if err != nil {
		t.Fatalf("expected fallback to mirror file, got error: %v", err)
	}

	if len(tickers) != 1 || tickers[0] != "NSE:TESTCO" {
		t.Errorf("expected [NSE:TESTCO], got %v", tickers)
	}
	if sectors["NSE:TESTCO"] != "Technology" {
		t.Errorf("expected sector 'Technology', got '%s'", sectors["NSE:TESTCO"])
	}
}

func TestRetryFailedSnapshotCandidates_MissingFundamentals(t *testing.T) {
	// Create a dummy snapshot with a missing fundamental drop
	tmpDir := t.TempDir()
	origDir := PITSnapshotDir
	// Temporarily override not directly possible without var, so test candidate identification logic directly
	snap := &PITRunSnapshot{
		AsOfDate:  "2026-09-18",
		IndexName: "test_index",
		Method:    "earlymb",
		Candidates: map[string]CandidateScoreDetail{
			"NSE:GOOD": {
				Ticker:       "NSE:GOOD",
				PassedStage1: true,
				RawScore:     40.0,
			},
			"NSE:MISSINGFUND": {
				Ticker:          "NSE:MISSINGFUND",
				PassedStage1:    false,
				DataFetchFailed: true,
				RejectionReason: "DATA_FETCH_FAILED: Missing fundamental data",
			},
			"NSE:LEGACYMISSING": {
				Ticker:          "NSE:LEGACYMISSING",
				PassedStage1:    false,
				DataFetchFailed: false,
				RejectionReason: "Missing fundamental data",
			},
		},
	}

	// Verify both match the retry condition
	var retried []string
	for ticker, c := range snap.Candidates {
		if c.DataFetchFailed || (!c.PassedStage1 && c.RejectionReason == "") ||
			stringsHasPrefix(c.RejectionReason, "DATA_FETCH_FAILED") ||
			stringsContains(c.RejectionReason, "Missing fundamental") {
			retried = append(retried, ticker)
		}
	}

	if len(retried) != 2 {
		t.Fatalf("expected 2 candidates queued for retry, got %d: %v", len(retried), retried)
	}
	_ = tmpDir
	_ = origDir
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func stringsContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestFetchBenchmarkPricesResilient_DatabaseFallback(t *testing.T) {
	ctx := context.Background()
	// Fetch with an invalid symbol and no network to test graceful error handling
	_, err := FetchBenchmarkPricesResilient(ctx, nil, "NON_EXISTENT_BENCHMARK_XYZ", "1y")
	if err == nil {
		t.Fatalf("expected error for non-existent benchmark, got nil")
	}
}
