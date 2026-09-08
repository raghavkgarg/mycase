package pithistory

import (
	"context"
	"fmt"
	"slices"
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

// RunDeepAnalysis performs institutional deduction analytics purely from DuckDB tables.
func (p *DB) RunDeepAnalysis(ctx context.Context, indexName, method string) error {
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
FROM pit_candidate_scores
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
FROM pit_candidate_scores
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
FROM pit_candidate_scores
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
FROM pit_candidate_scores
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
    curr.vcp_ratio,
    curr.composite_rs,
    curr.delivery_delta
FROM pit_candidate_scores curr
JOIN pit_candidate_scores prev 
  ON curr.ticker = prev.ticker 
 AND curr.index_name = prev.index_name 
 AND curr.method = prev.method
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
			fmt.Printf("  %-15s | %-20s | %-10s | %-10s | %-10s | %-8s | %-8s | %-9s\n",
				"Ticker", "Sector", "Prev Score", "Curr Score", "Score Shift", "VCP ATR", "Comp RS", "Deliv Δ")
			fmt.Println("  -------------------------------------------------------------------------------------------------------")
			countShifts := 0
			for shiftRows.Next() {
				var ticker, sec string
				var prevScore, currScore, diff, vcp, rs, deliv float64
				if err := shiftRows.Scan(&ticker, &sec, &prevScore, &currScore, &diff, &vcp, &rs, &deliv); err == nil {
					countShifts++
					fmt.Printf("  %-15s | %-20s | %10.1f | %10.1f | %+9.1fpt | %8.2f | %+7.1f%% | %+8.1f%%\n",
						ticker, sec, prevScore, currScore, diff, vcp, rs*100.0, deliv*100.0)
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
FROM pit_candidate_scores prev
JOIN pit_candidate_scores curr
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
FROM pit_candidate_scores
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
FROM pit_candidate_scores curr
JOIN pit_candidate_scores prev 
  ON curr.ticker = prev.ticker AND curr.index_name = prev.index_name AND curr.method = prev.method
LEFT JOIN pit_candidate_scores prev2
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

	fmt.Println("=======================================================================================================================")
	fmt.Println("Quantitative deduction analysis complete.")
	return nil
}
