package attribution

import (
	"context"
	"database/sql"
	"fmt"
	"time"
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
