package tax

import (
	"testing"
	"time"
)

func TestFindHarvestCandidates_IdentifiesLoss(t *testing.T) {
	now := date(2024, 6, 1)
	openLots := map[string][]Lot{
		"US:AAPL": {{ID: "a1", Ticker: "US:AAPL", Quantity: 10, CostPerShare: 200, AcquiredAt: date(2024, 1, 1)}},
		"US:MSFT": {{ID: "m1", Ticker: "US:MSFT", Quantity: 10, CostPerShare: 100, AcquiredAt: date(2024, 1, 1)}},
	}
	// AAPL down (150 < 200 → loss), MSFT up (120 > 100 → gain, not a candidate).
	prices := map[string]float64{"US:AAPL": 150, "US:MSFT": 120}

	p := DefaultHarvestParams()
	p.AsOf = now
	p.MinLoss = 10

	cands := FindHarvestCandidates(openLots, prices, nil, p)
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate (AAPL), got %d", len(cands))
	}
	c := cands[0]
	if c.Ticker != "US:AAPL" {
		t.Errorf("expected AAPL, got %s", c.Ticker)
	}
	// Loss = (150-200)*10 = -500.
	if !approx(c.UnrealizedLoss, -500) {
		t.Errorf("expected loss -500, got %.2f", c.UnrealizedLoss)
	}
	// Short-term (held < 365 as of 2024-06-01) → 0.37 rate → saving 185.
	if !approx(c.EstTaxSaving, 185) {
		t.Errorf("expected est saving 185, got %.2f", c.EstTaxSaving)
	}
}

func TestFindHarvestCandidates_SkipsSmallLoss(t *testing.T) {
	openLots := map[string][]Lot{
		"US:AAPL": {{ID: "a1", Ticker: "US:AAPL", Quantity: 1, CostPerShare: 100, AcquiredAt: date(2024, 1, 1)}},
	}
	prices := map[string]float64{"US:AAPL": 99} // loss of $1

	p := DefaultHarvestParams()
	p.MinLoss = 50

	cands := FindHarvestCandidates(openLots, prices, nil, p)
	if len(cands) != 0 {
		t.Errorf("expected no candidates below min-loss, got %d", len(cands))
	}
}

func TestFindHarvestCandidates_MixedLotsOnlyHarvestLosers(t *testing.T) {
	openLots := map[string][]Lot{
		"US:AAPL": {
			{ID: "a1", Ticker: "US:AAPL", Quantity: 10, CostPerShare: 50, AcquiredAt: date(2024, 1, 1)},  // winner @ price 100
			{ID: "a2", Ticker: "US:AAPL", Quantity: 10, CostPerShare: 150, AcquiredAt: date(2024, 2, 1)}, // loser @ price 100
		},
	}
	prices := map[string]float64{"US:AAPL": 100}

	p := DefaultHarvestParams()
	p.MinLoss = 10

	cands := FindHarvestCandidates(openLots, prices, nil, p)
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	// Only the loser lot: (100-150)*10 = -500. The winner lot is excluded.
	if !approx(cands[0].UnrealizedLoss, -500) {
		t.Errorf("expected loss -500 (only loser lot), got %.2f", cands[0].UnrealizedLoss)
	}
	if !approx(cands[0].Quantity, 10) {
		t.Errorf("expected 10 loss shares, got %.2f", cands[0].Quantity)
	}
}

func TestFindHarvestCandidates_WashSaleRisk(t *testing.T) {
	now := date(2024, 6, 15)
	openLots := map[string][]Lot{
		"US:AAPL": {{ID: "a1", Ticker: "US:AAPL", Quantity: 10, CostPerShare: 200, AcquiredAt: date(2024, 1, 1)}},
	}
	prices := map[string]float64{"US:AAPL": 150}

	p := DefaultHarvestParams()
	p.AsOf = now
	p.MinLoss = 10
	p.RecentBuys = map[string]time.Time{"US:AAPL": date(2024, 6, 1)} // 14 days ago

	cands := FindHarvestCandidates(openLots, prices, nil, p)
	if len(cands) != 1 || !cands[0].WashSaleRisk {
		t.Fatalf("expected wash-sale risk flagged, got %+v", cands)
	}
}

func TestSuggestSubstitute_SameSectorNotHeld(t *testing.T) {
	openLots := map[string][]Lot{
		"US:AAPL": {{ID: "a1", Ticker: "US:AAPL", Quantity: 10, CostPerShare: 200, AcquiredAt: date(2024, 1, 1)}},
	}
	prices := map[string]float64{"US:AAPL": 150}
	universe := []string{"US:AAPL", "US:MSFT", "US:XOM"}
	sectors := map[string]string{"US:AAPL": "Tech", "US:MSFT": "Tech", "US:XOM": "Energy"}

	p := DefaultHarvestParams()
	p.MinLoss = 10
	p.Sectors = sectors

	cands := FindHarvestCandidates(openLots, prices, universe, p)
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	// MSFT is same sector (Tech), not held → valid substitute. XOM wrong sector.
	if cands[0].Substitute != "US:MSFT" {
		t.Errorf("expected substitute US:MSFT, got %q", cands[0].Substitute)
	}
}

func TestDetectWashSales(t *testing.T) {
	realized := []RealizedGain{
		{Ticker: "US:AAPL", Gain: -100, SoldAt: date(2024, 6, 15)},
		{Ticker: "US:MSFT", Gain: 200, SoldAt: date(2024, 6, 15)}, // gain, not a wash sale
	}
	buys := []Transaction{
		{Ticker: "US:AAPL", Type: TxnBuy, TradedAt: date(2024, 6, 20)}, // 5 days after loss sale
		{Ticker: "US:MSFT", Type: TxnBuy, TradedAt: date(2024, 6, 18)},
	}
	v := DetectWashSales(realized, buys)
	if len(v) != 1 {
		t.Fatalf("expected 1 wash-sale violation, got %d", len(v))
	}
	if v[0].Ticker != "US:AAPL" {
		t.Errorf("expected AAPL violation, got %s", v[0].Ticker)
	}
}

func TestSummarizeRealized_STLTSplit(t *testing.T) {
	realized := []RealizedGain{
		{Gain: 100, LongTerm: false, SoldAt: date(2024, 3, 1)},
		{Gain: -40, LongTerm: false, SoldAt: date(2024, 4, 1)},
		{Gain: 300, LongTerm: true, SoldAt: date(2024, 5, 1)},
		{Gain: -50, LongTerm: true, SoldAt: date(2024, 6, 1)},
	}
	s := SummarizeRealized(realized, time.Time{})
	if !approx(s.NetShortTerm, 60) {
		t.Errorf("expected net ST 60, got %.2f", s.NetShortTerm)
	}
	if !approx(s.NetLongTerm, 250) {
		t.Errorf("expected net LT 250, got %.2f", s.NetLongTerm)
	}
	if !approx(s.NetTotal, 310) {
		t.Errorf("expected net total 310, got %.2f", s.NetTotal)
	}
	if s.Count != 4 {
		t.Errorf("expected count 4, got %d", s.Count)
	}
}

func TestSummarizeRealized_SinceFilter(t *testing.T) {
	realized := []RealizedGain{
		{Gain: 100, SoldAt: date(2023, 12, 1)}, // before cutoff
		{Gain: 50, SoldAt: date(2024, 3, 1)},   // after cutoff
	}
	s := SummarizeRealized(realized, date(2024, 1, 1))
	if s.Count != 1 || !approx(s.NetTotal, 50) {
		t.Errorf("expected 1 record net 50, got count=%d net=%.2f", s.Count, s.NetTotal)
	}
}
