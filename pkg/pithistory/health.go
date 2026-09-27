package pithistory

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DataHealthReport contains quantitative audit metrics for data recency, entropy, and fundamental quality.
type DataHealthReport struct {
	AsOfDate              string    `json:"as_of_date"`
	IndexName             string    `json:"index_name"`
	Method                string    `json:"method"`
	TotalCandidates       int       `json:"total_candidates"`
	PricesMaxDate         string    `json:"prices_max_date"`
	PricesStaleCount      int       `json:"prices_stale_count"`
	DeliveryMaxDate       string    `json:"delivery_max_date"`
	DeliveryStaleCount    int       `json:"delivery_stale_count"`
	PairedCandidatesCount int       `json:"paired_candidates_count"`
	FrozenDelivCount      int       `json:"frozen_deliv_count"`
	FrozenRSCount         int       `json:"frozen_rs_count"`
	FrozenVCPCount        int       `json:"frozen_vcp_count"`
	FrozenRVOLCount       int       `json:"frozen_rvol_count"`
	ZeroCFOCount          int       `json:"zero_cfo_count"`
	ZeroPATCount          int       `json:"zero_pat_count"`
	NullDECount           int       `json:"null_de_count"`
	DroppedHoldingsCount  int       `json:"dropped_holdings_count"`
	DroppedHoldingTickers []string  `json:"dropped_holding_tickers"`
	HealthStatus          string    `json:"health_status"`
	CreatedAt             time.Time `json:"created_at"`
}

// AuditDataHealth computes empirical freshness, metric variance, and data coverage metrics.
func (p *DB) AuditDataHealth(ctx context.Context, asOfDate, prevDate, indexName, method string) (*DataHealthReport, error) {
	rep := &DataHealthReport{
		AsOfDate:     asOfDate,
		IndexName:    indexName,
		Method:       method,
		HealthStatus: "OPTIMAL",
		CreatedAt:    time.Now(),
	}

	// 1. Price series recency & total candidates
	priceQuery := `
WITH t_prices AS (
    SELECT c.ticker, MAX(p.date) as max_d
    FROM v_pit_candidate_scores c
    LEFT JOIN prices p ON c.ticker = p.ticker
    WHERE c.as_of_date = ? AND c.index_name = ? AND c.method = ?
    GROUP BY c.ticker
)
SELECT 
    COUNT(*),
    COALESCE(STRFTIME(MAX(max_d), '%Y-%m-%d'), '1970-01-01'),
    COUNT(CASE WHEN max_d IS NULL OR max_d < ? THEN 1 END)
FROM t_prices;
`
	err := p.db.QueryRowContext(ctx, priceQuery, asOfDate, indexName, method, asOfDate).Scan(
		&rep.TotalCandidates,
		&rep.PricesMaxDate,
		&rep.PricesStaleCount,
	)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("audit price recency: %w", err)
	}

	// 2. Delivery series recency (from fundamentals table)
	delivQuery := `
WITH t_deliv AS (
    SELECT 
        c.ticker, 
        json_extract_string(f.raw_json, '$.DeliveryDate') as d_date
    FROM v_pit_candidate_scores c
    LEFT JOIN fundamentals f ON c.ticker = f.ticker
    WHERE c.as_of_date = ? AND c.index_name = ? AND c.method = ?
)
SELECT 
    COALESCE(MAX(d_date), '1970-01-01'),
    COUNT(CASE WHEN d_date IS NULL OR d_date < ? THEN 1 END)
FROM t_deliv;
`
	err = p.db.QueryRowContext(ctx, delivQuery, asOfDate, indexName, method, asOfDate).Scan(
		&rep.DeliveryMaxDate,
		&rep.DeliveryStaleCount,
	)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("audit delivery recency: %w", err)
	}

	// 3. Consecutive-run entropy / frozen metric detector
	if prevDate != "" {
		entropyQuery := `
WITH consecutive AS (
    SELECT 
        c0.ticker,
        c0.composite_rs - c1.composite_rs as rs_diff,
        c0.vcp_ratio - c1.vcp_ratio as vcp_diff,
        c0.rvol_z_score - c1.rvol_z_score as rvol_diff,
        c0.delivery_delta - c1.delivery_delta as deliv_diff
    FROM v_pit_candidate_scores c0
    JOIN v_pit_candidate_scores c1 
      ON c0.ticker = c1.ticker 
     AND c0.index_name = c1.index_name 
     AND c0.method = c1.method
    WHERE c0.as_of_date = ?
      AND c1.as_of_date = ?
      AND c0.index_name = ?
      AND c0.method = ?
)
SELECT 
    count(*) as total_paired,
    count(CASE WHEN abs(rs_diff) < 0.000001 THEN 1 END) as frozen_rs,
    count(CASE WHEN abs(vcp_diff) < 0.000001 THEN 1 END) as frozen_vcp,
    count(CASE WHEN abs(rvol_diff) < 0.000001 THEN 1 END) as frozen_rvol,
    count(CASE WHEN abs(deliv_diff) < 0.000001 THEN 1 END) as frozen_deliv
FROM consecutive;
`
		_ = p.db.QueryRowContext(ctx, entropyQuery, asOfDate, prevDate, indexName, method).Scan(
			&rep.PairedCandidatesCount,
			&rep.FrozenRSCount,
			&rep.FrozenVCPCount,
			&rep.FrozenRVOLCount,
			&rep.FrozenDelivCount,
		)
	}

	// 4. Fundamental coverage & zero rates
	fundQuery := `
SELECT 
    count(CASE WHEN json_extract(f.raw_json, '$.OperatingCashflow') IS NULL OR json_extract(f.raw_json, '$.OperatingCashflow') = '0' THEN 1 END) as zero_cfo,
    count(CASE WHEN json_extract(f.raw_json, '$.NetIncome') IS NULL OR json_extract(f.raw_json, '$.NetIncome') = '0' THEN 1 END) as zero_pat,
    count(CASE WHEN json_extract(f.raw_json, '$.DebtToEquity') IS NULL THEN 1 END) as null_de
FROM v_pit_candidate_scores c
LEFT JOIN fundamentals f ON c.ticker = f.ticker
WHERE c.as_of_date = ? AND c.index_name = ? AND c.method = ?;
`
	_ = p.db.QueryRowContext(ctx, fundQuery, asOfDate, indexName, method).Scan(
		&rep.ZeroCFOCount,
		&rep.ZeroPATCount,
		&rep.NullDECount,
	)

	// 5. Silent drops of active holdings
	if prevDate != "" {
		dropQuery := `
SELECT prev.ticker
FROM v_pit_candidate_scores prev
JOIN v_pit_candidate_scores curr
  ON prev.ticker = curr.ticker
 AND prev.index_name = curr.index_name
 AND prev.method = curr.method
WHERE prev.as_of_date = ?
  AND curr.as_of_date = ?
  AND prev.index_name = ?
  AND prev.method = ?
  AND prev.selected = true
  AND (curr.data_fetch_failed = true OR curr.rejection_reason LIKE 'DATA_FETCH_FAILED%' OR (curr.passed_stage1 = false AND (curr.rejection_reason IS NULL OR curr.rejection_reason = '')));
`
		rows, err := p.db.QueryContext(ctx, dropQuery, prevDate, asOfDate, indexName, method)
		if err == nil {
			for rows.Next() {
				var sym string
				if rows.Scan(&sym) == nil {
					rep.DroppedHoldingTickers = append(rep.DroppedHoldingTickers, sym)
				}
			}
			rows.Close()
		}
		rep.DroppedHoldingsCount = len(rep.DroppedHoldingTickers)
	}

	// 6. Overall Health Status Classification
	if rep.PairedCandidatesCount > 0 {
		frozenThreshold := rep.PairedCandidatesCount * 10 / 100 // > 10% frozen
		if rep.FrozenDelivCount > frozenThreshold || rep.FrozenRSCount > frozenThreshold ||
			rep.FrozenVCPCount > frozenThreshold || rep.FrozenRVOLCount > frozenThreshold {
			rep.HealthStatus = "CRITICAL_FROZEN"
		} else if rep.PricesStaleCount > (rep.TotalCandidates*2/100) || rep.DeliveryStaleCount > (rep.TotalCandidates*5/100) || rep.DroppedHoldingsCount > 0 {
			rep.HealthStatus = "DEGRADED"
		}
	}

	return rep, nil
}

// SaveDataHealth upserts a DataHealthReport into pit_data_health.
func (p *DB) SaveDataHealth(ctx context.Context, r *DataHealthReport) error {
	if r == nil {
		return nil
	}

	upsert := `
INSERT INTO pit_data_health (
    as_of_date, index_name, method, total_candidates,
    prices_max_date, prices_stale_count, delivery_max_date, delivery_stale_count,
    paired_candidates_count, frozen_deliv_count, frozen_rs_count, frozen_vcp_count, frozen_rvol_count,
    zero_cfo_count, zero_pat_count, null_de_count, dropped_holdings_count, health_status
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (as_of_date, index_name, method) DO UPDATE SET
    total_candidates = EXCLUDED.total_candidates,
    prices_max_date = EXCLUDED.prices_max_date,
    prices_stale_count = EXCLUDED.prices_stale_count,
    delivery_max_date = EXCLUDED.delivery_max_date,
    delivery_stale_count = EXCLUDED.delivery_stale_count,
    paired_candidates_count = EXCLUDED.paired_candidates_count,
    frozen_deliv_count = EXCLUDED.frozen_deliv_count,
    frozen_rs_count = EXCLUDED.frozen_rs_count,
    frozen_vcp_count = EXCLUDED.frozen_vcp_count,
    frozen_rvol_count = EXCLUDED.frozen_rvol_count,
    zero_cfo_count = EXCLUDED.zero_cfo_count,
    zero_pat_count = EXCLUDED.zero_pat_count,
    null_de_count = EXCLUDED.null_de_count,
    dropped_holdings_count = EXCLUDED.dropped_holdings_count,
    health_status = EXCLUDED.health_status,
    created_at = now();
`
	_, err := p.db.ExecContext(ctx, upsert,
		r.AsOfDate, r.IndexName, r.Method, r.TotalCandidates,
		r.PricesMaxDate, r.PricesStaleCount, r.DeliveryMaxDate, r.DeliveryStaleCount,
		r.PairedCandidatesCount, r.FrozenDelivCount, r.FrozenRSCount, r.FrozenVCPCount, r.FrozenRVOLCount,
		r.ZeroCFOCount, r.ZeroPATCount, r.NullDECount, r.DroppedHoldingsCount, r.HealthStatus,
	)
	return err
}
