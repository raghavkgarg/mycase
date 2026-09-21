package csvloader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPrintComparisonReport_DBSourcedRanks verifies the comparison report builds
// its rank/score rationale from the caller-supplied RankScore maps (sourced from
// the DuckDB selections history) rather than re-parsing any .txt report, and that
// it classifies additions/removals/weight changes correctly.
func TestPrintComparisonReport_DBSourcedRanks(t *testing.T) {
	dir := t.TempDir()
	golden := filepath.Join(dir, "acme.csv")   // dst / previous
	candidate := filepath.Join(dir, "new.csv") // src / current

	// Golden (previous) portfolio: AAA held, CCC held (will be removed).
	if err := os.WriteFile(golden, []byte("ticker,weight\nAAA,0.30\nCCC,0.20\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// New candidate set: AAA increased, BBB new, CCC dropped.
	if err := os.WriteFile(candidate, []byte("ticker,weight\nAAA,0.50\nBBB,0.25\n"), 0644); err != nil {
		t.Fatal(err)
	}

	prevRanks := map[string]RankScore{
		"AAA": {Rank: 3, Score: 71.5},
		"CCC": {Rank: 8, Score: 55.0},
	}
	currRanks := map[string]RankScore{
		"AAA": {Rank: 1, Score: 82.0},
		"BBB": {Rank: 2, Score: 78.3},
	}

	// PrintComparisonReport writes the report under report/<name>_<strategy>/
	// executions relative to CWD; run it inside the temp dir.
	origWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	PrintComparisonReport(candidate, golden, "quality", prevRanks, currRanks)

	// GetUniverseName("acme.csv") == "acme"; report dir is acme_quality.
	matches, err := filepath.Glob(filepath.Join("report", "acme_quality", "executions", "*_02_comparison.txt"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("expected a comparison report file, glob err=%v matches=%v", err, matches)
	}
	body, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	out := string(body)

	wantContains := []string{
		"Increased weight (Rank #3 -> #1, Score 71.5 -> 82.0)", // AAA, rank+score from the maps
		"New Addition (Rank #2, Score 78.3)",                   // BBB, current rank/score
		"Remove Action (Prev Rank #8)",                         // CCC, previous rank
		"1 New Additions, 1 Removals, 1 Increased",             // summary counts
	}
	for _, w := range wantContains {
		if !strings.Contains(out, w) {
			t.Errorf("comparison report missing %q\n---\n%s", w, out)
		}
	}
}

// TestPrintComparisonReport_NilRanksDegrade verifies that with no rank/score data
// (e.g. first run, nil maps) the report still renders, degrading to action
// strings without the rank rationale rather than panicking.
func TestPrintComparisonReport_NilRanksDegrade(t *testing.T) {
	dir := t.TempDir()
	golden := filepath.Join(dir, "acme.csv")
	candidate := filepath.Join(dir, "new.csv")
	if err := os.WriteFile(golden, []byte("ticker,weight\nAAA,0.30\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, []byte("ticker,weight\nBBB,0.25\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	PrintComparisonReport(candidate, golden, "quality", nil, nil)

	matches, _ := filepath.Glob(filepath.Join("report", "acme_quality", "executions", "*_02_comparison.txt"))
	if len(matches) == 0 {
		t.Fatal("expected a comparison report file even with nil rank maps")
	}
	body, _ := os.ReadFile(matches[0])
	out := string(body)
	// Bare action strings (no "(Rank ...)" suffix) when maps are nil.
	if !strings.Contains(out, "New Addition") || strings.Contains(out, "New Addition (Rank") {
		t.Errorf("expected bare 'New Addition' with nil ranks, got:\n%s", out)
	}
	if !strings.Contains(out, "Remove Action") || strings.Contains(out, "Remove Action (Prev") {
		t.Errorf("expected bare 'Remove Action' with nil ranks, got:\n%s", out)
	}
}
