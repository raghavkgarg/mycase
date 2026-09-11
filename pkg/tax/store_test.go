package tax_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/tax"
)

// newTestStore opens a fresh DuckDB cache in t.TempDir() and returns a tax.Store
// backed by its connection. The Store lazily creates its own tables (R16 P4:
// the tax domain owns its persistence, not pkg/cache).
func newTestStore(t *testing.T) *tax.Store {
	t.Helper()
	c, err := cache.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return tax.NewStore(c.Conn())
}

func TestInsertTransactions_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	txns := []tax.Transaction{
		{ID: "schwab_1", Ticker: "US:AAPL", Type: tax.TxnBuy, Quantity: 10, Price: 100, Fees: 0.5, TradedAt: time.Unix(1700000000, 0)},
		{ID: "schwab_2", Ticker: "US:AAPL", Type: tax.TxnSell, Quantity: 5, Price: 120, TradedAt: time.Unix(1700100000, 0)},
	}
	if err := s.InsertTransactions(ctx, txns); err != nil {
		t.Fatalf("InsertTransactions: %v", err)
	}

	got, err := s.GetTransactions(ctx)
	if err != nil {
		t.Fatalf("GetTransactions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(got))
	}
	// Ordered chronologically.
	if got[0].ID != "schwab_1" || got[1].ID != "schwab_2" {
		t.Errorf("unexpected order: %s, %s", got[0].ID, got[1].ID)
	}
	if got[0].Type != tax.TxnBuy || got[0].Fees != 0.5 {
		t.Errorf("buy round-trip wrong: %+v", got[0])
	}
}

func TestInsertTransactions_Idempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	txn := []tax.Transaction{{ID: "x", Ticker: "US:AAPL", Type: tax.TxnBuy, Quantity: 1, Price: 100, TradedAt: time.Unix(1700000000, 0)}}
	if err := s.InsertTransactions(ctx, txn); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertTransactions(ctx, txn); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetTransactions(ctx)
	if len(got) != 1 {
		t.Errorf("expected 1 (idempotent on txn_id), got %d", len(got))
	}
}

func TestReplaceOpenLots_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	lots := map[string][]tax.Lot{
		"US:AAPL": {
			{ID: "US:AAPL_20240101_1", Ticker: "US:AAPL", Quantity: 10, CostPerShare: 100, AcquiredAt: time.Unix(1700000000, 0), Source: "schwab_txn"},
		},
	}
	if err := s.ReplaceOpenLots(ctx, lots); err != nil {
		t.Fatalf("ReplaceOpenLots: %v", err)
	}

	got, err := s.GetOpenLots(ctx)
	if err != nil {
		t.Fatalf("GetOpenLots: %v", err)
	}
	if len(got["US:AAPL"]) != 1 {
		t.Fatalf("expected 1 lot, got %d", len(got["US:AAPL"]))
	}
	lot := got["US:AAPL"][0]
	if lot.Quantity != 10 || lot.CostPerShare != 100 {
		t.Errorf("lot round-trip wrong: %+v", lot)
	}

	// Replace with fewer lots — old ones should be gone.
	if err := s.ReplaceOpenLots(ctx, map[string][]tax.Lot{}); err != nil {
		t.Fatal(err)
	}
	got2, _ := s.GetOpenLots(ctx)
	if len(got2) != 0 {
		t.Errorf("expected lots cleared, got %d tickers", len(got2))
	}
}

func TestReplaceRealizedGains_AndSince(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	gains := []tax.RealizedGain{
		{TransactionID: "s1", LotID: "l1", Ticker: "US:AAPL", Quantity: 5, Proceeds: 600, CostBasis: 500, Gain: 100, SoldAt: time.Unix(1690000000, 0), LongTerm: false},
		{TransactionID: "s2", LotID: "l2", Ticker: "US:MSFT", Quantity: 3, Proceeds: 300, CostBasis: 360, Gain: -60, SoldAt: time.Unix(1700000000, 0), LongTerm: true},
	}
	if err := s.ReplaceRealizedGains(ctx, gains); err != nil {
		t.Fatalf("ReplaceRealizedGains: %v", err)
	}

	all, err := s.GetRealizedGains(ctx, time.Time{})
	if err != nil {
		t.Fatalf("GetRealizedGains: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 gains, got %d", len(all))
	}

	// Filter to only the later sale.
	since := time.Unix(1695000000, 0)
	recent, _ := s.GetRealizedGains(ctx, since)
	if len(recent) != 1 || recent[0].TransactionID != "s2" {
		t.Errorf("since filter wrong: %+v", recent)
	}
}

func TestLatestBuyDates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	txns := []tax.Transaction{
		{ID: "b1", Ticker: "US:AAPL", Type: tax.TxnBuy, Quantity: 1, Price: 100, TradedAt: time.Unix(1690000000, 0)},
		{ID: "b2", Ticker: "US:AAPL", Type: tax.TxnBuy, Quantity: 1, Price: 110, TradedAt: time.Unix(1700000000, 0)}, // later
		{ID: "s1", Ticker: "US:AAPL", Type: tax.TxnSell, Quantity: 1, Price: 120, TradedAt: time.Unix(1705000000, 0)},
	}
	if err := s.InsertTransactions(ctx, txns); err != nil {
		t.Fatal(err)
	}

	dates, err := s.LatestBuyDates(ctx)
	if err != nil {
		t.Fatalf("LatestBuyDates: %v", err)
	}
	if !dates["US:AAPL"].Equal(time.Unix(1700000000, 0)) {
		t.Errorf("expected latest buy 1700000000, got %d", dates["US:AAPL"].Unix())
	}
}
