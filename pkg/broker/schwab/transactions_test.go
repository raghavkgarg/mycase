package schwab

import (
	"testing"

	"github.com/raghavkgarg/mycase/pkg/tax"
)

func TestNormalizeTransactions_BuyAndSell(t *testing.T) {
	raw := []SchwabTransaction{
		{
			ActivityID: 111,
			Time:       "2024-03-14T14:30:00+0000",
			Type:       "TRADE",
			TransferItems: []SchwabTransferItem{
				{Instrument: SchwabTxnInstrument{AssetType: "EQUITY", Symbol: "AAPL"}, Amount: 10, Price: 150},
				{FeeType: "COMMISSION", Cost: -0.65},
			},
		},
		{
			ActivityID: 222,
			Time:       "2024-04-01T10:00:00+0000",
			Type:       "TRADE",
			TransferItems: []SchwabTransferItem{
				{Instrument: SchwabTxnInstrument{AssetType: "EQUITY", Symbol: "AAPL"}, Amount: -5, Price: 160},
			},
		},
	}

	txns := NormalizeTransactions(raw)
	if len(txns) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txns))
	}

	buy := txns[0]
	if buy.Type != tax.TxnBuy || buy.Ticker != "US:AAPL" || buy.Quantity != 10 || buy.Price != 150 {
		t.Errorf("unexpected buy: %+v", buy)
	}
	if buy.Fees != 0.65 {
		t.Errorf("expected fees 0.65, got %.2f", buy.Fees)
	}
	if buy.ID != "schwab_111" {
		t.Errorf("expected id schwab_111, got %s", buy.ID)
	}

	sell := txns[1]
	if sell.Type != tax.TxnSell || sell.Quantity != 5 {
		t.Errorf("unexpected sell (should be positive qty 5): %+v", sell)
	}
}

func TestNormalizeTransactions_SkipsNonTrade(t *testing.T) {
	raw := []SchwabTransaction{
		{ActivityID: 1, Time: "2024-03-14T14:30:00+0000", Type: "DIVIDEND_OR_INTEREST"},
		{ActivityID: 2, Time: "2024-03-14T14:30:00+0000", Type: "TRADE",
			TransferItems: []SchwabTransferItem{
				{Instrument: SchwabTxnInstrument{AssetType: "EQUITY", Symbol: "MSFT"}, Amount: 3, Price: 400},
			}},
	}
	txns := NormalizeTransactions(raw)
	if len(txns) != 1 {
		t.Fatalf("expected 1 transaction (non-trade skipped), got %d", len(txns))
	}
	if txns[0].Ticker != "US:MSFT" {
		t.Errorf("expected US:MSFT, got %s", txns[0].Ticker)
	}
}

func TestNormalizeTransactions_SkipsMalformedTime(t *testing.T) {
	raw := []SchwabTransaction{
		{ActivityID: 1, Time: "not-a-date", Type: "TRADE",
			TransferItems: []SchwabTransferItem{
				{Instrument: SchwabTxnInstrument{AssetType: "EQUITY", Symbol: "AAPL"}, Amount: 1, Price: 100},
			}},
	}
	txns := NormalizeTransactions(raw)
	if len(txns) != 0 {
		t.Errorf("expected malformed-time record to be skipped, got %d", len(txns))
	}
}

func TestParseSchwabTime_Formats(t *testing.T) {
	for _, s := range []string{
		"2024-03-14T14:30:00+0000",
		"2024-03-14T14:30:00Z",
		"2024-03-14T14:30:00.000+0000",
	} {
		if _, err := parseSchwabTime(s); err != nil {
			t.Errorf("failed to parse %q: %v", s, err)
		}
	}
}
