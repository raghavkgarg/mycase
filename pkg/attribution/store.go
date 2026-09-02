package attribution

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/raghavkgarg/mycase/pkg/cache"
)

// navHistoryDDL creates the append-only NAV series table. Owned by this package
// (not pkg/cache) so the dependency direction stays domain → cache (R16 P4).
// Timestamps are Unix epoch seconds in a BIGINT, per the cache-wide convention.
const navHistoryDDL = `
CREATE TABLE IF NOT EXISTS nav_history (
    portfolio  VARCHAR NOT NULL,
    ts         BIGINT  NOT NULL,
    nav        DOUBLE  NOT NULL,
    benchmark  DOUBLE  NOT NULL,
    PRIMARY KEY (portfolio, ts)
);`

// Store persists NAV series to DuckDB via a *sql.DB handle obtained from
// cache.Conn(). It lazily ensures its schema on first use.
type Store struct {
	db      *sql.DB
	ensured bool
}

// NewStore wraps a database handle (e.g. cache.GetDB().Conn()). Returns nil if
// db is nil so callers can treat "no cache" as "persistence disabled".
func NewStore(db *sql.DB) *Store {
	if db == nil {
		return nil
	}
	return &Store{db: db}
}

func (s *Store) ensureSchema(ctx context.Context) error {
	if s.ensured {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, navHistoryDDL); err != nil {
		return fmt.Errorf("create nav_history: %w", err)
	}
	s.ensured = true
	return nil
}

// InsertNAVPoints upserts a NAV series for a portfolio. Idempotent on
// (portfolio, ts): re-persisting the same day overwrites its values, so a
// re-run with corrected prices is safe. A nil Store is a no-op.
func (s *Store) InsertNAVPoints(ctx context.Context, portfolio string, points []NAVPoint) error {
	if s == nil || len(points) == 0 {
		return nil
	}
	if err := s.ensureSchema(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, p := range points {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO nav_history (portfolio, ts, nav, benchmark)
			VALUES (?, ?, ?, ?)
			ON CONFLICT (portfolio, ts) DO UPDATE SET
				nav = EXCLUDED.nav,
				benchmark = EXCLUDED.benchmark`,
			portfolio, p.Date.Unix(), p.PortfolioValue, p.BenchmarkValue,
		); err != nil {
			return fmt.Errorf("insert nav point %s: %w", p.Date.Format("2006-01-02"), err)
		}
	}
	return tx.Commit()
}

// GetNAVHistory returns a portfolio's NAV series ordered by date ascending.
// If since is non-zero, only points on/after that time are returned. A nil
// Store returns an empty slice.
func (s *Store) GetNAVHistory(ctx context.Context, portfolio string, since time.Time) ([]NAVPoint, error) {
	if s == nil {
		return nil, nil
	}
	if err := s.ensureSchema(ctx); err != nil {
		return nil, err
	}

	query := `SELECT ts, nav, benchmark FROM nav_history WHERE portfolio = ?`
	args := []any{portfolio}
	if !since.IsZero() {
		query += ` AND ts >= ?`
		args = append(args, since.Unix())
	}
	query += ` ORDER BY ts ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []NAVPoint
	for rows.Next() {
		var ts int64
		var nav, bench float64
		if err := rows.Scan(&ts, &nav, &bench); err != nil {
			return nil, err
		}
		out = append(out, NAVPoint{
			Date:           time.Unix(ts, 0),
			PortfolioValue: nav,
			BenchmarkValue: bench,
		})
	}
	return out, rows.Err()
}

// runReader is the subset of *cache.Cache the rebalance-history loader needs.
// Declaring it here keeps attribution testable with a fake and documents the
// exact coupling (attribution → cache, one-way).
type runReader interface {
	ListRunsByPortfolio(ctx context.Context, portfolio string, limit int) ([]cache.PipelineRun, error)
	GetProposals(ctx context.Context, runID, stage string) ([]cache.Proposal, error)
}

// LoadRebalanceHistory reconstructs a portfolio's target-weight history from the
// pipeline_runs + proposals tables. Each completed run becomes one RebalanceEvent
// timestamped at the run's start. For each run the executed basket
// (stage="final", roadmap Phase 9) is preferred when present, falling back to the
// proposed basket (stage="optimized") for runs that predate the final stage or
// were never executed. Runs with neither are skipped (an interrupted or
// draft-only run is not a rebalance). Results are returned oldest-first.
//
// Preferring "final" means the decomposition measures *realized* rebalance weights
// (what was actually confirmed and submitted) rather than merely intended ones,
// tightening the "did rebalancing add value" measurement.
//
// The portfolio name here is the pipeline "portfolio"/universe name (e.g.
// "us_sp500"), which is what pipeline_runs stores — the same value
// csvloader.GetUniverseName yields for the basket CSV.
func LoadRebalanceHistory(ctx context.Context, r runReader, portfolio string, since time.Time) ([]RebalanceEvent, error) {
	if r == nil {
		return nil, nil
	}
	runs, err := r.ListRunsByPortfolio(ctx, portfolio, 0)
	if err != nil {
		return nil, fmt.Errorf("list runs for %s: %w", portfolio, err)
	}
	var events []RebalanceEvent
	for _, run := range runs {
		if run.Status != cache.RunStatusCompleted {
			continue
		}
		if !since.IsZero() && run.StartedAt.Before(since) {
			continue
		}
		// Prefer the executed basket (final) over the proposed one (optimized).
		props, perr := r.GetProposals(ctx, run.RunID, "final")
		if perr != nil {
			return nil, fmt.Errorf("get final proposals for %s: %w", run.RunID, perr)
		}
		if len(props) == 0 {
			props, perr = r.GetProposals(ctx, run.RunID, "optimized")
			if perr != nil {
				return nil, fmt.Errorf("get proposals for %s: %w", run.RunID, perr)
			}
		}
		if len(props) == 0 {
			continue
		}
		weights := make(map[string]float64, len(props))
		for _, p := range props {
			if p.Weight > 0 {
				weights[p.Ticker] = p.Weight
			}
		}
		if len(weights) == 0 {
			continue
		}
		events = append(events, RebalanceEvent{
			When:    run.StartedAt,
			RunID:   run.RunID,
			Weights: weights,
		})
	}
	// ListRunsByPortfolio returns newest-first; decomposition wants oldest-first.
	sort.Slice(events, func(i, j int) bool { return events[i].When.Before(events[j].When) })
	return events, nil
}
