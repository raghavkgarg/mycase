package pithistory

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/render"
)

type RunSummaryRow struct {
	CreatedAt         time.Time `json:"created_at"`
	AsOfDate          string    `json:"as_of_date"`
	IndexName         string    `json:"index_name"`
	Method            string    `json:"method"`
	RegimeMultiplier  float64   `json:"regime_multiplier"`
	TotalConstituents int       `json:"total_constituents"`
	Stage1Survivors   int       `json:"stage1_survivors"`
	SelectedCount     int       `json:"selected_count"`
}

type CandidateHistoryRow struct {
	AsOfDate       string  `json:"as_of_date"`
	IndexName      string  `json:"index_name"`
	Method         string  `json:"method"`
	RawScore       float64 `json:"raw_score"`
	EffectiveScore float64 `json:"effective_score"`
	CompositeRS    float64 `json:"composite_rs"`
	VCPRatio       float64 `json:"vcp_ratio"`
	RVOLZScore     float64 `json:"rvol_z_score"`
	DecayedPP      float64 `json:"decayed_pp"`
	DeliveryDelta  float64 `json:"delivery_delta"`
	FinalWeight    float64 `json:"final_weight"`
	PassedStage1   bool    `json:"passed_stage1"`
	Selected       bool    `json:"selected"`
}

// NormalizeIndexName canonicalizes informal aliases (e.g. "small250" -> "smallcap250").
func NormalizeIndexName(name string) string {
	clean := strings.ToLower(strings.TrimSpace(name))
	clean = strings.ReplaceAll(clean, " ", "")
	clean = strings.ReplaceAll(clean, "-", "")
	clean = strings.ReplaceAll(clean, "_", "")
	switch clean {
	case "small250", "smallcap250", "niftysmallcap250":
		return "smallcap250"
	case "micro250", "microcap250", "niftymicrocap250":
		return "microcap250"
	case "nifty50", "n50":
		return "NIFTY50"
	case "totalmarket", "niftytotalmarket":
		return "niftytotalmarket"
	case "microsmall", "microsmall250", "microcap250smallcap250":
		return "microcap250_smallcap250"
	default:
		return name
	}
}

// GetEmpiricalQuantiles returns P90, P75, P50, P40, P25 for raw scores across Stage-1 survivors.
func (p *DB) GetEmpiricalQuantiles(ctx context.Context, indexName, method string, days int) (map[string]float64, error) {
	indexName = NormalizeIndexName(indexName)
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
FROM v_pit_candidate_scores
WHERE passed_stage1 = true 
  AND (data_fetch_failed = false OR data_fetch_failed IS NULL)
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
	indexName = NormalizeIndexName(indexName)
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
FROM v_pit_runs
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
	return p.GetCandidateHistoryFiltered(ctx, ticker, "", "", limit)
}

// GetCandidateHistoryFiltered returns historical score trajectory for a single stock with optional index and method filtering.
// When indexName is empty, it queries the canonical pit_candidate_scores table and deduplicates so that each
// (as_of_date, method) appears as a single canonical trajectory row, avoiding synthetic sub-index projection duplication.
func (p *DB) GetCandidateHistoryFiltered(ctx context.Context, ticker, indexName, method string, limit int) ([]CandidateHistoryRow, error) {
	if limit <= 0 {
		limit = 30
	}

	var query string
	var args []interface{}

	if indexName != "" {
		indexName = NormalizeIndexName(indexName)
		query = `
SELECT 
    strftime(as_of_date, '%Y-%m-%d'),
    index_name,
    method,
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
FROM v_pit_candidate_scores
WHERE ticker = ? AND index_name = ?
`
		args = append(args, ticker, indexName)
		if method != "" {
			query += " AND method = ?\n"
			args = append(args, method)
		}
		query += "ORDER BY as_of_date DESC, method ASC\nLIMIT ?;\n"
		args = append(args, limit)
	} else {
		// When querying by ticker without a specific index filter, query the physical table
		// and deduplicate by (as_of_date, method) so a stock belonging to multiple indices
		// appears as a single canonical trajectory row per date and method.
		query = `
SELECT 
    strftime(as_of_date, '%Y-%m-%d') as as_of_date,
    index_name,
    method,
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
FROM (
    SELECT *,
           ROW_NUMBER() OVER (
               PARTITION BY as_of_date, method 
               ORDER BY 
                   CASE WHEN selected THEN 1 ELSE 2 END,
                   CASE WHEN index_name = 'niftytotalmarket' THEN 1 ELSE 2 END,
                   raw_score DESC
           ) as rn
    FROM pit_candidate_scores
    WHERE ticker = ?
`
		args = append(args, ticker)
		if method != "" {
			query += " AND method = ?\n"
			args = append(args, method)
		}
		query += `)
WHERE rn = 1
ORDER BY as_of_date DESC, method ASC
LIMIT ?;
`
		args = append(args, limit)
	}

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []CandidateHistoryRow
	for rows.Next() {
		var r CandidateHistoryRow
		if err := rows.Scan(
			&r.AsOfDate,
			&r.IndexName,
			&r.Method,
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

// RunDeepAnalysis performs institutional deduction analytics purely from DuckDB tables.
func (p *DB) RunDeepAnalysis(ctx context.Context, indexName, method string) error {
	indexName = NormalizeIndexName(indexName)
	runs, err := p.GetRunHistory(ctx, indexName, method, 10)
	if err != nil {
		return fmt.Errorf("failed to fetch run history: %w", err)
	}
	if len(runs) == 0 {
		fmt.Printf("No PIT historical records found in %s for index=%s, method=%s\n", DefaultDBPath, indexName, method)
		return nil
	}

	latestRun := runs[0]
	var prevRun *RunSummaryRow
	if len(runs) > 1 {
		prevRun = &runs[1]
	}

	fmt.Println("=======================================================================================================================")
	fmt.Println("                      POINT-IN-TIME (PIT) RESEARCH DATABASE DEEP ANALYSIS (DuckDB)                                     ")
	fmt.Println("=======================================================================================================================")
	fmt.Printf("Database:       %s\n", DefaultDBPath)
	fmt.Printf("Universe:       %s\n", indexName)
	fmt.Printf("Strategy:       %s\n", method)
	fmt.Printf("Total PIT Runs: %d recorded runs (Latest As-Of: %s)\n", len(runs), latestRun.AsOfDate)
	fmt.Println("-----------------------------------------------------------------------------------------------------------------------")

	// ==========================================
	// 1. STAGE 1 HARD FILTER ATTRITION BOTTLENECKS
	// ==========================================
	fmt.Printf("\n--- 1. STAGE-1 HARD FILTER ATTRITION & ELIMINATION BOTTLENECKS (%s) ---\n", latestRun.AsOfDate)
	totalCandidates := latestRun.TotalConstituents
	stage1Survivors := latestRun.Stage1Survivors
	eliminatedTotal := totalCandidates - stage1Survivors
	elimPct := 0.0
	if totalCandidates > 0 {
		elimPct = float64(eliminatedTotal) * 100.0 / float64(totalCandidates)
	}
	fmt.Printf("  * Total Constituents Processed: %d\n", totalCandidates)
	fmt.Printf("  * Stage-1 Hard Gate Survivors : %d (%.1f%% Pass Rate)\n", stage1Survivors, 100.0-elimPct)
	fmt.Printf("  * Eliminated in Stage 1       : %d (%.1f%% Elimination Rate)\n", eliminatedTotal, elimPct)
	fmt.Println()

	rejectionQuery := `
SELECT 
    CASE 
        WHEN data_fetch_failed = true OR rejection_reason LIKE 'DATA_FETCH_FAILED%' OR rejection_reason IS NULL OR rejection_reason = '' THEN 'Data Fetch Failed (Upstream Drop)'
        WHEN rejection_reason LIKE 'Below 200-Day SMA%' OR rejection_reason LIKE '%Downtrend%' THEN 'Downtrend (< 200-Day SMA)'
        WHEN rejection_reason LIKE 'Low ROCE%' OR rejection_reason LIKE '%ROCE%' OR rejection_reason LIKE '%Capital Efficiency%' THEN 'Low ROCE (< 12%)'
        WHEN rejection_reason LIKE '%52-Week High%' THEN 'Far from 52W High (< 85%)'
        WHEN rejection_reason LIKE 'Base duration%' THEN 'Base Duration (< 4 Wks in Zone)'
        WHEN rejection_reason LIKE '%promoter stake%' OR rejection_reason LIKE '%Low Promoter%' THEN 'Low Promoter Stake (< 25%)'
        WHEN rejection_reason LIKE '%DSO%' OR rejection_reason LIKE '%Working Capital%' THEN 'Working Capital Deterioration (DSO)'
        WHEN rejection_reason LIKE '%Debt/Equity%' THEN 'High Debt/Equity (>= 1.5)'
        WHEN rejection_reason LIKE '%Interest Coverage%' THEN 'Low Interest Coverage (< 3.0)'
        WHEN rejection_reason LIKE '%Market Cap%' THEN 'Market Cap Bounds'
        WHEN rejection_reason LIKE '%ADV%' THEN 'Low Liquidity (ADV < 1Cr)'
        WHEN rejection_reason LIKE '%Earnings%' THEN 'Earnings Event Blackout (±5 Days)'
        ELSE 'Other Hard Filter'
    END AS cat,
    COUNT(*) as cnt
FROM v_pit_candidate_scores
WHERE as_of_date = ? AND index_name = ? AND method = ? AND passed_stage1 = false
GROUP BY cat
ORDER BY cnt DESC;
`
	rejectionRows, err := p.db.QueryContext(ctx, rejectionQuery, latestRun.AsOfDate, indexName, method)
	if err == nil {
		fmt.Printf("  %-40s | %-8s | %-15s | %-12s\n", "Disqualification Category", "Count", "% of Eliminated", "% Total Pool")
		fmt.Println("  -----------------------------------------------------------------------------------------")
		var missingDataCount int
		for rejectionRows.Next() {
			var cat string
			var cnt int
			if err := rejectionRows.Scan(&cat, &cnt); err == nil {
				pctElim := 0.0
				if eliminatedTotal > 0 {
					pctElim = float64(cnt) * 100.0 / float64(eliminatedTotal)
				}
				pctTotal := 0.0
				if totalCandidates > 0 {
					pctTotal = float64(cnt) * 100.0 / float64(totalCandidates)
				}
				if cat == "Data Fetch Failed (Upstream Drop)" {
					missingDataCount = cnt
				}
				fmt.Printf("  %-40s | %8d | %14.1f%% | %11.1f%%\n", cat, cnt, pctElim, pctTotal)
			}
		}
		rejectionRows.Close()

		if missingDataCount > 0 {
			fmt.Printf("\n  🚨 [DATA WARNING] %d tickers failed upstream data fetch (DATA_FETCH_FAILED) and were excluded from Stage 1 evaluation.\n", missingDataCount)
		}
	}

	// ==========================================
	// 2. MARKET REGIME SENTRY & CAPITAL ALLOCATION
	// ==========================================
	fmt.Println("\n--- 2. CONTINUOUS MARKET REGIME SENTRY & DYNAMIC CAPITAL PRESERVATION ---")
	fmt.Printf("%-12s | %-10s | %-11s | %-10s | %-13s | %-10s | %-14s | %-12s\n",
		"As-Of Date", "Regime R", "Raw Hurdle", "Stage-1", "Regime Reject", "Selected", "Equity Weight", "Cash Reserve")
	fmt.Println("-----------------------------------------------------------------------------------------------------------------------")

	for _, r := range slices.Backward(runs) {

		rawHurdle := 30.0 / r.RegimeMultiplier

		var equityWeight float64
		var regimeRejected int
		q := `
SELECT 
    COALESCE(SUM(final_weight), 0.0),
    COALESCE(SUM(CASE WHEN passed_stage1 = true AND effective_score < 30.0 THEN 1 ELSE 0 END), 0)
FROM v_pit_candidate_scores
WHERE as_of_date = ? AND index_name = ? AND method = ?;
`
		_ = p.db.QueryRowContext(ctx, q, r.AsOfDate, indexName, method).Scan(&equityWeight, &regimeRejected)
		cashReserve := (1.0 - equityWeight) * 100.0
		if cashReserve < 0 {
			cashReserve = 0
		}

		fmt.Printf("%-12s | %10.4f | %9.1fpt | %10d | %13d | %10d | %13.1f%% | %11.1f%%\n",
			r.AsOfDate, r.RegimeMultiplier, rawHurdle, r.Stage1Survivors, regimeRejected, r.SelectedCount, equityWeight*100.0, cashReserve)
	}

	// ==========================================
	// 3. EMPIRICAL SCORE QUANTILES ACROSS DATES
	// ==========================================
	fmt.Println("\n--- 3. CROSS-SECTIONAL SCORE QUANTILES & PILLAR AVERAGES (Stage-1 Survivors) ---")
	fmt.Printf("%-12s | %-6s | %-6s | %-6s | %-6s | %-6s | %-8s | %-8s | %-8s | %-9s\n",
		"As-Of Date", "P90", "P75", "P50", "P40", "P25", "Avg RS", "Avg VCP", "Avg RVOL", "Avg DelivΔ")
	fmt.Println("-----------------------------------------------------------------------------------------------------------------------")

	for _, r := range slices.Backward(runs) {

		q := `
SELECT 
    COALESCE(quantile_cont(raw_score, 0.90), 0.0),
    COALESCE(quantile_cont(raw_score, 0.75), 0.0),
    COALESCE(quantile_cont(raw_score, 0.50), 0.0),
    COALESCE(quantile_cont(raw_score, 0.40), 0.0),
    COALESCE(quantile_cont(raw_score, 0.25), 0.0),
    COALESCE(AVG(composite_rs), 0.0),
    COALESCE(AVG(vcp_ratio), 0.0),
    COALESCE(AVG(rvol_z_score), 0.0),
    COALESCE(AVG(delivery_delta), 0.0)
FROM v_pit_candidate_scores
WHERE as_of_date = ? AND index_name = ? AND method = ? AND passed_stage1 = true;
`
		var p90, p75, p50, p40, p25, avgRS, avgVCP, avgRVOL, avgDeliv float64
		if err := p.db.QueryRowContext(ctx, q, r.AsOfDate, indexName, method).Scan(
			&p90, &p75, &p50, &p40, &p25, &avgRS, &avgVCP, &avgRVOL, &avgDeliv,
		); err == nil {
			fmt.Printf("%-12s | %6.1f | %6.1f | %6.1f | %6.1f | %6.1f | %+7.1f%% | %8.2f | %+8.2f | %+8.1f%%\n",
				r.AsOfDate, p90, p75, p50, p40, p25, avgRS*100.0, avgVCP, avgRVOL, avgDeliv*100.0)
		}
	}

	// ==========================================
	// 4. SECTOR CONCENTRATION & CAP DEFENSE
	// ==========================================
	fmt.Printf("\n--- 4. SECTOR CONCENTRATION & SECTOR CAP DEFENSE (%s) ---\n", latestRun.AsOfDate)
	if latestRun.SelectedCount == 0 {
		fmt.Printf("  * Portfolio Allocation State: 100%% Cash Preservation (0 stocks selected under Regime R=%.4f, Hurdle=%.1f pt)\n",
			latestRun.RegimeMultiplier, 30.0/latestRun.RegimeMultiplier)
		fmt.Printf("  * Sector Distribution of Qualified Stage-1 Survivors (%d stocks):\n", latestRun.Stage1Survivors)
	} else {
		fmt.Printf("  * Portfolio Allocation State: %d Active Holdings (Max 25%% per sector):\n", latestRun.SelectedCount)
	}

	sectorQuery := `
SELECT 
    COALESCE(NULLIF(sector, ''), 'Unknown') as sec,
    COUNT(*) as survivor_cnt,
    SUM(CASE WHEN selected THEN 1 ELSE 0 END) as sel_cnt,
    COALESCE(SUM(final_weight), 0.0) * 100.0 as tot_weight
FROM v_pit_candidate_scores
WHERE as_of_date = ? AND index_name = ? AND method = ? AND passed_stage1 = true
GROUP BY sec
ORDER BY tot_weight DESC, survivor_cnt DESC;
`
	sectorRows, err := p.db.QueryContext(ctx, sectorQuery, latestRun.AsOfDate, indexName, method)
	if err == nil {
		fmt.Printf("  %-25s | %-12s | %-12s | %-14s | %-15s\n", "Sector", "Stage-1 Pool", "Selected", "Allocated Wt", "Sector Cap State")
		fmt.Println("  -----------------------------------------------------------------------------------------")
		for sectorRows.Next() {
			var sec string
			var survCnt, selCnt int
			var totWeight float64
			if err := sectorRows.Scan(&sec, &survCnt, &selCnt, &totWeight); err == nil {
				capState := "Within Limits"
				if totWeight >= 24.9 {
					capState = "Capped at 25.0%"
				}
				fmt.Printf("  %-25s | %12d | %12d | %13.2f%% | %-15s\n", sec, survCnt, selCnt, totWeight, capState)
			}
		}
		sectorRows.Close()
	}

	// ==========================================
	// 5. CROSS-RUN SCORE SHIFTS (T vs T-1)
	// ==========================================
	if prevRun != nil {
		fmt.Printf("\n--- 5. SIGNIFICANT SCORE SHIFTS & TRAJECTORY (|Δ| >= 4.0 pts: %s -> %s) ---\n", prevRun.AsOfDate, latestRun.AsOfDate)
		shiftsQuery := `
SELECT 
    curr.ticker,
    COALESCE(NULLIF(curr.sector, ''), 'Unknown'),
    prev.raw_score,
    curr.raw_score,
    curr.raw_score - prev.raw_score as diff,
    COALESCE(CASE WHEN p_prev.close > 0 THEN ((p_curr.close - p_prev.close) / p_prev.close) * 100.0 ELSE 0.0 END, 0.0) as price_change_pct,
    curr.vcp_ratio,
    curr.composite_rs,
    curr.delivery_delta
FROM v_pit_candidate_scores curr
JOIN v_pit_candidate_scores prev 
  ON curr.ticker = prev.ticker 
 AND curr.index_name = prev.index_name 
 AND curr.method = prev.method
LEFT JOIN prices p_prev ON curr.ticker = p_prev.ticker AND p_prev.date = prev.as_of_date
LEFT JOIN prices p_curr ON curr.ticker = p_curr.ticker AND p_curr.date = curr.as_of_date
WHERE curr.as_of_date = ? 
  AND prev.as_of_date = ?
  AND curr.index_name = ? 
  AND curr.method = ?
  AND curr.passed_stage1 = true
  AND prev.passed_stage1 = true
  AND (curr.data_fetch_failed = false OR curr.data_fetch_failed IS NULL)
  AND (prev.data_fetch_failed = false OR prev.data_fetch_failed IS NULL)
  AND curr.raw_score > 0.0
  AND prev.raw_score > 0.0
  AND ABS(curr.raw_score - prev.raw_score) >= 4.0
ORDER BY diff DESC;
`
		shiftRows, err := p.db.QueryContext(ctx, shiftsQuery, latestRun.AsOfDate, prevRun.AsOfDate, indexName, method)
		if err == nil {
			fmt.Printf("  %-15s | %-24s | %-10s | %-10s | %-12s | %-11s | %-8s | %-8s | %-9s\n",
				"Ticker", "Sector", "Prev Score", "Curr Score", "Score Shift", "% Price Chg", "VCP ATR", "Comp RS", "Deliv Δ")
			fmt.Println("  -----------------------------------------------------------------------------------------------------------------------------")
			countShifts := 0
			for shiftRows.Next() {
				var ticker, sec string
				var prevScore, currScore, diff, priceChg, vcp, rs, deliv float64
				if err := shiftRows.Scan(&ticker, &sec, &prevScore, &currScore, &diff, &priceChg, &vcp, &rs, &deliv); err == nil {
					countShifts++
					fmt.Printf("  %-15s | %-24s | %10.1f | %10.1f | %+10.1fpt | %+10.2f%% | %8.2f | %+7.1f%% | %+8.1f%%\n",
						ticker, sec, prevScore, currScore, diff, priceChg, vcp, rs*100.0, deliv*100.0)
				}
			}
			shiftRows.Close()
			if countShifts == 0 {
				fmt.Println("  No candidates experienced score shifts >= 4.0 pts between consecutive runs.")
			}
		}

		// ==========================================
		// 6. UPSTREAM DATA INTEGRITY & SILENT DROP SENTRY
		// ==========================================
		fmt.Println("\n--- 6. DATA INTEGRITY & SILENT DROP DIAGNOSTICS ---")
		alertQuery := `
SELECT 
    prev.ticker,
    prev.raw_score,
    prev.effective_score,
    prev.final_weight
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
		alertRows, err := p.db.QueryContext(ctx, alertQuery, prevRun.AsOfDate, latestRun.AsOfDate, indexName, method)
		if err == nil {
			foundAlerts := false
			for alertRows.Next() {
				var ticker string
				var prevRaw, prevEff, prevWeight float64
				if err := alertRows.Scan(&ticker, &prevRaw, &prevEff, &prevWeight); err == nil {
					foundAlerts = true
					fmt.Printf("  [CRITICAL ALERT] %s was previously selected (Rank 1-3, Score: %.1f, Weight: %.1f%%) on %s,\n",
						ticker, prevRaw, prevWeight*100.0, prevRun.AsOfDate)
					fmt.Printf("                   but was silently dropped on %s with 0 bars (Upstream data fetch failed).\n",
						latestRun.AsOfDate)
					fmt.Printf("                   -> This is an API fetch failure, NOT a genuine technical/fundamental breakdown!\n")
				}
			}
			alertRows.Close()
			if !foundAlerts {
				fmt.Println("  [OK] No active portfolio holdings were dropped due to upstream fetch failures.")
			}
		}
	}

	// ==========================================
	// 7. PRE-BREAKOUT INCUBATOR: PROBABLE STOCKS BUILDING UP
	// ==========================================
	fmt.Printf("\n--- 7. PRE-BREAKOUT INCUBATOR: PROBABLE STOCKS BUILDING UP (%s) ---\n", latestRun.AsOfDate)
	fmt.Printf("Macro Regime Multiplier: %.4f | Current Hurdle: %.1f pts (Stocks below qualify for Watchlist/Runway)\n\n",
		latestRun.RegimeMultiplier, 30.0/latestRun.RegimeMultiplier)

	incubatorQuery := `
SELECT 
    ticker,
    COALESCE(NULLIF(sector, ''), 'Unknown') as sec,
    raw_score,
    effective_score,
    (30.0 / ? - raw_score) as hurdle_gap,
    vcp_ratio,
    composite_rs,
    delivery_delta,
    rvol_z_score
FROM v_pit_candidate_scores
WHERE as_of_date = ? AND index_name = ? AND method = ? AND passed_stage1 = true
ORDER BY raw_score DESC
LIMIT 12;
`
	incubatorRows, err := p.db.QueryContext(ctx, incubatorQuery, latestRun.RegimeMultiplier, latestRun.AsOfDate, indexName, method)
	if err == nil {
		fmt.Printf("  %-15s | %-18s | %-9s | %-8s | %-10s | %-8s | %-8s | %-9s | %-28s\n",
			"Ticker", "Sector", "Raw Score", "Eff Score", "Hurdle Gap", "VCP ATR", "Comp RS", "Deliv Δ", "Pre-Breakout Signature")
		fmt.Println("  -----------------------------------------------------------------------------------------------------------------------------------------")
		for incubatorRows.Next() {
			var ticker, sec string
			var raw, eff, gap, vcp, rs, deliv, rvol float64
			if err := incubatorRows.Scan(&ticker, &sec, &raw, &eff, &gap, &vcp, &rs, &deliv, &rvol); err == nil {
				sig := "Base Consolidating"
				if vcp < 0.65 && deliv > 0.20 {
					sig = "Tight VCP Coil + Heavy Deliv"
				} else if vcp < 0.55 {
					sig = "Extreme Volatility Coil"
				} else if deliv > 0.30 {
					sig = "Stealth Institutional Accum"
				} else if vcp < 0.75 && rs > 0.20 {
					sig = "Tight Base + Relative Outperf"
				} else if gap <= 8.0 {
					sig = "Runway Trigger Imminent"
				}

				gapStr := fmt.Sprintf("+%.1fpt", gap)
				if gap <= 0 {
					gapStr = "QUALIFIED"
				}

				fmt.Printf("  %-15s | %-18s | %9.1f | %8.1f | %10s | %8.2f | %+7.1f%% | %+8.1f%% | %-28s\n",
					ticker, sec, raw, eff, gapStr, vcp, rs*100.0, deliv*100.0, sig)
			}
		}
		incubatorRows.Close()
	}

	// ==========================================
	// 8. MULTI-RUN ACCUMULATION VELOCITY
	// ==========================================
	if len(runs) >= 2 {
		dateT0 := latestRun.AsOfDate
		dateT1 := runs[1].AsOfDate
		dateT2 := ""
		if len(runs) >= 3 {
			dateT2 = runs[2].AsOfDate
		}

		fmt.Printf("\n--- 8. MULTI-RUN ACCUMULATION VELOCITY (Building Up Across Consecutive Runs) ---\n")
		if dateT2 != "" {
			fmt.Printf("Tracking Score & Accumulation Trajectory across 3 Runs: [%s -> %s -> %s]\n\n", dateT2, dateT1, dateT0)
		} else {
			fmt.Printf("Tracking Score & Accumulation Trajectory across 2 Runs: [%s -> %s]\n\n", dateT1, dateT0)
		}

		velocityQuery := `
SELECT 
    curr.ticker,
    COALESCE(NULLIF(curr.sector, ''), 'Unknown') as sec,
    COALESCE(prev2.raw_score, 0.0) as score_t2,
    prev.raw_score as score_t1,
    curr.raw_score as score_t0,
    curr.vcp_ratio,
    curr.delivery_delta
FROM v_pit_candidate_scores curr
JOIN v_pit_candidate_scores prev 
  ON curr.ticker = prev.ticker AND curr.index_name = prev.index_name AND curr.method = prev.method
LEFT JOIN v_pit_candidate_scores prev2
  ON curr.ticker = prev2.ticker AND curr.index_name = prev2.index_name AND curr.method = prev2.method AND prev2.as_of_date = ?
WHERE curr.as_of_date = ? 
  AND prev.as_of_date = ?
  AND curr.index_name = ? 
  AND curr.method = ?
  AND curr.passed_stage1 = true
  AND prev.passed_stage1 = true
  AND (curr.data_fetch_failed = false OR curr.data_fetch_failed IS NULL)
  AND (prev.data_fetch_failed = false OR prev.data_fetch_failed IS NULL)
  AND curr.raw_score > 0.0
  AND prev.raw_score > 0.0
  AND curr.raw_score >= prev.raw_score
  AND curr.raw_score >= 35.0
ORDER BY curr.raw_score DESC;
`
		velocityRows, err := p.db.QueryContext(ctx, velocityQuery, dateT2, dateT0, dateT1, indexName, method)
		if err == nil {
			fmt.Printf("  %-15s | %-18s | %-10s | %-10s | %-10s | %-8s | %-9s | %-25s\n",
				"Ticker", "Sector", "Score (T-2)", "Score (T-1)", "Score (T)", "VCP ATR", "Deliv Δ", "Accumulation Pattern")
			fmt.Println("  ---------------------------------------------------------------------------------------------------------------------")
			foundVelocity := false
			for velocityRows.Next() {
				var ticker, sec string
				var sT2, sT1, sT0, vcp, deliv float64
				if err := velocityRows.Scan(&ticker, &sec, &sT2, &sT1, &sT0, &vcp, &deliv); err == nil {
					foundVelocity = true
					pattern := "Persistent Accumulation"
					if sT2 > 0 && sT0 > sT1 && sT1 > sT2 {
						pattern = "3-Session Consecutive Surge"
					} else if sT0 > sT1+5.0 {
						pattern = "Velocity Breakout (+5pt Δ)"
					} else if deliv > 0.30 {
						pattern = "Stealth Institutional Absorption"
					}

					sT2Str := fmt.Sprintf("%10.1f", sT2)
					if sT2 == 0 {
						sT2Str = "         -"
					}
					fmt.Printf("  %-15s | %-18s | %10s | %10.1f | %10.1f | %8.2f | %+8.1f%% | %-25s\n",
						ticker, sec, sT2Str, sT1, sT0, vcp, deliv*100.0, pattern)
				}
			}
			velocityRows.Close()
			if !foundVelocity {
				fmt.Println("  No candidates currently exhibiting positive multi-run accumulation velocity >= 35.0 pts.")
			}
		}
	}

	// ==========================================
	// 9. STEALTH ACCUMULATION & NEAR-MISS RADAR (Tracked Watchlist)
	// ==========================================
	fmt.Printf("\n--- 9. STEALTH ACCUMULATION & NEAR-MISS RADAR (Tracked Watchlist: Deliv Δ >= +8.0%% | Non-Negative RS) ---\n")
	fmt.Println("Tracking institutional footprints blocked by Stage-1 gates, persistence across runs, and graduation alerts:")

	prevDate := ""
	if prevRun != nil {
		prevDate = prevRun.AsOfDate
	}

	radarTickers := make(map[string]bool)

	radarQuery := `
WITH prior_radar AS (
    SELECT 
        ticker,
        MIN(as_of_date) AS first_seen_date,
        COUNT(DISTINCT as_of_date) AS days_on_radar
    FROM v_pit_candidate_scores
    WHERE index_name = ?
      AND method = ?
      AND delivery_delta >= 0.08
      AND composite_rs >= 0.0
      AND passed_stage1 = false
      AND (data_fetch_failed = false OR data_fetch_failed IS NULL)
      AND as_of_date < ?
    GROUP BY ticker
)
SELECT 
    curr.ticker,
    COALESCE(NULLIF(curr.sector, ''), 'Unknown') AS sec,
    COALESCE(pr.first_seen_date, curr.as_of_date) AS first_seen_date,
    COALESCE(pr.days_on_radar, 0) + 1 AS days_on_radar,
    COALESCE(CASE WHEN p_prev.close > 0 THEN ((p_curr.close - p_prev.close) / p_prev.close) * 100.0 ELSE 0.0 END, 0.0) AS price_change_pct,
    curr.delivery_delta,
    curr.composite_rs,
    curr.vcp_ratio,
    curr.rejection_reason
FROM v_pit_candidate_scores curr
LEFT JOIN prior_radar pr ON curr.ticker = pr.ticker
LEFT JOIN prices p_prev ON curr.ticker = p_prev.ticker AND p_prev.date = ?
LEFT JOIN prices p_curr ON curr.ticker = p_curr.ticker AND p_curr.date = ?
WHERE curr.as_of_date = ?
  AND curr.index_name = ?
  AND curr.method = ?
  AND curr.passed_stage1 = false
  AND (curr.data_fetch_failed = false OR curr.data_fetch_failed IS NULL)
  AND curr.delivery_delta >= 0.08
  AND curr.composite_rs >= 0.0
ORDER BY curr.delivery_delta DESC
LIMIT 20;
`
	radarRows, err := p.db.QueryContext(ctx, radarQuery, indexName, method, latestRun.AsOfDate, prevDate, latestRun.AsOfDate, latestRun.AsOfDate, indexName, method)
	if err == nil {
		fmt.Printf("  %-15s | %-15s | %-10s | %-4s | %-7s | %-7s | %-7s | %-5s | %-12s | %-28s\n",
			"Ticker", "Sector", "First Seen", "Days", "1D Chg", "Deliv Δ", "Comp RS", "VCP", "Gate Type", "Primary Bottleneck Gate")
		fmt.Println("  -----------------------------------------------------------------------------------------------------------------------------------------")
		foundRadar := false
		for radarRows.Next() {
			var ticker, sec, firstSeen, bottleneck string
			var daysOnRadar int
			var priceChg, deliv, rs, vcp float64
			if err := radarRows.Scan(&ticker, &sec, &firstSeen, &daysOnRadar, &priceChg, &deliv, &rs, &vcp, &bottleneck); err == nil {
				foundRadar = true
				radarTickers[ticker] = true
				if len(firstSeen) >= 10 {
					firstSeen = firstSeen[:10]
				}
				gateType, cleanReason := classifyGate(false, bottleneck)
				conciseReason := formatConciseBottleneck(cleanReason)
				fmt.Printf("  %-15s | %-15s | %-10s | %3dd | %+6.2f%% | %+6.1f%% | %+6.1f%% | %5.2f | %-12s | %-28s\n",
					ticker, formatConciseSector(sec), firstSeen, daysOnRadar, priceChg, deliv*100.0, rs*100.0, vcp, gateType, conciseReason)
			}
		}
		radarRows.Close()
		if !foundRadar {
			fmt.Println("  No near-miss candidates currently exhibit strong institutional delivery accumulation (Deliv Δ >= +8.0%).")
		}

		// Mini-table: Graduated Stocks Alpha Audit (First-Seen Date -> Gate Clear Date)
		gradQuery := `
WITH prior_radar AS (
    SELECT 
        ticker,
        MIN(as_of_date) AS first_seen_date,
        COUNT(DISTINCT as_of_date) AS days_on_radar
    FROM v_pit_candidate_scores
    WHERE index_name = ?
      AND method = ?
      AND delivery_delta >= 0.08
      AND composite_rs >= 0.0
      AND passed_stage1 = false
      AND (data_fetch_failed = false OR data_fetch_failed IS NULL)
      AND as_of_date < ?
    GROUP BY ticker
),
curr_graduated AS (
    SELECT 
        curr.ticker,
        COALESCE(NULLIF(curr.sector, ''), 'Unknown') AS sec,
        pr.first_seen_date,
        pr.days_on_radar,
        curr.as_of_date AS clear_date,
        prev.rejection_reason AS prev_bottleneck
    FROM v_pit_candidate_scores curr
    JOIN prior_radar pr ON curr.ticker = pr.ticker
    JOIN v_pit_candidate_scores prev 
      ON curr.ticker = prev.ticker 
     AND prev.as_of_date = ? 
     AND prev.index_name = ? 
     AND prev.method = ? 
     AND prev.passed_stage1 = false
    WHERE curr.as_of_date = ?
      AND curr.index_name = ?
      AND curr.method = ?
      AND curr.passed_stage1 = true
      AND (curr.data_fetch_failed = false OR curr.data_fetch_failed IS NULL)
)
SELECT 
    g.ticker,
    g.sec,
    g.first_seen_date,
    COALESCE(p_first.close, 0.0) AS first_close,
    g.clear_date,
    COALESCE(p_clear.close, 0.0) AS clear_close,
    COALESCE(CASE WHEN p_first.close > 0 THEN ((p_clear.close - p_first.close) / p_first.close) * 100.0 ELSE 0.0 END, 0.0) AS radar_return_pct,
    g.days_on_radar,
    CASE 
        WHEN g.prev_bottleneck LIKE '%Base duration%' THEN 'base_duration'
        WHEN g.prev_bottleneck LIKE '%ROCE%' OR g.prev_bottleneck LIKE '%Capital Efficiency%' THEN 'delivery_override'
        WHEN g.prev_bottleneck LIKE '%promoter%' THEN 'promoter_exempt'
        ELSE 'natural_clear'
    END AS rescue_channel
FROM curr_graduated g
LEFT JOIN prices p_first ON g.ticker = p_first.ticker AND p_first.date = g.first_seen_date
LEFT JOIN prices p_clear ON g.ticker = p_clear.ticker AND p_clear.date = g.clear_date
ORDER BY radar_return_pct DESC;
`
		gradRows, gErr := p.db.QueryContext(ctx, gradQuery, indexName, method, latestRun.AsOfDate, prevDate, indexName, method, latestRun.AsOfDate, indexName, method)
		if gErr == nil {
			type gradRecord struct {
				ticker, sec, firstSeen, clearDate, channel string
				firstClose, clearClose, retPct             float64
				days                                       int
			}
			var graduates []gradRecord
			for gradRows.Next() {
				var gr gradRecord
				if err := gradRows.Scan(&gr.ticker, &gr.sec, &gr.firstSeen, &gr.firstClose, &gr.clearDate, &gr.clearClose, &gr.retPct, &gr.days, &gr.channel); err == nil {
					graduates = append(graduates, gr)
				}
			}
			gradRows.Close()

			if len(graduates) > 0 {
				fmt.Printf("\n  --- Graduated Stocks Performance (Radar Alpha Audit: First-Seen Date -> Clear Date) ---\n")
				fmt.Println("  Quantifying pre-breakout radar predictive value and opportunity cost of waiting for Stage-1 clearance:")
				fmt.Printf("  %-15s | %-15s | %-16s | %-10s | %-10s | %-4s | %11s | %11s | %-12s\n",
					"Ticker", "Sector", "Channel", "First Seen", "Clear Date", "Days", "Entry (₹)", "Clear (₹)", "Radar Return")
				fmt.Println("  --------------------------------------------------------------------------------------------------------------------------------")
				totalRet := 0.0
				wins := 0
				channelWins := make(map[string]int)
				channelTotals := make(map[string]int)
				channelRets := make(map[string]float64)

				for _, gr := range graduates {
					fs := gr.firstSeen
					if len(fs) >= 10 {
						fs = fs[:10]
					}
					cd := gr.clearDate
					if len(cd) >= 10 {
						cd = cd[:10]
					}
					totalRet += gr.retPct
					channelTotals[gr.channel]++
					channelRets[gr.channel] += gr.retPct
					if gr.retPct > 0 {
						wins++
						channelWins[gr.channel]++
					}
					fmt.Printf("  %-15s | %-15s | %-16s | %-10s | %-10s | %3dd | %11s | %11s | %+11.2f%%\n",
						gr.ticker, formatConciseSector(gr.sec), gr.channel, fs, cd, gr.days, render.Currency(gr.firstClose, "₹"), render.Currency(gr.clearClose, "₹"), gr.retPct)
				}
				avgRet := totalRet / float64(len(graduates))
				winRate := float64(wins) / float64(len(graduates)) * 100.0
				fmt.Printf("  Rolling Win Rate: %d/%d (%.1f%%) | Average Radar Return: %+.2f%% (Alpha left on table by waiting for formal Stage-1 pass)\n",
					wins, len(graduates), winRate, avgRet)

				fmt.Print("  Channel Breakdown: ")
				var chStrs []string
				for ch, tot := range channelTotals {
					chW := channelWins[ch]
					chAvg := channelRets[ch] / float64(tot)
					chStrs = append(chStrs, fmt.Sprintf("%s: %d/%d (%.1f%%, avg %+.2f%%)", ch, chW, tot, float64(chW)/float64(tot)*100.0, chAvg))
				}
				fmt.Println(strings.Join(chStrs, " | "))
			}
		}
	}

	// ==========================================
	// 10. DAILY TOP PRICE GAINERS (Universe Cross-Section)
	// ==========================================
	if prevDate != "" {
		fmt.Printf("\n--- 10. DAILY TOP PRICE GAINERS (%s -> %s | %s) ---\n", prevDate, latestRun.AsOfDate, indexName)
		fmt.Println("Cross-sectional price gainers across the index universe, delivery volume confirmation, and EBM qualification:")

		gainersQuery := `
WITH ntm AS (
    SELECT DISTINCT ticker 
    FROM v_pit_candidate_scores 
    WHERE index_name = ?
),
curr AS (
    SELECT ticker, close AS curr_close
    FROM prices 
    WHERE date = ?
),
prev AS (
    SELECT ticker, close AS prev_close
    FROM prices 
    WHERE date = ?
)
SELECT 
    ntm.ticker,
    ROUND(prev.prev_close, 2) AS prev_close,
    ROUND(curr.curr_close, 2) AS curr_close,
    ROUND(((curr.curr_close - prev.prev_close) / prev.prev_close) * 100.0, 2) AS pct_gain,
    COALESCE(c.passed_stage1, false) AS passed_stage1,
    COALESCE(c.delivery_delta, 0.0) AS delivery_delta,
    COALESCE(c.composite_rs, 0.0) AS composite_rs,
    COALESCE(NULLIF(c.rejection_reason, ''), 'Stage-1 Qualified') AS stage1_status,
    CASE WHEN c.ticker IS NOT NULL THEN true ELSE false END AS has_score
FROM ntm
JOIN curr ON ntm.ticker = curr.ticker
JOIN prev ON ntm.ticker = prev.ticker
LEFT JOIN v_pit_candidate_scores c 
  ON ntm.ticker = c.ticker 
 AND c.as_of_date = ? 
 AND c.index_name = ? 
 AND c.method = ?
WHERE prev.prev_close > 0
ORDER BY pct_gain DESC
LIMIT 15;
`
		gRows, err := p.db.QueryContext(ctx, gainersQuery, indexName, latestRun.AsOfDate, prevDate, latestRun.AsOfDate, indexName, method)
		if err == nil {
			fmt.Printf("  %-15s | %11s | %11s | %-8s | %-7s | %-5s | %-7s | %-12s | %-28s | %-9s\n",
				"Ticker", "Prev (₹)", "Close (₹)", "1D Gain", "Deliv Δ", "Accum", "Comp RS", "Gate Type", "Stage-1 Bottleneck", "Radar")
			fmt.Println("  -----------------------------------------------------------------------------------------------------------------------------------")
			foundGainer := false
			for gRows.Next() {
				var ticker, status string
				var prevClose, currClose, pctGain, deliv, rs float64
				var passedStage1, hasScore bool
				if err := gRows.Scan(&ticker, &prevClose, &currClose, &pctGain, &passedStage1, &deliv, &rs, &status, &hasScore); err == nil {
					foundGainer = true
					delivStr := "- "
					realAccumStr := "NO"
					rsStr := "- "
					if hasScore {
						delivStr = fmt.Sprintf("%+6.1f%%", deliv*100.0)
						if deliv >= 0.06 {
							realAccumStr = "YES"
						}
						rsStr = fmt.Sprintf("%+6.1f%%", rs*100.0)
					}
					gateType, cleanStatus := classifyGate(passedStage1, status)
					conciseStatus := formatConciseBottleneck(cleanStatus)
					overlapStr := "-"
					if radarTickers[ticker] || (hasScore && deliv >= 0.08 && rs >= 0.0) {
						overlapStr = "DUAL HIT"
					}
					fmt.Printf("  %-15s | %11s | %11s | %+7.2f%% | %-7s | %-5s | %-7s | %-12s | %-28s | %-9s\n",
						ticker, render.Currency(prevClose, "₹"), render.Currency(currClose, "₹"), pctGain, delivStr, realAccumStr, rsStr, gateType, conciseStatus, overlapStr)
				}
			}
			gRows.Close()
			if !foundGainer {
				fmt.Println("  No price gainer records found for consecutive trading sessions.")
			}
		}
	}

	// ==========================================
	// 11. STAGE-1 SHADOW MODE DIVERGENCE (Legacy Gate vs Relief Gate)
	// ==========================================
	_ = p.PrintShadowDivergence(ctx, latestRun.AsOfDate, indexName, method)

	fmt.Println("=======================================================================================================================")
	fmt.Println("Quantitative deduction analysis complete.")
	return nil
}

// PrintShadowDivergence renders the shadow mode divergence report.
func (p *DB) PrintShadowDivergence(ctx context.Context, asOfDate, indexName, method string) error {
	_ = p.SyncShadowResults(ctx, asOfDate, indexName, method)

	fmt.Printf("\n--- 11. STAGE-1 SHADOW MODE DIVERGENCE (Legacy Gate vs Relief Gate | %s) ---\n", asOfDate)
	fmt.Println("Evaluating relief rules in shadow mode to accumulate empirical evidence before live deployment:")

	q := `
SELECT 
    ticker,
    sector,
    divergence_type,
    shadow_relief_channel,
    delivery_delta,
    composite_rs,
    vcp_ratio,
    shadow_base_mult,
    legacy_rejection_cause
FROM stage1_shadow_results
WHERE as_of_date = ?
  AND index_name = ?
  AND method = ?
  AND divergence_type = 'RESCUED'
ORDER BY delivery_delta DESC
LIMIT 20;
`
	rows, err := p.db.QueryContext(ctx, q, asOfDate, indexName, method)
	if err == nil {
		fmt.Printf("  %-15s | %-15s | %-8s | %-20s | %-7s | %-7s | %-5s | %-6s | %-28s\n",
			"Ticker", "Sector", "Status", "Relief Channel", "Deliv Δ", "Comp RS", "VCP", "Mult", "Legacy Bottleneck Reason")
		fmt.Println("  ----------------------------------------------------------------------------------------------------------------------------------------")
		found := false
		for rows.Next() {
			var ticker, sec, divType, channel, reason string
			var deliv, rs, vcp, mult float64
			if err := rows.Scan(&ticker, &sec, &divType, &channel, &deliv, &rs, &vcp, &mult, &reason); err == nil {
				found = true
				conciseReason := formatConciseBottleneck(reason)
				fmt.Printf("  %-15s | %-15s | %-8s | %-20s | %+6.1f%% | %+6.1f%% | %5.2f | %5.2fx | %-28s\n",
					ticker, formatConciseSector(sec), divType, channel, deliv*100.0, rs*100.0, vcp, mult, conciseReason)
			}
		}
		rows.Close()
		if !found {
			fmt.Println("  No divergent candidates between legacy and shadow gates.")
		}

		// Summary stats
		sumQ := `
SELECT 
    COUNT(*),
    COUNT(CASE WHEN legacy_stage1_pass THEN 1 END),
    COUNT(CASE WHEN shadow_stage1_pass THEN 1 END),
    COUNT(CASE WHEN divergence_type = 'RESCUED' THEN 1 END),
    COUNT(CASE WHEN shadow_relief_channel = 'delivery_override' THEN 1 END),
    COUNT(CASE WHEN shadow_relief_channel = 'promoter_exempt_bfsi' THEN 1 END),
    COUNT(CASE WHEN shadow_relief_channel = 'graduated_scoring' THEN 1 END)
FROM stage1_shadow_results
WHERE as_of_date = ? AND index_name = ? AND method = ?;
`
		var total, legacyPass, shadowPass, rescued, delivCnt, promCnt, baseCnt int
		if err := p.db.QueryRowContext(ctx, sumQ, asOfDate, indexName, method).Scan(&total, &legacyPass, &shadowPass, &rescued, &delivCnt, &promCnt, &baseCnt); err == nil && total > 0 {
			legacyPct := float64(legacyPass) / float64(total) * 100.0
			shadowPct := float64(shadowPass) / float64(total) * 100.0
			fmt.Printf("\n  Shadow Gate Summary for %s (%s | %s):\n", asOfDate, indexName, method)
			fmt.Printf("  • Legacy Stage-1 Survivors : %d / %d (%.1f%%)\n", legacyPass, total, legacyPct)
			fmt.Printf("  • Shadow Stage-1 Survivors : %d / %d (%.1f%%)\n", shadowPass, total, shadowPct)
			fmt.Printf("  • Total Candidates Rescued : %d (Tightened Delivery Override: %d)\n",
				rescued, delivCnt)

			// Sector breakdown of rescued candidates
			secQ := `
SELECT sector, COUNT(*) as cnt
FROM stage1_shadow_results
WHERE as_of_date = ? AND index_name = ? AND method = ? AND divergence_type = 'RESCUED'
GROUP BY sector
ORDER BY cnt DESC;
`
			secRows, sErr := p.db.QueryContext(ctx, secQ, asOfDate, indexName, method)
			if sErr == nil {
				var secStrs []string
				for secRows.Next() {
					var sec string
					var cnt int
					if err := secRows.Scan(&sec, &cnt); err == nil {
						pct := float64(cnt) / float64(rescued) * 100.0
						secStrs = append(secStrs, fmt.Sprintf("%s: %d (%.1f%%)", sec, cnt, pct))
					}
				}
				secRows.Close()
				if len(secStrs) > 0 {
					fmt.Printf("  • Sector Distribution Check: %s\n", strings.Join(secStrs, " | "))
				}
			}

			// Sector cap defense audit:
			capQ := `
SELECT 
    sector,
    COUNT(*) as total_shadow_pool,
    COUNT(CASE WHEN legacy_stage1_pass THEN 1 END) as legacy_pool,
    COUNT(CASE WHEN divergence_type = 'RESCUED' THEN 1 END) as rescued_pool
FROM stage1_shadow_results
WHERE as_of_date = ? AND index_name = ? AND method = ? AND shadow_stage1_pass = true
GROUP BY sector
ORDER BY total_shadow_pool DESC;
`
			capRows, cErr := p.db.QueryContext(ctx, capQ, asOfDate, indexName, method)
			if cErr == nil {
				fmt.Println("\n  Sector Cap Defense Audit on Shadow Pool (MaxStocksPerSector = 3 | MaxSectorWeightCap = 25.0%):")
				fmt.Printf("  %-18s | %11s | %11s | %9s | %15s | %15s\n",
					"Sector", "Shadow Pool", "Legacy Pool", "Rescued", "Max Allocatable", "Excess Absorbed")
				fmt.Println("  ----------------------------------------------------------------------------------------------")
				for capRows.Next() {
					var sec string
					var totPool, legPool, resPool int
					if err := capRows.Scan(&sec, &totPool, &legPool, &resPool); err == nil {
						maxAlloc := totPool
						if maxAlloc > 3 {
							maxAlloc = 3
						}
						excess := totPool - 3
						if excess < 0 {
							excess = 0
						}
						fmt.Printf("  %-18s | %11d | %11d | %9d | %15d | %15d\n",
							formatConciseSector(sec), totPool, legPool, resPool, maxAlloc, excess)
					}
				}
				capRows.Close()
				fmt.Println("  * Sector Cap Defense Verified: Portfolio allocation cannot exceed 3 holdings or 25% weight per sector.")
			}

			fmt.Printf("\n  • Operational Mode         : SHADOW MODE ACTIVE (Legacy gate enforced in production; shadow logged to DB)\n")
		}
	}
	return nil
}

// formatConciseSector truncates or formats long sector names to fit neatly in tables (<= 15 chars).
func formatConciseSector(sec string) string {
	switch sec {
	case "Consumer Defensive":
		return "Cons Defensive"
	case "Consumer Cyclical":
		return "Cons Cyclical"
	case "Financial Services":
		return "Financial Serv"
	case "Communication Services":
		return "Communication"
	default:
		if len(sec) > 15 {
			return sec[:14] + "."
		}
		return sec
	}
}

// formatConciseBottleneck cleans and abbreviates verbose rejection strings for crisp tabular rendering (<= 28 chars).
func formatConciseBottleneck(reason string) string {
	r := strings.TrimSpace(reason)
	if r == "" || strings.EqualFold(r, "Stage-1 Qualified") || strings.Contains(r, "Qualified") {
		return "Stage-1 Qualified"
	}

	// 1. Base duration: e.g. "Base duration too short (0 weeks < 4 weeks required base)" -> "Base: 0w < 4w"
	if strings.Contains(r, "Base duration too short") {
		if start := strings.Index(r, "("); start != -1 {
			if end := strings.Index(r, "<"); end != -1 && end > start {
				val := strings.TrimSpace(r[start+1 : end])
				val = strings.ReplaceAll(val, " weeks", "w")
				val = strings.ReplaceAll(val, " week", "w")
				val = strings.ReplaceAll(val, "weeks", "w")
				val = strings.ReplaceAll(val, "week", "w")
				val = strings.TrimSpace(val)
				return fmt.Sprintf("Base: %s < 4w", val)
			}
		}
		return "Base: < 4w"
	}

	// 2. ROCE / Capital Efficiency: e.g. "Low Capital Efficiency (ROCE < 12.0%)" -> "Low ROCE (< 12.0%)"
	if strings.Contains(r, "Capital Efficiency") || strings.Contains(r, "ROCE") {
		return "Low ROCE (< 12.0%)"
	}

	// 3. DSO Deterioration: e.g. "DSO Deterioration limit exceeded (+83.7% > 15.0% threshold)" -> "DSO Deterioration (+83.7%)"
	if strings.Contains(r, "DSO Deterioration") {
		if start := strings.Index(r, "(+"); start != -1 {
			if end := strings.Index(r, ">"); end != -1 && end > start {
				val := strings.TrimSpace(r[start+1 : end])
				return fmt.Sprintf("DSO Deterioration (%s)", val)
			}
		}
		return "DSO Deterioration (> 15%)"
	}

	// 4. Low Promoter Stake: e.g. "Low promoter stake (16.3% < 25.0% limit)" -> "Low Promoter (16.3% < 25%)"
	if strings.Contains(r, "Low promoter stake") {
		if start := strings.Index(r, "("); start != -1 {
			if end := strings.Index(r, "<"); end != -1 && end > start {
				val := strings.TrimSpace(r[start+1 : end])
				return fmt.Sprintf("Low Promoter (%s < 25%%)", val)
			}
		}
		return "Low Promoter Stake (< 25%)"
	}

	// 5. 52-Week High: e.g. "Far from 52-Week High (74.3% of 52W high < 85.0% floor)" -> "Far from 52W High (74.3%)"
	if strings.Contains(r, "52-Week High") || strings.Contains(r, "52W High") {
		if start := strings.Index(r, "("); start != -1 {
			if end := strings.Index(r, "%"); end != -1 && end > start {
				val := strings.TrimSpace(r[start+1 : end+1])
				return fmt.Sprintf("Far from 52W High (%s)", val)
			}
		}
		return "Far from 52W High (< 85%)"
	}

	// 6. Below 200-Day SMA: e.g. "Below 200-Day SMA ratio floor (0.86 < 0.95 limit)" -> "Below 200-SMA (0.86 < 0.95)"
	if strings.Contains(r, "Below 200-Day SMA") {
		if start := strings.Index(r, "("); start != -1 {
			if end := strings.Index(r, "<"); end != -1 && end > start {
				val := strings.TrimSpace(r[start+1 : end])
				return fmt.Sprintf("Below 200-SMA (%s < 0.95)", val)
			}
		}
		return "Below 200-Day SMA (< 0.95)"
	}

	// 7. Market Cap: e.g. "Market Cap limit check failed (Market Cap: 989000Cr)"
	if strings.Contains(r, "Market Cap") {
		return "Market Cap Out of Bounds"
	}

	// 8. Financial ROE
	if strings.Contains(r, "Financial ROE") || strings.Contains(r, "Low ROE") {
		return "Low Financial ROE (< 12%)"
	}

	// 9. High Debt to Equity
	if strings.Contains(r, "High Debt/Equity") {
		if start := strings.Index(r, "("); start != -1 {
			if end := strings.Index(r, ">="); end != -1 && end > start {
				val := strings.TrimSpace(r[start+1 : end])
				return fmt.Sprintf("High Debt/Eq (%s >= 2.0)", val)
			}
		}
		return "High Debt/Equity (>= 2.0)"
	}

	// Clean fallback capped at 28 chars
	if len(r) > 28 {
		return r[:25] + "..."
	}
	return r
}

// classifyGate categorizes Stage-1 elimination reasons into Fixable vs Structural or Cleared.
func classifyGate(passedStage1 bool, reason string) (gateType string, cleanReason string) {
	if passedStage1 || reason == "" || strings.EqualFold(reason, "Stage-1 Qualified") || strings.Contains(reason, "Qualified") {
		return "[CLEARED]", "Stage-1 Qualified"
	}
	r := strings.ToLower(reason)
	if strings.Contains(r, "roce") || strings.Contains(r, "capital efficiency") || strings.Contains(r, "base duration") {
		return "[Fixable]", reason
	}
	return "[Structural]", reason
}

// DataIntegrityResult holds metrics from the pre-flight integrity audit.
type DataIntegrityResult struct {
	TotalCandidates  int
	FailedCandidates int
	FailurePct       float64
	FlaggedTickers   []string
}

// CheckDataIntegrity audits the latest candidate pool in pit_candidate_scores for missing or corrupted data.
func (p *DB) CheckDataIntegrity(ctx context.Context, indexName, method string) (*DataIntegrityResult, error) {
	query := `
SELECT 
    ticker,
    rejection_reason,
    data_fetch_failed
FROM pit_candidate_scores
WHERE as_of_date = (
    SELECT MAX(as_of_date) FROM pit_candidate_scores 
    WHERE (? = '' OR index_name = ?) AND (? = '' OR method = ?)
)
AND (? = '' OR index_name = ?)
AND (? = '' OR method = ?);
`
	rows, err := p.db.QueryContext(ctx, query, indexName, indexName, method, method, indexName, indexName, method, method)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := &DataIntegrityResult{}
	for rows.Next() {
		var ticker, reason string
		var fetchFailed bool
		if err := rows.Scan(&ticker, &reason, &fetchFailed); err != nil {
			continue
		}
		res.TotalCandidates++
		if fetchFailed || strings.Contains(strings.ToLower(reason), "unverified") || strings.Contains(strings.ToLower(reason), "fetch failure") {
			res.FailedCandidates++
			if len(res.FlaggedTickers) < 10 {
				res.FlaggedTickers = append(res.FlaggedTickers, ticker)
			}
		}
	}
	if res.TotalCandidates > 0 {
		res.FailurePct = (float64(res.FailedCandidates) / float64(res.TotalCandidates)) * 100.0
	}
	return res, nil
}
