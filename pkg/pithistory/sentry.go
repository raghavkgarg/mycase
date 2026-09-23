package pithistory

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/render"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// SentryHoldingResult encapsulates the deterioration risk status for an active portfolio holding.
type SentryHoldingResult struct {
	Ticker             string  `json:"ticker"`
	Sector             string  `json:"sector"`
	CurrentWeight      float64 `json:"current_weight"`
	CurrentPrice       float64 `json:"current_price"`
	SMA50              float64 `json:"sma_50"`
	SMA200             float64 `json:"sma_200"`
	High52W            float64 `json:"high_52w"`
	PeakDrawdownPct    float64 `json:"peak_drawdown_pct"`
	VCPRatio           float64 `json:"vcp_ratio"`
	RVOLZScore         float64 `json:"rvol_z_score"`
	DeliveryDelta3D    float64 `json:"delivery_delta_3d"`
	PriceDeclining     bool    `json:"price_declining"`
	SentryLevel        int     `json:"sentry_level"` // 0=Healthy, 1=Coil Decay, 2=Distribution, 3=Trend Rupture
	SentryLabel        string  `json:"sentry_label"`
	Action             string  `json:"action"`
	ProposedSwapTicker string  `json:"proposed_swap_ticker,omitempty"`
}

// BasketOverlapResult tracks the relationship between active live holdings and staged candidates.
type BasketOverlapResult struct {
	Ticker         string  `json:"ticker"`
	Sector         string  `json:"sector"`
	LiveWeight     float64 `json:"live_weight"`
	StagedWeight   float64 `json:"staged_weight"`
	SetupQuality   float64 `json:"setup_quality"`
	SignalType     string  `json:"signal_type"` // REINFORCEMENT, FADING, RE-ENTRY RISK, CO-OCCURRING
	Recommendation string  `json:"recommendation"`
}

// EvaluateHoldingSentry evaluates active holdings in basketPath against Sentry Levels 1-3,
// and audits overlap with staged candidates from stagedPath.
func (p *DB) EvaluateHoldingSentry(ctx context.Context, basketPath, stagedPath string) ([]SentryHoldingResult, []BasketOverlapResult, error) {
	if basketPath == "" {
		basketPath = config.DataPath("microsmall.csv")
	}
	if stagedPath == "" {
		stagedPath = config.DataPath("pre_microsmall.csv")
	}

	liveWeights, err := csvloader.ReadCSVWeights(basketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("reading live basket %s: %w", basketPath, err)
	}

	stagedWeights := make(map[string]float64)
	if sw, sErr := csvloader.ReadCSVWeights(stagedPath); sErr == nil {
		for t, w := range sw {
			stagedWeights[cleanSymbol(t)] = w
		}
	}

	// Fetch staged candidates metadata from pre_production_staging if available
	stagedMeta := make(map[string]StagedCandidate)
	sRows, qErr := p.db.QueryContext(ctx, `
SELECT ticker, sector, weight, setup_quality, delivery_delta, vcp_ratio, composite_rs
FROM pre_production_staging
ORDER BY as_of_date DESC;
`)
	if qErr == nil {
		for sRows.Next() {
			var sc StagedCandidate
			if sRows.Scan(&sc.Ticker, &sc.Sector, &sc.Weight, &sc.SetupQuality, &sc.DeliveryDelta, &sc.VCPRatio, &sc.CompositeRS) == nil {
				cleanT := cleanSymbol(sc.Ticker)
				if _, exists := stagedMeta[cleanT]; !exists {
					stagedMeta[cleanT] = sc
				}
			}
		}
		sRows.Close()
	}

	// Identify recently exited candidates (< 30 calendar days) from live portfolio runs
	recentExits := make(map[string]bool)
	exitQuery := `
SELECT DISTINCT ticker 
FROM pit_candidate_scores 
WHERE index_name = 'niftytotalmarket' 
  AND method = 'multibagger' 
  AND selected = true 
  AND as_of_date >= CURRENT_DATE - INTERVAL 30 DAYS;
`
	if eRows, eErr := p.db.QueryContext(ctx, exitQuery); eErr == nil {
		for eRows.Next() {
			var t string
			if eRows.Scan(&t) == nil {
				cleanT := cleanSymbol(t)
				if _, isLive := liveWeights[cleanT]; !isLive {
					recentExits[cleanT] = true
				}
			}
		}
		eRows.Close()
	}

	// Ranked candidate pool for Level 3 swaps
	var eligibleSwapCandidates []StagedCandidate
	for t, sc := range stagedMeta {
		if _, isLive := liveWeights[t]; !isLive && !recentExits[t] {
			eligibleSwapCandidates = append(eligibleSwapCandidates, sc)
		}
	}
	sort.Slice(eligibleSwapCandidates, func(i, j int) bool {
		return eligibleSwapCandidates[i].SetupQuality > eligibleSwapCandidates[j].SetupQuality
	})

	var sentryResults []SentryHoldingResult
	activeSectors := make(map[string]int)

	// Evaluate each live holding
	for ticker, weight := range liveWeights {
		if weight <= 0 {
			continue
		}
		cleanT := cleanSymbol(ticker)

		// Fetch price history from DuckDB prices table
		pQuery := `
SELECT close, open, volume 
FROM prices 
WHERE ticker = ? 
ORDER BY date DESC 
LIMIT 260;
`
		var closes, opens, volumes []float64
		var sector string
		if pRows, err := p.db.QueryContext(ctx, pQuery, cleanT); err == nil {
			for pRows.Next() {
				var c, o, v float64
				if pRows.Scan(&c, &o, &v) == nil {
					closes = append(closes, c)
					opens = append(opens, o)
					volumes = append(volumes, v)
				}
			}
			pRows.Close()
		}

		// Reverse to chronological order (oldest to newest)
		chronCloses := make([]float64, len(closes))
		chronOpens := make([]float64, len(opens))
		chronVolumes := make([]float64, len(volumes))
		for i := range closes {
			chronCloses[i] = closes[len(closes)-1-i]
			chronOpens[i] = opens[len(opens)-1-i]
			chronVolumes[i] = volumes[len(volumes)-1-i]
		}

		// Retrieve sector from database
		_ = p.db.QueryRowContext(ctx, "SELECT COALESCE(NULLIF(sector, ''), 'Unknown') FROM pit_candidate_scores WHERE ticker = ? ORDER BY as_of_date DESC LIMIT 1;", cleanT).Scan(&sector)
		if sector == "" {
			sector = "Unknown"
		}
		activeSectors[sector]++

		res := SentryHoldingResult{
			Ticker:        cleanT,
			Sector:        sector,
			CurrentWeight: weight,
		}

		if len(closes) > 0 {
			res.CurrentPrice = closes[0]
			if len(closes) > 1 {
				res.PriceDeclining = closes[0] < closes[1]
			}
		}

		// SMA50 & SMA200
		if len(closes) >= 50 {
			var sum50 float64
			for i := range 50 {
				sum50 += closes[i]
			}
			res.SMA50 = sum50 / 50.0
		}
		if len(closes) >= 200 {
			var sum200 float64
			for i := range 200 {
				sum200 += closes[i]
			}
			res.SMA200 = sum200 / 200.0
		}

		// 52W High & Peak Drawdown
		var maxHigh float64
		for i := range closes {
			if closes[i] > maxHigh {
				maxHigh = closes[i]
			}
			if opens[i] > maxHigh {
				maxHigh = opens[i]
			}
		}
		res.High52W = maxHigh
		if maxHigh > 0 && res.CurrentPrice > 0 {
			res.PeakDrawdownPct = ((maxHigh - res.CurrentPrice) / maxHigh) * 100.0
		}

		// Technical Metrics: VCP and RVOL Z-Score
		if len(chronCloses) >= 60 {
			res.VCPRatio, _ = yfinance.CalculateVCPTightness(chronCloses, chronOpens)
			res.RVOLZScore = yfinance.CalculateWinsorizedRVOLZScore(chronVolumes, 5, 50, 4.0)
		}

		// 3-Day Cumulative Delivery Delta from PIT records
		dQuery := `
SELECT COALESCE(SUM(delivery_delta), 0.0) 
FROM (
    SELECT delivery_delta 
    FROM pit_candidate_scores 
    WHERE ticker = ? 
    ORDER BY as_of_date DESC 
    LIMIT 3
);
`
		_ = p.db.QueryRowContext(ctx, dQuery, cleanT).Scan(&res.DeliveryDelta3D)

		// Evaluate Sentry Triggers
		// Level 3: TREND RUPTURE
		isTrendRupture := false
		if res.SMA200 > 0 && res.CurrentPrice < 0.95*res.SMA200 {
			isTrendRupture = true
		} else if res.High52W > 0 && res.CurrentPrice < 0.80*res.High52W {
			isTrendRupture = true
		}

		if isTrendRupture {
			res.SentryLevel = 3
			res.SentryLabel = "🔴 [L3: TREND RUPTURE]"
			res.Action = "Immediate Swap Candidate: Slipping trend/52W support"

			// Find top swap candidate from pre_microsmall in allowable sector
			for _, swap := range eligibleSwapCandidates {
				if activeSectors[swap.Sector] < 3 {
					res.ProposedSwapTicker = swap.Ticker
					break
				}
			}
		} else if res.SMA50 > 0 && res.CurrentPrice <= res.SMA50 && res.DeliveryDelta3D <= -0.10 {
			// Level 2: INSTITUTIONAL DISTRIBUTION
			res.SentryLevel = 2
			res.SentryLabel = "🟠 [L2: DISTRIBUTION]"
			res.Action = "Tag for expedited review; 3D Deliv Δ <= -10% under 50-SMA"
		} else if res.VCPRatio > 1.60 && res.RVOLZScore > 2.0 && res.PriceDeclining {
			// Level 1: COIL DECAY
			res.SentryLevel = 1
			res.SentryLabel = "🟡 [L1: COIL DECAY]"
			res.Action = "Freeze fresh capital allocation; VCP expanded > 1.60 on down-volume"
		} else {
			// Level 0: HEALTHY
			res.SentryLevel = 0
			res.SentryLabel = "🟢 HEALTHY"
			res.Action = "Maintain position; technical structure intact"
		}

		sentryResults = append(sentryResults, res)
	}

	// Sort Sentry Results by severity (Level 3 first, then Level 2, Level 1, Level 0)
	sort.Slice(sentryResults, func(i, j int) bool {
		if sentryResults[i].SentryLevel != sentryResults[j].SentryLevel {
			return sentryResults[i].SentryLevel > sentryResults[j].SentryLevel
		}
		return sentryResults[i].CurrentWeight > sentryResults[j].CurrentWeight
	})

	// Basket Overlap Audit
	var overlaps []BasketOverlapResult

	// 1. Check live holdings against staged candidates
	for ticker, lWeight := range liveWeights {
		cleanT := cleanSymbol(ticker)
		sWeight := stagedWeights[cleanT]
		meta, inStaged := stagedMeta[cleanT]

		ov := BasketOverlapResult{
			Ticker:       cleanT,
			Sector:       meta.Sector,
			LiveWeight:   lWeight,
			StagedWeight: sWeight,
			SetupQuality: meta.SetupQuality,
		}

		if inStaged {
			if meta.SetupQuality >= 2.50 {
				ov.SignalType = "🟢 REINFORCEMENT"
				ov.Recommendation = "Position strength confirmed (Setup >= 2.5). Prime candidate to overweight."
			} else {
				ov.SignalType = "🔵 CO-OCCURRING"
				ov.Recommendation = "Holding re-qualifies in staging bullpen."
			}
		} else {
			ov.SignalType = "🟡 FADING"
			ov.Recommendation = "Holding does not qualify in Tier-2 bullpen. Monitor technical stops."
		}
		overlaps = append(overlaps, ov)
	}

	// 2. Check staged candidates for re-entry risk
	for ticker, sWeight := range stagedWeights {
		cleanT := cleanSymbol(ticker)
		if _, isLive := liveWeights[cleanT]; !isLive {
			if recentExits[cleanT] {
				meta := stagedMeta[cleanT]
				overlaps = append(overlaps, BasketOverlapResult{
					Ticker:         cleanT,
					Sector:         meta.Sector,
					LiveWeight:     0.0,
					StagedWeight:   sWeight,
					SetupQuality:   meta.SetupQuality,
					SignalType:     "🔴 RE-ENTRY RISK",
					Recommendation: "Recently exited live portfolio (< 30d). Suppress re-entry to curb turnover.",
				})
			}
		}
	}

	return sentryResults, overlaps, nil
}

func cleanSymbol(t string) string {
	clean := strings.ToUpper(strings.TrimSpace(t))
	if !strings.HasPrefix(clean, "NSE:") && !strings.HasPrefix(clean, "BSE:") && !strings.HasPrefix(clean, "US:") {
		clean = "NSE:" + clean
	}
	return clean
}

// PrintSentryReport renders the full Holding Sentry and Overlap report.
func PrintSentryReport(results []SentryHoldingResult, overlaps []BasketOverlapResult, basketPath, stagedPath string) {
	fmt.Println()
	render.Banner(os.Stdout, "ACTIVE PORTFOLIO EARLY DETERIORATION HOLDING SENTRY (TIER 3 LIVE DEFENSE)")
	fmt.Printf("Live Portfolio Basket : %s (%d Holdings)\n", basketPath, len(results))
	fmt.Printf("Pre-Production Staging: %s\n", stagedPath)
	fmt.Printf("Monitoring Levels     : L1 (Coil Decay), L2 (Distribution), L3 (Trend Rupture -> Swap)\n")
	fmt.Println(strings.Repeat("-", 125))
	fmt.Printf("%-15s  %-16s  %6s  %10s  %10s  %9s  %8s  %-22s  %s\n",
		"Ticker", "Sector", "Weight", "Close (₹)", "SMA 200", "52W High", "Peak DD", "Sentry State", "Operational Action")
	fmt.Printf("%-15s  %-16s  %6s  %10s  %10s  %9s  %8s  %-22s  %s\n",
		"------", "------", "------", "---------", "-------", "--------", "-------", "------------", "------------------")

	l3Count := 0
	l2Count := 0
	l1Count := 0
	for _, r := range results {
		sec := r.Sector
		if len(sec) > 16 {
			sec = sec[:16]
		}
		ddStr := fmt.Sprintf("-%.1f%%", r.PeakDrawdownPct)
		smaStr := fmt.Sprintf("₹%.1f", r.SMA200)
		if r.SMA200 <= 0 {
			smaStr = "-"
		}

		actionStr := r.Action
		if r.ProposedSwapTicker != "" {
			actionStr = fmt.Sprintf("SWAP -> %s", r.ProposedSwapTicker)
		}

		switch r.SentryLevel {
		case 3:
			l3Count++
		case 2:
			l2Count++
		case 1:
			l1Count++
		}

		fmt.Printf("%-15s  %-16s  %5.1f%%  %10.2f  %10s  %9.1f  %8s  %-22s  %s\n",
			r.Ticker, sec, r.CurrentWeight*100.0, r.CurrentPrice, smaStr, r.High52W, ddStr, r.SentryLabel, actionStr)
	}

	fmt.Println(strings.Repeat("-", 125))
	fmt.Printf("Holding Sentry Summary: %d L3 Trend Ruptures | %d L2 Distribution Warnings | %d L1 Coil Decays | %d Healthy\n",
		l3Count, l2Count, l1Count, len(results)-l3Count-l2Count-l1Count)

	// Overlap Audit Section
	fmt.Println()
	render.Banner(os.Stdout, "LIVE & STAGED BASKET OVERLAP AUDIT (§7D)")
	fmt.Printf("%-15s  %-16s  %7s  %7s  %13s  %-18s  %s\n",
		"Ticker", "Sector", "Live Wt", "Staged", "Setup Quality", "Overlap Signal", "Audit Recommendation")
	fmt.Printf("%-15s  %-16s  %7s  %7s  %13s  %-18s  %s\n",
		"------", "------", "-------", "------", "-------------", "--------------", "--------------------")

	for _, ov := range overlaps {
		sec := ov.Sector
		if len(sec) > 16 {
			sec = sec[:16]
		}
		liveStr := fmt.Sprintf("%.1f%%", ov.LiveWeight*100.0)
		if ov.LiveWeight <= 0 {
			liveStr = "-"
		}
		stagedStr := fmt.Sprintf("%.1f%%", ov.StagedWeight*100.0)
		if ov.StagedWeight <= 0 {
			stagedStr = "-"
		}

		fmt.Printf("%-15s  %-16s  %7s  %7s  %13.2f  %-18s  %s\n",
			ov.Ticker, sec, liveStr, stagedStr, ov.SetupQuality, ov.SignalType, ov.Recommendation)
	}
	fmt.Println(strings.Repeat("=", 125))
}
