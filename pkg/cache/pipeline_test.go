package cache

import (
	"context"
	"testing"
	"time"
)

func insertTestRun(t *testing.T, c *Cache, runID string) {
	t.Helper()
	err := c.InsertRun(context.Background(), PipelineRun{
		RunID:     runID,
		StartedAt: time.Now(),
		Status:    RunStatusRunning,
		Portfolio: "us_sp500",
		Method:    "us_quality_momentum",
	})
	if err != nil {
		t.Fatalf("insertTestRun(%s): %v", runID, err)
	}
}

// ---------------------------------------------------------------------------
// IndexPicks
// ---------------------------------------------------------------------------

func TestInsertIndexPicks_RoundTrip(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()
	insertTestRun(t, c, "run_test_picks")

	picks := []IndexPick{
		{IndexName: "sp500", Ticker: "AAPL", Score: 85.2, Rank: 1, Weight: 0.08, Sector: "Technology"},
		{IndexName: "sp500", Ticker: "MSFT", Score: 82.1, Rank: 2, Weight: 0.07, Sector: "Technology"},
		{IndexName: "sp500", Ticker: "JNJ", Score: 78.5, Rank: 3, Weight: 0.06, Sector: "Healthcare"},
	}
	if err := c.InsertIndexPicks(ctx, "run_test_picks", "sp500", picks); err != nil {
		t.Fatalf("InsertIndexPicks: %v", err)
	}

	got, err := c.GetIndexPicks(ctx, "run_test_picks", "sp500")
	if err != nil {
		t.Fatalf("GetIndexPicks: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 picks, got %d", len(got))
	}
	// Ordered by rank.
	if got[0].Ticker != "AAPL" || got[1].Ticker != "MSFT" || got[2].Ticker != "JNJ" {
		t.Errorf("unexpected order: %v", got)
	}
	if got[0].Score != 85.2 {
		t.Errorf("Score: got %v, want 85.2", got[0].Score)
	}
	if got[0].Sector != "Technology" {
		t.Errorf("Sector: got %q, want Technology", got[0].Sector)
	}
}

func TestInsertIndexPicks_Empty(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()

	// Should not error on empty slice.
	if err := c.InsertIndexPicks(ctx, "run_empty", "sp500", nil); err != nil {
		t.Fatalf("InsertIndexPicks(nil): %v", err)
	}
}

func TestInsertIndexPicks_Upsert(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()
	insertTestRun(t, c, "run_upsert_picks")

	original := []IndexPick{
		{IndexName: "sp500", Ticker: "AAPL", Score: 80.0, Rank: 1, Weight: 0.05, Sector: "Technology"},
	}
	if err := c.InsertIndexPicks(ctx, "run_upsert_picks", "sp500", original); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	updated := []IndexPick{
		{IndexName: "sp500", Ticker: "AAPL", Score: 90.0, Rank: 1, Weight: 0.09, Sector: "Technology"},
	}
	if err := c.InsertIndexPicks(ctx, "run_upsert_picks", "sp500", updated); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := c.GetIndexPicks(ctx, "run_upsert_picks", "sp500")
	if err != nil {
		t.Fatalf("GetIndexPicks: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 pick after upsert, got %d", len(got))
	}
	if got[0].Score != 90.0 {
		t.Errorf("Score not updated: got %v, want 90.0", got[0].Score)
	}
}

func TestGetAllIndexPicks_MultiIndex(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()
	insertTestRun(t, c, "run_multi_idx")

	sp500 := []IndexPick{
		{IndexName: "sp500", Ticker: "AAPL", Score: 85.0, Rank: 1, Weight: 0.08, Sector: "Technology"},
	}
	midcap := []IndexPick{
		{IndexName: "midcap400", Ticker: "WSM", Score: 72.0, Rank: 1, Weight: 0.05, Sector: "Consumer"},
	}
	if err := c.InsertIndexPicks(ctx, "run_multi_idx", "sp500", sp500); err != nil {
		t.Fatalf("InsertIndexPicks sp500: %v", err)
	}
	if err := c.InsertIndexPicks(ctx, "run_multi_idx", "midcap400", midcap); err != nil {
		t.Fatalf("InsertIndexPicks midcap400: %v", err)
	}

	got, err := c.GetAllIndexPicks(ctx, "run_multi_idx")
	if err != nil {
		t.Fatalf("GetAllIndexPicks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 picks across indices, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Proposals
// ---------------------------------------------------------------------------

func TestInsertProposals_RoundTrip(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()
	insertTestRun(t, c, "run_test_proposals")

	proposals := []Proposal{
		{Ticker: "AAPL", Weight: 0.08, Score: 85.2, Rank: 1, Sector: "Technology"},
		{Ticker: "MSFT", Weight: 0.07, Score: 82.1, Rank: 2, Sector: "Technology"},
		{Ticker: "JNJ", Weight: 0.06, Score: 78.5, Rank: 3, Sector: "Healthcare"},
	}
	if err := c.InsertProposals(ctx, "run_test_proposals", "draft", proposals); err != nil {
		t.Fatalf("InsertProposals: %v", err)
	}

	got, err := c.GetProposals(ctx, "run_test_proposals", "draft")
	if err != nil {
		t.Fatalf("GetProposals: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 proposals, got %d", len(got))
	}
	if got[0].Ticker != "AAPL" {
		t.Errorf("first proposal: got %q, want AAPL", got[0].Ticker)
	}
	if got[0].Weight != 0.08 {
		t.Errorf("Weight: got %v, want 0.08", got[0].Weight)
	}
}

func TestInsertProposals_MultiStage(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()
	insertTestRun(t, c, "run_stages")

	draft := []Proposal{
		{Ticker: "AAPL", Weight: 0.05, Score: 85.0, Rank: 1, Sector: "Technology"},
		{Ticker: "GOOG", Weight: 0.04, Score: 80.0, Rank: 2, Sector: "Technology"},
	}
	optimized := []Proposal{
		{Ticker: "AAPL", Weight: 0.08, Score: 85.0, Rank: 1, Sector: "Technology"},
	}
	if err := c.InsertProposals(ctx, "run_stages", "draft", draft); err != nil {
		t.Fatalf("InsertProposals draft: %v", err)
	}
	if err := c.InsertProposals(ctx, "run_stages", "optimized", optimized); err != nil {
		t.Fatalf("InsertProposals optimized: %v", err)
	}

	gotDraft, _ := c.GetProposals(ctx, "run_stages", "draft")
	gotOpt, _ := c.GetProposals(ctx, "run_stages", "optimized")

	if len(gotDraft) != 2 {
		t.Errorf("draft: expected 2, got %d", len(gotDraft))
	}
	if len(gotOpt) != 1 {
		t.Errorf("optimized: expected 1, got %d", len(gotOpt))
	}
}

func TestInsertProposals_Empty(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()

	if err := c.InsertProposals(ctx, "run_empty", "draft", nil); err != nil {
		t.Fatalf("InsertProposals(nil): %v", err)
	}
}

// ---------------------------------------------------------------------------
// DeleteRunData
// ---------------------------------------------------------------------------

func TestDeleteRunData(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()
	insertTestRun(t, c, "run_to_delete")

	// Add data across all tables.
	c.InsertIndexPicks(ctx, "run_to_delete", "sp500", []IndexPick{
		{IndexName: "sp500", Ticker: "AAPL", Score: 85.0, Rank: 1, Weight: 0.08, Sector: "Tech"},
	})
	c.InsertProposals(ctx, "run_to_delete", "draft", []Proposal{
		{Ticker: "AAPL", Weight: 0.08, Score: 85.0, Rank: 1, Sector: "Tech"},
	})

	if err := c.DeleteRunData(ctx, "run_to_delete"); err != nil {
		t.Fatalf("DeleteRunData: %v", err)
	}

	// Everything should be gone.
	for _, q := range []string{
		`SELECT COUNT(*) FROM pipeline_runs WHERE run_id = 'run_to_delete'`,
		`SELECT COUNT(*) FROM index_picks WHERE run_id = 'run_to_delete'`,
		`SELECT COUNT(*) FROM proposals WHERE run_id = 'run_to_delete'`,
	} {
		var n int
		c.db.QueryRowContext(ctx, q).Scan(&n)
		if n != 0 {
			t.Errorf("DeleteRunData left %d rows: %s", n, q)
		}
	}
}

func TestDeleteRunData_DoesNotAffectOtherRuns(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()
	insertTestRun(t, c, "run_keep")
	insertTestRun(t, c, "run_remove")

	c.InsertIndexPicks(ctx, "run_keep", "sp500", []IndexPick{
		{IndexName: "sp500", Ticker: "MSFT", Score: 80.0, Rank: 1, Weight: 0.07, Sector: "Tech"},
	})
	c.InsertIndexPicks(ctx, "run_remove", "sp500", []IndexPick{
		{IndexName: "sp500", Ticker: "AAPL", Score: 85.0, Rank: 1, Weight: 0.08, Sector: "Tech"},
	})

	if err := c.DeleteRunData(ctx, "run_remove"); err != nil {
		t.Fatalf("DeleteRunData: %v", err)
	}

	// run_keep data should be intact.
	got, err := c.GetIndexPicks(ctx, "run_keep", "sp500")
	if err != nil {
		t.Fatalf("GetIndexPicks: %v", err)
	}
	if len(got) != 1 || got[0].Ticker != "MSFT" {
		t.Errorf("run_keep data was affected: %v", got)
	}
}
