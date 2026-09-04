package pithistory

import (
	"context"
	"fmt"
	"time"
)

type RunSummaryRow struct {
	AsOfDate          string    `json:"as_of_date"`
	IndexName         string    `json:"index_name"`
	Method            string    `json:"method"`
	RegimeMultiplier  float64   `json:"regime_multiplier"`
	TotalConstituents int       `json:"total_constituents"`
	Stage1Survivors   int       `json:"stage1_survivors"`
	SelectedCount     int       `json:"selected_count"`
	CreatedAt         time.Time `json:"created_at"`
}

type CandidateHistoryRow struct {
	AsOfDate       string  `json:"as_of_date"`
	PassedStage1   bool    `json:"passed_stage1"`
	RawScore       float64 `json:"raw_score"`
	EffectiveScore float64 `json:"effective_score"`
	CompositeRS    float64 `json:"composite_rs"`
	VCPRatio       float64 `json:"vcp_ratio"`
	RVOLZScore     float64 `json:"rvol_z_score"`
	DecayedPP      float64 `json:"decayed_pp"`
	DeliveryDelta  float64 `json:"delivery_delta"`
	Selected       bool    `json:"selected"`
	FinalWeight    float64 `json:"final_weight"`
}

// GetEmpiricalQuantiles returns P90, P75, P50, P40, P25 for raw scores across Stage-1 survivors.
func (p *DB) GetEmpiricalQuantiles(ctx context.Context, indexName, method string, days int) (map[string]float64, error) {
	dateFilter := ""
	if days > 0 {
		dateFilter = fmt.Sprintf("AND as_of_date >= CURRENT_DATE - INTERVAL %d DAY", days)
	}

	query := fmt.Sprintf(`
SELECT 
    COALESCE(quantile_cont(raw_score, 0.90), 0.0) AS p90,
    COALESCE(quantile_cont(raw_score, 0.75), 0.0) AS p75,
    COALESCE(quantile_cont(raw_score, 0.50), 0.0) AS p50,
    COALESCE(quantile_cont(raw_score, 0.40), 0.0) AS p40,
    COALESCE(quantile_cont(raw_score, 0.25), 0.0) AS p25,
    COUNT(*) as total_samples
FROM pit_candidate_scores
WHERE passed_stage1 = true 
  AND index_name = ? 
  AND method = ?
  %s;
`, dateFilter)

	var p90, p75, p50, p40, p25 float64
	var count int64

	row := p.db.QueryRowContext(ctx, query, indexName, method)
	if err := row.Scan(&p90, &p75, &p50, &p40, &p25, &count); err != nil {
		return nil, fmt.Errorf("query empirical quantiles: %w", err)
	}

	return map[string]float64{
		"p90":     p90,
		"p75":     p75,
		"p50":     p50,
		"p40":     p40,
		"p25":     p25,
		"samples": float64(count),
	}, nil
}

// GetRunHistory returns chronological run records for an index/method.
func (p *DB) GetRunHistory(ctx context.Context, indexName, method string, limit int) ([]RunSummaryRow, error) {
	if limit <= 0 {
		limit = 30
	}
	query := `
SELECT 
    strftime(as_of_date, '%Y-%m-%d'),
    index_name,
    method,
    regime_multiplier,
    total_constituents,
    stage1_survivors,
    selected_count,
    created_at
FROM pit_runs
WHERE index_name = ? AND method = ?
ORDER BY as_of_date DESC
LIMIT ?;
`
	rows, err := p.db.QueryContext(ctx, query, indexName, method, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []RunSummaryRow
	for rows.Next() {
		var r RunSummaryRow
		if err := rows.Scan(
			&r.AsOfDate,
			&r.IndexName,
			&r.Method,
			&r.RegimeMultiplier,
			&r.TotalConstituents,
			&r.Stage1Survivors,
			&r.SelectedCount,
			&r.CreatedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

// GetCandidateHistory returns historical score trajectory for a single stock.
func (p *DB) GetCandidateHistory(ctx context.Context, ticker string, limit int) ([]CandidateHistoryRow, error) {
	if limit <= 0 {
		limit = 30
	}
	query := `
SELECT 
    strftime(as_of_date, '%Y-%m-%d'),
    passed_stage1,
    raw_score,
    effective_score,
    composite_rs,
    vcp_ratio,
    rvol_z_score,
    decayed_pp,
    delivery_delta,
    selected,
    final_weight
FROM pit_candidate_scores
WHERE ticker = ?
ORDER BY as_of_date DESC
LIMIT ?;
`
	rows, err := p.db.QueryContext(ctx, query, ticker, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []CandidateHistoryRow
	for rows.Next() {
		var r CandidateHistoryRow
		if err := rows.Scan(
			&r.AsOfDate,
			&r.PassedStage1,
			&r.RawScore,
			&r.EffectiveScore,
			&r.CompositeRS,
			&r.VCPRatio,
			&r.RVOLZScore,
			&r.DecayedPP,
			&r.DeliveryDelta,
			&r.Selected,
			&r.FinalWeight,
		); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

// GetPendingForwardDates returns historical dates where forward_return_21d is 0.0 and date is at least minDaysAgo old.
func (p *DB) GetPendingForwardDates(ctx context.Context, minDaysAgo int) ([]string, error) {
	query := `
SELECT DISTINCT strftime(as_of_date, '%Y-%m-%d')
FROM pit_candidate_scores
WHERE forward_return_21d = 0.0 
  AND passed_stage1 = true
  AND as_of_date <= CURRENT_DATE - INTERVAL ? DAY
ORDER BY as_of_date ASC;
`
	rows, err := p.db.QueryContext(ctx, query, minDaysAgo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err == nil {
			dates = append(dates, d)
		}
	}
	return dates, nil
}
