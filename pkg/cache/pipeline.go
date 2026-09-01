package cache

import (
	"context"
	"database/sql"
	"fmt"
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

// Selection represents a final portfolio selection with structured driver metrics
// and cross-run delta fields. It is the queryable audit trail of *why* a stock is
// held and *how the roster changed* since the previous rebalance (roadmap Phase 8),
// persisted at pipeline finalization.
type Selection struct {
	Ticker      string
	Weight      float64
	Score       float64
	Rank        int
	Sector      string
	TTMGrowth   float64
	RevenueCagr float64
	DSODelta    float64
	RSI         float64
	Momentum1Y  float64
	FCFYield    float64
	ROIC        float64
	Action      string  // "new", "retained", "removed"
	PrevRank    int     // 0 if new
	PrevWeight  float64 // 0 if new
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

// InsertSelections bulk-inserts final portfolio selections for a run.
func (c *Cache) InsertSelections(ctx context.Context, runID string, selections []Selection) error {
	if len(selections) == 0 {
		return nil
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, s := range selections {
		var prevRank sql.NullInt64
		var prevWeight sql.NullFloat64
		if s.PrevRank != 0 {
			prevRank = sql.NullInt64{Int64: int64(s.PrevRank), Valid: true}
		}
		if s.PrevWeight != 0 {
			prevWeight = sql.NullFloat64{Float64: s.PrevWeight, Valid: true}
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO selections (run_id, ticker, weight, score, rank, sector,
				ttm_growth, revenue_cagr, dso_delta, rsi, momentum_1y, fcf_yield, roic,
				action, prev_rank, prev_weight)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (run_id, ticker) DO UPDATE SET
				weight = EXCLUDED.weight, score = EXCLUDED.score, rank = EXCLUDED.rank,
				sector = EXCLUDED.sector,
				ttm_growth = EXCLUDED.ttm_growth, revenue_cagr = EXCLUDED.revenue_cagr,
				dso_delta = EXCLUDED.dso_delta, rsi = EXCLUDED.rsi,
				momentum_1y = EXCLUDED.momentum_1y, fcf_yield = EXCLUDED.fcf_yield,
				roic = EXCLUDED.roic, action = EXCLUDED.action,
				prev_rank = EXCLUDED.prev_rank, prev_weight = EXCLUDED.prev_weight`,
			runID, s.Ticker, s.Weight, s.Score, s.Rank, s.Sector,
			s.TTMGrowth, s.RevenueCagr, s.DSODelta, s.RSI, s.Momentum1Y, s.FCFYield, s.ROIC,
			s.Action, prevRank, prevWeight,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetSelections retrieves selections for a run, ordered by rank.
func (c *Cache) GetSelections(ctx context.Context, runID string) ([]Selection, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT ticker, weight, score, rank, sector,
			ttm_growth, revenue_cagr, dso_delta, rsi, momentum_1y, fcf_yield, roic,
			action, prev_rank, prev_weight
		FROM selections
		WHERE run_id = ?
		ORDER BY rank ASC`,
		runID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var selections []Selection
	for rows.Next() {
		var s Selection
		var score, ttm, rev, dso, rsi, mom, fcf, roic, prevW sql.NullFloat64
		var rank, prevR sql.NullInt64
		var action, sector sql.NullString
		if err := rows.Scan(
			&s.Ticker, &s.Weight, &score, &rank, &sector,
			&ttm, &rev, &dso, &rsi, &mom, &fcf, &roic,
			&action, &prevR, &prevW,
		); err != nil {
			return nil, err
		}
		if score.Valid {
			s.Score = score.Float64
		}
		if rank.Valid {
			s.Rank = int(rank.Int64)
		}
		if sector.Valid {
			s.Sector = sector.String
		}
		if ttm.Valid {
			s.TTMGrowth = ttm.Float64
		}
		if rev.Valid {
			s.RevenueCagr = rev.Float64
		}
		if dso.Valid {
			s.DSODelta = dso.Float64
		}
		if rsi.Valid {
			s.RSI = rsi.Float64
		}
		if mom.Valid {
			s.Momentum1Y = mom.Float64
		}
		if fcf.Valid {
			s.FCFYield = fcf.Float64
		}
		if roic.Valid {
			s.ROIC = roic.Float64
		}
		if action.Valid {
			s.Action = action.String
		}
		if prevR.Valid {
			s.PrevRank = int(prevR.Int64)
		}
		if prevW.Valid {
			s.PrevWeight = prevW.Float64
		}
		selections = append(selections, s)
	}
	return selections, rows.Err()
}

// GetPreviousSelections retrieves selections from the most recent completed run
// for a portfolio+method. Used for cross-run comparison (replaces txt parsing).
func (c *Cache) GetPreviousSelections(ctx context.Context, portfolio, method string) ([]Selection, error) {
	run, err := c.LatestRun(ctx, portfolio, method)
	if err != nil {
		return nil, fmt.Errorf("get previous selections: %w", err)
	}
	return c.GetSelections(ctx, run.RunID)
}

// DeleteRunData removes all pipeline data for a run (index_picks, proposals, selections, and the run itself).
func (c *Cache) DeleteRunData(ctx context.Context, runID string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, q := range []string{
		`DELETE FROM index_picks WHERE run_id = ?`,
		`DELETE FROM proposals WHERE run_id = ?`,
		`DELETE FROM selections WHERE run_id = ?`,
		`DELETE FROM pipeline_runs WHERE run_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, runID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
