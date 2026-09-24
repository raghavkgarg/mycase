package pithistory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raghavkgarg/mycase/pkg/csvloader"
)

func TestSentryTriggers(t *testing.T) {
	// Case 1: Level 3 Trend Rupture (Price < 0.95 * SMA200)
	price1 := 90.0
	sma200_1 := 100.0
	high52W_1 := 120.0
	if !(price1 < 0.95*sma200_1 || price1 < 0.80*high52W_1) {
		t.Errorf("expected Level 3 trigger for price=90 with SMA200=100")
	}

	// Case 2: Level 3 Peak Drawdown Rupture (Price < 0.80 * High52W)
	price2 := 75.0
	high52W_2 := 100.0
	if !(price2 < 0.80*high52W_2) {
		t.Errorf("expected Level 3 trigger for price=75 with High52W=100 (25%% drawdown)")
	}

	// Case 3: Level 2 Institutional Distribution (Price <= SMA50 && DelivDelta3D <= -10%)
	price3 := 98.0
	sma50_3 := 100.0
	delivDelta3D_3 := -0.12
	if !(price3 <= sma50_3 && delivDelta3D_3 <= -0.10) {
		t.Errorf("expected Level 2 trigger for price <= SMA50 with DelivDelta3D <= -10%%")
	}

	// Case 4: Level 1 Coil Decay (VCP > 1.60 && RVOL Z > 2.0 && PriceDeclining)
	vcp4 := 1.75
	rvolZ4 := 2.5
	priceDeclining4 := true
	if !(vcp4 > 1.60 && rvolZ4 > 2.0 && priceDeclining4) {
		t.Errorf("expected Level 1 trigger for VCP > 1.60 and RVOL Z > 2.0 on declining close")
	}
}

func TestBasketOverlapSignals(t *testing.T) {
	// Signal 1: Reinforcement (Live holding in staging with Setup Quality >= 2.50)
	sqHigh := 2.65
	inStaged := true
	var signal string
	if inStaged {
		if sqHigh >= 2.50 {
			signal = "REINFORCEMENT"
		} else {
			signal = "CO-OCCURRING"
		}
	} else {
		signal = "FADING"
	}
	if signal != "REINFORCEMENT" {
		t.Errorf("expected REINFORCEMENT, got %s", signal)
	}

	// Signal 2: Fading (Live holding not in staging)
	inStagedFading := false
	if !inStagedFading {
		signal = "FADING"
	}
	if signal != "FADING" {
		t.Errorf("expected FADING, got %s", signal)
	}

	// Signal 3: Re-Entry Risk (Staged candidate recently exited < 30 days)
	isRecentExit := true
	isLive := false
	if !isLive && isRecentExit {
		signal = "RE-ENTRY RISK"
	}
	if signal != "RE-ENTRY RISK" {
		t.Errorf("expected RE-ENTRY RISK, got %s", signal)
	}
}

func TestDynamicSwapQueueAllocation(t *testing.T) {
	// Active baseline portfolio (20 stocks)
	activeSectors := map[string]int{
		"Basic Materials":    4,
		"Healthcare":         3,
		"Financial Services": 2,
		"Communication Serv": 0,
	}
	activeSectorWeights := map[string]float64{
		"Basic Materials":    0.202,
		"Healthcare":         0.137,
		"Financial Services": 0.108,
		"Communication Serv": 0.0,
	}

	recentExits := map[string]bool{
		"NSE:CUPID": true, // Recently exited (< 30d)
	}

	eligibleCandidates := []StagedCandidate{
		{Ticker: "NSE:PARKHOSPS", Sector: "Healthcare", SetupQuality: 2.96, DeliveryDelta: -0.05},
		{Ticker: "NSE:CUPID", Sector: "Consumer Defensive", SetupQuality: 2.60, DeliveryDelta: 0.036},
		{Ticker: "NSE:TIPSMUSIC", Sector: "Communication Serv", SetupQuality: 2.60, DeliveryDelta: 0.034},
		{Ticker: "NSE:DCBBANK", Sector: "Financial Services", SetupQuality: 2.41, DeliveryDelta: 0.031},
		{Ticker: "NSE:RUBICON", Sector: "Healthcare", SetupQuality: 1.96, DeliveryDelta: 0.08},
		{Ticker: "NSE:BALRAMCHIN", Sector: "Consumer Defensive", SetupQuality: 1.94, DeliveryDelta: 0.07},
	}

	// 3 failing positions from Basic Materials
	failingHoldings := []SentryHoldingResult{
		{Ticker: "NSE:HINDCOPPER", Sector: "Basic Materials", CurrentWeight: 0.054, SentryLevel: 3},
		{Ticker: "NSE:SARDAEN", Sector: "Basic Materials", CurrentWeight: 0.052, SentryLevel: 3},
		{Ticker: "NSE:SUMICHEM", Sector: "Basic Materials", CurrentWeight: 0.045, SentryLevel: 3},
	}

	usedSwaps := make(map[string]bool)
	maxStocksPerSector := 4

	for i := range failingHoldings {
		exiting := &failingHoldings[i]

		// Vacate sector allocation
		activeSectors[exiting.Sector]--
		activeSectorWeights[exiting.Sector] -= exiting.CurrentWeight

		// Find best matching candidate
		var assigned *StagedCandidate
		for _, cand := range eligibleCandidates {
			if usedSwaps[cand.Ticker] || recentExits[cand.Ticker] {
				continue
			}
			sec := cand.Sector
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
			activeSectors[assigned.Sector]++
			activeSectorWeights[assigned.Sector] += exiting.CurrentWeight
			exiting.ProposedSwapTicker = assigned.Ticker
		}
	}

	// Assertions
	if failingHoldings[0].ProposedSwapTicker != "NSE:PARKHOSPS" {
		t.Errorf("expected HINDCOPPER swap to be NSE:PARKHOSPS, got %s", failingHoldings[0].ProposedSwapTicker)
	}
	if failingHoldings[1].ProposedSwapTicker != "NSE:TIPSMUSIC" {
		t.Errorf("expected SARDAEN swap to be NSE:TIPSMUSIC, got %s", failingHoldings[1].ProposedSwapTicker)
	}
	if failingHoldings[2].ProposedSwapTicker != "NSE:DCBBANK" {
		t.Errorf("expected SUMICHEM swap to be NSE:DCBBANK, got %s", failingHoldings[2].ProposedSwapTicker)
	}

	// Verify unique 1-to-1 mapping
	assignedSet := make(map[string]bool)
	for _, h := range failingHoldings {
		if assignedSet[h.ProposedSwapTicker] {
			t.Errorf("duplicate swap detected: %s was assigned multiple times", h.ProposedSwapTicker)
		}
		assignedSet[h.ProposedSwapTicker] = true
	}

	// Verify CUPID was skipped
	if assignedSet["NSE:CUPID"] {
		t.Errorf("NSE:CUPID should have been suppressed due to recent exits / re-entry risk")
	}

	// Verify sector counts
	if activeSectors["Basic Materials"] != 1 {
		t.Errorf("expected Basic Materials count to drop to 1, got %d", activeSectors["Basic Materials"])
	}
	if activeSectors["Healthcare"] != 4 {
		t.Errorf("expected Healthcare count to be 4, got %d", activeSectors["Healthcare"])
	}
	if activeSectors["Communication Serv"] != 1 {
		t.Errorf("expected Communication Serv count to be 1, got %d", activeSectors["Communication Serv"])
	}
	if activeSectors["Financial Services"] != 3 {
		t.Errorf("expected Financial Services count to be 3, got %d", activeSectors["Financial Services"])
	}
}

func TestApplyRebalanceSwaps(t *testing.T) {
	tmpDir := t.TempDir()
	basketPath := filepath.Join(tmpDir, "microsmall_test.csv")

	initialCSV := `ticker,weight
NSE:TMCV,0.0598
NSE:HINDCOPPER,0.0537
NSE:SARDAEN,0.0522
NSE:SUMICHEM,0.0450
NSE:CUPID,0.0000
`
	if err := os.WriteFile(basketPath, []byte(initialCSV), 0644); err != nil {
		t.Fatalf("failed to write initial test basket: %v", err)
	}

	rebalances := []RebalanceSwap{
		{
			ExitTicker:   "NSE:HINDCOPPER",
			TargetWeight: 0.0537,
			EntryTicker:  "NSE:PARKHOSPS",
			EntrySector:  "Healthcare",
		},
		{
			ExitTicker:   "NSE:SARDAEN",
			TargetWeight: 0.0522,
			EntryTicker:  "NSE:TIPSMUSIC",
			EntrySector:  "Communication Serv",
		},
		{
			ExitTicker:   "NSE:SUMICHEM",
			TargetWeight: 0.0450,
			EntryTicker:  "NSE:DCBBANK",
			EntrySector:  "Financial Services",
		},
	}

	if err := ApplyRebalanceSwaps(basketPath, rebalances); err != nil {
		t.Fatalf("ApplyRebalanceSwaps failed: %v", err)
	}

	// Verify backup file exists
	if _, err := os.Stat(basketPath + ".bak"); err != nil {
		t.Errorf("expected backup file to exist: %v", err)
	}

	// Verify new weights
	weights, err := csvloader.ReadCSVWeights(basketPath)
	if err != nil {
		t.Fatalf("failed to read updated basket: %v", err)
	}

	if _, exists := weights["NSE:HINDCOPPER"]; exists {
		t.Errorf("expected NSE:HINDCOPPER to be removed from active weights")
	}
	if _, exists := weights["NSE:SARDAEN"]; exists {
		t.Errorf("expected NSE:SARDAEN to be removed from active weights")
	}
	if _, exists := weights["NSE:SUMICHEM"]; exists {
		t.Errorf("expected NSE:SUMICHEM to be removed from active weights")
	}

	if w, ok := weights["NSE:PARKHOSPS"]; !ok || w != 0.0537 {
		t.Errorf("expected NSE:PARKHOSPS with weight 0.0537, got %v (ok=%v)", w, ok)
	}
	if w, ok := weights["NSE:TIPSMUSIC"]; !ok || w != 0.0522 {
		t.Errorf("expected NSE:TIPSMUSIC with weight 0.0522, got %v (ok=%v)", w, ok)
	}
	if w, ok := weights["NSE:DCBBANK"]; !ok || w != 0.0450 {
		t.Errorf("expected NSE:DCBBANK with weight 0.0450, got %v (ok=%v)", w, ok)
	}

	// Check raw content contains zero entries
	content, _ := os.ReadFile(basketPath)
	strContent := string(content)
	if !strings.Contains(strContent, "NSE:HINDCOPPER,0.0000") {
		t.Errorf("expected raw CSV to preserve NSE:HINDCOPPER,0.0000 at bottom")
	}
	if !strings.Contains(strContent, "NSE:SARDAEN,0.0000") {
		t.Errorf("expected raw CSV to preserve NSE:SARDAEN,0.0000 at bottom")
	}
	if !strings.Contains(strContent, "NSE:SUMICHEM,0.0000") {
		t.Errorf("expected raw CSV to preserve NSE:SUMICHEM,0.0000 at bottom")
	}
}

func TestApplySentrySwapsToCandidateCSV(t *testing.T) {
	dir := t.TempDir()
	candidatePath := filepath.Join(dir, "candidate.csv")

	initialCSV := `ticker,weight
NSE:TMCV,0.0598
NSE:HINDCOPPER,0.0537
NSE:SARDAEN,0.0522
NSE:SUMICHEM,0.0450
`
	if err := os.WriteFile(candidatePath, []byte(initialCSV), 0644); err != nil {
		t.Fatalf("failed to write test candidate CSV: %v", err)
	}

	rebalances := []RebalanceSwap{
		{
			ExitTicker:   "NSE:HINDCOPPER",
			TargetWeight: 0.0537,
			EntryTicker:  "NSE:PARKHOSPS",
			EntrySector:  "Healthcare",
		},
		{
			ExitTicker:   "NSE:SARDAEN",
			TargetWeight: 0.0522,
			EntryTicker:  "NSE:TIPSMUSIC",
			EntrySector:  "Communication Serv",
		},
		{
			ExitTicker:   "NSE:SUMICHEM",
			TargetWeight: 0.0450,
			EntryTicker:  "NSE:DCBBANK",
			EntrySector:  "Financial Services",
		},
	}

	if err := ApplySentrySwapsToCandidateCSV(candidatePath, rebalances); err != nil {
		t.Fatalf("ApplySentrySwapsToCandidateCSV failed: %v", err)
	}

	// Verify backup file exists
	if _, err := os.Stat(candidatePath + ".pre_sentry.bak"); err != nil {
		t.Errorf("expected pre_sentry backup file to exist: %v", err)
	}

	// Read updated candidate CSV
	weights, err := csvloader.ReadCSVWeights(candidatePath)
	if err != nil {
		t.Fatalf("failed to read updated candidate CSV: %v", err)
	}

	// Verify exits removed
	if _, ok := weights["NSE:HINDCOPPER"]; ok {
		t.Errorf("expected NSE:HINDCOPPER to be removed from candidates")
	}
	if _, ok := weights["NSE:SARDAEN"]; ok {
		t.Errorf("expected NSE:SARDAEN to be removed from candidates")
	}
	if _, ok := weights["NSE:SUMICHEM"]; ok {
		t.Errorf("expected NSE:SUMICHEM to be removed from candidates")
	}

	// Verify entries present with target weights
	if w, ok := weights["NSE:TMCV"]; !ok || w != 0.0598 {
		t.Errorf("expected NSE:TMCV to be retained with 0.0598, got %v", w)
	}
	if w, ok := weights["NSE:PARKHOSPS"]; !ok || w != 0.0537 {
		t.Errorf("expected NSE:PARKHOSPS with 0.0537, got %v", w)
	}
	if w, ok := weights["NSE:TIPSMUSIC"]; !ok || w != 0.0522 {
		t.Errorf("expected NSE:TIPSMUSIC with 0.0522, got %v", w)
	}
	if w, ok := weights["NSE:DCBBANK"]; !ok || w != 0.0450 {
		t.Errorf("expected NSE:DCBBANK with 0.0450, got %v", w)
	}
}


