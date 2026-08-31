package cache

import (
	"context"
	"database/sql"
)

// IndexPick represents a scored candidate from a single index within a pipeline run.
type IndexPick struct {
	IndexName string
	Ticker    string
	Score     float64
	Rank      int
	Weight    float64
	Sector    string
}

// Proposal represents a candidate at a specific pipeline stage.
type Proposal struct {
	Ticker string
	Weight float64
	Score  float64
	Rank   int
	Sector string
}

// InsertIndexPicks bulk-inserts scored candidates for one index within a run.
func (c *Cache) InsertIndexPicks(ctx context.Context, runID, indexName string, picks []IndexPick) error {
	if len(picks) == 0 {
		return nil
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, p := range picks {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO index_picks (run_id, index_name, ticker, score, rank, weight, sector)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (run_id, index_name, ticker) DO UPDATE SET
				score = EXCLUDED.score, rank = EXCLUDED.rank,
				weight = EXCLUDED.weight, sector = EXCLUDED.sector`,
			runID, indexName, p.Ticker, p.Score, p.Rank, p.Weight, p.Sector,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetIndexPicks retrieves all picks for a run+index, ordered by rank.
func (c *Cache) GetIndexPicks(ctx context.Context, runID, indexName string) ([]IndexPick, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT index_name, ticker, score, rank, weight, sector
		FROM index_picks
		WHERE run_id = ? AND index_name = ?
		ORDER BY rank ASC`,
		runID, indexName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var picks []IndexPick
	for rows.Next() {
		var p IndexPick
		var score, weight sql.NullFloat64
		var rank sql.NullInt64
		var sector sql.NullString
		if err := rows.Scan(&p.IndexName, &p.Ticker, &score, &rank, &weight, &sector); err != nil {
			return nil, err
		}
		if score.Valid {
			p.Score = score.Float64
		}
		if rank.Valid {
			p.Rank = int(rank.Int64)
		}
		if weight.Valid {
			p.Weight = weight.Float64
		}
		if sector.Valid {
			p.Sector = sector.String
		}
		picks = append(picks, p)
	}
	return picks, rows.Err()
}

// GetAllIndexPicks retrieves all picks across all indices for a run, ordered by index then rank.
// This replaces the "combine" step that previously read multiple CSVs.
func (c *Cache) GetAllIndexPicks(ctx context.Context, runID string) ([]IndexPick, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT index_name, ticker, score, rank, weight, sector
		FROM index_picks
		WHERE run_id = ?
		ORDER BY index_name, rank ASC`,
		runID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var picks []IndexPick
	for rows.Next() {
		var p IndexPick
		var score, weight sql.NullFloat64
		var rank sql.NullInt64
		var sector sql.NullString
		if err := rows.Scan(&p.IndexName, &p.Ticker, &score, &rank, &weight, &sector); err != nil {
			return nil, err
		}
		if score.Valid {
			p.Score = score.Float64
		}
		if rank.Valid {
			p.Rank = int(rank.Int64)
		}
		if weight.Valid {
			p.Weight = weight.Float64
		}
		if sector.Valid {
			p.Sector = sector.String
		}
		picks = append(picks, p)
	}
	return picks, rows.Err()
}

// InsertProposals bulk-inserts candidates at a given pipeline stage.
func (c *Cache) InsertProposals(ctx context.Context, runID, stage string, proposals []Proposal) error {
	if len(proposals) == 0 {
		return nil
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, p := range proposals {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO proposals (run_id, stage, ticker, weight, score, rank, sector)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (run_id, stage, ticker) DO UPDATE SET
				weight = EXCLUDED.weight, score = EXCLUDED.score,
				rank = EXCLUDED.rank, sector = EXCLUDED.sector`,
			runID, stage, p.Ticker, p.Weight, p.Score, p.Rank, p.Sector,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetProposals retrieves proposals for a run at a given stage, ordered by rank.
func (c *Cache) GetProposals(ctx context.Context, runID, stage string) ([]Proposal, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT ticker, weight, score, rank, sector
		FROM proposals
		WHERE run_id = ? AND stage = ?
		ORDER BY rank ASC`,
		runID, stage,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proposals []Proposal
	for rows.Next() {
		var p Proposal
		var score sql.NullFloat64
		var rank sql.NullInt64
		var sector sql.NullString
		if err := rows.Scan(&p.Ticker, &p.Weight, &score, &rank, &sector); err != nil {
			return nil, err
		}
		if score.Valid {
			p.Score = score.Float64
		}
		if rank.Valid {
			p.Rank = int(rank.Int64)
		}
		if sector.Valid {
			p.Sector = sector.String
		}
		proposals = append(proposals, p)
	}
	return proposals, rows.Err()
}

// DeleteRunData removes all pipeline data for a run (index_picks, proposals, and the run itself).
func (c *Cache) DeleteRunData(ctx context.Context, runID string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, q := range []string{
		`DELETE FROM index_picks WHERE run_id = ?`,
		`DELETE FROM proposals WHERE run_id = ?`,
		`DELETE FROM pipeline_runs WHERE run_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, runID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
