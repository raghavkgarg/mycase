package tax

import (
	"math"
	"testing"
	"time"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestBuildLots_SingleBuyNoSell(t *testing.T) {
	txns := []Transaction{
		{ID: "1", Ticker: "US:AAPL", Type: TxnBuy, Quantity: 10, Price: 100, TradedAt: date(2024, 1, 1)},
	}
	res := BuildLots(txns)

	lots := res.OpenLots["US:AAPL"]
	if len(lots) != 1 {
		t.Fatalf("expected 1 open lot, got %d", len(lots))
	}
	if !approx(lots[0].Quantity, 10) || !approx(lots[0].CostPerShare, 100) {
		t.Errorf("unexpected lot: %+v", lots[0])
	}
	if len(res.RealizedGains) != 0 {
		t.Errorf("expected no realized gains, got %d", len(res.RealizedGains))
	}
}

func TestBuildLots_FIFOOrder(t *testing.T) {
	// Two buys at different prices, then sell part of the first lot.
	txns := []Transaction{
		{ID: "1", Ticker: "US:AAPL", Type: TxnBuy, Quantity: 10, Price: 100, TradedAt: date(2024, 1, 1)},
		{ID: "2", Ticker: "US:AAPL", Type: TxnBuy, Quantity: 10, Price: 200, TradedAt: date(2024, 6, 1)},
		{ID: "3", Ticker: "US:AAPL", Type: TxnSell, Quantity: 5, Price: 150, TradedAt: date(2024, 7, 1)},
	}
	res := BuildLots(txns)

	// FIFO: the 5 sold come from the $100 lot → gain (150-100)*5 = 250.
	if len(res.RealizedGains) != 1 {
		t.Fatalf("expected 1 realized gain, got %d", len(res.RealizedGains))
	}
	g := res.RealizedGains[0]
	if !approx(g.Gain, 250) {
		t.Errorf("expected gain 250, got %.2f", g.Gain)
	}
	if !approx(g.CostBasis, 500) {
		t.Errorf("expected cost basis 500, got %.2f", g.CostBasis)
	}

	// Remaining: 5 shares @ $100 + 10 shares @ $200.
	lots := res.OpenLots["US:AAPL"]
	if len(lots) != 2 {
		t.Fatalf("expected 2 open lots, got %d", len(lots))
	}
	if !approx(lots[0].Quantity, 5) || !approx(lots[0].CostPerShare, 100) {
		t.Errorf("first lot wrong: %+v", lots[0])
	}
	if !approx(lots[1].Quantity, 10) || !approx(lots[1].CostPerShare, 200) {
		t.Errorf("second lot wrong: %+v", lots[1])
	}
}

func TestBuildLots_SellSpansMultipleLots(t *testing.T) {
	txns := []Transaction{
		{ID: "1", Ticker: "US:MSFT", Type: TxnBuy, Quantity: 5, Price: 100, TradedAt: date(2024, 1, 1)},
		{ID: "2", Ticker: "US:MSFT", Type: TxnBuy, Quantity: 5, Price: 120, TradedAt: date(2024, 2, 1)},
		{ID: "3", Ticker: "US:MSFT", Type: TxnSell, Quantity: 8, Price: 130, TradedAt: date(2024, 3, 1)},
	}
	res := BuildLots(txns)

	if len(res.RealizedGains) != 2 {
		t.Fatalf("expected 2 realized gains (spanning 2 lots), got %d", len(res.RealizedGains))
	}
	// Lot 1: 5 @ 100 → (130-100)*5 = 150. Lot 2: 3 @ 120 → (130-120)*3 = 30.
	total := res.RealizedGains[0].Gain + res.RealizedGains[1].Gain
	if !approx(total, 180) {
		t.Errorf("expected total gain 180, got %.2f", total)
	}
	// Remaining: 2 @ 120.
	lots := res.OpenLots["US:MSFT"]
	if len(lots) != 1 || !approx(lots[0].Quantity, 2) {
		t.Fatalf("expected 1 lot of 2 shares, got %+v", lots)
	}
}

func TestBuildLots_OversellWarns(t *testing.T) {
	txns := []Transaction{
		{ID: "1", Ticker: "US:TSLA", Type: TxnBuy, Quantity: 5, Price: 100, TradedAt: date(2024, 1, 1)},
		{ID: "2", Ticker: "US:TSLA", Type: TxnSell, Quantity: 8, Price: 90, TradedAt: date(2024, 2, 1)},
	}
	res := BuildLots(txns)

	if len(res.Warnings) == 0 {
		t.Error("expected a warning for overselling")
	}
	// Only the 5 available shares are matched.
	if len(res.RealizedGains) != 1 || !approx(res.RealizedGains[0].Quantity, 5) {
		t.Errorf("expected 1 gain of 5 shares, got %+v", res.RealizedGains)
	}
	if _, ok := res.OpenLots["US:TSLA"]; ok {
		t.Error("expected no open lots after overselling")
	}
}

func TestBuildLots_HoldingPeriodClassification(t *testing.T) {
	txns := []Transaction{
		{ID: "1", Ticker: "US:AAPL", Type: TxnBuy, Quantity: 10, Price: 100, TradedAt: date(2023, 1, 1)},
		// Sold 400 days later → long term.
		{ID: "2", Ticker: "US:AAPL", Type: TxnSell, Quantity: 10, Price: 150, TradedAt: date(2024, 2, 5)},
	}
	res := BuildLots(txns)
	if len(res.RealizedGains) != 1 {
		t.Fatalf("expected 1 gain, got %d", len(res.RealizedGains))
	}
	if !res.RealizedGains[0].LongTerm {
		t.Errorf("expected long-term (held >365 days), got holdingDays=%d", res.RealizedGains[0].HoldingDays)
	}
}

func TestBuildLots_BuyFeesIncreaseBasis(t *testing.T) {
	txns := []Transaction{
		{ID: "1", Ticker: "US:AAPL", Type: TxnBuy, Quantity: 10, Price: 100, Fees: 10, TradedAt: date(2024, 1, 1)},
	}
	res := BuildLots(txns)
	lot := res.OpenLots["US:AAPL"][0]
	// Cost per share = 100 + 10/10 = 101.
	if !approx(lot.CostPerShare, 101) {
		t.Errorf("expected cost/share 101 with fees, got %.4f", lot.CostPerShare)
	}
}

func TestBuildLots_UnsortedInputIsSorted(t *testing.T) {
	// Provide sell before buy in slice order; BuildLots must sort chronologically.
	txns := []Transaction{
		{ID: "2", Ticker: "US:AAPL", Type: TxnSell, Quantity: 5, Price: 150, TradedAt: date(2024, 7, 1)},
		{ID: "1", Ticker: "US:AAPL", Type: TxnBuy, Quantity: 10, Price: 100, TradedAt: date(2024, 1, 1)},
	}
	res := BuildLots(txns)
	if len(res.RealizedGains) != 1 || !approx(res.RealizedGains[0].Gain, 250) {
		t.Errorf("chronological sort failed: %+v", res.RealizedGains)
	}
}
