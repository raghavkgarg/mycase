package cache

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewRunID_Format(t *testing.T) {
	id := NewRunID()
	if !strings.HasPrefix(id, "run_") {
		t.Errorf("NewRunID() = %q, want prefix 'run_'", id)
	}
	// Format: run_YYYYMMDD_HHMMSS → 19 chars
	if len(id) != 19 {
		t.Errorf("NewRunID() = %q, length %d, want 19", id, len(id))
	}
}

func TestInsertRun_AndGetRun(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()

	run := PipelineRun{
		RunID:      "run_20260826_100000",
		StartedAt:  time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC),
		Status:     RunStatusRunning,
		Portfolio:  "us_sp500",
		Method:     "us_quality_momentum",
		ConfigJSON: `{"top":20}`,
	}
	if err := c.InsertRun(ctx, run); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	got, err := c.GetRun(ctx, "run_20260826_100000")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.RunID != run.RunID {
		t.Errorf("RunID: got %q, want %q", got.RunID, run.RunID)
	}
	if got.Status != RunStatusRunning {
		t.Errorf("Status: got %q, want %q", got.Status, RunStatusRunning)
	}
	if got.Portfolio != "us_sp500" {
		t.Errorf("Portfolio: got %q, want %q", got.Portfolio, "us_sp500")
	}
	if got.Method != "us_quality_momentum" {
		t.Errorf("Method: got %q, want %q", got.Method, "us_quality_momentum")
	}
	if got.ConfigJSON != `{"top":20}` {
		t.Errorf("ConfigJSON: got %q, want %q", got.ConfigJSON, `{"top":20}`)
	}
	if got.CompletedAt.Unix() > 0 {
		t.Errorf("CompletedAt should be zero, got %v", got.CompletedAt)
	}
}

func TestGetRun_NotFound(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()

	_, err := c.GetRun(ctx, "run_nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent run")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestCompleteRun(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()

	run := PipelineRun{
		RunID:     "run_20260826_110000",
		StartedAt: time.Now(),
		Status:    RunStatusRunning,
		Portfolio: "microsmall",
		Method:    "multibagger",
	}
	if err := c.InsertRun(ctx, run); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	if err := c.CompleteRun(ctx, run.RunID); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}

	got, err := c.GetRun(ctx, run.RunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != RunStatusCompleted {
		t.Errorf("Status: got %q, want %q", got.Status, RunStatusCompleted)
	}
	if got.CompletedAt.IsZero() {
		t.Error("CompletedAt should be non-zero after CompleteRun")
	}
}

func TestFailRun(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()

	run := PipelineRun{
		RunID:     "run_20260826_120000",
		StartedAt: time.Now(),
		Status:    RunStatusRunning,
		Portfolio: "us_sp500",
		Method:    "us_quality_momentum",
	}
	if err := c.InsertRun(ctx, run); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	if err := c.FailRun(ctx, run.RunID); err != nil {
		t.Fatalf("FailRun: %v", err)
	}

	got, err := c.GetRun(ctx, run.RunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != RunStatusFailed {
		t.Errorf("Status: got %q, want %q", got.Status, RunStatusFailed)
	}
}

func TestCompleteRun_NotFound(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()

	err := c.CompleteRun(ctx, "run_ghost")
	if err == nil {
		t.Fatal("expected error for nonexistent run")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestListRuns_OrderAndLimit(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()

	// Insert 3 runs with different times.
	for i, id := range []string{"run_20260826_010000", "run_20260826_020000", "run_20260826_030000"} {
		run := PipelineRun{
			RunID:     id,
			StartedAt: time.Date(2026, 8, 26, i+1, 0, 0, 0, time.UTC),
			Status:    RunStatusRunning,
			Portfolio: "us_sp500",
			Method:    "us_quality_momentum",
		}
		if err := c.InsertRun(ctx, run); err != nil {
			t.Fatalf("InsertRun %s: %v", id, err)
		}
	}

	// All runs.
	all, err := c.ListRuns(ctx, 0)
	if err != nil {
		t.Fatalf("ListRuns(0): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 runs, got %d", len(all))
	}
	// Should be newest first.
	if all[0].RunID != "run_20260826_030000" {
		t.Errorf("first run should be newest, got %q", all[0].RunID)
	}

	// Limited.
	limited, err := c.ListRuns(ctx, 2)
	if err != nil {
		t.Fatalf("ListRuns(2): %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("expected 2 runs with limit=2, got %d", len(limited))
	}
}

func TestListRunsByPortfolio(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()

	runs := []PipelineRun{
		{RunID: "run_20260826_040000", StartedAt: time.Now(), Status: RunStatusCompleted, Portfolio: "us_sp500", Method: "us_quality_momentum"},
		{RunID: "run_20260826_050000", StartedAt: time.Now(), Status: RunStatusCompleted, Portfolio: "microsmall", Method: "multibagger"},
		{RunID: "run_20260826_060000", StartedAt: time.Now(), Status: RunStatusRunning, Portfolio: "us_sp500", Method: "us_quality_momentum"},
	}
	for _, r := range runs {
		if err := c.InsertRun(ctx, r); err != nil {
			t.Fatalf("InsertRun %s: %v", r.RunID, err)
		}
	}

	got, err := c.ListRunsByPortfolio(ctx, "us_sp500", 0)
	if err != nil {
		t.Fatalf("ListRunsByPortfolio: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 us_sp500 runs, got %d", len(got))
	}
}

func TestLatestRun(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()

	// Insert a completed and a running run.
	completedRun := PipelineRun{
		RunID:     "run_20260826_070000",
		StartedAt: time.Date(2026, 8, 26, 7, 0, 0, 0, time.UTC),
		Status:    RunStatusRunning,
		Portfolio: "us_sp500",
		Method:    "us_quality_momentum",
	}
	runningRun := PipelineRun{
		RunID:     "run_20260826_080000",
		StartedAt: time.Date(2026, 8, 26, 8, 0, 0, 0, time.UTC),
		Status:    RunStatusRunning,
		Portfolio: "us_sp500",
		Method:    "us_quality_momentum",
	}
	for _, r := range []PipelineRun{completedRun, runningRun} {
		if err := c.InsertRun(ctx, r); err != nil {
			t.Fatalf("InsertRun: %v", err)
		}
	}
	// Mark the first one as completed.
	if err := c.CompleteRun(ctx, completedRun.RunID); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}

	// LatestRun should return the completed one, not the running one.
	got, err := c.LatestRun(ctx, "us_sp500", "us_quality_momentum")
	if err != nil {
		t.Fatalf("LatestRun: %v", err)
	}
	if got.RunID != "run_20260826_070000" {
		t.Errorf("LatestRun: got %q, want run_20260826_070000", got.RunID)
	}
}

func TestLatestRun_NoneCompleted(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()

	_, err := c.LatestRun(ctx, "us_sp500", "us_quality_momentum")
	if err == nil {
		t.Fatal("expected error when no completed runs exist")
	}
}
