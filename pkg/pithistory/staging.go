package pithistory

import (
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/render"
)

// StagedCandidate represents a qualified candidate staged for Tier-2 pre-production.
type StagedCandidate struct {
	Ticker            string  `json:"ticker"`
	Sector            string  `json:"sector"`
	Weight            float64 `json:"weight"`
	SetupQuality      float64 `json:"setup_quality"`
	IgnitionSignal    string  `json:"ignition_signal"`
	IgnitionCode      string  `json:"ignition_code"`
	DeliveryDelta     float64 `json:"delivery_delta"`
	VCPRatio          float64 `json:"vcp_ratio"`
	CompositeRS       float64 `json:"composite_rs"`
	RawScore          float64 `json:"raw_score"`
	ScoreCV           float64 `json:"score_cv"`
	PersistenceStreak int     `json:"persistence_streak"`
	CurrentPrice      float64 `json:"current_price"`
	ATR20             float64 `json:"atr_20"`
	ADV20D            float64 `json:"adv_20d"`
}

// CalculateSetupQuality measures kinetic coiling tension:
// Setup Quality = max(0.10, 1 + Composite RS) / (VCP Ratio + 0.10)
func CalculateSetupQuality(compositeRS, vcpRatio float64) float64 {
	num := math.Max(0.10, 1.0+compositeRS)
	denom := vcpRatio + 0.10
	if denom <= 0 {
		denom = 0.10
	}
	return num / denom
}

// GetIgnitionStatus returns the timing readiness classification for Pillar 4 Delivery Delta.
func GetIgnitionStatus(delivDelta float64) (code string, label string) {
	if delivDelta >= 0.05 {
		return "ACTIVE", "🟢 IGNITION ACTIVE"
	} else if delivDelta <= -0.05 {
		return "COOLING", "🔴 COOLING"
	}
	return "NEUTRAL", "🟡 NEUTRAL"
}

// CalculateScoreCV computes the coefficient of variation (sigma / mean) across raw scores.
func CalculateScoreCV(scores []float64) float64 {
	if len(scores) < 2 {
		return 0.0
	}
	var sum float64
	for _, s := range scores {
		sum += s
	}
	mean := sum / float64(len(scores))
	if mean <= 0 {
		return 0.0
	}

	var varianceSum float64
	for _, s := range scores {
		diff := s - mean
		varianceSum += diff * diff
	}
	variance := varianceSum / float64(len(scores))
	stdDev := math.Sqrt(variance)
	return stdDev / mean
}

// CalculateRiskParityWeights sizes candidates using Inverse Volatility with 8% single-stock
// and 25% sector caps, iteratively redistributing unallocated residuals.
func CalculateRiskParityWeights(candidates []StagedCandidate) []StagedCandidate {
	if len(candidates) == 0 {
		return nil
	}

	// 1. Sector pruning: max 3 stocks per sector (sorted by SetupQuality descending)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].SetupQuality > candidates[j].SetupQuality
	})

	sectorCounts := make(map[string]int)
	var filtered []StagedCandidate
	for _, c := range candidates {
		sec := c.Sector
		if sec == "" {
			sec = "Unknown"
		}
		if sectorCounts[sec] < 3 {
			sectorCounts[sec]++
			filtered = append(filtered, c)
		}
	}
	candidates = filtered
	n := len(candidates)
	if n == 0 {
		return nil
	}

	// 2. Compute Inverse Volatility raw weight: w_raw_i = Price / ATR_20
	// If candidate CV > 0.30, apply a 50% stability penalty.
	rawWeights := make([]float64, n)
	var totalRaw float64
	for i, c := range candidates {
		atrPct := 0.03 // fallback 3%
		if c.CurrentPrice > 0 && c.ATR20 > 0 {
			atrPct = c.ATR20 / c.CurrentPrice
		}
		if atrPct < 0.005 {
			atrPct = 0.005
		}

		invVol := 1.0 / atrPct
		if c.ScoreCV > 0.30 {
			invVol *= 0.50
		}
		rawWeights[i] = invVol
		totalRaw += invVol
	}

	weights := make([]float64, n)
	for i := range weights {
		if totalRaw > 0 {
			weights[i] = rawWeights[i] / totalRaw
		} else {
			weights[i] = 1.0 / float64(n)
		}
	}

	// 3. Iterative constraint satisfaction (single stock cap = 8%, sector cap = 25%)
	const maxStockWeight = 0.08
	const maxSectorWeight = 0.25

	for iter := 0; iter < 50; iter++ {
		constrained := false

		// Check single stock cap
		for i := 0; i < n; i++ {
			if weights[i] > maxStockWeight+1e-7 {
				weights[i] = maxStockWeight
				constrained = true
			}
		}

		// Check sector cap
		sectorAlloc := make(map[string]float64)
		for i, c := range candidates {
			sec := c.Sector
			if sec == "" {
				sec = "Unknown"
			}
			sectorAlloc[sec] += weights[i]
		}

		for sec, sWeight := range sectorAlloc {
			if sWeight > maxSectorWeight+1e-7 {
				constrained = true
				scale := maxSectorWeight / sWeight
				for i, c := range candidates {
					cSec := c.Sector
					if cSec == "" {
						cSec = "Unknown"
					}
					if cSec == sec {
						weights[i] *= scale
					}
				}
			}
		}

		// Normalize / redistribute unallocated residual weight across unconstrained stocks
		var sumWeight float64
		for _, w := range weights {
			sumWeight += w
		}

		if math.Abs(sumWeight-1.0) < 1e-4 || !constrained {
			break
		}

		residual := 1.0 - sumWeight
		if residual > 0 {
			var eligibleTotal float64
			var eligibleIndices []int
			for i, c := range candidates {
				sec := c.Sector
				if sec == "" {
					sec = "Unknown"
				}
				if weights[i] < maxStockWeight-1e-5 && sectorAlloc[sec] < maxSectorWeight-1e-5 {
					eligibleIndices = append(eligibleIndices, i)
					eligibleTotal += weights[i]
				}
			}

			if len(eligibleIndices) == 0 || eligibleTotal <= 0 {
				break
			}

			for _, idx := range eligibleIndices {
				weights[idx] += residual * (weights[idx] / eligibleTotal)
			}
		}
	}

	// Normalize final weights to sum exactly to 1.0
	var finalSum float64
	for _, w := range weights {
		finalSum += w
	}
	if finalSum > 0 {
		for i := range weights {
			candidates[i].Weight = math.Round((weights[i]/finalSum)*1000.0) / 1000.0
		}
	}

	return candidates
}

// StagePreProduction generates the pre-production staging basket (data/pre_microsmall.csv)
// and updates the companion DuckDB audit table.
// If excludeBasketPath is provided, any active holdings in that basket are filtered out to ensure mutual exclusion.
func (p *DB) StagePreProduction(ctx context.Context, indexName, method string, topN int, outCSVPath string, excludeBasketPath ...string) ([]StagedCandidate, error) {
	indexName = NormalizeIndexName(indexName)
	if outCSVPath == "" {
		outCSVPath = config.DataPath("pre_microsmall.csv")
	}
	if topN <= 0 {
		topN = 15
	}

	excludedTickers := make(map[string]bool)
	if len(excludeBasketPath) > 0 && excludeBasketPath[0] != "" {
		if exWeights, exErr := csvloader.ReadCSVWeights(excludeBasketPath[0]); exErr == nil {
			for t, w := range exWeights {
				if w > 0.00001 {
					excludedTickers[cleanSymbol(t)] = true
				}
			}
		}
	}

	latestDate, err := p.GetLatestRunDate(ctx, indexName, method)
	if err != nil || latestDate == "" {
		latestDate, _ = p.GetLatestRunDate(ctx, "niftytotalmarket", method)
		if latestDate == "" {
			return nil, fmt.Errorf("no historical runs found for %s (%s)", indexName, method)
		}
	}

	// 1. Query Stage-1 survivors from latest run with their Markov persistence and score trajectory
	query := `
WITH recent_runs AS (
    SELECT as_of_date 
    FROM pit_runs 
    WHERE index_name = ? AND method = ? AND as_of_date <= ?
    ORDER BY as_of_date DESC 
    LIMIT 5
),
candidate_history AS (
    SELECT 
        c.ticker,
        COUNT(DISTINCT c.as_of_date) AS runs_present,
        SUM(CASE WHEN c.passed_stage1 THEN 1 ELSE 0 END) AS runs_passed,
        AVG(c.raw_score) AS avg_score,
        STDDEV_POP(c.raw_score) AS std_score
    FROM pit_candidate_scores c
    JOIN recent_runs r ON c.as_of_date = r.as_of_date
    WHERE c.index_name = ? AND c.method = ?
    GROUP BY c.ticker
),
latest_candidates AS (
    SELECT 
        c.ticker,
        COALESCE(NULLIF(c.sector, ''), 'Unknown') AS sector,
        c.raw_score,
        c.effective_score,
        c.composite_rs,
        c.vcp_ratio,
        c.delivery_delta,
        c.rvol_z_score,
        COALESCE(h.runs_passed, 1) AS persistence_streak,
        CASE 
            WHEN COALESCE(h.avg_score, 0) > 0 THEN COALESCE(h.std_score, 0) / h.avg_score 
            ELSE 0.0 
        END AS score_cv
    FROM v_pit_candidate_scores c
    LEFT JOIN candidate_history h ON c.ticker = h.ticker
    WHERE c.as_of_date = ?
      AND c.index_name = ?
      AND c.method = ?
      AND c.passed_stage1 = true
      AND (c.data_fetch_failed = false OR c.data_fetch_failed IS NULL)
)
SELECT 
    ticker,
    sector,
    raw_score,
    effective_score,
    composite_rs,
    vcp_ratio,
    delivery_delta,
    rvol_z_score,
    persistence_streak,
    score_cv
FROM latest_candidates
WHERE persistence_streak >= 1
ORDER BY raw_score DESC;
`
	rows, err := p.db.QueryContext(ctx, query, indexName, method, latestDate, indexName, method, latestDate, indexName, method)
	if err != nil {
		return nil, fmt.Errorf("querying candidates for staging: %w", err)
	}

	var rawCandidates []StagedCandidate
	for rows.Next() {
		var cand StagedCandidate
		var eff, rvol float64
		if err := rows.Scan(
			&cand.Ticker,
			&cand.Sector,
			&cand.RawScore,
			&eff,
			&cand.CompositeRS,
			&cand.VCPRatio,
			&cand.DeliveryDelta,
			&rvol,
			&cand.PersistenceStreak,
			&cand.ScoreCV,
		); err == nil {
			cand.SetupQuality = CalculateSetupQuality(cand.CompositeRS, cand.VCPRatio)
			cand.IgnitionCode, cand.IgnitionSignal = GetIgnitionStatus(cand.DeliveryDelta)
			rawCandidates = append(rawCandidates, cand)
		}
	}
	rows.Close()

	var qualified []StagedCandidate
	for _, cand := range rawCandidates {
		// Fetch latest price and ATR20 from prices table
		pQuery := `
SELECT close, ABS(close - COALESCE(open, close)) as bar_atr, COALESCE(volume, 0) * close as traded_val
FROM prices 
WHERE ticker = ? AND date <= ?
ORDER BY date DESC 
LIMIT 20;
`
		if pRows, pErr := p.db.QueryContext(ctx, pQuery, cand.Ticker, latestDate); pErr == nil {
			var closes []float64
			var atrs []float64
			var advSum float64
			for pRows.Next() {
				var cl, barAtr, val float64
				if pRows.Scan(&cl, &barAtr, &val) == nil {
					closes = append(closes, cl)
					atrs = append(atrs, barAtr)
					advSum += val
				}
			}
			pRows.Close()

			if len(closes) > 0 {
				cand.CurrentPrice = closes[0]
			}
			if len(atrs) > 0 {
				var sumAtr float64
				for _, a := range atrs {
					sumAtr += a
				}
				cand.ATR20 = sumAtr / float64(len(atrs))
				cand.ADV20D = advSum / float64(len(atrs))
			}
		}

		// Minimum liquidity capacity defense: ADV >= ₹50L (fallback ₹1 Cr preferred)
		if cand.ADV20D > 0 && cand.ADV20D < 5000000 {
			continue
		}

		cleanT := cleanSymbol(cand.Ticker)
		if excludedTickers[cleanT] {
			continue
		}

		qualified = append(qualified, cand)
	}

	if len(qualified) == 0 {
		return nil, fmt.Errorf("no candidates passed Stage-1 qualification and persistence filters for staging")
	}

	// Sort by Setup Quality (primary) and tiebreak with Ignition Signal
	sort.Slice(qualified, func(i, j int) bool {
		if math.Abs(qualified[i].SetupQuality-qualified[j].SetupQuality) > 0.05 {
			return qualified[i].SetupQuality > qualified[j].SetupQuality
		}
		return qualified[i].DeliveryDelta > qualified[j].DeliveryDelta
	})

	if len(qualified) > topN {
		qualified = qualified[:topN]
	}

	// Size portfolio with Inverse Volatility Risk Parity
	staged := CalculateRiskParityWeights(qualified)

	// Persist to data/pre_microsmall.csv
	if err := os.MkdirAll(filepath.Dir(outCSVPath), 0755); err != nil {
		return nil, fmt.Errorf("create dir for %s: %w", outCSVPath, err)
	}
	file, err := os.Create(outCSVPath)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", outCSVPath, err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write([]string{"ticker", "weight"}); err != nil {
		return nil, err
	}
	for _, s := range staged {
		if err := writer.Write([]string{s.Ticker, fmt.Sprintf("%.3f", s.Weight)}); err != nil {
			return nil, err
		}
	}

	// Persist companion audit table in DuckDB
	if err := p.saveStagingAudit(ctx, latestDate, staged); err != nil {
		fmt.Printf("Notice: failed to record staging audit in DuckDB: %v\n", err)
	}

	return staged, nil
}

func (p *DB) saveStagingAudit(ctx context.Context, asOfDate string, staged []StagedCandidate) error {
	schemaQuery := `
CREATE TABLE IF NOT EXISTS pre_production_staging (
    as_of_date VARCHAR,
    ticker VARCHAR,
    sector VARCHAR,
    weight DOUBLE,
    setup_quality DOUBLE,
    ignition_signal VARCHAR,
    delivery_delta DOUBLE,
    vcp_ratio DOUBLE,
    composite_rs DOUBLE,
    raw_score DOUBLE,
    score_cv DOUBLE,
    persistence_streak INT,
    current_price DOUBLE,
    atr_20 DOUBLE,
    adv_20d DOUBLE,
    created_at TIMESTAMP,
    PRIMARY KEY (as_of_date, ticker)
);
`
	if _, err := p.db.ExecContext(ctx, schemaQuery); err != nil {
		return err
	}

	// Delete existing records for today
	_, _ = p.db.ExecContext(ctx, "DELETE FROM pre_production_staging WHERE as_of_date = ?", asOfDate)

	insertQuery := `
INSERT INTO pre_production_staging (
    as_of_date, ticker, sector, weight, setup_quality, ignition_signal,
    delivery_delta, vcp_ratio, composite_rs, raw_score, score_cv,
    persistence_streak, current_price, atr_20, adv_20d, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP);
`
	for _, s := range staged {
		_, err := p.db.ExecContext(ctx, insertQuery,
			asOfDate, s.Ticker, s.Sector, s.Weight, s.SetupQuality, s.IgnitionSignal,
			s.DeliveryDelta, s.VCPRatio, s.CompositeRS, s.RawScore, s.ScoreCV,
			s.PersistenceStreak, s.CurrentPrice, s.ATR20, s.ADV20D,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// PrintStagingSummary renders the terminal report for pre-production staging.
func PrintStagingSummary(candidates []StagedCandidate, outCSVPath string, excludeBasketPath ...string) {
	fmt.Println()
	render.Banner(os.Stdout, "PRE-PRODUCTION STAGING BASKET (TIER 2 INCUBATOR)")
	fmt.Printf("Staged Output Path : %s\n", outCSVPath)
	if len(excludeBasketPath) > 0 && excludeBasketPath[0] != "" {
		fmt.Printf("Excluded Basket    : %s (Air-Gapped Mutual Exclusion Active)\n", excludeBasketPath[0])
	}
	fmt.Printf("Weighting Engine   : Inverse-Volatility Risk Parity (Max 8%% Single Stock / Max 25%% Sector Cap)\n")
	fmt.Printf("Ranking Metric     : Setup Quality = max(0.10, 1 + Comp RS) / (VCP + 0.10) [Insulated from Delivery]\n")
	fmt.Printf("Timing Overlay     : Ignition Signal = Delivery Delta %% (Tiebreaker & Visual Flag)\n")
	fmt.Println(strings.Repeat("-", 125))
	fmt.Printf("%-4s  %-15s  %-18s  %7s  %13s  %-18s  %7s  %7s  %7s  %8s  %s\n",
		"Rank", "Ticker", "Sector", "Weight", "Setup Quality", "Ignition Signal", "VCP", "Comp RS", "Deliv Δ", "Score CV", "Streak")
	fmt.Printf("%-4s  %-15s  %-18s  %7s  %13s  %-18s  %7s  %7s  %7s  %8s  %s\n",
		"----", "------", "------", "------", "-------------", "---------------", "---", "-------", "-------", "--------", "------")

	var totalWeight float64
	for i, c := range candidates {
		totalWeight += c.Weight
		sector := c.Sector
		if len(sector) > 18 {
			sector = sector[:18]
		}
		fmt.Printf("%-4d  %-15s  %-18s  %6.1f%%  %13.2f  %-18s  %7.2f  %+6.1f%%  %+6.1f%%  %7.2f   %dx\n",
			i+1, c.Ticker, sector, c.Weight*100.0, c.SetupQuality, c.IgnitionSignal,
			c.VCPRatio, c.CompositeRS*100.0, c.DeliveryDelta*100.0, c.ScoreCV, c.PersistenceStreak)
	}
	fmt.Println(strings.Repeat("-", 125))
	fmt.Printf("Total Allocated Basket Weight: %.1f%% across %d candidates\n", totalWeight*100.0, len(candidates))
	fmt.Println(strings.Repeat("=", 125))
}
