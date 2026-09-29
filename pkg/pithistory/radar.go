package pithistory

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
)

type RadarEpisode struct {
	EpisodeID       string
	Ticker          string
	Sector          string
	FirstSeenDate   string
	IndexName       string
	Method          string
	EntryDate       string
	EntryPxOpen     float64
	EntryPxCloseT0  float64
	CriteriaVersion string
	BlockerAtEntry  string
}

type HorizonReturnRecord struct {
	EpisodeID     string
	Horizon       int
	ExitDate      string
	ExitPx        float64
	Ret           float64
	BenchEWMean   float64
	BenchEWMedian float64
	BenchN        int
	Excess        float64
	StateAtH      string
	LastPriceFlag bool
}

// SyncRadarEpisodes scans historical candidate scores and groups radar signals into episodes
// applying the 10-session re-entry hysteresis rule.
func (p *DB) SyncRadarEpisodes(ctx context.Context, indexName, method string) (int, error) {
	indexName = NormalizeIndexName(indexName)

	// Fetch all radar appearances in chronological order
	query := `
SELECT 
    s.as_of_date::VARCHAR,
    s.ticker,
    COALESCE(NULLIF(s.sector, ''), 'Unknown') AS sector,
    COALESCE(NULLIF(s.rejection_reason, ''), 'Stage-1 Blocked') AS blocker,
    t.seq AS date_seq
FROM pit_candidate_scores s
JOIN trading_days t ON s.as_of_date = t.d
WHERE s.index_name = ? AND s.method = ?
  AND s.passed_stage1 = false
  AND s.delivery_delta >= 0.08
  AND s.composite_rs >= 0.0
  AND (s.data_fetch_failed = false OR s.data_fetch_failed IS NULL)
ORDER BY s.ticker, t.seq;
`
	rows, err := p.db.QueryContext(ctx, query, indexName, method)
	if err != nil {
		return 0, fmt.Errorf("query radar appearances: %w", err)
	}
	defer rows.Close()

	type radarHit struct {
		asOfDate string
		ticker   string
		sector   string
		blocker  string
		seq      int
	}

	tickerHits := make(map[string][]radarHit)
	for rows.Next() {
		var h radarHit
		if err := rows.Scan(&h.asOfDate, &h.ticker, &h.sector, &h.blocker, &h.seq); err == nil {
			tickerHits[h.ticker] = append(tickerHits[h.ticker], h)
		}
	}

	var episodesToInsert []RadarEpisode

	for ticker, hits := range tickerHits {
		if len(hits) == 0 {
			continue
		}

		var currentEpisodeStart radarHit
		var lastSeq int

		for i, h := range hits {
			if i == 0 {
				currentEpisodeStart = h
				lastSeq = h.seq
				continue
			}

			// If gap between consecutive sightings is > 10 trading sessions, close previous episode
			if h.seq-lastSeq > 10 {
				// Record closed episode
				episodesToInsert = append(episodesToInsert, p.buildRadarEpisode(ctx, currentEpisodeStart, indexName, method))
				currentEpisodeStart = h
			}
			lastSeq = h.seq
		}
		// Record final episode
		episodesToInsert = append(episodesToInsert, p.buildRadarEpisode(ctx, currentEpisodeStart, indexName, method))
		_ = ticker
	}

	inserted := 0
	insertQuery := `
INSERT INTO radar_episodes (
    episode_id, ticker, sector, first_seen_date, index_name, method,
    entry_date, entry_px_open, entry_px_close_t0, criteria_version, blocker_at_entry
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (episode_id) DO NOTHING;
`
	for _, ep := range episodesToInsert {
		if ep.EntryPxOpen <= 0 {
			continue
		}
		res, err := p.db.ExecContext(ctx, insertQuery,
			ep.EpisodeID, ep.Ticker, ep.Sector, ep.FirstSeenDate, ep.IndexName, ep.Method,
			ep.EntryDate, ep.EntryPxOpen, ep.EntryPxCloseT0, ep.CriteriaVersion, ep.BlockerAtEntry,
		)
		if err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				inserted++
			}
		}
	}

	return inserted, nil
}

func (p *DB) buildRadarEpisode(ctx context.Context, h struct {
	asOfDate string
	ticker   string
	sector   string
	blocker  string
	seq      int
}, indexName, method string) RadarEpisode {
	episodeID := fmt.Sprintf("%s|%s", h.ticker, h.asOfDate)

	// Find next trading session (T+1) for execution
	var entryDate string
	var nextSeq int
	_ = p.db.QueryRowContext(ctx, "SELECT d::VARCHAR, seq FROM trading_days WHERE seq = ? + 1;", h.seq).Scan(&entryDate, &nextSeq)

	// Fetch T0 close
	var closeT0 float64
	_ = p.db.QueryRowContext(ctx, "SELECT close FROM prices WHERE ticker = ? AND date = ?;", h.ticker, h.asOfDate).Scan(&closeT0)

	// Fetch T+1 open (primary execution)
	var openT1 float64
	if entryDate != "" {
		_ = p.db.QueryRowContext(ctx, "SELECT COALESCE(open, close) FROM prices WHERE ticker = ? AND date = ?;", h.ticker, entryDate).Scan(&openT1)
	}
	if openT1 <= 0 {
		openT1 = closeT0
	}

	critVer := "live"
	if h.asOfDate < "2026-09-25" {
		critVer = "backfilled"
	}

	return RadarEpisode{
		EpisodeID:       episodeID,
		Ticker:          h.ticker,
		Sector:          h.sector,
		FirstSeenDate:   h.asOfDate,
		IndexName:       indexName,
		Method:          method,
		EntryDate:       entryDate,
		EntryPxOpen:     openT1,
		EntryPxCloseT0:  closeT0,
		CriteriaVersion: critVer,
		BlockerAtEntry:  h.blocker,
	}
}

// EvaluateRadarHorizonReturns evaluates returns for all episodes that have reached horizons 5, 10, or 21 sessions.
func (p *DB) EvaluateRadarHorizonReturns(ctx context.Context, indexName, method string) (int, error) {
	indexName = NormalizeIndexName(indexName)
	horizons := []int{5, 10, 21}

	// Fetch all episodes not yet evaluated for each horizon
	episodesQuery := `
SELECT 
    e.episode_id,
    e.ticker,
    e.first_seen_date::VARCHAR,
    e.entry_date::VARCHAR,
    e.entry_px_open,
    t0.seq AS t0_seq
FROM radar_episodes e
JOIN trading_days t0 ON e.first_seen_date = t0.d
WHERE e.index_name = ? AND e.method = ?;
`
	rows, err := p.db.QueryContext(ctx, episodesQuery, indexName, method)
	if err != nil {
		return 0, fmt.Errorf("query episodes for eval: %w", err)
	}
	defer rows.Close()

	type epRecord struct {
		episodeID   string
		ticker      string
		firstSeen   string
		entryDate   string
		entryPxOpen float64
		t0Seq       int
	}
	var episodes []epRecord
	for rows.Next() {
		var ep epRecord
		if err := rows.Scan(&ep.episodeID, &ep.ticker, &ep.firstSeen, &ep.entryDate, &ep.entryPxOpen, &ep.t0Seq); err == nil {
			episodes = append(episodes, ep)
		}
	}

	evaluatedCount := 0
	insertQuery := `
INSERT INTO radar_horizon_returns (
    episode_id, horizon, exit_date, exit_px, ret,
    bench_ew_mean, bench_ew_median, bench_n, excess,
    state_at_h, last_price_flag, computed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT (episode_id, horizon) DO NOTHING;
`

	for _, ep := range episodes {
		for _, h := range horizons {
			// Check if already evaluated (immutability)
			var exists bool
			_ = p.db.QueryRowContext(ctx, "SELECT true FROM radar_horizon_returns WHERE episode_id = ? AND horizon = ?;", ep.episodeID, h).Scan(&exists)
			if exists {
				continue
			}

			// Find exit date at T0_seq + h
			var exitDate string
			err := p.db.QueryRowContext(ctx, "SELECT d::VARCHAR FROM trading_days WHERE seq = ? + ?;", ep.t0Seq, h).Scan(&exitDate)
			if err != nil || exitDate == "" {
				// Horizon has not matured yet
				continue
			}

			// Fetch exit price for candidate
			var exitPx float64
			var lastPriceFlag bool
			_ = p.db.QueryRowContext(ctx, "SELECT close FROM prices WHERE ticker = ? AND date = ?;", ep.ticker, exitDate).Scan(&exitPx)
			if exitPx <= 0 {
				// Delisted / suspended: fall back to last available price on or before exitDate
				_ = p.db.QueryRowContext(ctx, "SELECT close FROM prices WHERE ticker = ? AND date <= ? ORDER BY date DESC LIMIT 1;", ep.ticker, exitDate).Scan(&exitPx)
				lastPriceFlag = true
			}
			if exitPx <= 0 || ep.entryPxOpen <= 0 {
				continue
			}

			candRet := (exitPx / ep.entryPxOpen) - 1.0

			// Compute Equal-Weight Universe Benchmark Return over the exact same window
			benchMean, benchMed, benchN := p.calculateBenchmarkReturn(ctx, ep.firstSeen, ep.entryDate, exitDate, indexName)

			excess := candRet - benchMean

			// Determine state at horizon: DELISTED -> CLEARED -> ACTIVE -> EXITED
			stateAtH := p.deriveStateAtHorizon(ctx, ep.ticker, exitDate, indexName, method, lastPriceFlag)

			_, err = p.db.ExecContext(ctx, insertQuery,
				ep.episodeID, h, exitDate, exitPx, candRet,
				benchMean, benchMed, benchN, excess,
				stateAtH, lastPriceFlag,
			)
			if err == nil {
				evaluatedCount++
			}
		}
	}

	return evaluatedCount, nil
}

func (p *DB) calculateBenchmarkReturn(ctx context.Context, asOfDate, entryDate, exitDate, indexName string) (mean, median float64, n int) {
	// Query all PIT constituents for asOfDate
	q := `
WITH univ AS (
    SELECT DISTINCT ticker FROM pit_candidate_scores WHERE as_of_date = ? AND index_name = ?
),
px_entry AS (
    SELECT u.ticker, COALESCE(p.open, p.close) as p_entry
    FROM univ u
    JOIN prices p ON u.ticker = p.ticker AND p.date = ?
    WHERE p.close > 0
),
px_exit AS (
    SELECT u.ticker, p.close as p_exit
    FROM univ u
    JOIN prices p ON u.ticker = p.ticker AND p.date = ?
    WHERE p.close > 0
)
SELECT 
    AVG(e.p_exit / en.p_entry - 1.0) AS ew_mean,
    MEDIAN(e.p_exit / en.p_entry - 1.0) AS ew_med,
    COUNT(*) AS count_n
FROM px_entry en
JOIN px_exit e ON en.ticker = e.ticker
WHERE en.p_entry > 0;
`
	_ = p.db.QueryRowContext(ctx, q, asOfDate, indexName, entryDate, exitDate).Scan(&mean, &median, &n)
	return mean, median, n
}

func (p *DB) deriveStateAtHorizon(ctx context.Context, ticker, exitDate, indexName, method string, isLastPrice bool) string {
	if isLastPrice {
		return "DELISTED"
	}
	var passedStage1 bool
	var delivDelta, compRS float64
	err := p.db.QueryRowContext(ctx, `
SELECT passed_stage1, delivery_delta, composite_rs 
FROM pit_candidate_scores 
WHERE ticker = ? AND as_of_date = ? AND index_name = ? AND method = ?;
`, ticker, exitDate, indexName, method).Scan(&passedStage1, &delivDelta, &compRS)

	if err != nil {
		return "EXITED"
	}
	if passedStage1 {
		return "CLEARED"
	}
	if delivDelta >= 0.08 && compRS >= 0.0 {
		return "ACTIVE"
	}
	return "EXITED"
}

// BootstrapClusterCI performs block bootstrap by first_seen_date cluster.
func BootstrapClusterCI(excess []float64, dates []string, iterations int, seed int64) (lower, upper float64) {
	if len(excess) == 0 || len(excess) != len(dates) {
		return 0, 0
	}

	// Group excess returns by date
	dateMap := make(map[string][]float64)
	for i, d := range dates {
		dateMap[d] = append(dateMap[d], excess[i])
	}

	uniqueDates := make([]string, 0, len(dateMap))
	for d := range dateMap {
		uniqueDates = append(uniqueDates, d)
	}
	sort.Strings(uniqueDates)

	if len(uniqueDates) == 0 {
		return 0, 0
	}

	rng := rand.New(rand.NewSource(seed))
	sampleMeans := make([]float64, iterations)

	for iter := 0; iter < iterations; iter++ {
		var sum float64
		var count int
		for i := 0; i < len(uniqueDates); i++ {
			sampledDate := uniqueDates[rng.Intn(len(uniqueDates))]
			for _, val := range dateMap[sampledDate] {
				sum += val
				count++
			}
		}
		if count > 0 {
			sampleMeans[iter] = sum / float64(count)
		}
	}

	sort.Float64s(sampleMeans)
	idxLower := int(math.Floor(0.025 * float64(iterations)))
	idxUpper := int(math.Floor(0.975 * float64(iterations)))
	if idxUpper >= iterations {
		idxUpper = iterations - 1
	}

	return sampleMeans[idxLower], sampleMeans[idxUpper]
}

// PrintRadarPerformanceAudit renders the fixed-horizon radar audit with bootstrap CIs and small-n guards.
func (p *DB) PrintRadarPerformanceAudit(ctx context.Context, indexName, method string) error {
	indexName = NormalizeIndexName(indexName)
	_, _ = p.SyncRadarEpisodes(ctx, indexName, method)
	_, _ = p.EvaluateRadarHorizonReturns(ctx, indexName, method)

	fmt.Printf("\n--- 8B. FIXED-HORIZON RADAR ALPHA AUDIT (Frozen Cohorts vs Equal-Weight Benchmark) ---\n")
	fmt.Println("Tracking all radar entrants at fixed horizons (T+5, T+10, T+21) from next-day open:")

	horizons := []int{5, 10, 21}
	fmt.Printf("  %-7s | %-12s | %-7s | %-11s | %-8s | %-7s | %-24s | %s\n",
		"Horizon", "Matured (n)", "Dates", "Mean Excess", "Median", "Hit %", "95% CI (by-date bootstrap)", "Status")
	fmt.Println("  -----------------------------------------------------------------------------------------------------------------")

	for _, h := range horizons {
		q := `
SELECT 
    r.excess,
    e.first_seen_date::VARCHAR
FROM radar_horizon_returns r
JOIN radar_episodes e ON r.episode_id = e.episode_id
WHERE e.index_name = ? AND e.method = ? AND r.horizon = ?;
`
		rows, err := p.db.QueryContext(ctx, q, indexName, method, h)
		if err != nil {
			continue
		}

		var excesses []float64
		var dates []string
		for rows.Next() {
			var ex float64
			var d string
			if err := rows.Scan(&ex, &d); err == nil {
				excesses = append(excesses, ex)
				dates = append(dates, d)
			}
		}
		rows.Close()

		dateSet := make(map[string]bool)
		for _, d := range dates {
			dateSet[d] = true
		}
		nDates := len(dateSet)
		nEpisodes := len(excesses)

		hdr := fmt.Sprintf("T+%d", h)
		if nEpisodes == 0 {
			// Find earliest date that will mature
			var earliestDate string
			_ = p.db.QueryRowContext(ctx, `
SELECT d::VARCHAR FROM trading_days 
WHERE seq = (SELECT MIN(t.seq) FROM radar_episodes e JOIN trading_days t ON e.first_seen_date = t.d) + ?;
`, h).Scan(&earliestDate)
			if earliestDate == "" {
				earliestDate = "Pending session accumulation"
			}
			fmt.Printf("  %-7s | %12d | %7d | %11s | %8s | %7s | %-24s | [PENDING: matures %s]\n",
				hdr, 0, 0, "-", "-", "-", "-", earliestDate)
			continue
		}

		meanExcess := 0.0
		wins := 0
		for _, ex := range excesses {
			meanExcess += ex
			if ex > 0 {
				wins++
			}
		}
		meanExcess /= float64(nEpisodes)
		hitRate := (float64(wins) / float64(nEpisodes)) * 100.0

		// Median
		sortedExcess := make([]float64, len(excesses))
		copy(sortedExcess, excesses)
		sort.Float64s(sortedExcess)
		medianExcess := sortedExcess[len(sortedExcess)/2]

		lowerCI, upperCI := BootstrapClusterCI(excesses, dates, 1000, 42)
		ciStr := fmt.Sprintf("[%+.1f%%, %+.1f%%]", lowerCI*100.0, upperCI*100.0)

		statusStr := "🟢 MATURED"
		if nEpisodes < 30 || nDates < 10 {
			statusStr = fmt.Sprintf("⚠️ n=%d [INSUFFICIENT SAMPLE]", nEpisodes)
		}

		fmt.Printf("  %-7s | %12d | %7d | %+10.2f%% | %+7.2f%% | %6.1f%% | %-24s | %s\n",
			hdr, nEpisodes, nDates, meanExcess*100.0, medianExcess*100.0, hitRate, ciStr, statusStr)
	}

	// Stratification by entry blocker for matured horizons
	p.printRadarStratification(ctx, indexName, method)
	return nil
}

func (p *DB) printRadarStratification(ctx context.Context, indexName, method string) {
	q := `
SELECT 
    r.horizon,
    CASE
        WHEN e.blocker_at_entry LIKE '%52-Week High%' THEN '52W-HIGH'
        WHEN e.blocker_at_entry LIKE '%Base duration%' THEN 'BASE-SHORT'
        WHEN e.blocker_at_entry LIKE '%DSO%' THEN 'DSO-SPIKE'
        WHEN e.blocker_at_entry LIKE '%200-Day SMA%' THEN 'SMA200-SUB'
        WHEN e.blocker_at_entry LIKE '%promoter%' THEN 'PROMOTER-LOW'
        WHEN e.blocker_at_entry LIKE '%Cash Flow%' OR e.blocker_at_entry LIKE '%CFO%' THEN 'CF-QUALITY'
        WHEN e.blocker_at_entry LIKE '%ROCE%' OR e.blocker_at_entry LIKE '%Capital Efficiency%' THEN 'ROCE-WEAK'
        WHEN e.blocker_at_entry LIKE '%Debt/Equity%' THEN 'DE-HIGH'
        ELSE 'OTHER'
    END AS entry_gate,
    COUNT(*) AS n,
    ROUND(AVG(r.excess) * 100.0, 2) AS mean_excess,
    ROUND(MEDIAN(r.excess) * 100.0, 2) AS median_excess,
    ROUND(COUNT(CASE WHEN r.excess > 0 THEN 1 END) * 100.0 / COUNT(*), 1) AS hit_rate
FROM radar_horizon_returns r
JOIN radar_episodes e ON r.episode_id = e.episode_id
WHERE e.index_name = ? AND e.method = ?
GROUP BY r.horizon, entry_gate
ORDER BY r.horizon, n DESC;
`
	rows, err := p.db.QueryContext(ctx, q, indexName, method)
	if err != nil {
		return
	}
	defer rows.Close()

	fmt.Println("\n  ▶ Stratification by Entry Blocker (Evaluating Opportunity Cost of Waiting by Gate):")
	fmt.Printf("  %-7s | %-14s | %-7s | %-12s | %-10s | %s\n",
		"Horizon", "Entry Blocker", "Count", "Mean Excess", "Median", "Hit Rate")
	fmt.Println("  -------------------------------------------------------------------------------")
	found := false
	for rows.Next() {
		found = true
		var h int
		var gate string
		var count int
		var meanEx, medEx, hitRate float64
		if err := rows.Scan(&h, &gate, &count, &meanEx, &medEx, &hitRate); err == nil {
			fmt.Printf("  T+%-5d | %-14s | %7d | %+11.2f%% | %+9.2f%% | %6.1f%%\n",
				h, gate, count, meanEx, medEx, hitRate)
		}
	}
	if !found {
		fmt.Println("  No matured horizon states to stratify yet.")
	}
}
