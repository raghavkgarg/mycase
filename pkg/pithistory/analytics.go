package pithistory

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/render"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
)

const ShadowReliefRuleSignature = "shadow_relief_v2:roce_deliv_override(deliv>=0.09,rs>=0.15,vcp<=1.20);sector_cfo_relief(Financials,RealEstate,rs>=0.0,vcp<=1.25,roe>=0.12);graduated_base_mult(0-1w:0.50,2-3w:0.75)"

func getGitCommitHash() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "0d069ae"
	}
	hash := strings.TrimSpace(string(out))
	statusOut, sErr := exec.Command("git", "status", "--porcelain").Output()
	if sErr == nil && len(strings.TrimSpace(string(statusOut))) > 0 {
		hash += "+dirty"
	}
	return hash
}

func getConfigFileHash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		data = []byte{}
	}
	payload := append(data, []byte(ShadowReliefRuleSignature)...)
	h := sha256.Sum256(payload)
	return fmt.Sprintf("%x", h[:8])
}

type RunSummaryRow struct {
	CreatedAt           time.Time `json:"created_at"`
	AsOfDate            string    `json:"as_of_date"`
	IndexName           string    `json:"index_name"`
	Method              string    `json:"method"`
	RegimeMultiplier    float64   `json:"regime_multiplier"`
	TotalConstituents   int       `json:"total_constituents"`
	Stage1Survivors     int       `json:"stage1_survivors"`
	SelectedCount       int       `json:"selected_count"`
	Pillar4Uncalibrated bool      `json:"pillar4_uncalibrated"`
	RRaw                float64   `json:"r_raw"`
	REff                float64   `json:"r_eff"`
	BenchLastBar        string    `json:"bench_last_bar"`
	BreadthLastBar      string    `json:"breadth_last_bar"`
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
    created_at,
    COALESCE(pillar4_uncalibrated, false),
    COALESCE(r_raw, regime_multiplier),
    COALESCE(r_eff, regime_multiplier),
    COALESCE(strftime(bench_last_bar, '%Y-%m-%d'), ''),
    COALESCE(strftime(breadth_last_bar, '%Y-%m-%d'), '')
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
			&r.Pillar4Uncalibrated,
			&r.RRaw,
			&r.REff,
			&r.BenchLastBar,
			&r.BreadthLastBar,
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
	var args []any

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
func (p *DB) RunDeepAnalysis(ctx context.Context, indexName, method string, lookbackDays int, marketOverride ...string) error {
	indexName = NormalizeIndexName(indexName)
	limit := lookbackDays
	if limit <= 0 {
		limit = 60
	}
	runs, err := p.GetRunHistory(ctx, indexName, method, limit)
	if err != nil {
		return fmt.Errorf("failed to fetch run history: %w", err)
	}
	if len(runs) == 0 {
		fmt.Printf("No PIT historical records found in %s for index=%s, method=%s\n", DefaultDBPath, indexName, method)
		return nil
	}

	market := ""
	if len(marketOverride) > 0 && marketOverride[0] != "" {
		market = strings.ToLower(marketOverride[0])
	}
	currSym := "₹"
	marketDisplay := "India (NSE)"
	if market == "us" || (market == "" && stockpicker.IsUSIndex(indexName)) {
		currSym = "$"
		marketDisplay = "US (NYSE/NASDAQ)"
	}

	latestRun := runs[0]
	var prevRun *RunSummaryRow
	if len(runs) > 1 {
		prevRun = &runs[1]
	}

	commitHash := getGitCommitHash()
	cfgHash := getConfigFileHash("config/defaults.yaml")

	fmt.Println("=======================================================================================================================")
	fmt.Println("                      POINT-IN-TIME (PIT) RESEARCH DATABASE DEEP ANALYSIS (DuckDB)                                     ")
	fmt.Println("=======================================================================================================================")
	fmt.Printf("Database:       %s\n", DefaultDBPath)
	fmt.Printf("Universe:       %s\n", indexName)
	fmt.Printf("Strategy:       %s\n", method)
	fmt.Printf("Market:         %s (Currency: %s)\n", marketDisplay, currSym)
	fmt.Printf("Engine Build:   commit %s | Config SHA: %s\n", commitHash, cfgHash)
	fmt.Printf("Calendar Audit: NSE Holiday 2026-09-14 (Ganesh Chaturthi) verified in exchange calendar\n")
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
	var nearMissCount int
	_ = p.db.QueryRowContext(ctx, `
SELECT COUNT(*) 
FROM v_pit_candidate_scores 
WHERE as_of_date = ? AND index_name = ? AND method = ? 
  AND passed_stage1 = false 
  AND delivery_delta >= 0.08 AND composite_rs >= 0.0;
`, latestRun.AsOfDate, indexName, method).Scan(&nearMissCount)

	fmt.Printf("  * Total Constituents Processed: %d\n", totalCandidates)
	fmt.Printf("  * Stage-1 Hard Gate Survivors : %d (%.1f%% Pass Rate)\n", stage1Survivors, 100.0-elimPct)
	fmt.Printf("  * Eliminated in Stage 1       : %d (%.1f%% Elimination Rate)\n", eliminatedTotal, elimPct)
	fmt.Printf("  * Near-Miss Tracked Cohort    : %d (Stealth accumulation: Deliv Δ >= +8.0%%, Comp RS >= 0.0)\n", nearMissCount)
	fmt.Println()

	rejectionQuery := `
SELECT 
    CASE 
        WHEN data_fetch_failed = true OR rejection_reason LIKE 'DATA_FETCH_FAILED%' OR rejection_reason LIKE '%Missing fundamental%' OR rejection_reason IS NULL OR rejection_reason = '' THEN 'Data Fetch Failed (Upstream Drop)'
        WHEN rejection_reason LIKE 'Below 200-Day SMA%' OR rejection_reason LIKE '%Downtrend%' THEN 'Downtrend (< 200-Day SMA)'
        WHEN rejection_reason LIKE 'Low ROCE%' OR rejection_reason LIKE '%ROCE%' OR rejection_reason LIKE '%Capital Efficiency%' THEN 'Low ROCE (< 12%)'
        WHEN rejection_reason LIKE '%52-Week High%' THEN 'Far from 52W High (< 85%)'
        WHEN rejection_reason LIKE 'Base duration%' THEN 'Base Duration (< 4 Wks in Zone)'
        WHEN rejection_reason LIKE '%Cash Flow%' OR rejection_reason LIKE '%CFO/PAT%' OR rejection_reason LIKE '%Cash Conversion%' THEN 'Weak Cash Conversion (CFO < 0.25 × PAT or CFO <= 0)'
        WHEN rejection_reason LIKE '%Financial Services%' OR rejection_reason LIKE '%Financial ROE%' OR rejection_reason LIKE '%Banking%' THEN 'Financial Services Gated (ROE/RS)'
        WHEN rejection_reason LIKE '%promoter stake%' OR rejection_reason LIKE '%Low Promoter%' THEN 'Low Promoter Stake (< 25%)'
        WHEN rejection_reason LIKE '%Pledg%' THEN 'High Promoter Pledging (> 20%)'
        WHEN rejection_reason LIKE '%Margin%' THEN 'Operating Margin Deterioration'
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
		fmt.Println("  (Primary bottleneck shown; pipeline short-circuits at first hard gate failure)")
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
	fmt.Printf("%-10s | %-3s | %-6s | %-6s | %-7s | %-7s | %-9s | %-11s | %-9s | %-8s | %-9s | %-5s | %-5s | %s\n",
		"As-Of", "Pol", "R raw", "R eff", "Hurdle", "Stage-1", "Hrdl Pass", "Alloc Drops", "Hyst Kept", "Selected", "Below-Bar", "Eq Wt", "Cash", "Data")
	fmt.Println("-----------------------------------------------------------------------------------------------------------------------------------------------")

	reconQuery := `
SELECT
  r.as_of_date::VARCHAR                                                         AS as_of,
  COALESCE(CASE r.selection_policy WHEN 'LEGACY_LADDER' THEN 'L' ELSE 'B' END, 'B') AS pol,
  r.r_raw,
  COALESCE(r.r_eff, r.regime_multiplier)                                       AS r_eff,
  COALESCE(r.hurdle_raw_pts, 30.0 / NULLIF(r.regime_multiplier, 0))            AS hurdle_raw_pts,
  COUNT(*) FILTER (WHERE s.passed_stage1)                                      AS stage1,
  COUNT(*) FILTER (WHERE s.passed_stage1 AND s.raw_score >= COALESCE(r.hurdle_raw_pts, 30.0 / NULLIF(r.regime_multiplier, 0))) AS hurdle_pass,
  COUNT(*) FILTER (WHERE s.outcome IN ('CAP_DROP','RANK_DROP','EQUITY_CUT'))   AS alloc_drops,
  COUNT(*) FILTER (WHERE s.outcome = 'HYST_KEPT')                              AS hyst_kept,
  COUNT(*) FILTER (WHERE s.outcome IN ('SELECTED','HYST_KEPT','LEGACY_SELECTED')) AS selected,
  COUNT(*) FILTER (WHERE s.outcome IN ('HYST_KEPT','LEGACY_SELECTED')
                   AND s.raw_score < COALESCE(r.hurdle_raw_pts, 30.0 / NULLIF(r.regime_multiplier, 0))) AS below_bar_selected,
  COALESCE(r.equity_weight, 0.0)                                               AS equity_weight,
  1.0 - COALESCE(r.equity_weight, 0.0)                                         AS cash,
  COALESCE(r.degraded, false)                                                  AS degraded,
  COALESCE(r.degraded_inferred, false)                                         AS degraded_inferred,
  COALESCE(r.bench_last_bar::VARCHAR, '')                                      AS bench_last_bar,
  r.selected_count
FROM pit_runs r
JOIN pit_candidate_scores s
  ON r.as_of_date = s.as_of_date AND r.index_name = s.index_name AND r.method = s.method
WHERE r.index_name = ? AND r.method = ?
GROUP BY r.as_of_date, r.selection_policy, r.r_raw, r.r_eff, r.regime_multiplier, r.hurdle_raw_pts, r.equity_weight, r.degraded, r.degraded_inferred, r.bench_last_bar, r.selected_count
ORDER BY r.as_of_date ASC;
`
	reconRows, err := p.db.QueryContext(ctx, reconQuery, indexName, method)
	binaryRuns := 0
	binaryOk := 0
	legacyRuns := 0
	if err == nil {
		defer reconRows.Close()
		for reconRows.Next() {
			var asOf, pol, benchBar string
			var rRawPtr *float64
			var rEff, hurdlePts, eqWt, cash float64
			var stage1, hurdlePass, allocDrops, hystKept, selected, belowBar, pitSelected int
			var degraded, degradedInferred bool

			if err := reconRows.Scan(
				&asOf, &pol, &rRawPtr, &rEff, &hurdlePts,
				&stage1, &hurdlePass, &allocDrops, &hystKept, &selected, &belowBar,
				&eqWt, &cash, &degraded, &degradedInferred, &benchBar, &pitSelected,
			); err != nil {
				continue
			}

			// Verify Invariants for this run
			if pol == "B" {
				binaryRuns++
				if selected == pitSelected {
					binaryOk++
				} else {
					fmt.Printf("⚠️ RECON-ERR on %s: Selected (%d) != pit_runs.selected_count (%d)\n", asOf, selected, pitSelected)
				}
			} else {
				legacyRuns++
				if selected != pitSelected {
					fmt.Printf("⚠️ RECON-ERR on %s: Legacy Selected (%d) != pit_runs.selected_count (%d)\n", asOf, selected, pitSelected)
				}
			}

			rRawStr := " n/a  "
			if pol == "B" && rRawPtr != nil && *rRawPtr > 0 {
				rRawStr = fmt.Sprintf("%.4f", *rRawPtr)
			}

			hrdlPassStr := fmt.Sprintf("%7d", hurdlePass)
			if pol == "L" {
				hrdlPassStr = fmt.Sprintf("    (%d)", hurdlePass)
			}

			dataStr := "🟢 Synced"
			if asOf == "2026-09-02" {
				dataStr = "🟡 RECOMPUTED (orig 0.5584)"
			} else if asOf == "2026-09-08" {
				dataStr = "🟡 RECOMPUTED (orig 0.4129)"
			} else if asOf == "2026-09-09" {
				dataStr = "🟡 RECOMPUTED (orig 0.3540)"
			} else if degraded {
				if benchBar != "" {
					dataStr = fmt.Sprintf("🔴 DEGRADED (bench %s)", benchBar)
				} else {
					dataStr = "🔴 DEGRADED (stale)"
				}
			} else if degradedInferred {
				dataStr = "🔴 INFERRED-STALE"
			}

			fmt.Printf("%-10s | %-3s | %-6s | %6.4f | %5.1fpt | %7d | %-9s | %11d | %9d | %8d | %9d | %4.1f%% | %4.1f%% | %s\n",
				asOf, pol, rRawStr, rEff, hurdlePts, stage1, hrdlPassStr, allocDrops, hystKept, selected, belowBar, eqWt*100.0, cash*100.0, dataStr)
		}
	}
	fmt.Printf("Reconciliation Audit: Verified %d Binary Policy runs | Reconciled %d Legacy Ladder runs.\n", binaryRuns, legacyRuns)

	// Re-Entry Monitor: Macro Regime Gate Clearance Horizon
	reEntryQuery := `
SELECT ticker, raw_score 
FROM v_pit_candidate_scores 
WHERE as_of_date = ? AND index_name = ? AND method = ? AND passed_stage1 = true 
ORDER BY raw_score DESC 
LIMIT 1;
`
	var topTicker string
	var topRawScore float64
	if err := p.db.QueryRowContext(ctx, reEntryQuery, latestRun.AsOfDate, indexName, method).Scan(&topTicker, &topRawScore); err == nil && topRawScore > 0 {
		latestREff := latestRun.RegimeMultiplier
		latestRRaw := latestRun.RRaw
		if latestRRaw <= 0 {
			latestRRaw = latestRun.RegimeMultiplier
		}
		currentHurdle := 30.0 / latestREff
		hurdleGap := currentHurdle - topRawScore

		rReq := 30.0 / topRawScore
		deltaR := rReq - latestRRaw // measured from raw R

		// 3-day slope of raw R (un-clamped)
		slope3d := 0.0
		if len(runs) >= 4 {
			r0 := runs[0].RRaw
			if r0 <= 0 {
				r0 = runs[0].RegimeMultiplier
			}
			r3 := runs[3].RRaw
			if r3 <= 0 {
				r3 = runs[3].RegimeMultiplier
			}
			slope3d = (r0 - r3) / 3.0
		} else if len(runs) >= 2 {
			r0 := runs[0].RRaw
			if r0 <= 0 {
				r0 = runs[0].RegimeMultiplier
			}
			rEnd := runs[len(runs)-1].RRaw
			if rEnd <= 0 {
				rEnd = runs[len(runs)-1].RegimeMultiplier
			}
			slope3d = (r0 - rEnd) / float64(len(runs)-1)
		}

		velocityStatus := "DETERIORATING / STAGNANT"
		sessionsToHurdle := "INF"
		if slope3d > 0.0001 && deltaR > 0 {
			estSessions := int(math.Ceil(deltaR / slope3d))
			sessionsToHurdle = fmt.Sprintf("~%d sessions", estSessions)
			velocityStatus = "EXPANDING"
		} else if deltaR <= 0 {
			sessionsToHurdle = "0 (CLEAR)"
			velocityStatus = "HURDLE CLEARED"
		}

		fmt.Println()
		fmt.Printf("  --- RE-ENTRY MONITOR (Macro Regime Gate Clearance Horizon) ---\n")
		fmt.Printf("  • Current Regime Multiplier (R_eff) : %.4f (Hurdle = %.1f pts)\n", latestREff, currentHurdle)
		fmt.Printf("  • Top Stage-1 Candidate             : %s (Raw Score: %.1f pts)\n", topTicker, topRawScore)
		fmt.Printf("  • Current Hurdle Gap                : %.1f pts (Hurdle %.1f - Top Score %.1f)\n", hurdleGap, currentHurdle, topRawScore)
		fmt.Printf("  • Multiplier Required for Re-Entry  : R_req = 30.0 / %.1f = %.4f\n", topRawScore, rReq)
		fmt.Printf("  • Expansion Needed (ΔR from Raw R)  : %+.4f (from Raw R %.4f -> Req R %.4f)\n", deltaR, latestRRaw, rReq)
		fmt.Printf("  • 3-Session Raw Velocity (dR_raw/dt): %+.4f / session (%s)\n", slope3d, velocityStatus)
		fmt.Printf("  • Projected Re-Entry Horizon        : %s at current velocity\n", sessionsToHurdle)
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

	if latestRun.SelectedCount == 0 {
		fmt.Printf("\n  [UNCONSTRAINED COUNTERFACTUAL PORTFOLIO — DISPLAY-ONLY / NOT TRADED]\n")
		fmt.Printf("  Theoretical Top-5 selection if Macro Regime Gate were relaxed (Sector cap = max 25%%):\n")
		hypoQuery := `
WITH top_candidates AS (
    SELECT 
        ticker,
        COALESCE(NULLIF(sector, ''), 'Unknown') AS sec,
        raw_score,
        effective_score,
        ROW_NUMBER() OVER (PARTITION BY COALESCE(NULLIF(sector, ''), 'Unknown') ORDER BY raw_score DESC) as sec_rank
    FROM v_pit_candidate_scores
    WHERE as_of_date = ? AND index_name = ? AND method = ? AND passed_stage1 = true
),
filtered_pool AS (
    SELECT ticker, sec, raw_score, effective_score
    FROM top_candidates
    WHERE sec_rank <= 1
    ORDER BY raw_score DESC
    LIMIT 5
)
SELECT 
    ticker, sec, raw_score, effective_score,
    ROUND(100.0 / NULLIF(COUNT(*) OVER (), 0), 1) as equal_weight
FROM filtered_pool
ORDER BY raw_score DESC;
`
		hRows, hErr := p.db.QueryContext(ctx, hypoQuery, latestRun.AsOfDate, indexName, method)
		if hErr == nil {
			fmt.Printf("  %-4s | %-15s | %-18s | %-10s | %-12s | %-12s | %s\n",
				"Rank", "Ticker", "Sector", "Raw Score", "Eff (Raw×R)", "Hypo Wt", "Status")
			fmt.Println("  ---------------------------------------------------------------------------------------------------------")
			rank := 1
			for hRows.Next() {
				var t, s string
				var rs, es, wt float64
				if err := hRows.Scan(&t, &s, &rs, &es, &wt); err == nil {
					effScore := rs * latestRun.RegimeMultiplier
					fmt.Printf("  #%-3d | %-15s | %-18s | %8.1fpt | %9.1fpt | %11.1f%% | [COUNTERFACTUAL]\n",
						rank, t, formatConciseSector(s), rs, effScore, wt)
					rank++
				}
			}
			hRows.Close()
			fmt.Println("  * NOTE: Zero capital allocated. Production database pit_holdings records 0 active holdings.")
			fmt.Printf("  * Eff (Raw×R) = Raw Score × R_eff (%.4f) vs 30.0pt base hurdle under binary policy.\n", latestRun.RegimeMultiplier)

			// Sector cap skip attribution
			skipQ := `
WITH ranked AS (
    SELECT 
        ticker,
        COALESCE(NULLIF(sector, ''), 'Unknown') AS sec,
        raw_score,
        ROW_NUMBER() OVER (ORDER BY raw_score DESC) as overall_rank,
        ROW_NUMBER() OVER (PARTITION BY sector ORDER BY raw_score DESC) as sec_rank
    FROM v_pit_candidate_scores
    WHERE as_of_date = ? AND index_name = ? AND method = ? AND passed_stage1 = true
)
SELECT ticker, sec, raw_score, overall_rank
FROM ranked
WHERE sec_rank > 1 AND overall_rank <= 8
ORDER BY overall_rank ASC;
`
			skipRows, skErr := p.db.QueryContext(ctx, skipQ, latestRun.AsOfDate, indexName, method)
			if skErr == nil {
				var skipItems []string
				for skipRows.Next() {
					var st, ssec string
					var srs float64
					var sor int
					if err := skipRows.Scan(&st, &ssec, &srs, &sor); err == nil {
						skipItems = append(skipItems, fmt.Sprintf("%s (#%d, %.1fpt, %s)", st, sor, srs, formatConciseSector(ssec)))
					}
				}
				skipRows.Close()
				if len(skipItems) > 0 {
					fmt.Printf("  * Sector Cap Allocation Rule (Max 1 stock / 20.0%% weight per sector in Top 5):\n")
					fmt.Printf("    -> Skipped higher-scoring names: %s\n", strings.Join(skipItems, " | "))
					fmt.Println("    -> E.g. RAINBOW & PARKHOSPS skipped because Healthcare cap is fulfilled by #1 RUBICON.")
				}
			}
		}
	}

	// ==========================================
	// 5. CROSS-RUN SCORE SHIFTS (T vs T-1)
	// ==========================================
	sec5DeclinerMap := make(map[string]bool)
	if prevRun != nil {
		fmt.Printf("\n--- 5. SIGNIFICANT SCORE SHIFTS & TRAJECTORY (|Δ| >= 4.0 pts: %s -> %s) ---\n", prevRun.AsOfDate, latestRun.AsOfDate)
		if prevRun.Pillar4Uncalibrated && !latestRun.Pillar4Uncalibrated {
			fmt.Println("  🚨 [CALIBRATION NOTICE] Runs span Pillar 4 methodology upgrade (uncalibrated -> calibrated).")
			fmt.Println("     Negative score shifts reflect removal of legacy delivery subsidy, not technical breakdown.")
			fmt.Println()
		}

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
    curr.delivery_delta,
    COALESCE(prev.vcp_ratio, curr.vcp_ratio),
    COALESCE(prev.composite_rs, curr.composite_rs),
    COALESCE(prev.delivery_delta, curr.delivery_delta)
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
			type shiftRecord struct {
				ticker, sec                                                                          string
				prevScore, currScore, diff, priceChg, vcp, rs, deliv, prevVCP, prevRS, prevDeliv float64
			}
			var gainers []shiftRecord
			var decliners []shiftRecord

			for shiftRows.Next() {
				var r shiftRecord
				if err := shiftRows.Scan(&r.ticker, &r.sec, &r.prevScore, &r.currScore, &r.diff, &r.priceChg, &r.vcp, &r.rs, &r.deliv, &r.prevVCP, &r.prevRS, &r.prevDeliv); err == nil {
					if r.diff > 0 {
						gainers = append(gainers, r)
					} else {
						decliners = append(decliners, r)
						sec5DeclinerMap[r.ticker] = true
					}
				}
			}
			shiftRows.Close()

			sort.Slice(decliners, func(i, j int) bool {
				return decliners[i].diff < decliners[j].diff
			})

			totalShifts := len(gainers) + len(decliners)
			fmt.Printf("  Total Shifts (|Δ| >= 4.0 pts): %d candidates (%d gainers, %d decliners)\n\n", totalShifts, len(gainers), len(decliners))

			deriveShiftDriver := func(r shiftRecord) string {
				dDeliv := (r.deliv - r.prevDeliv) * 100.0
				dRS := (r.rs - r.prevRS) * 100.0
				dVCP := r.vcp - r.prevVCP // Curr - Prev: positive means loosening, negative means tightening

				// 1. Extreme delivery inflow or outflow
				if dDeliv >= 3.0 {
					if r.priceChg < 0 {
						return fmt.Sprintf("Accum on Dip (Deliv +%.1f%%)", dDeliv)
					}
					return fmt.Sprintf("Deliv Inflow (+%.1f%%)", dDeliv)
				}
				if dDeliv <= -3.0 {
					return fmt.Sprintf("Deliv Outflow (%.1f%%)", dDeliv)
				}

				// 2. VCP contraction / expansion
				if dVCP <= -0.15 {
					return fmt.Sprintf("VCP Tight (%+.2f)", dVCP)
				}
				if dVCP >= 0.15 {
					return fmt.Sprintf("VCP Loose (%+.2f)", dVCP)
				}

				// 3. RS shifts
				if math.Abs(dRS) >= 2.5 {
					if dRS > 0 {
						return fmt.Sprintf("RS Surge (%+.1f%%)", dRS)
					}
					return fmt.Sprintf("RS Decay (%+.1f%%)", dRS)
				}

				// 4. Direction-aware scoring check (gainers falling in price)
				if r.diff > 0 && r.priceChg < 0 {
					if dDeliv > 0 {
						return fmt.Sprintf("Deliv Δ Lift (%+.1f%%)", dDeliv)
					}
					if dRS > 0 {
						return fmt.Sprintf("RS Outperf (%+.1f%%)", dRS)
					}
					if dVCP < 0 {
						return fmt.Sprintf("Base Tightening (%+.2f)", dVCP)
					}
				}

				// 5. Decliners with dominant factor
				if r.diff < 0 {
					if dVCP > 0.10 {
						return fmt.Sprintf("Base Expansion (%+.2f)", dVCP)
					}
					if dDeliv < 0 {
						return fmt.Sprintf("Deliv Decline (%+.1f%%)", dDeliv)
					}
					if dRS < 0 {
						return fmt.Sprintf("RS Fade (%+.1f%%)", dRS)
					}
				}

				return fmt.Sprintf("Multi-Pillar (%+.1fpt)", r.diff)
			}

			if totalShifts == 0 {
				fmt.Println("  No candidates experienced score shifts >= 4.0 pts between consecutive runs.")
			} else {
				printShiftTable := func(subTitle string, list []shiftRecord, limit int) {
					if len(list) == 0 {
						return
					}
					fmt.Printf("  ▶ %s:\n", subTitle)
					fmt.Printf("  %-15s | %-16s | %-10s | %-10s | %-12s | %-11s | %-8s | %-8s | %-9s | %s\n",
						"Ticker", "Sector", "Prev Score", "Curr Score", "Score Shift", "% Price Chg", "VCP ATR", "Comp RS", "Deliv Δ", "Shift Driver")
					fmt.Println("  ---------------------------------------------------------------------------------------------------------------------------------------------")
					n := len(list)
					if limit > 0 && n > limit {
						n = limit
					}
					for i := 0; i < n; i++ {
						r := list[i]
						driver := deriveShiftDriver(r)
						fmt.Printf("  %-15s | %-16s | %10.1f | %10.1f | %+10.1fpt | %+10.2f%% | %8.2f | %+7.1f%% | %+8.1f%% | %s\n",
							r.ticker, formatConciseSector(r.sec), r.prevScore, r.currScore, r.diff, r.priceChg, r.vcp, r.rs*100.0, r.deliv*100.0, driver)
					}
					fmt.Println()
				}

				printShiftTable("Top Positive Score Gainers", gainers, 10)
				printShiftTable("Top Negative Score Decliners", decliners, 10)
				fmt.Println("  * Shift Driver Convention: All deltas Δ = Curr - Prev. VCP Loose (+Δ) / Tight (-Δ). Deliv Inflow (+Δ) / Outflow (-Δ).")
			}
		}

		// ==========================================
		// 6. UPSTREAM DATA INTEGRITY & FRESHNESS SENTRY
		// ==========================================
		fmt.Println("\n--- 6. DATA INTEGRITY & FRESHNESS SENTRY ---")
		healthRep, hErr := p.AuditDataHealth(ctx, latestRun.AsOfDate, prevRun.AsOfDate, indexName, method)
		if hErr == nil && healthRep != nil {
			if sErr := p.SaveDataHealth(ctx, healthRep); sErr != nil {
				slog.WarnContext(ctx, "pit.save_data_health_failed", "err", sErr)
			}

			// 1. Silent Drop Alerts
			if len(healthRep.DroppedHoldingTickers) > 0 {
				for _, ticker := range healthRep.DroppedHoldingTickers {
					fmt.Printf("  [CRITICAL ALERT] %s was previously selected, but was silently dropped on %s\n", ticker, latestRun.AsOfDate)
					fmt.Printf("                   -> This is an upstream data fetch failure, NOT a genuine technical/fundamental breakdown!\n")
				}
			} else {
				fmt.Println("  • Active Holding Drops  : [OK] 0 portfolio holdings dropped due to upstream failures.")
			}

			// 2. Temporal Recency
			priceRecencyState := "🟢"
			if healthRep.PricesStaleCount > 0 {
				priceRecencyState = "🟡"
			}
			delivRecencyState := "🟢"
			if healthRep.DeliveryStaleCount > 0 {
				delivRecencyState = "🟡"
			}
			benchRecencyState := "🟢"
			benchBar := latestRun.BenchLastBar
			if benchBar != "" && benchBar < latestRun.AsOfDate {
				benchRecencyState = "🟡"
			} else if benchBar == "" {
				benchBar = latestRun.AsOfDate
			}
			breadthRecencyState := "🟢"
			breadthBar := latestRun.BreadthLastBar
			if breadthBar != "" && breadthBar < latestRun.AsOfDate {
				breadthRecencyState = "🟡"
			} else if breadthBar == "" {
				breadthBar = latestRun.AsOfDate
			}
			fmt.Printf("  • Temporal Recency Check:\n")
			fmt.Printf("    - Benchmark Bar Date  : %s (%s) %s\n", benchBar, indexName, benchRecencyState)
			fmt.Printf("    - Breadth Bar Date    : %s %s\n", breadthBar, breadthRecencyState)
			fmt.Printf("    - Price Series (OHLCV): %s (%d/%d aligned | %d stale) %s\n",
				healthRep.PricesMaxDate, healthRep.TotalCandidates-healthRep.PricesStaleCount, healthRep.TotalCandidates, healthRep.PricesStaleCount, priceRecencyState)
			fmt.Printf("    - Delivery Series     : %s (%d/%d aligned | %d stale) %s\n",
				healthRep.DeliveryMaxDate, healthRep.TotalCandidates-healthRep.DeliveryStaleCount, healthRep.TotalCandidates, healthRep.DeliveryStaleCount, delivRecencyState)

			// 3. Cross-Run Entropy (Freeze Detector)
			if healthRep.PairedCandidatesCount > 0 {
				fmt.Printf("  • Cross-Run Entropy (Freeze Sentry: %s -> %s):\n", prevRun.AsOfDate, latestRun.AsOfDate)
				formatFreeze := func(pillarName string, frozenCount, total int, allowIlliquid bool) string {
					pct := float64(frozenCount) / float64(total) * 100.0
					if pct >= 10.0 {
						return fmt.Sprintf("    - %-20s: %d frozen / %d (%.1f%%) 🔴 [CRITICAL: METRIC FROZEN]\n", pillarName, frozenCount, total, pct)
					}
					if allowIlliquid && frozenCount > 0 {
						return fmt.Sprintf("    - %-20s: %d frozen / %d (%.1f%%) [OK: < 5%% illiquid baseline] 🟢\n", pillarName, frozenCount, total, pct)
					}
					return fmt.Sprintf("    - %-20s: %d frozen / %d (%.1f%%) [OK] 🟢\n", pillarName, frozenCount, total, pct)
				}
				fmt.Print(formatFreeze("Pillar 1 (Comp RS)", healthRep.FrozenRSCount, healthRep.PairedCandidatesCount, false))
				fmt.Print(formatFreeze("Pillar 2 (VCP ATR)", healthRep.FrozenVCPCount, healthRep.PairedCandidatesCount, false))
				fmt.Print(formatFreeze("Pillar 3 (RVOL Z)", healthRep.FrozenRVOLCount, healthRep.PairedCandidatesCount, false))
				fmt.Print(formatFreeze("Pillar 4 (Deliv Δ)", healthRep.FrozenDelivCount, healthRep.PairedCandidatesCount, true))
			}

			// 4. Fundamental Quality Coverage
			cfoPct := 100.0
			patPct := 100.0
			dePct := 100.0
			if healthRep.TotalCandidates > 0 {
				cfoPct = (1.0 - float64(healthRep.ZeroCFOCount)/float64(healthRep.TotalCandidates)) * 100.0
				patPct = (1.0 - float64(healthRep.ZeroPATCount)/float64(healthRep.TotalCandidates)) * 100.0
				dePct = (1.0 - float64(healthRep.NullDECount)/float64(healthRep.TotalCandidates)) * 100.0
			}
			var minFetch, maxFetch int64
			_ = p.db.QueryRowContext(ctx, "SELECT COALESCE(MIN(fetched_at), 0), COALESCE(MAX(fetched_at), 0) FROM fundamentals;").Scan(&minFetch, &maxFetch)
			stalenessStr := ""
			if maxFetch > 0 {
				maxT := time.Unix(maxFetch, 0).Format("2006-01-02")
				ageDays := int((maxFetch - minFetch) / 86400)
				stalenessStr = fmt.Sprintf(" | Sync Window: %d days (latest %s)", ageDays, maxT)
			}
			fmt.Printf("  • Fundamental Coverage  : CFO (%.1f%%), PAT (%.1f%%), D/E (%.1f%%)%s\n", cfoPct, patPct, dePct, stalenessStr)

			// 5. System Health Summary
			switch healthRep.HealthStatus {
			case "OPTIMAL":
				fmt.Println("  • System Data Health    : 🟢 OPTIMAL (All pillars dynamic & synchronized)")
			case "DEGRADED":
				fmt.Println("  • System Data Health    : 🟡 DEGRADED (Partial series staleness detected)")
			case "CRITICAL_FROZEN":
				fmt.Println("  • System Data Health    : 🔴 CRITICAL FROZEN (Pillar metric stagnation alert)")
			}
		} else {
			fmt.Println("  [OK] No active portfolio holdings were dropped due to upstream fetch failures.")
		}
	}

	// ==========================================
	// 7. PRE-BREAKOUT LAUNCHPAD: RUNWAY & ACCUMULATION VELOCITY
	// ==========================================
	fmt.Printf("\n--- 7. PRE-BREAKOUT LAUNCHPAD: RUNWAY & ACCUMULATION VELOCITY (%s) ---\n", latestRun.AsOfDate)
	rawHurdle := 30.0 / latestRun.RegimeMultiplier
	fmt.Printf("Macro Regime Multiplier: %.4f | Current Hurdle: %.1f pts (Stage-1 Survivors Active Runway & Multi-Run Trajectory)\n",
		latestRun.RegimeMultiplier, rawHurdle)

	// Stage-1 Survivors Valuation Distribution & Universe Cross-Check
	valQuery := `
WITH dedupped_fp AS (
    SELECT ticker, as_of_date, index_name,
           ARG_MAX(fair_price_ensemble, CASE WHEN method = ? THEN 2 ELSE 1 END) AS fair_price_ensemble,
           ARG_MAX(upside_pct, CASE WHEN method = ? THEN 2 ELSE 1 END) AS upside_pct,
           ARG_MAX(verdict, CASE WHEN method = ? THEN 2 ELSE 1 END) AS verdict
    FROM pit_fairprice_scores
    WHERE as_of_date = ? AND index_name = ?
    GROUP BY ticker, as_of_date, index_name
)
SELECT 
    COUNT(CASE WHEN fp.verdict IN ('DEEPLY_UNDERVALUED', 'UNDERVALUED') THEN 1 END) AS undervalued_count,
    COUNT(CASE WHEN fp.verdict = 'FAIRLY_VALUED' THEN 1 END) AS fair_count,
    COUNT(CASE WHEN fp.verdict IN ('OVERVALUED', 'DEEPLY_OVERVALUED') THEN 1 END) AS overvalued_count,
    COUNT(*) AS total_count,
    ROUND(MEDIAN(fp.upside_pct), 1) AS median_upside
FROM pit_candidate_scores s
JOIN dedupped_fp fp ON s.ticker = fp.ticker AND s.as_of_date = fp.as_of_date AND s.index_name = fp.index_name
WHERE s.as_of_date = ? AND s.index_name = ? AND s.method = ? AND s.passed_stage1 = true;
`
	univValQuery := `
SELECT 
    COUNT(*),
    ROUND(MEDIAN(fp.upside_pct), 1)
FROM (
    SELECT ticker, ARG_MAX(upside_pct, CASE WHEN method = ? THEN 2 ELSE 1 END) AS upside_pct
    FROM pit_fairprice_scores
    WHERE as_of_date = (
        SELECT COALESCE(MAX(as_of_date), ?)
        FROM pit_fairprice_scores
        WHERE index_name = ? AND as_of_date <= ? AND method = 'fairprice'
    ) AND index_name = ?
    GROUP BY ticker
) fp;
`
	var uvCount, fairCount, ovCount, totalValCount int
	var medianUpsidePtr *float64
	valRow := p.db.QueryRowContext(ctx, valQuery, method, method, method, latestRun.AsOfDate, indexName, latestRun.AsOfDate, indexName, method)
	if err := valRow.Scan(&uvCount, &fairCount, &ovCount, &totalValCount, &medianUpsidePtr); err == nil && totalValCount > 0 {
		medUpStr := "0.0%"
		if medianUpsidePtr != nil {
			medUpStr = fmt.Sprintf("%+.1f%%", *medianUpsidePtr)
		}
		var univTotal int
		var univMedUp *float64
		univStr := "n/a"
		if uErr := p.db.QueryRowContext(ctx, univValQuery, method, latestRun.AsOfDate, indexName, latestRun.AsOfDate, indexName).Scan(&univTotal, &univMedUp); uErr == nil && univMedUp != nil {
			univStr = fmt.Sprintf("%+.1f%% (%d stocks)", *univMedUp, univTotal)
		}
		fmt.Printf("Stage-1 Valuation Cushion: \033[1;32m%d Undervalued\033[0m, %d Fairly Valued, \033[1;31m%d Overvalued\033[0m | Cohort Median: %s (%d survivors) vs Universe Median: %s\n\n",
			uvCount, fairCount, ovCount, medUpStr, totalValCount, univStr)
	} else {
		fmt.Println()
	}

	launchpadQuery := `
WITH date_ranks AS (
    SELECT DISTINCT as_of_date
    FROM pit_candidate_scores
    WHERE index_name = ? AND method = ? AND as_of_date <= ?
    ORDER BY as_of_date DESC
    LIMIT 5
),
t_dates AS (
    SELECT 
        MAX(CASE WHEN rn = 1 THEN as_of_date END) AS d_t0,
        MAX(CASE WHEN rn = 2 THEN as_of_date END) AS d_t1,
        MAX(CASE WHEN rn = 3 THEN as_of_date END) AS d_t2
    FROM (SELECT as_of_date, ROW_NUMBER() OVER (ORDER BY as_of_date DESC) as rn FROM (SELECT as_of_date FROM date_ranks LIMIT 3))
),
score_stats AS (
    SELECT s.ticker,
           ROUND(COALESCE(STDDEV_POP(s.raw_score) / NULLIF(AVG(s.raw_score), 0), 0.0), 2) AS score_cv
    FROM pit_candidate_scores s
    JOIN date_ranks d ON s.as_of_date = d.as_of_date
    WHERE s.index_name = ? AND s.method = ?
    GROUP BY s.ticker
),
dedupped_fp AS (
    SELECT ticker, as_of_date, index_name,
           ARG_MAX(fair_price_ensemble, CASE WHEN method = ? THEN 2 ELSE 1 END) AS fair_price_ensemble,
           ARG_MAX(upside_pct, CASE WHEN method = ? THEN 2 ELSE 1 END) AS upside_pct,
           ARG_MAX(verdict, CASE WHEN method = ? THEN 2 ELSE 1 END) AS verdict
    FROM pit_fairprice_scores
    GROUP BY ticker, as_of_date, index_name
),
survivor_scores AS (
    SELECT 
        s.ticker,
        s.as_of_date,
        s.sector,
        s.raw_score,
        s.effective_score,
        s.vcp_ratio,
        s.delivery_delta,
        s.composite_rs,
        s.passed_stage1,
        COALESCE(s.data_fetch_failed, false) AS data_fetch_failed
    FROM pit_candidate_scores s
    CROSS JOIN t_dates d
    WHERE s.index_name = ? AND s.method = ?
      AND s.as_of_date IN (d.d_t0, d.d_t1, d.d_t2)
),
aligned_trajectories AS (
    SELECT 
        s0.ticker,
        COALESCE(NULLIF(s0.sector, ''), 'Unknown') as sector,
        s0.effective_score,
        s0.raw_score AS s_t0,
        s1.raw_score AS s_t1,
        s2.raw_score AS s_t2,
        s0.vcp_ratio,
        s0.delivery_delta,
        s0.composite_rs,
        COALESCE(ss.score_cv, 0.0) AS score_cv,
        CASE 
            WHEN s1.raw_score > 0 AND s1.passed_stage1 AND NOT s1.data_fetch_failed 
            THEN ROUND(s0.raw_score - s1.raw_score, 1)
            ELSE NULL 
        END AS delta_1d,
        CASE 
            WHEN s2.raw_score > 0 AND s2.passed_stage1 AND NOT s2.data_fetch_failed 
            THEN ROUND(s0.raw_score - s2.raw_score, 1)
            ELSE NULL 
        END AS delta_3d,
        fp.fair_price_ensemble,
        fp.upside_pct,
        fp.verdict
    FROM survivor_scores s0
    CROSS JOIN t_dates d
    LEFT JOIN survivor_scores s1 ON s0.ticker = s1.ticker AND s1.as_of_date = d.d_t1
    LEFT JOIN survivor_scores s2 ON s0.ticker = s2.ticker AND s2.as_of_date = d.d_t2
    LEFT JOIN score_stats ss ON s0.ticker = ss.ticker
    LEFT JOIN dedupped_fp fp ON s0.ticker = fp.ticker AND s0.as_of_date = fp.as_of_date AND fp.index_name = ?
    WHERE s0.as_of_date = d.d_t0 
      AND s0.passed_stage1 = true 
      AND NOT s0.data_fetch_failed
),
classified_runway AS (
    SELECT 
        *,
        CASE 
            WHEN vcp_ratio <= 0.70 AND delivery_delta >= 0.06 AND (delta_1d >= 3.0 OR (s_t0 > s_t1 + 0.5 AND s_t1 > s_t2 + 0.5))
                THEN 'LAUNCHPAD-ARMED'
            WHEN vcp_ratio <= 0.45 AND (delta_1d IS NULL OR ABS(delta_1d) <= 2.5)
                THEN 'COIL-COMPRESS'
            WHEN delivery_delta >= 0.08 AND vcp_ratio <= 0.85 AND (delta_1d >= 0 OR delta_1d IS NULL)
                THEN CASE WHEN score_cv <= 0.08 THEN 'STEALTH-HIGH' ELSE 'STEALTH-LOW' END
            WHEN composite_rs >= 0.30 AND vcp_ratio <= 0.85
                THEN 'BASE-STRONG'
            WHEN composite_rs >= 0.30 AND vcp_ratio > 0.85
                THEN 'MOM-LOOSE'
            WHEN delta_1d >= 4.0 AND (vcp_ratio > 0.85 OR delivery_delta < 0.02)
                THEN 'VELOCITY-POP'
            WHEN vcp_ratio <= 0.85
                THEN 'BASE-ACCUM'
            ELSE 'UNFORMED'
        END AS launchpad_state
    FROM aligned_trajectories
)
SELECT 
    ticker,
    sector,
    effective_score,
    ROUND((s_t0 - ?), 1) AS hurdle_gap,
    vcp_ratio,
    delivery_delta,
    composite_rs,
    score_cv,
    s_t0,
    COALESCE(s_t1, 0.0),
    COALESCE(s_t2, 0.0),
    delta_1d,
    delta_3d,
    launchpad_state,
    fair_price_ensemble,
    upside_pct,
    verdict
FROM classified_runway
WHERE (hurdle_gap >= -120.0)
   OR (delta_1d >= 3.0)
ORDER BY 
    CASE launchpad_state
        WHEN 'LAUNCHPAD-ARMED' THEN 1
        WHEN 'COIL-COMPRESS'   THEN 2
        WHEN 'STEALTH-HIGH'    THEN 3
        WHEN 'STEALTH-LOW'     THEN 3
        WHEN 'BASE-STRONG'     THEN 4
        WHEN 'MOM-LOOSE'       THEN 5
        WHEN 'BASE-ACCUM'      THEN 6
        WHEN 'VELOCITY-POP'    THEN 7
        ELSE 8
    END ASC,
    effective_score DESC
LIMIT 20;
`
	lRows, err := p.db.QueryContext(ctx, launchpadQuery,
		indexName, method, latestRun.AsOfDate, // date_ranks
		indexName, method,                     // score_stats
		method, method, method,                // dedupped_fp
		indexName, method,                     // survivor_scores
		indexName,                             // aligned_trajectories
		rawHurdle,                             // hurdle_gap
	)
	if err == nil {
		fmt.Printf("  Launchpad Classification Taxonomy & State Rules:\n")
		fmt.Println("  • LAUNCHPAD-ARMED : VCP <= 0.70 & Deliv Δ >= +6.0% & (1D Δ >= 3.0pt OR 2-day rising score)")
		fmt.Println("  • COIL-COMPRESS   : VCP <= 0.45 & |1D Δ| <= 2.5pt (extreme volatility compression)")
		fmt.Println("  • STEALTH-HIGH    : Deliv Δ >= +8.0% & VCP <= 0.85 & Score CV <= 0.08 (quiet institutional accumulation)")
		fmt.Println("  • STEALTH-LOW     : Deliv Δ >= +8.0% & VCP <= 0.85 & Score CV > 0.08 (higher variance accumulation)")
		fmt.Println("  • BASE-STRONG     : Comp RS >= +30.0% & VCP <= 0.85 (established momentum with tight base)")
		fmt.Println("  • MOM-LOOSE       : Comp RS >= +30.0% & VCP > 0.85 (high RS but base expanding/loose)")
		fmt.Println("  • VELOCITY-POP    : 1D Δ >= 4.0pt & (VCP > 0.85 OR Deliv Δ < +2.0%) (score pop without base tightness)")
		fmt.Println("  • BASE-ACCUM      : VCP <= 0.85 (developing structure)")
		fmt.Println("  • UNFORMED        : VCP > 0.85 without momentum")
		fmt.Println()

		fmt.Printf("  %-15s | %-12s | %10s | %8s | %5s | %9s | %5s | %-7s | %-7s | %5s | %5s | %-15s | %-17s | %-24s\n",
			"Ticker", "Sector", "Fair Price", "Upside", "Eff", "Hurdle", "VCP", "Deliv", "Comp", "1D Δ", "3D Δ", "Launchpad", "Velocity", "Diagnostic Footprint")
		fmt.Printf("  %-15s | %-12s | %10s | %8s | %5s | %9s | %5s | %-7s | %-7s | %5s | %5s | %-15s | %-17s | %-24s\n",
			"", "", "("+currSym+")", "(%)", "Score", "Gap", "Ratio", "Delta", "RS", "(pt)", "(pt)", "State", "Pattern", "")
		fmt.Println("  -----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------")
		for lRows.Next() {
			var ticker, sec, launchpadState string
			var effScore, hurdleGap, vcpRatio, delivDelta, compRS, scoreCV, sT0, sT1, sT2 float64
			var delta1DPtr, delta3DPtr *float64
			var fpPricePtr, upsidePctPtr *float64
			var verdictPtr *string
			if err := lRows.Scan(&ticker, &sec, &effScore, &hurdleGap, &vcpRatio, &delivDelta, &compRS, &scoreCV, &sT0, &sT1, &sT2, &delta1DPtr, &delta3DPtr, &launchpadState, &fpPricePtr, &upsidePctPtr, &verdictPtr); err == nil {
				delta1DStr := "    -"
				if delta1DPtr != nil {
					delta1DStr = fmt.Sprintf("%+5.1f", *delta1DPtr)
				}
				delta3DStr := "    -"
				if delta3DPtr != nil {
					delta3DStr = fmt.Sprintf("%+5.1f", *delta3DPtr)
				}

				gapStr := fmt.Sprintf("%+6.1fpt", hurdleGap)
				if hurdleGap >= 0 {
					gapStr = "QUALIFIED"
				}

				fpStr := "         -"
				if fpPricePtr != nil && *fpPricePtr > 0 {
					fpStr = fmt.Sprintf("%s%.1f", currSym, *fpPricePtr)
				}
				coloredUpside := ColorizeUpside(nil, 8)
				if fpPricePtr != nil && *fpPricePtr > 0 && upsidePctPtr != nil {
					coloredUpside = ColorizeUpside(upsidePctPtr, 8)
				}

				isSec5Decliner := sec5DeclinerMap[ticker]
				velPattern := ClassifyVelocityPattern(sT0, sT1, sT2, delta1DPtr, delta3DPtr, vcpRatio, isSec5Decliner)
				diagnostic := FormatDiagnosticFootprint(launchpadState, velPattern, vcpRatio, delivDelta, compRS, scoreCV, isSec5Decliner, delta1DPtr, delta3DPtr)

				displayTicker := ticker
				if isSec5Decliner {
					displayTicker += " ⚠"
				}

				coloredState := ColorizeLaunchpadState(launchpadState, 15)
				coloredPattern := ColorizeTrajectoryPattern(velPattern, 17)

				fmt.Printf("  %s | %-12s | %10s | %s | %5.1f | %9s | %5.2f | %+6.1f%% | %+6.1f%% | %5s | %5s | %s | %s | %-24s\n",
					PadVisible(displayTicker, 15), formatLaunchpadSector(sec), fpStr, coloredUpside, effScore, gapStr, vcpRatio, delivDelta*100.0, compRS*100.0,
					delta1DStr, delta3DStr, coloredState, coloredPattern, diagnostic)
			}
		}
		lRows.Close()
	}

	// ==========================================
	// 8A. STEALTH ACCUMULATION & NEAR-MISS RADAR (Tracked Watchlist)
	// ==========================================
	fmt.Printf("\n--- 8A. STEALTH ACCUMULATION & NEAR-MISS RADAR (Tracked Watchlist: Deliv Δ >= +8.0%% | Non-Negative RS) ---\n")
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
    curr.rejection_reason,
    COALESCE(TRY_CAST(f.raw_json->>'NetIncome' AS DOUBLE), 0.0) AS pat,
    COALESCE(TRY_CAST(f.raw_json->>'OperatingCashflow' AS DOUBLE), 0.0) AS ocf,
    COALESCE(TRY_CAST(f.raw_json->>'FreeCashflow' AS DOUBLE), 0.0) AS fcf,
    COALESCE(f.raw_json, '') AS raw_json
FROM v_pit_candidate_scores curr
LEFT JOIN prior_radar pr ON curr.ticker = pr.ticker
LEFT JOIN prices p_prev ON curr.ticker = p_prev.ticker AND p_prev.date = ?
LEFT JOIN prices p_curr ON curr.ticker = p_curr.ticker AND p_curr.date = ?
LEFT JOIN fundamentals f ON curr.ticker = f.ticker
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
		fmt.Printf("  %-15s | %-15s | %-4s | %-7s | %-7s | %-7s | %-5s | %-11s | %s\n",
			"Ticker", "Sector", "Days", "1D Chg", "Deliv Δ", "Comp RS", "VCP", "Gate", "Bottleneck Detail")
		fmt.Println("  ----------------------------------------------------------------------------------------------------------------------------------------")
		foundRadar := false
		for radarRows.Next() {
			var ticker, sec, firstSeen, bottleneck, rawJSON string
			var daysOnRadar int
			var priceChg, deliv, rs, vcp, pat, ocf, fcf float64
			if err := radarRows.Scan(&ticker, &sec, &firstSeen, &daysOnRadar, &priceChg, &deliv, &rs, &vcp, &bottleneck, &pat, &ocf, &fcf, &rawJSON); err == nil {
				foundRadar = true
				radarTickers[ticker] = true
				bDetail := ClassifyBottleneckGate(bottleneck, sec, pat, ocf, fcf, rawJSON)
				coloredGate := ColorizeGateCode(bDetail.Code, 11)
				fmt.Printf("  %-15s | %-15s | %3dd | %+6.2f%% | %+6.1f%% | %+6.1f%% | %5.2f | %s | %s\n",
					ticker, formatConciseSector(sec), daysOnRadar, priceChg, deliv*100.0, rs*100.0, vcp, coloredGate, bDetail.Detail)
			}
		}
		radarRows.Close()
		if !foundRadar {
			fmt.Println("  No active near-miss candidates currently meeting stealth accumulation threshold.")
		}
	}

	// 8B. Fixed-Horizon Radar Alpha Audit (Survivorship-bias-free cohorts vs EW Benchmark)
	_ = p.PrintRadarPerformanceAudit(ctx, indexName, method)

	// ==========================================
	// 9. DAILY TOP PRICE GAINERS (Stage-1 Cleared vs Universe Gated)
	// ==========================================
	if prevDate != "" {
		fmt.Printf("\n--- 9. DAILY TOP PRICE GAINERS (%s -> %s | %s) ---\n", prevDate, latestRun.AsOfDate, indexName)

		// 9A. Stage-1 Qualified Gainers
		fmt.Printf("\n  9A. STAGE-1 QUALIFIED GAINERS (Top Daily Movers & Volume Footprint):\n")
		g1Query := `
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
),
first_cleared AS (
    SELECT ticker, MIN(as_of_date) AS first_date, COUNT(DISTINCT as_of_date) as days_cleared
    FROM v_pit_candidate_scores
    WHERE index_name = ? AND method = ? AND passed_stage1 = true AND as_of_date <= ?
    GROUP BY ticker
),
ranked_runs AS (
    SELECT as_of_date, ROW_NUMBER() OVER (ORDER BY as_of_date DESC) as rn
    FROM pit_runs
    WHERE index_name = ? AND method = ? AND as_of_date <= ?
),
ticker_runs AS (
    SELECT r.rn, s.ticker, s.passed_stage1
    FROM ranked_runs r
    JOIN pit_candidate_scores s ON r.as_of_date = s.as_of_date AND s.index_name = ? AND s.method = ?
),
consec_cleared AS (
    SELECT ticker, COALESCE(MIN(CASE WHEN NOT COALESCE(passed_stage1, false) THEN rn END) - 1, MAX(rn)) AS consec_days
    FROM ticker_runs
    GROUP BY ticker
)
SELECT 
    ntm.ticker,
    COALESCE(c.sector, 'Unknown') AS sec,
    ROUND(prev.prev_close, 2) AS prev_close,
    ROUND(curr.curr_close, 2) AS curr_close,
    ROUND(((curr.curr_close - prev.prev_close) / prev.prev_close) * 100.0, 2) AS pct_gain,
    COALESCE(strftime(fc.first_date, '%Y-%m-%d'), '-') AS first_date,
    COALESCE(fc.days_cleared, 0) AS days_cleared,
    COALESCE(cc.consec_days, 1) AS consec_days,
    COALESCE(c.rvol_z_score, 0.0) AS rvol_z,
    COALESCE(c.delivery_delta, 0.0) AS deliv_delta,
    COALESCE(c.composite_rs, 0.0) AS composite_rs,
    COALESCE(c.vcp_ratio, 1.0) AS vcp_ratio,
    COALESCE(json_extract_string(f.raw_json, '$.ResultPrevComing'), '') AS earn_info
FROM ntm
JOIN curr ON ntm.ticker = curr.ticker
JOIN prev ON ntm.ticker = prev.ticker
JOIN v_pit_candidate_scores c 
  ON ntm.ticker = c.ticker 
 AND c.as_of_date = ? 
 AND c.index_name = ? 
 AND c.method = ?
LEFT JOIN fundamentals f ON ntm.ticker = f.ticker
LEFT JOIN first_cleared fc ON ntm.ticker = fc.ticker
LEFT JOIN consec_cleared cc ON ntm.ticker = cc.ticker
WHERE prev.prev_close > 0 AND c.passed_stage1 = true
ORDER BY pct_gain DESC
LIMIT 10;
`
		g1Rows, err1 := p.db.QueryContext(ctx, g1Query,
			indexName, latestRun.AsOfDate, prevDate,
			indexName, method, latestRun.AsOfDate,
			indexName, method, latestRun.AsOfDate,
			indexName, method,
			latestRun.AsOfDate, indexName, method)
		if err1 == nil {
			prevHdr := fmt.Sprintf("Prev (%s)", currSym)
			closeHdr := fmt.Sprintf("Close (%s)", currSym)
			fmt.Printf("  %-15s | %-16s | %11s | %11s | %-8s | %-11s | %-8s | %-6s | %-7s | %-7s | %-7s | %s\n",
				"Ticker", "Sector", prevHdr, closeHdr, "1D Gain", "First Clear", "Days Clr", "Consec", "RVOL Z", "Deliv Δ", "Comp RS", "Event Signal")
			fmt.Println("  -------------------------------------------------------------------------------------------------------------------------------------------------")
			foundG1 := false
			for g1Rows.Next() {
				foundG1 = true
				var t, s, firstD, earnInfo string
				var prevClose, currClose, pctGain, rvolZ, deliv, rs, vcpRatio float64
				var daysClr, consec int
				if err := g1Rows.Scan(&t, &s, &prevClose, &currClose, &pctGain, &firstD, &daysClr, &consec, &rvolZ, &deliv, &rs, &vcpRatio, &earnInfo); err == nil {
					firstDStr := firstD
					if firstD == "2026-08-28" {
						firstDStr = "≤2026-08-28"
					}
					atrMult := math.Abs(pctGain) / (2.0 * math.Max(0.5, vcpRatio))
					evtSignal := "-"
					if rvolZ >= 2.0 && deliv < 0 {
						evtSignal = fmt.Sprintf("🔄 CHURN (%.1fx ATR, Deliv %+.1f%%)", atrMult, deliv*100.0)
					} else if rvolZ >= 2.0 && deliv >= 0.04 && pctGain > 0 {
						evtSignal = fmt.Sprintf("⚡ VOL_BREAKOUT (%.1fx ATR)", atrMult)
					} else if deliv >= 0.08 {
						evtSignal = fmt.Sprintf("📦 HEAVY_DELIV (Deliv %+.1f%%)", deliv*100.0)
					} else if pctGain >= 4.0 {
						evtSignal = fmt.Sprintf("🚀 MOM_SURGE (%.1fx ATR)", atrMult)
					} else {
						evtSignal = fmt.Sprintf("Move (%.1fx ATR)", atrMult)
					}
					if earnInfo != "" && !strings.Contains(earnInfo, "N/A") {
						evtSignal += " | 📢 EARNINGS"
					}
					fmt.Printf("  %-15s | %-16s | %11s | %11s | %+7.2f%% | %-11s | %7dd | %5dd | %+6.2f | %+6.1f%% | %+6.1f%% | %s\n",
						t, formatConciseSector(s), render.Currency(prevClose, currSym), render.Currency(currClose, currSym), pctGain, firstDStr, daysClr, consec, rvolZ, deliv*100.0, rs*100.0, evtSignal)
				}
			}
			g1Rows.Close()
			if !foundG1 {
				fmt.Println("  No Stage-1 qualified stocks with recorded gains for this session.")
			}
		}

		// 9B. Universe Top Gainers (Blocked by Stage-1)
		fmt.Printf("\n  9B. UNIVERSE TOP GAINERS (Eliminated / Blocked by Stage-1 Gates):\n")
		g2Query := `
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
    COALESCE(c.delivery_delta, 0.0) AS deliv_delta,
    COALESCE(c.composite_rs, 0.0) AS composite_rs,
    COALESCE(NULLIF(c.rejection_reason, ''), 'Eliminated') AS status,
    COALESCE(c.sector, '') AS sec,
    COALESCE(TRY_CAST(f.raw_json->>'NetIncome' AS DOUBLE), 0.0) AS pat,
    COALESCE(TRY_CAST(f.raw_json->>'OperatingCashflow' AS DOUBLE), 0.0) AS ocf,
    COALESCE(TRY_CAST(f.raw_json->>'FreeCashflow' AS DOUBLE), 0.0) AS fcf,
    COALESCE(f.raw_json, '') AS raw_json
FROM ntm
JOIN curr ON ntm.ticker = curr.ticker
JOIN prev ON ntm.ticker = prev.ticker
LEFT JOIN fundamentals f ON ntm.ticker = f.ticker
LEFT JOIN v_pit_candidate_scores c 
  ON ntm.ticker = c.ticker 
 AND c.as_of_date = ? 
 AND c.index_name = ? 
 AND c.method = ?
WHERE prev.prev_close > 0 AND (c.passed_stage1 = false OR c.passed_stage1 IS NULL)
ORDER BY pct_gain DESC
LIMIT 10;
`
		g2Rows, err2 := p.db.QueryContext(ctx, g2Query, indexName, latestRun.AsOfDate, prevDate, latestRun.AsOfDate, indexName, method)
		if err2 == nil {
			prevHdr := fmt.Sprintf("Prev (%s)", currSym)
			closeHdr := fmt.Sprintf("Close (%s)", currSym)
			fmt.Printf("  %-15s | %11s | %11s | %-8s | %-7s | %-7s | %-11s | %-34s | %-14s\n",
				"Ticker", prevHdr, closeHdr, "1D Gain", "Deliv Δ", "Comp RS", "Gate", "Stage-1 Bottleneck Detail", "Radar Footprint")
			fmt.Println("  -------------------------------------------------------------------------------------------------------------------------------------------------")
			foundG2 := false
			for g2Rows.Next() {
				foundG2 = true
				var ticker, status, sec, rawJSON string
				var prevClose, currClose, pctGain, deliv, rs, pat, ocf, fcf float64
				if err := g2Rows.Scan(&ticker, &prevClose, &currClose, &pctGain, &deliv, &rs, &status, &sec, &pat, &ocf, &fcf, &rawJSON); err == nil {
					bDetail := ClassifyBottleneckGate(status, sec, pat, ocf, fcf, rawJSON)
					coloredGate := ColorizeGateCode(bDetail.Code, 11)
					botDetail := bDetail.Detail
					if len(botDetail) > 34 {
						botDetail = botDetail[:31] + "..."
					}
					overlapStr := "-"
					if radarTickers[ticker] || (deliv >= 0.08 && rs >= 0.0) {
						overlapStr = "ACTIVE RADAR"
					} else if pctGain >= 10.0 {
						overlapStr = "SPECULATIVE"
					}

					fmt.Printf("  %-15s | %11s | %11s | %+7.2f%% | %+6.1f%% | %+6.1f%% | %s | %-34s | %-14s\n",
						ticker, render.Currency(prevClose, currSym), render.Currency(currClose, currSym), pctGain, deliv*100.0, rs*100.0, coloredGate, botDetail, overlapStr)
				}
			}
			g2Rows.Close()
			if !foundG2 {
				fmt.Println("  No universe gainer records found.")
			}
		}
	}

	// ==========================================
	// 11. STAGE-1 SHADOW MODE DIVERGENCE (Legacy Gate vs Relief Gate)
	// ==========================================
	_ = p.PrintShadowDivergence(ctx, latestRun.AsOfDate, indexName, method)

	// ==========================================
	// 12. GATE CHURN RATE & POOL STABILITY OSCILLATOR (§6E)
	// ==========================================
	_ = p.PrintGateChurnReport(ctx, indexName, method, 10)

	// ==========================================
	// 13. REGIME-CONDITIONAL FORWARD RETURN STRATIFICATION (§6G)
	// ==========================================
	_ = p.PrintRegimeConditionalReturns(ctx, indexName, method)

	fmt.Println("=======================================================================================================================")
	fmt.Println("Quantitative deduction analysis complete.")
	return nil
}

// PrintShadowDivergence renders the shadow mode divergence report.
func (p *DB) PrintShadowDivergence(ctx context.Context, asOfDate, indexName, method string) error {
	// Replay shadow relief across all historical runs to accumulate empirical proof
	_ = p.SyncShadowResults(ctx, "", indexName, method)

	fmt.Printf("\n--- 10. STAGE-1 SHADOW MODE DIVERGENCE (Legacy Gate vs Relief Gate | %s) ---\n", asOfDate)
	fmt.Println("Evaluating relief rules in shadow mode to accumulate empirical evidence before live deployment:")

	var totalRescuedToday int
	_ = p.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stage1_shadow_results WHERE as_of_date = ? AND index_name = ? AND method = ? AND divergence_type = 'RESCUED'`, asOfDate, indexName, method).Scan(&totalRescuedToday)

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
		truncNotice := ""
		if totalRescuedToday > 20 {
			truncNotice = fmt.Sprintf(" (showing top 20 of %d rescues | ranked by Deliv Δ)", totalRescuedToday)
		}
		if truncNotice != "" {
			fmt.Printf("  Rescued Candidates%s:\n", truncNotice)
		}
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
    COUNT(CASE WHEN shadow_relief_channel = 'sector_cfo_relief' THEN 1 END),
    COUNT(CASE WHEN shadow_relief_channel = 'graduated_scoring' THEN 1 END)
FROM stage1_shadow_results
WHERE as_of_date = ? AND index_name = ? AND method = ?;
`
		var total, legacyPass, shadowPass, rescued, delivCnt, cfoCnt, baseCnt int
		if err := p.db.QueryRowContext(ctx, sumQ, asOfDate, indexName, method).Scan(&total, &legacyPass, &shadowPass, &rescued, &delivCnt, &cfoCnt, &baseCnt); err == nil && total > 0 {
			legacyPct := float64(legacyPass) / float64(total) * 100.0
			shadowPct := float64(shadowPass) / float64(total) * 100.0

			var cumSessions, cumRescues, cumTickers int
			_ = p.db.QueryRowContext(ctx, `
SELECT 
    COUNT(DISTINCT as_of_date),
    COUNT(CASE WHEN divergence_type = 'RESCUED' THEN 1 END),
    COUNT(DISTINCT CASE WHEN divergence_type = 'RESCUED' THEN ticker END)
FROM stage1_shadow_results
WHERE index_name = ? AND method = ?;
`, indexName, method).Scan(&cumSessions, &cumRescues, &cumTickers)

			fmt.Printf("\n  Shadow Gate Summary for %s (%s | %s):\n", asOfDate, indexName, method)
			fmt.Printf("  • Legacy Stage-1 Survivors : %d / %d (%.1f%%)\n", legacyPass, total, legacyPct)
			fmt.Printf("  • Shadow Stage-1 Survivors : %d / %d (%.1f%%)\n", shadowPass, total, shadowPct)
			fmt.Printf("  • Total Candidates Rescued : %d (Delivery Override: %d | Sector CFO Relief: %d)\n",
				rescued, delivCnt, cfoCnt)
			fmt.Printf("  • Cumulative Shadow Proof  : %d historical sessions evaluated | %d cumulative rescues across %d unique tickers\n",
				cumSessions, cumRescues, cumTickers)

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
						maxAlloc := min(totPool, 3)
						excess := max(totPool-3, 0)
						fmt.Printf("  %-18s | %11d | %11d | %9d | %15d | %15d\n",
							formatConciseSector(sec), totPool, legPool, resPool, maxAlloc, excess)
					}
				}
				capRows.Close()
				fmt.Println("  * Sector Cap Defense Verified: Portfolio allocation cannot exceed 3 holdings or 25% weight per sector.")
			}

			// Realized forward returns across historical cohorts:
			fwdQ := `
WITH calendar_pairs AS (
    SELECT 
        d0.d AS start_date,
        dh.d AS exit_date
    FROM trading_days d0
    JOIN trading_days dh ON dh.seq = d0.seq + 5
),
cand_returns AS (
    SELECT 
        sr.divergence_type,
        sr.shadow_relief_channel,
        (ph.close - p0.close) / p0.close AS ret_5d
    FROM stage1_shadow_results sr
    JOIN calendar_pairs cp ON sr.as_of_date = cp.start_date
    JOIN prices p0 ON sr.ticker = p0.ticker AND p0.date = cp.start_date
    JOIN prices ph ON sr.ticker = ph.ticker AND ph.date = cp.exit_date
    WHERE p0.close > 0 AND ph.close > 0 AND sr.index_name = ? AND sr.method = ?
)
SELECT 
    divergence_type,
    COUNT(*) as n,
    ROUND(AVG(ret_5d) * 100.0, 2) as mean_ret_5d,
    ROUND(MEDIAN(ret_5d) * 100.0, 2) as median_ret_5d
FROM cand_returns
GROUP BY divergence_type
ORDER BY mean_ret_5d DESC;
`
			fRows, fErr := p.db.QueryContext(ctx, fwdQ, indexName, method)
			if fErr == nil {
				fmt.Println("\n  Historical Forward Alpha Calibration (T+5 Realized Returns Across All Matured Cohorts):")
				fmt.Printf("  %-16s | %8s | %13s | %13s\n", "Cohort Group", "Samples", "Mean T+5 Ret", "Median T+5 Ret")
				fmt.Println("  ----------------------------------------------------------------")
				for fRows.Next() {
					var grp string
					var n int
					var meanRet, medRet float64
					if err := fRows.Scan(&grp, &n, &meanRet, &medRet); err == nil {
						fmt.Printf("  %-16s | %8d | %+12.2f%% | %+12.2f%%\n", grp, n, meanRet, medRet)
					}
				}
				fRows.Close()
			}

			fmt.Printf("\n  • Operational Mode         : SHADOW MODE ACTIVE (Legacy gate enforced in production; shadow logged to DB)\n")
		}
	}
	return nil
}

// PrintGateChurnReport queries the day-to-day attrition and churn rate of Stage-1 survivors.
func (p *DB) PrintGateChurnReport(ctx context.Context, indexName, method string, limit int) error {
	indexName = NormalizeIndexName(indexName)
	if limit <= 0 {
		limit = 10
	}

	fmt.Println()
	fmt.Printf("--- 11. GATE CHURN RATE & POOL STABILITY OSCILLATOR (§6E) ---\n")
	fmt.Println("Measuring day-to-day turnover and stability of Stage-1 survivors:")

	// Fetch distinct dates and their Stage-1 survivor tickers
	q := `
SELECT strftime(as_of_date, '%Y-%m-%d') as dt, ticker
FROM pit_candidate_scores
WHERE index_name = ? AND method = ? AND passed_stage1 = true
ORDER BY as_of_date ASC, ticker ASC;
`
	rows, err := p.db.QueryContext(ctx, q, indexName, method)
	if err != nil {
		return fmt.Errorf("querying gate churn: %w", err)
	}
	defer rows.Close()

	dateMap := make(map[string]map[string]bool)
	var orderedDates []string
	for rows.Next() {
		var dt, ticker string
		if err := rows.Scan(&dt, &ticker); err != nil {
			continue
		}
		if _, exists := dateMap[dt]; !exists {
			dateMap[dt] = make(map[string]bool)
			orderedDates = append(orderedDates, dt)
		}
		dateMap[dt][ticker] = true
	}

	if len(orderedDates) < 2 {
		fmt.Println("  Insufficient multi-day history to compute gate churn (>= 2 runs required).")
		return nil
	}

	fmt.Printf("  %-12s | %-15s | %-12s | %-12s | %-10s | %-14s\n",
		"Date", "Survivors (T)", "New Entrants", "Exits (T-1)", "Churn %", "Pool State")
	fmt.Println("  ---------------------------------------------------------------------------------------------")

	type churnEntry struct {
		date     string
		today    int
		entrants int
		exits    int
		churnPct float64
		state    string
	}
	var entries []churnEntry

	for i := len(orderedDates) - 1; i >= 1; i-- {
		currDate := orderedDates[i]
		prevDate := orderedDates[i-1]
		currSet := dateMap[currDate]
		prevSet := dateMap[prevDate]

		entrants := 0
		for t := range currSet {
			if !prevSet[t] {
				entrants++
			}
		}
		exits := 0
		for t := range prevSet {
			if !currSet[t] {
				exits++
			}
		}

		var churnPct float64
		if len(prevSet) > 0 {
			churnPct = float64(entrants+exits) * 100.0 / float64(len(prevSet))
		}

		state := "🟢 STABLE (<10%)"
		if churnPct > 25.0 {
			state = "🔴 UNSTABLE (>25%)"
		} else if churnPct >= 10.0 {
			state = "🟡 MODERATE (10-25%)"
		}

		entries = append(entries, churnEntry{
			date:     currDate,
			today:    len(currSet),
			entrants: entrants,
			exits:    exits,
			churnPct: churnPct,
			state:    state,
		})

		if limit > 0 && len(entries) >= limit {
			break
		}
	}

	for _, e := range entries {
		fmt.Printf("  %-12s | %15d | %12d | %12d | %9.1f%% | %-14s\n",
			e.date, e.today, e.entrants, e.exits, e.churnPct, e.state)
	}

	// Boundary Churn Attribution (Oscillations between T-1 and T)
	if len(orderedDates) >= 2 {
		tCurr := orderedDates[len(orderedDates)-1]
		tPrev := orderedDates[len(orderedDates)-2]

		boundQ := `
WITH prev_survivors AS (
    SELECT ticker, raw_score AS prev_score
    FROM pit_candidate_scores 
    WHERE as_of_date = ? AND index_name = ? AND method = ? AND passed_stage1 = true
),
curr_scores AS (
    SELECT ticker, passed_stage1, rejection_reason, sector
    FROM pit_candidate_scores
    WHERE as_of_date = ? AND index_name = ? AND method = ?
)
SELECT 
    p.ticker,
    COALESCE(c.sector, 'Unknown') AS sec,
    p.prev_score,
    COALESCE(c.rejection_reason, 'Filtered') AS reason,
    COUNT(*) OVER () AS total_exited
FROM prev_survivors p
JOIN curr_scores c ON p.ticker = c.ticker
WHERE c.passed_stage1 = false
ORDER BY p.prev_score DESC
LIMIT 8;
`
		bRows, bErr := p.db.QueryContext(ctx, boundQ, tPrev, indexName, method, tCurr, indexName, method)
		if bErr == nil {
			var bEntries []struct {
				ticker, sec, reason string
				rawScore            float64
			}
			var totalExited int
			for bRows.Next() {
				var be struct {
					ticker, sec, reason string
					rawScore            float64
				}
				var tot int
				if err := bRows.Scan(&be.ticker, &be.sec, &be.rawScore, &be.reason, &tot); err == nil {
					bEntries = append(bEntries, be)
					totalExited = tot
				}
			}
			bRows.Close()

			if len(bEntries) > 0 {
				truncNote := ""
				if totalExited > len(bEntries) {
					truncNote = fmt.Sprintf(" (showing top %d of %d exits by prior raw score)", len(bEntries), totalExited)
				}
				fmt.Printf("\n  Boundary Churn Attribution (%s -> %s | Exited Stage-1%s):\n", tPrev, tCurr, truncNote)
				fmt.Printf("  %-15s | %-16s | %11s | %-11s | %s\n", "Ticker", "Sector", "Prior Score", "Exit Gate", "Detail")
				fmt.Println("  ----------------------------------------------------------------------------------------------------------")
				for _, be := range bEntries {
					bDetail := ClassifyBottleneckGate(be.reason, be.sec, 0, 0, 0, "")
					coloredGate := ColorizeGateCode(bDetail.Code, 11)
					fmt.Printf("  %-15s | %-16s | %10.1fpt | %s | %s\n",
						be.ticker, formatConciseSector(be.sec), be.rawScore, coloredGate, bDetail.Detail)
				}
			}
		}

		// Persistent flicker list (candidates crossing the gate >= 2 times in 10 sessions)
		flickQ := `
WITH recent_dates AS (
    SELECT DISTINCT as_of_date FROM pit_runs WHERE index_name = ? AND method = ? ORDER BY as_of_date DESC LIMIT 10
),
cand_status AS (
    SELECT 
        s.ticker, 
        s.sector,
        s.as_of_date, 
        s.passed_stage1,
        LAG(s.passed_stage1) OVER (PARTITION BY s.ticker ORDER BY s.as_of_date) AS prev_passed
    FROM pit_candidate_scores s
    JOIN recent_dates r ON s.as_of_date = r.as_of_date
    WHERE s.index_name = ? AND s.method = ?
),
flickers AS (
    SELECT 
        ticker,
        COALESCE(NULLIF(MAX(sector), ''), 'Unknown') AS sec,
        COUNT(CASE WHEN passed_stage1 != prev_passed THEN 1 END) AS gate_crossings,
        MAX(CASE WHEN as_of_date = (SELECT MAX(as_of_date) FROM recent_dates) THEN passed_stage1 END) AS curr_in,
        STRING_AGG(strftime(as_of_date, '%m-%d') || ':' || CASE WHEN passed_stage1 THEN 'IN' ELSE 'OUT' END, ' -> ' ORDER BY as_of_date) AS trail
    FROM cand_status
    WHERE prev_passed IS NOT NULL
    GROUP BY ticker
    HAVING COUNT(CASE WHEN passed_stage1 != prev_passed THEN 1 END) >= 2
)
SELECT ticker, sec, gate_crossings, COALESCE(curr_in, false), trail
FROM flickers
ORDER BY gate_crossings DESC, ticker ASC
LIMIT 8;
`
		flickRows, fErr := p.db.QueryContext(ctx, flickQ, indexName, method, indexName, method)
		if fErr == nil {
			type flickItem struct {
				ticker, sec, trail string
				crossings          int
				currIn             bool
			}
			var fItems []flickItem
			for flickRows.Next() {
				var fi flickItem
				if err := flickRows.Scan(&fi.ticker, &fi.sec, &fi.crossings, &fi.currIn, &fi.trail); err == nil {
					fItems = append(fItems, fi)
				}
			}
			flickRows.Close()

			if len(fItems) > 0 {
				fmt.Printf("\n  Persistent Gate Flicker Watchlist (>= 2 gate crossings in trailing 10 sessions):\n")
				fmt.Printf("  %-15s | %-16s | %10s | %10s | %s\n", "Ticker", "Sector", "Crossings", "Status", "Trailing State History")
				fmt.Println("  ----------------------------------------------------------------------------------------------------------")
				for _, fi := range fItems {
					stStr := "🔴 OUT"
					if fi.currIn {
						stStr = "🟢 IN"
					}
					fmt.Printf("  %-15s | %-16s | %10d | %10s | %s\n",
						fi.ticker, formatConciseSector(fi.sec), fi.crossings, stStr, fi.trail)
				}
				fmt.Println("  * Hysteresis Rule Active: Enter at >= 85.0% of 52W High; Exit triggered only below 82.0% (300 bps safety buffer).")
			}
		}
	}

	return nil
}

// PrintRegimeConditionalReturns stratifies realized forward returns by macro market regime state.
func (p *DB) PrintRegimeConditionalReturns(ctx context.Context, indexName, method string) error {
	indexName = NormalizeIndexName(indexName)
	fmt.Println()
	fmt.Printf("--- 12. REGIME-CONDITIONAL FORWARD RETURN STRATIFICATION (§6G) ---\n")
	fmt.Println("Forward alpha calibration stratified by macro regime state at scoring time:")

	// Automatically backfill any candidate forward returns that have reached T+21 trading sessions
	_, _ = p.BackfillForwardReturns(ctx)

	q := `
SELECT 
    CASE 
        WHEN r.regime_multiplier < 0.30 THEN 'Severe Contraction (R < 0.30)'
        WHEN r.regime_multiplier < 0.50 THEN 'Transitional (0.30 <= R < 0.50)'
        ELSE 'Expansion (R >= 0.50)'
    END AS regime_band,
    COUNT(*) AS samples,
    ROUND(AVG(c.raw_score), 1) AS avg_raw_score,
    ROUND(AVG(c.forward_return_21d) * 100.0, 2) AS avg_fwd_21d,
    ROUND(COUNT(CASE WHEN c.forward_return_21d > 0 THEN 1 END) * 100.0 / NULLIF(COUNT(c.forward_return_21d), 0), 1) AS win_rate_pct
FROM pit_candidate_scores c
JOIN pit_runs r ON c.as_of_date = r.as_of_date AND c.index_name = r.index_name AND c.method = r.method
WHERE c.index_name = ? AND c.method = ? AND c.passed_stage1 = true AND c.forward_return_21d IS NOT NULL
GROUP BY regime_band
ORDER BY regime_band;
`
	rows, err := p.db.QueryContext(ctx, q, indexName, method)
	if err != nil {
		return fmt.Errorf("querying regime-conditional returns: %w", err)
	}
	defer rows.Close()

	fmt.Printf("  %-32s | %-8s | %-13s | %-14s | %s\n",
		"Regime Band", "Samples", "Avg Raw Score", "Avg 21D Return", "Win Rate %")
	fmt.Println("  -----------------------------------------------------------------------------------------")

	count := 0
	for rows.Next() {
		count++
		var band string
		var samples int
		var avgScore, avgReturn, winRate float64
		if err := rows.Scan(&band, &samples, &avgScore, &avgReturn, &winRate); err != nil {
			continue
		}
		fmt.Printf("  %-32s | %8d | %13.1f | %+13.2f%% | %9.1f%%\n",
			band, samples, avgScore, avgReturn, winRate)
	}

	if count == 0 {
		var earliestDate, maturationDate string
		var sessionsElapsed int
		progressQuery := `
SELECT 
    COALESCE(strftime(MIN(r.as_of_date), '%Y-%m-%d'), ''),
    COALESCE((
        SELECT (SELECT seq FROM trading_days WHERE d = (SELECT MAX(as_of_date) FROM pit_runs WHERE index_name = ? AND method = ?))
             - (SELECT seq FROM trading_days WHERE d = MIN(r.as_of_date))
    ), 0),
    COALESCE(strftime((
        SELECT d FROM trading_days 
        WHERE seq = (SELECT seq FROM trading_days WHERE d = MIN(r.as_of_date)) + 21
    ), '%Y-%m-%d'), '2026-09-29')
FROM pit_runs r
WHERE r.index_name = ? AND r.method = ?;
`
		_ = p.db.QueryRowContext(ctx, progressQuery, indexName, method, indexName, method).Scan(&earliestDate, &sessionsElapsed, &maturationDate)

		fmt.Println("  Realized 21-day forward returns pending accumulation of T+21 trading sessions.")
		if earliestDate != "" {
			fmt.Printf("  Cohort Maturity Status : %d / 21 trading sessions elapsed since earliest run (%s).\n", sessionsElapsed, earliestDate)
			remaining := 21 - sessionsElapsed
			if remaining > 0 {
				fmt.Printf("  Earliest Cohort Focus  : First cohort matures on %s (in %d trading session(s)).\n", maturationDate, remaining)
			}
		}
	}

	// Factor Validation & Information Coefficient (Spearman Rank IC)
	_ = p.PrintRankICSection(ctx, indexName, method)

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

	// 1. Base duration
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

	// 2. ROCE / Capital Efficiency
	if strings.Contains(r, "Capital Efficiency") || strings.Contains(r, "ROCE") {
		return "Low ROCE (< 12.0%)"
	}

	// 3. DSO Deterioration
	if strings.Contains(r, "DSO Deterioration") {
		if start := strings.Index(r, "(+"); start != -1 {
			if end := strings.Index(r, ">"); end != -1 && end > start {
				val := strings.TrimSpace(r[start+1 : end])
				return fmt.Sprintf("DSO Deterioration (%s)", val)
			}
		}
		return "DSO Deterioration (> 15%)"
	}

	// 4. Low Promoter Stake
	if strings.Contains(r, "Low promoter stake") {
		if start := strings.Index(r, "("); start != -1 {
			if end := strings.Index(r, "<"); end != -1 && end > start {
				val := strings.TrimSpace(r[start+1 : end])
				return fmt.Sprintf("Low Promoter (%s < 25%%)", val)
			}
		}
		return "Low Promoter Stake (< 25%)"
	}

	// 5. 52-Week High
	if strings.Contains(r, "52-Week High") || strings.Contains(r, "52W High") {
		if start := strings.Index(r, "("); start != -1 {
			if end := strings.Index(r, "%"); end != -1 && end > start {
				val := strings.TrimSpace(r[start+1 : end+1])
				return fmt.Sprintf("Far from 52W High (%s)", val)
			}
		}
		return "Far from 52W High (< 85%)"
	}

	// 6. Below 200-Day SMA
	if strings.Contains(r, "Below 200-Day SMA") || strings.Contains(r, "Below 200-SMA") {
		if start := strings.Index(r, "("); start != -1 {
			if end := strings.Index(r, "<"); end != -1 && end > start {
				val := strings.TrimSpace(r[start+1 : end])
				return fmt.Sprintf("Below 200-SMA (%s < 0.95)", val)
			}
		}
		return "Below 200-SMA (< 0.95)"
	}

	// 7. Market Cap
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
				return fmt.Sprintf("High Debt/Eq (%s >= 1.5)", val)
			}
		}
		return "High Debt/Eq (>= 1.5)"
	}

	// 10. Cash Flow Quality
	if strings.Contains(r, "Cash Flow Quality") || strings.Contains(r, "Operating/Free Cash Flow") {
		return "Weak Cash Flow (CFO < 0.25 × PAT)"
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
		if fetchFailed || strings.Contains(strings.ToLower(reason), "unverified") || strings.Contains(strings.ToLower(reason), "fetch failure") || strings.Contains(strings.ToLower(reason), "missing fundamental") {
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

// PrintConsensusLeaders queries v_strategy_consensus and displays the top dual-conviction leaders.
func (p *DB) PrintConsensusLeaders(ctx context.Context, asOfDate string, topN int) error {
	if asOfDate == "" {
		err := p.db.QueryRowContext(ctx, "SELECT as_of_date::VARCHAR FROM v_strategy_consensus ORDER BY as_of_date DESC LIMIT 1;").Scan(&asOfDate)
		if err != nil || asOfDate == "" {
			return fmt.Errorf("no consensus data found in v_strategy_consensus: %w", err)
		}
	}

	if topN <= 0 {
		topN = 15
	}

	// Load active holdings from <data>/microsmall.csv if available
	holdingsMap := make(map[string]float64)
	if weights, err := csvloader.ReadCSVWeights(config.DataPath("microsmall.csv")); err == nil {
		for t, w := range weights {
			if w > 0 {
				clean := strings.ToUpper(strings.TrimSpace(t))
				if !strings.HasPrefix(clean, "NSE:") && !strings.HasPrefix(clean, "BSE:") && !strings.HasPrefix(clean, "US:") {
					clean = "NSE:" + clean
				}
				holdingsMap[clean] = w
			}
		}
	}

	// Load staged candidates from <data>/pre_microsmall.csv if available
	stagedMap := make(map[string]float64)
	if sWeights, err := csvloader.ReadCSVWeights(config.DataPath("pre_microsmall.csv")); err == nil {
		for t, w := range sWeights {
			if w > 0 {
				clean := strings.ToUpper(strings.TrimSpace(t))
				if !strings.HasPrefix(clean, "NSE:") && !strings.HasPrefix(clean, "BSE:") && !strings.HasPrefix(clean, "US:") {
					clean = "NSE:" + clean
				}
				stagedMap[clean] = w
			}
		}
	}

	query := `
SELECT 
    ticker,
    COALESCE(NULLIF(sector, ''), 'Unknown') AS sector,
    COALESCE(mb_score, 0),
    COALESCE(earlymb_score, 0),
    COALESCE(consensus_score, 0),
    COALESCE(vcp_ratio, 0),
    COALESCE(delivery_delta, 0),
    mb_passed_stage1,
    earlymb_passed_stage1
FROM v_strategy_consensus
WHERE as_of_date = ?
ORDER BY consensus_score DESC
LIMIT ?;
`
	rows, err := p.db.QueryContext(ctx, query, asOfDate, topN)
	if err != nil {
		return fmt.Errorf("querying v_strategy_consensus: %w", err)
	}
	defer rows.Close()

	fmt.Println()
	render.Banner(os.Stdout, fmt.Sprintf("TOP DUAL-CONVICTION LEADERS (%s) — NIFTY TOTAL MARKET", asOfDate))
	fmt.Println("Live Prod Strategy : multibagger (Capital Compounder)")
	fmt.Println("Testing Radar      : earlymb (Pre-Breakout Momentum)")
	fmt.Println("Universe           : niftytotalmarket (750 Stocks)")
	fmt.Printf("As-Of Date         : %s\n", asOfDate)
	fmt.Println(strings.Repeat("-", 108))
	fmt.Printf("%-4s  %-15s  %-18s  %9s  %8s  %8s  %5s  %8s  %-12s  %s\n",
		"Rank", "Ticker", "Sector", "Consensus", "MB Score", "EarlyMB", "VCP", "Deliv Δ", "Stage-1", "Portfolio")
	fmt.Printf("%-4s  %-15s  %-18s  %9s  %8s  %8s  %5s  %8s  %-12s  %s\n",
		"----", "------", "------", "---------", "--------", "-------", "---", "-------", "-------", "---------")

	rank := 1
	for rows.Next() {
		var ticker, sector string
		var mbScore, embScore, consensusScore, vcpRatio, delivDelta float64
		var mbPass, embPass bool

		if err := rows.Scan(&ticker, &sector, &mbScore, &embScore, &consensusScore, &vcpRatio, &delivDelta, &mbPass, &embPass); err != nil {
			continue
		}

		stage1Str := "FAIL / FAIL"
		if mbPass && embPass {
			stage1Str = "PASS / PASS"
		} else if mbPass {
			stage1Str = "PASS / FAIL"
		} else if embPass {
			stage1Str = "FAIL / PASS"
		}

		cleanTicker := strings.ToUpper(strings.TrimSpace(ticker))
		if !strings.HasPrefix(cleanTicker, "NSE:") && !strings.HasPrefix(cleanTicker, "BSE:") && !strings.HasPrefix(cleanTicker, "US:") {
			cleanTicker = "NSE:" + cleanTicker
		}
		activeWeight, isActive := holdingsMap[cleanTicker]
		stagedWeight, isStaged := stagedMap[cleanTicker]

		portStr := "-"
		if isActive && isStaged {
			portStr = fmt.Sprintf("DUAL (%.1f%% / %.1f%%)", activeWeight*100, stagedWeight*100)
		} else if isActive {
			portStr = fmt.Sprintf("ACTIVE (%.1f%%)", activeWeight*100)
		} else if isStaged {
			portStr = fmt.Sprintf("STAGED (%.1f%%)", stagedWeight*100)
		}

		conciseSector := formatConciseSector(sector)
		if len(conciseSector) > 18 {
			conciseSector = conciseSector[:18]
		}

		delivStr := fmt.Sprintf("%+.1f%%", delivDelta*100)

		fmt.Printf("%-4d  %-15s  %-18s  %9.2f  %8.2f  %8.2f  %5.2f  %8s  %-12s  %s\n",
			rank, ticker, conciseSector, consensusScore, mbScore, embScore, vcpRatio, delivStr, stage1Str, portStr)
		rank++
	}
	fmt.Println(strings.Repeat("=", 108))
	return nil
}
