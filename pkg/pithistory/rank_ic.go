package pithistory

import (
	"context"
	"fmt"
	"math"
	"sort"
)

// DailyRankIC represents the Spearman Rank IC on a single trading date.
type DailyRankIC struct {
	Date          string
	Horizon       int
	SampleN       int
	SpearmanRho   float64
	BenchEWReturn float64
}

// RankICSummary encapsulates the aggregated multi-date factor predictive efficacy.
type RankICSummary struct {
	Horizon        int
	CohortType     string // "Universe" or "Stage-1 Survivors"
	DatesEvaluated int
	MeanRho        float64
	StdDevRho      float64
	StandardError  float64
	TStatistic     float64
	PositiveHitPct float64
	Status         string
	DailyICs       []DailyRankIC
}

// rankItem holds a value and its original index for ranking with tie handling.
type rankItem struct {
	val float64
	idx int
}

// computeRanks calculates fractional ranks (1-based, average rank for ties).
func computeRanks(values []float64) []float64 {
	n := len(values)
	ranks := make([]float64, n)
	if n == 0 {
		return ranks
	}

	items := make([]rankItem, n)
	for i, v := range values {
		items[i] = rankItem{val: v, idx: i}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].val < items[j].val
	})

	i := 0
	for i < n {
		j := i
		for j < n-1 && items[j+1].val == items[j].val {
			j++
		}
		// Average rank for tied items (1-indexed)
		rankSum := 0.0
		for k := i; k <= j; k++ {
			rankSum += float64(k + 1)
		}
		avgRank := rankSum / float64(j-i+1)
		for k := i; k <= j; k++ {
			ranks[items[k].idx] = avgRank
		}
		i = j + 1
	}

	return ranks
}

// ComputeSpearmanCorrelation computes Pearson correlation on ranks, correctly handling ties.
func ComputeSpearmanCorrelation(x, y []float64) float64 {
	if len(x) != len(y) || len(x) < 3 {
		return 0.0
	}
	rx := computeRanks(x)
	ry := computeRanks(y)

	n := float64(len(rx))
	var sumX, sumY float64
	for i := 0; i < len(rx); i++ {
		sumX += rx[i]
		sumY += ry[i]
	}
	meanX := sumX / n
	meanY := sumY / n

	var num, denX, denY float64
	for i := 0; i < len(rx); i++ {
		dx := rx[i] - meanX
		dy := ry[i] - meanY
		num += dx * dy
		denX += dx * dx
		denY += dy * dy
	}

	den := math.Sqrt(denX * denY)
	if den <= 1e-12 {
		return 0.0
	}
	return num / den
}

// ComputeHorizonRankIC evaluates daily Spearman Rank IC across historical dates for a given horizon.
func (p *DB) ComputeHorizonRankIC(ctx context.Context, indexName, method string, horizon int, stage1Only bool) (*RankICSummary, error) {
	cohortType := "Stage-1 Survivors"
	if !stage1Only {
		cohortType = "Universe (NTM)"
	}

	// 1. Fetch distinct candidate score dates where horizon sessions exist in trading_days
	dateQuery := `
WITH calendar_pairs AS (
    SELECT 
        d0.d::VARCHAR AS start_date,
        dh.d::VARCHAR AS exit_date
    FROM trading_days d0
    JOIN trading_days dh ON dh.seq = d0.seq + ?
)
SELECT DISTINCT strftime(s.as_of_date, '%Y-%m-%d') as dt, cp.exit_date
FROM v_pit_candidate_scores s
JOIN calendar_pairs cp ON strftime(s.as_of_date, '%Y-%m-%d') = cp.start_date
WHERE s.index_name = ? AND s.method = ?
ORDER BY dt ASC;
`
	rows, err := p.db.QueryContext(ctx, dateQuery, horizon, indexName, method)
	if err != nil {
		return nil, fmt.Errorf("querying eligible dates for horizon %d: %w", horizon, err)
	}
	defer rows.Close()

	type datePair struct {
		startDate string
		exitDate  string
	}
	var pairs []datePair
	for rows.Next() {
		var dp datePair
		if err := rows.Scan(&dp.startDate, &dp.exitDate); err == nil {
			pairs = append(pairs, dp)
		}
	}

	summary := &RankICSummary{
		Horizon:    horizon,
		CohortType: cohortType,
	}

	if len(pairs) == 0 {
		summary.Status = fmt.Sprintf("[PENDING: requires %d forward sessions; 0 cohorts matured]", horizon)
		return summary, nil
	}

	var rhos []float64
	positiveCount := 0

	for _, dp := range pairs {
		// Fetch scores and prices for this date pair
		// Panel A (stage1Only=true): only Stage-1 survivors with raw_score > 0
		// Panel B (stage1Only=false): full universe including zero-score tied rejects
		candQuery := `
SELECT 
    s.ticker,
    s.raw_score,
    p0.close AS p0_close,
    ph.close AS ph_close
FROM v_pit_candidate_scores s
JOIN prices p0 ON s.ticker = p0.ticker AND p0.date = ?
JOIN prices ph ON s.ticker = ph.ticker AND ph.date = ?
WHERE s.as_of_date = ? 
  AND s.index_name = ? 
  AND s.method = ?
  AND (? = false OR s.passed_stage1 = true)
  AND (s.data_fetch_failed = false OR s.data_fetch_failed IS NULL)
  AND (? = false OR s.raw_score > 0.0)
  AND p0.close > 0 AND ph.close > 0;
`
		candRows, cErr := p.db.QueryContext(ctx, candQuery, dp.startDate, dp.exitDate, dp.startDate, indexName, method, stage1Only, stage1Only)
		if cErr != nil {
			continue
		}

		var rawScores []float64
		var fwdReturns []float64
		var retSum float64

		for candRows.Next() {
			var ticker string
			var rawScore, p0Close, phClose float64
			if err := candRows.Scan(&ticker, &rawScore, &p0Close, &phClose); err == nil {
				fwdRet := (phClose - p0Close) / p0Close
				rawScores = append(rawScores, rawScore)
				fwdReturns = append(fwdReturns, fwdRet)
				retSum += fwdRet
			}
		}
		candRows.Close()

		n := len(rawScores)
		if n < 5 {
			continue
		}

		meanRet := retSum / float64(n)
		excessReturns := make([]float64, n)
		for i, r := range fwdReturns {
			excessReturns[i] = r - meanRet
		}

		rho := ComputeSpearmanCorrelation(rawScores, excessReturns)
		rhos = append(rhos, rho)
		if rho > 0 {
			positiveCount++
		}

		summary.DailyICs = append(summary.DailyICs, DailyRankIC{
			Date:          dp.startDate,
			Horizon:       horizon,
			SampleN:       n,
			SpearmanRho:   rho,
			BenchEWReturn: meanRet,
		})
	}

	summary.DatesEvaluated = len(rhos)
	if len(rhos) == 0 {
		summary.Status = fmt.Sprintf("[PENDING: requires %d forward sessions; 0 cohorts matured]", horizon)
		return summary, nil
	}

	// Compute Mean Rho
	var sumRho float64
	for _, r := range rhos {
		sumRho += r
	}
	summary.MeanRho = sumRho / float64(len(rhos))

	T := len(rhos)
	if T > 1 {
		// Newey-West variance estimator for overlapping return horizons:
		// Lag truncation L = horizon - 1
		L := horizon - 1
		if L >= T {
			L = T - 1
		}

		// gamma_0 (sample variance)
		var gamma0 float64
		for _, r := range rhos {
			diff := r - summary.MeanRho
			gamma0 += diff * diff
		}
		gamma0 /= float64(T)

		omega := gamma0
		for l := 1; l <= L; l++ {
			var gammaL float64
			for t := l; t < T; t++ {
				gammaL += (rhos[t] - summary.MeanRho) * (rhos[t-l] - summary.MeanRho)
			}
			gammaL /= float64(T)

			// Bartlett kernel weight
			wL := 1.0 - float64(l)/float64(L+1)
			omega += 2.0 * wL * gammaL
		}

		if omega <= 0 {
			omega = gamma0
		}

		// Newey-West standard error of the mean
		summary.StandardError = math.Sqrt(omega / float64(T))
		summary.StdDevRho = math.Sqrt(gamma0 * float64(T) / float64(T-1))
		if summary.StandardError > 1e-9 {
			summary.TStatistic = summary.MeanRho / summary.StandardError
		}
	}

	summary.PositiveHitPct = float64(positiveCount) * 100.0 / float64(len(rhos))

	effSamples := float64(summary.DatesEvaluated) / float64(horizon)
	if summary.DatesEvaluated < 10 || effSamples < 2.0 {
		summary.Status = fmt.Sprintf("n = %d dates (eff indep = %.1f) [INSUFFICIENT SAMPLE]", summary.DatesEvaluated, effSamples)
	} else if math.Abs(summary.TStatistic) < 1.96 {
		summary.Status = fmt.Sprintf("UNCONFIRMED / NOISY (|t_NW| = %.2f < 1.96)", math.Abs(summary.TStatistic))
	} else {
		summary.Status = "CONFIRMED (p < 0.05, NW-adj)"
	}

	return summary, nil
}

// PrintRankICSection outputs Section 13 Factor Predictive Efficacy (Spearman Rank IC).
func (p *DB) PrintRankICSection(ctx context.Context, indexName, method string) error {
	fmt.Printf("\n--- 13. FACTOR VALIDATION & INFORMATION COEFFICIENT (Rank IC by Session Date) ---\n")
	fmt.Println("Evaluating monotonic predictive power of Stage-1 raw scores versus forward excess returns:")
	fmt.Println("Formula: Daily Spearman ρ(Raw Score, Forward Excess Return) across matured trading sessions.")

	horizons := []int{5, 10, 21}

	fmt.Printf("\n  A. STAGE-1 QUALIFIED SURVIVORS (Alpha Separation within Screened Cohort):\n")
	fmt.Printf("  %-8s | %-12s | %-10s | %-10s | %-8s | %-9s | %s\n",
		"Horizon", "Dates Eval", "Mean IC (ρ)", "Std Dev", "t-Stat", "Hit Rate", "Statistical Status")
	fmt.Println("  -----------------------------------------------------------------------------------------------------")

	for _, h := range horizons {
		sum, err := p.ComputeHorizonRankIC(ctx, indexName, method, h, true)
		if err != nil {
			fmt.Printf("  T+%-5d | ERROR: %v\n", h, err)
			continue
		}
		if sum.DatesEvaluated == 0 {
			fmt.Printf("  T+%-5d | %12d | %10s | %10s | %8s | %9s | %s\n",
				h, 0, "n/a", "n/a", "n/a", "n/a", sum.Status)
			continue
		}
		fmt.Printf("  T+%-5d | %12d | %+10.4f | %10.4f | %+8.2f | %8.1f%% | %s\n",
			h, sum.DatesEvaluated, sum.MeanRho, sum.StdDevRho, sum.TStatistic, sum.PositiveHitPct, sum.Status)
	}

	fmt.Printf("\n  B. FULL INDEX UNIVERSE (Cross-Sectional Factor Monotonicity):\n")
	fmt.Printf("  %-8s | %-12s | %-10s | %-10s | %-8s | %-9s | %s\n",
		"Horizon", "Dates Eval", "Mean IC (ρ)", "Std Dev", "t-Stat", "Hit Rate", "Statistical Status")
	fmt.Println("  -----------------------------------------------------------------------------------------------------")

	for _, h := range horizons {
		sum, err := p.ComputeHorizonRankIC(ctx, indexName, method, h, false)
		if err != nil {
			fmt.Printf("  T+%-5d | ERROR: %v\n", h, err)
			continue
		}
		if sum.DatesEvaluated == 0 {
			fmt.Printf("  T+%-5d | %12d | %10s | %10s | %8s | %9s | %s\n",
				h, 0, "n/a", "n/a", "n/a", "n/a", sum.Status)
			continue
		}
		fmt.Printf("  T+%-5d | %12d | %+10.4f | %10.4f | %+8.2f | %8.1f%% | %s\n",
			h, sum.DatesEvaluated, sum.MeanRho, sum.StdDevRho, sum.TStatistic, sum.PositiveHitPct, sum.Status)
	}

	fmt.Println("\n  * Methodological Notes on Factor Efficacy Validation:")
	fmt.Println("    - Overlapping Multi-Day Windows: Daily cohorts (T+5, T+10) share forward price returns; unadjusted t-statistics inflate statistical confidence due to serial correlation.")
	fmt.Println("    - Score Ties in Full Universe: Candidates dropped by Stage-1 gates receive a raw score of 0.0; universe rank correlation captures the binary gate barrier alongside score monotonicity.")

	return nil
}
