package cache

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RunStatus represents the lifecycle state of a pipeline run.
type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

// PipelineRun represents a single pipeline execution record.
type PipelineRun struct {
	RunID       string
	StartedAt   time.Time
	CompletedAt time.Time // zero value if not yet completed
	Status      RunStatus
	Portfolio   string
	Method      string
	ConfigJSON  string // optional snapshot of pipeline config
}

// NewRunID generates a timestamp-based run ID: run_20260826_143022
func NewRunID() string {
	return fmt.Sprintf("run_%s", time.Now().Format("20060102_150405"))
}

// InsertRun creates a new pipeline_runs row with status "running".
func (c *Cache) InsertRun(ctx context.Context, run PipelineRun) error {
	_, err := c.db.ExecContext(ctx, `
		INSERT INTO pipeline_runs (run_id, started_at, status, portfolio, method, config_json)
		VALUES (?, ?, ?, ?, ?, ?)`,
		run.RunID, run.StartedAt.Unix(), string(RunStatusRunning), run.Portfolio, run.Method, run.ConfigJSON,
	)
	return err
}

// CompleteRun marks a run as completed with the current timestamp.
func (c *Cache) CompleteRun(ctx context.Context, runID string) error {
	now := time.Now().Unix()
	res, err := c.db.ExecContext(ctx, `
		UPDATE pipeline_runs SET status = ?, completed_at = ? WHERE run_id = ?`,
		string(RunStatusCompleted), now, runID,
	)
	if err != nil {
		return err
	}
	return checkRowAffected(res, runID)
}

// FailRun marks a run as failed with the current timestamp.
func (c *Cache) FailRun(ctx context.Context, runID string) error {
	now := time.Now().Unix()
	res, err := c.db.ExecContext(ctx, `
		UPDATE pipeline_runs SET status = ?, completed_at = ? WHERE run_id = ?`,
		string(RunStatusFailed), now, runID,
	)
	if err != nil {
		return err
	}
	return checkRowAffected(res, runID)
}

// GetRun retrieves a single pipeline run by ID.
func (c *Cache) GetRun(ctx context.Context, runID string) (PipelineRun, error) {
	var r PipelineRun
	var startedAt int64
	var completedAt sql.NullInt64
	var configJSON sql.NullString

	err := c.db.QueryRowContext(ctx, `
		SELECT run_id, started_at, completed_at, status, portfolio, method, config_json
		FROM pipeline_runs WHERE run_id = ?`, runID,
	).Scan(&r.RunID, &startedAt, &completedAt, &r.Status, &r.Portfolio, &r.Method, &configJSON)
	if err == sql.ErrNoRows {
		return r, fmt.Errorf("run %q not found", runID)
	}
	if err != nil {
		return r, err
	}
	r.StartedAt = time.Unix(startedAt, 0)
	if completedAt.Valid {
		r.CompletedAt = time.Unix(completedAt.Int64, 0)
	}
	if configJSON.Valid {
		r.ConfigJSON = configJSON.String
	}
	return r, nil
}

// ListRuns returns the most recent pipeline runs, ordered by started_at desc.
// Pass limit <= 0 for all runs.
func (c *Cache) ListRuns(ctx context.Context, limit int) ([]PipelineRun, error) {
	query := `SELECT run_id, started_at, completed_at, status, portfolio, method, config_json
		FROM pipeline_runs ORDER BY started_at DESC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []PipelineRun
	for rows.Next() {
		var r PipelineRun
		var startedAt int64
		var completedAt sql.NullInt64
		var configJSON sql.NullString
		if err := rows.Scan(&r.RunID, &startedAt, &completedAt, &r.Status, &r.Portfolio, &r.Method, &configJSON); err != nil {
			return nil, err
		}
		r.StartedAt = time.Unix(startedAt, 0)
		if completedAt.Valid {
			r.CompletedAt = time.Unix(completedAt.Int64, 0)
		}
		if configJSON.Valid {
			r.ConfigJSON = configJSON.String
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// ListRunsByPortfolio returns runs filtered by portfolio name, ordered by started_at desc.
func (c *Cache) ListRunsByPortfolio(ctx context.Context, portfolio string, limit int) ([]PipelineRun, error) {
	query := `SELECT run_id, started_at, completed_at, status, portfolio, method, config_json
		FROM pipeline_runs WHERE portfolio = ? ORDER BY started_at DESC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := c.db.QueryContext(ctx, query, portfolio)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []PipelineRun
	for rows.Next() {
		var r PipelineRun
		var startedAt int64
		var completedAt sql.NullInt64
		var configJSON sql.NullString
		if err := rows.Scan(&r.RunID, &startedAt, &completedAt, &r.Status, &r.Portfolio, &r.Method, &configJSON); err != nil {
			return nil, err
		}
		r.StartedAt = time.Unix(startedAt, 0)
		if completedAt.Valid {
			r.CompletedAt = time.Unix(completedAt.Int64, 0)
		}
		if configJSON.Valid {
			r.ConfigJSON = configJSON.String
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// LatestRun returns the most recent completed run for a portfolio+method combo.
// Returns sql.ErrNoRows wrapped if none found.
func (c *Cache) LatestRun(ctx context.Context, portfolio, method string) (PipelineRun, error) {
	var r PipelineRun
	var startedAt int64
	var completedAt sql.NullInt64
	var configJSON sql.NullString

	err := c.db.QueryRowContext(ctx, `
		SELECT run_id, started_at, completed_at, status, portfolio, method, config_json
		FROM pipeline_runs
		WHERE portfolio = ? AND method = ? AND status = ?
		ORDER BY started_at DESC LIMIT 1`,
		portfolio, method, string(RunStatusCompleted),
	).Scan(&r.RunID, &startedAt, &completedAt, &r.Status, &r.Portfolio, &r.Method, &configJSON)
	if err == sql.ErrNoRows {
		return r, fmt.Errorf("no completed run found for %s/%s", portfolio, method)
	}
	if err != nil {
		return r, err
	}
	r.StartedAt = time.Unix(startedAt, 0)
	if completedAt.Valid {
		r.CompletedAt = time.Unix(completedAt.Int64, 0)
	}
	if configJSON.Valid {
		r.ConfigJSON = configJSON.String
	}
	return r, nil
}

func checkRowAffected(res sql.Result, runID string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("run %q not found", runID)
	}
	return nil
}
