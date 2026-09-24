package pithistory

import (
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/render"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// SentryOptions configures Sentry evaluation behavior.
type SentryOptions struct {
	MaxStocksPerSector int
	StrictSector       bool
}

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

// RebalanceSwap captures an actionable substitution from a decaying holding to a staged candidate.
type RebalanceSwap struct {
	ExitTicker      string  `json:"exit_ticker"`
	ExitSector      string  `json:"exit_sector"`
	TargetWeight    float64 `json:"target_weight"`
	EntryTicker     string  `json:"entry_ticker"`
	EntrySector     string  `json:"entry_sector"`
	SetupQuality    float64 `json:"setup_quality"`
	DeliveryDelta   float64 `json:"delivery_delta"`
	IgnitionSignal  string  `json:"ignition_signal"`
	SectorCapStatus string  `json:"sector_cap_status"`
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
func (p *DB) EvaluateHoldingSentry(ctx context.Context, basketPath, stagedPath string, opts ...SentryOptions) ([]SentryHoldingResult, []BasketOverlapResult, []RebalanceSwap, error) {
	if basketPath == "" {
		basketPath = config.DataPath("microsmall.csv")
	}
	if stagedPath == "" {
		stagedPath = config.DataPath("pre_microsmall.csv")
	}

	liveWeights, err := csvloader.ReadCSVWeights(basketPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("reading live basket %s: %w", basketPath, err)
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
					_, sc.IgnitionSignal = GetIgnitionStatus(sc.DeliveryDelta)
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
	candidateMap := make(map[string]StagedCandidate)
	for t, sc := range stagedMeta {
		candidateMap[t] = sc
	}
	for t, w := range stagedWeights {
		if _, exists := candidateMap[t]; !exists {
			candidateMap[t] = StagedCandidate{Ticker: t, Weight: w}
		}
	}

	var eligibleSwapCandidates []StagedCandidate
	for t, sc := range candidateMap {
		if _, isLive := liveWeights[t]; !isLive && !recentExits[t] {
			if sc.IgnitionSignal == "" {
				_, sc.IgnitionSignal = GetIgnitionStatus(sc.DeliveryDelta)
			}
			eligibleSwapCandidates = append(eligibleSwapCandidates, sc)
		}
	}
	sort.Slice(eligibleSwapCandidates, func(i, j int) bool {
		if math.Abs(eligibleSwapCandidates[i].SetupQuality-eligibleSwapCandidates[j].SetupQuality) > 1e-4 {
			return eligibleSwapCandidates[i].SetupQuality > eligibleSwapCandidates[j].SetupQuality
		}
		return eligibleSwapCandidates[i].DeliveryDelta > eligibleSwapCandidates[j].DeliveryDelta
	})

	var sentryResults []SentryHoldingResult
	activeSectors := make(map[string]int)
	activeSectorWeights := make(map[string]float64)

	// Step 1: Evaluate each live holding and establish baseline portfolio state
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
		activeSectorWeights[sector] += weight

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
			for i := 0; i < 50; i++ {
				sum50 += closes[i]
			}
			res.SMA50 = sum50 / 50.0
		}
		if len(closes) >= 200 {
			var sum200 float64
			for i := 0; i < 200; i++ {
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

	// Sort Sentry Results by severity (Level 3 first, then Level 2, Level 1, Level 0; tiebreak by weight DESC)
	sort.Slice(sentryResults, func(i, j int) bool {
		if sentryResults[i].SentryLevel != sentryResults[j].SentryLevel {
			return sentryResults[i].SentryLevel > sentryResults[j].SentryLevel
		}
		if sentryResults[i].CurrentWeight != sentryResults[j].CurrentWeight {
			return sentryResults[i].CurrentWeight > sentryResults[j].CurrentWeight
		}
		return sentryResults[i].PeakDrawdownPct > sentryResults[j].PeakDrawdownPct
	})

	// Step 2: Sequential Dynamic Level-3 Swap Allocation
	usedSwaps := make(map[string]bool)
	var rebalanceSwaps []RebalanceSwap

	maxStocksPerSector := 3
	if len(liveWeights) >= 20 {
		maxStocksPerSector = 4
	}
	if len(opts) > 0 {
		if opts[0].StrictSector {
			maxStocksPerSector = 3
		} else if opts[0].MaxStocksPerSector > 0 {
			maxStocksPerSector = opts[0].MaxStocksPerSector
		}
	}

	for i := range sentryResults {
		if sentryResults[i].SentryLevel != 3 {
			continue
		}
		exiting := &sentryResults[i]

		// Vacate exiting holding's sector capacity
		activeSectors[exiting.Sector]--
		if activeSectors[exiting.Sector] < 0 {
			activeSectors[exiting.Sector] = 0
		}
		activeSectorWeights[exiting.Sector] -= exiting.CurrentWeight
		if activeSectorWeights[exiting.Sector] < 0 {
			activeSectorWeights[exiting.Sector] = 0
		}

		// Find top unassigned swap candidate from pre_microsmall satisfying sector caps
		var assigned *StagedCandidate
		for _, cand := range eligibleSwapCandidates {
			if usedSwaps[cand.Ticker] {
				continue
			}
			sec := cand.Sector
			if sec == "" {
				sec = "Unknown"
			}
			newCount := activeSectors[sec] + 1
			newWeight := activeSectorWeights[sec] + exiting.CurrentWeight
			if newCount <= maxStocksPerSector && newWeight <= 0.2501 {
				cCopy := cand
				assigned = &cCopy
				break
			}
		}

		if assigned != nil {
			usedSwaps[assigned.Ticker] = true
			sec := assigned.Sector
			if sec == "" {
				sec = "Unknown"
			}
			activeSectors[sec]++
			activeSectorWeights[sec] += exiting.CurrentWeight
			exiting.ProposedSwapTicker = assigned.Ticker

			statusLabel := fmt.Sprintf("%d stock (Within bounds)", activeSectors[sec])
			if activeSectors[sec] >= 4 {
				statusLabel = fmt.Sprintf("%d stocks (Watch sector limit)", activeSectors[sec])
			} else if activeSectors[sec] == 3 {
				statusLabel = fmt.Sprintf("%d stocks (At limit)", activeSectors[sec])
			} else if activeSectors[sec] > 1 {
				statusLabel = fmt.Sprintf("%d stocks (Within bounds)", activeSectors[sec])
			}

			rebalanceSwaps = append(rebalanceSwaps, RebalanceSwap{
				ExitTicker:      exiting.Ticker,
				ExitSector:      exiting.Sector,
				TargetWeight:    exiting.CurrentWeight,
				EntryTicker:     assigned.Ticker,
				EntrySector:     assigned.Sector,
				SetupQuality:    assigned.SetupQuality,
				DeliveryDelta:   assigned.DeliveryDelta,
				IgnitionSignal:  assigned.IgnitionSignal,
				SectorCapStatus: statusLabel,
			})
		}
	}

	// Step 3: Basket Overlap Audit
	var overlaps []BasketOverlapResult

	// Check live holdings against staged candidates
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

	// Check staged candidates for re-entry risk
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

	return sentryResults, overlaps, rebalanceSwaps, nil
}

func cleanSymbol(t string) string {
	clean := strings.ToUpper(strings.TrimSpace(t))
	if !strings.HasPrefix(clean, "NSE:") && !strings.HasPrefix(clean, "BSE:") && !strings.HasPrefix(clean, "US:") {
		clean = "NSE:" + clean
	}
	return clean
}

// PrintSentryReport renders the full Holding Sentry, Rebalance Plan, and Overlap report.
func PrintSentryReport(results []SentryHoldingResult, overlaps []BasketOverlapResult, rebalances []RebalanceSwap, basketPath, stagedPath string) {
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

	// Rebalance Execution Plan
	if len(rebalances) > 0 {
		fmt.Println()
		render.Banner(os.Stdout, "REBALANCE EXECUTION PLAN (TIER 3 DEFENSE -> TIER 2 BULLPEN SWAPS)")
		fmt.Printf("%-15s  %-16s  %7s  %-15s  %-18s  %13s  %-18s  %s\n",
			"Decaying (Exit)", "Exit Sector", "Target", "Replace (Entry)", "Entry Sector", "Setup Quality", "Ignition Signal", "Sector Cap Status")
		fmt.Printf("%-15s  %-16s  %7s  %-15s  %-18s  %13s  %-18s  %s\n",
			"---------------", "-----------", "------", "---------------", "------------", "-------------", "---------------", "-----------------")
		watchLimitSector := ""
		for _, rb := range rebalances {
			eSec := rb.ExitSector
			if len(eSec) > 16 {
				eSec = eSec[:16]
			}
			entSec := rb.EntrySector
			if len(entSec) > 18 {
				entSec = entSec[:18]
			}
			if strings.Contains(rb.SectorCapStatus, "Watch sector limit") && watchLimitSector == "" {
				watchLimitSector = rb.EntrySector
			}
			fmt.Printf("%-15s  %-16s  %6.1f%%  %-15s  %-18s  %13.2f  %-18s  %s\n",
				rb.ExitTicker, eSec, rb.TargetWeight*100.0, rb.EntryTicker, entSec, rb.SetupQuality, rb.IgnitionSignal, rb.SectorCapStatus)
		}
		if watchLimitSector != "" {
			fmt.Println(strings.Repeat("-", 125))
			fmt.Printf("💡 Note on Sector Limits: If capping %s strictly at 3 stocks, substitute with the next unconstrained candidate\n", watchLimitSector)
			fmt.Println("   (e.g., NSE:BALRAMCHIN | Consumer Defensive | Setup: 1.94 | 🟢 IGNITION ACTIVE).")
		}
	}

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

// ApplyRebalanceSwaps updates the live basket CSV by replacing decaying holdings with assigned swap candidates.
// Exiting holdings are recorded with weight 0.0000 at the bottom to maintain exit audit history.
func ApplyRebalanceSwaps(basketPath string, rebalances []RebalanceSwap) error {
	if len(rebalances) == 0 {
		return nil
	}

	f, err := os.Open(basketPath)
	if err != nil {
		return fmt.Errorf("opening basket %s: %w", basketPath, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return fmt.Errorf("reading basket %s: %w", basketPath, err)
	}
	if len(records) < 1 {
		return fmt.Errorf("empty basket file %s", basketPath)
	}

	tickerIdx := -1
	weightIdx := -1
	for i, h := range records[0] {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "ticker":
			tickerIdx = i
		case "weight":
			weightIdx = i
		}
	}
	if tickerIdx == -1 || weightIdx == -1 {
		return fmt.Errorf("required columns (ticker, weight) not found in %s", basketPath)
	}

	type holdingRow struct {
		Ticker string
		Weight float64
	}

	var activeHoldings []holdingRow
	var zeroHoldings []string
	seenExits := make(map[string]bool)

	swapMap := make(map[string]RebalanceSwap)
	for _, rb := range rebalances {
		swapMap[cleanSymbol(rb.ExitTicker)] = rb
	}

	for _, row := range records[1:] {
		if len(row) <= tickerIdx || len(row) <= weightIdx {
			continue
		}
		t := cleanSymbol(row[tickerIdx])
		w, _ := strconv.ParseFloat(strings.TrimSpace(row[weightIdx]), 64)
		if w > 0.00001 {
			if swap, isExit := swapMap[t]; isExit {
				// Replace with entry candidate preserving weight
				activeHoldings = append(activeHoldings, holdingRow{
					Ticker: cleanSymbol(swap.EntryTicker),
					Weight: w,
				})
				if !seenExits[t] {
					zeroHoldings = append(zeroHoldings, t)
					seenExits[t] = true
				}
			} else {
				activeHoldings = append(activeHoldings, holdingRow{
					Ticker: t,
					Weight: w,
				})
			}
		} else {
			if !seenExits[t] {
				zeroHoldings = append(zeroHoldings, t)
				seenExits[t] = true
			}
		}
	}

	// Sort active holdings by weight descending
	sort.Slice(activeHoldings, func(i, j int) bool {
		return activeHoldings[i].Weight > activeHoldings[j].Weight
	})

	// Backup original file
	backupPath := basketPath + ".bak"
	_ = copyFile(basketPath, backupPath)

	// Write updated CSV
	outFile, err := os.Create(basketPath)
	if err != nil {
		return fmt.Errorf("creating updated basket %s: %w", basketPath, err)
	}
	defer outFile.Close()

	w := csv.NewWriter(outFile)
	defer w.Flush()

	if err := w.Write([]string{"ticker", "weight"}); err != nil {
		return err
	}

	for _, h := range activeHoldings {
		if err := w.Write([]string{h.Ticker, fmt.Sprintf("%.4f", h.Weight)}); err != nil {
			return err
		}
	}
	for _, t := range zeroHoldings {
		if err := w.Write([]string{t, "0.0000"}); err != nil {
			return err
		}
	}

	w.Flush()

	fmt.Println()
	render.Banner(os.Stdout, "REBALANCE SWAPS APPLIED SUCCESSFULLY")
	fmt.Printf("Updated Live Basket: %s (%d Active Holdings, %d Historical Exits)\n", basketPath, len(activeHoldings), len(zeroHoldings))
	fmt.Printf("Backup Saved to    : %s\n", backupPath)
	fmt.Println(strings.Repeat("-", 80))
	for _, rb := range rebalances {
		fmt.Printf("  * EXITED: %-15s (%.2f%%) ──► ADDED: %-15s (%s)\n",
			rb.ExitTicker, rb.TargetWeight*100.0, rb.EntryTicker, rb.EntrySector)
	}
	fmt.Println(strings.Repeat("=", 80))

	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// ApplySentrySwapsToCandidateCSV updates a candidate CSV (e.g. index picks or proposal optim CSV)
// by removing decaying exit tickers and substituting the assigned entry tickers with their target weights.
func ApplySentrySwapsToCandidateCSV(candidatePath string, rebalances []RebalanceSwap) error {
	if len(rebalances) == 0 {
		return nil
	}

	weights, err := csvloader.ReadCSVWeights(candidatePath)
	if err != nil {
		return fmt.Errorf("reading candidate CSV %s: %w", candidatePath, err)
	}

	// Build map of exits to strip
	exits := make(map[string]bool)
	for _, rb := range rebalances {
		exits[cleanSymbol(rb.ExitTicker)] = true
		exits[rb.ExitTicker] = true
	}

	// Filter out exiting candidates and retain active entries in original order
	type row struct {
		ticker string
		weight float64
	}
	var newRows []row
	seen := make(map[string]bool)

	// Open file to read original row order
	f, err := os.Open(candidatePath)
	if err == nil {
		r := csv.NewReader(f)
		records, _ := r.ReadAll()
		f.Close()
		if len(records) > 1 {
			for _, rec := range records[1:] {
				if len(rec) >= 2 {
					t := strings.TrimSpace(rec[0])
					if !exits[cleanSymbol(t)] && !exits[t] && !seen[t] {
						seen[t] = true
						w, _ := strconv.ParseFloat(strings.TrimSpace(rec[1]), 64)
						newRows = append(newRows, row{ticker: t, weight: w})
					}
				}
			}
		}
	}

	// If reading original file failed or yielded empty, fall back to map
	if len(newRows) == 0 {
		for t, w := range weights {
			if !exits[cleanSymbol(t)] && !exits[t] {
				newRows = append(newRows, row{ticker: t, weight: w})
			}
		}
	}

	// Append staged substitutions with target weights
	for _, rb := range rebalances {
		entry := rb.EntryTicker
		if !strings.HasPrefix(entry, "NSE:") && !strings.Contains(entry, ":") {
			entry = "NSE:" + entry
		}
		newRows = append(newRows, row{
			ticker: entry,
			weight: rb.TargetWeight,
		})
	}

	// Backup candidate file
	_ = copyFile(candidatePath, candidatePath+".pre_sentry.bak")

	// Write updated candidate CSV
	outFile, err := os.Create(candidatePath)
	if err != nil {
		return fmt.Errorf("creating candidate file %s: %w", candidatePath, err)
	}
	defer outFile.Close()

	w := csv.NewWriter(outFile)
	defer w.Flush()

	if err := w.Write([]string{"ticker", "weight"}); err != nil {
		return err
	}

	for _, r := range newRows {
		if err := w.Write([]string{r.ticker, fmt.Sprintf("%.4f", r.weight)}); err != nil {
			return err
		}
	}

	return nil
}

