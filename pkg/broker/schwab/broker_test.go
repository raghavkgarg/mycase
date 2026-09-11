package schwab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/broker"
)

func buildBrokerTestClient(t *testing.T, handler http.Handler) (*Client, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	dir := t.TempDir()
	tokenPath := dir + "/token.json"
	SaveToken(tokenPath, &Token{
		AccessToken:      "test_token",
		RefreshToken:     "test_refresh",
		ExpiresAt:        time.Now().Add(1 * time.Hour).Unix(),
		RefreshExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(),
	})
	app := &AppConfig{ClientID: "test", ClientSecret: "test"}
	tm := NewTokenManager(app, tokenPath)

	client := NewClient(tm)
	client.SetTraderBase(server.URL)
	client.SetMarketDataBase(server.URL)
	return client, server.URL
}

func TestSchwabBrokerGetHoldings(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(AccountResponse{
			SecuritiesAccount: SecuritiesAccount{
				Type:      "INDIVIDUAL",
				AccountID: "12345",
				Positions: []Position{
					{
						LongQuantity: 100,
						AveragePrice: 150.00,
						MarketValue:  18500.00,
						Instrument:   PositionInstrument{AssetType: "EQUITY", Symbol: "AAPL"},
					},
					{
						LongQuantity: 50,
						AveragePrice: 350.00,
						MarketValue:  19000.00,
						Instrument:   PositionInstrument{AssetType: "EQUITY", Symbol: "MSFT"},
					},
					{
						// Non-equity should be skipped
						LongQuantity: 1000,
						AveragePrice: 1.00,
						MarketValue:  1000.00,
						Instrument:   PositionInstrument{AssetType: "CASH_EQUIVALENT", Symbol: "USD"},
					},
				},
			},
		})
	})

	client, _ := buildBrokerTestClient(t, handler)
	b := NewBroker(client, "hash123")

	holdings, err := b.GetHoldings()
	if err != nil {
		t.Fatalf("GetHoldings: %v", err)
	}

	if len(holdings) != 2 {
		t.Fatalf("len(holdings) = %d, want 2 (cash equivalent should be filtered)", len(holdings))
	}

	aapl := holdings[0]
	if aapl.TradingSymbol != "AAPL" {
		t.Errorf("holdings[0].TradingSymbol = %q, want AAPL", aapl.TradingSymbol)
	}
	if aapl.Quantity != 100 {
		t.Errorf("AAPL Quantity = %d, want 100", aapl.Quantity)
	}
	if aapl.AveragePrice != 150.00 {
		t.Errorf("AAPL AveragePrice = %v, want 150.00", aapl.AveragePrice)
	}
	if aapl.Exchange != "US" {
		t.Errorf("AAPL Exchange = %q, want US", aapl.Exchange)
	}
	// LastPrice = MarketValue / Qty = 18500 / 100 = 185
	if aapl.LastPrice != 185.00 {
		t.Errorf("AAPL LastPrice = %v, want 185.00", aapl.LastPrice)
	}
	// PnL = MarketValue - (AvgPrice * Qty) = 18500 - 15000 = 3500
	if aapl.PnL != 3500.00 {
		t.Errorf("AAPL PnL = %v, want 3500.00", aapl.PnL)
	}
}

func TestSchwabBrokerGetQuotes(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(QuoteResponse{
			"AAPL": {Symbol: "AAPL", Quote: QuoteDetail{LastPrice: 185.50}},
			"MSFT": {Symbol: "MSFT", Quote: QuoteDetail{LastPrice: 378.25}},
		})
	})

	client, _ := buildBrokerTestClient(t, handler)
	b := NewBroker(client, "hash123")

	prices, err := b.GetQuotes([]string{"US:AAPL", "US:MSFT"})
	if err != nil {
		t.Fatalf("GetQuotes: %v", err)
	}

	if prices["US:AAPL"] != 185.50 {
		t.Errorf("AAPL = %v, want 185.50", prices["US:AAPL"])
	}
	if prices["US:MSFT"] != 378.25 {
		t.Errorf("MSFT = %v, want 378.25", prices["US:MSFT"])
	}
}

func TestSchwabBrokerPlaceOrder(t *testing.T) {
	var receivedOrder SchwabOrder

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}

		json.NewDecoder(r.Body).Decode(&receivedOrder)

		w.Header().Set("Location", "/trader/v1/accounts/hash123/orders/ORDER-789")
		w.WriteHeader(http.StatusCreated)
	})

	client, _ := buildBrokerTestClient(t, handler)
	b := NewBroker(client, "hash123")

	result, err := b.PlaceOrder("regular", broker.Order{
		TradingSymbol:   "AAPL",
		Exchange:        "US",
		TransactionType: "BUY",
		Quantity:        10,
		OrderType:       "LIMIT",
		Price:           185.00,
	})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}

	if result.OrderID != "ORDER-789" {
		t.Errorf("OrderID = %q, want ORDER-789", result.OrderID)
	}

	// Verify the order payload
	if receivedOrder.OrderType != "LIMIT" {
		t.Errorf("orderType = %q, want LIMIT", receivedOrder.OrderType)
	}
	if receivedOrder.Price != 185.00 {
		t.Errorf("price = %v, want 185.00", receivedOrder.Price)
	}
	if receivedOrder.Session != "NORMAL" {
		t.Errorf("session = %q, want NORMAL", receivedOrder.Session)
	}
	if receivedOrder.Duration != "DAY" {
		t.Errorf("duration = %q, want DAY", receivedOrder.Duration)
	}
	if len(receivedOrder.OrderLegCollection) != 1 {
		t.Fatalf("legs = %d, want 1", len(receivedOrder.OrderLegCollection))
	}

	leg := receivedOrder.OrderLegCollection[0]
	if leg.Instruction != "BUY" {
		t.Errorf("instruction = %q, want BUY", leg.Instruction)
	}
	if leg.Quantity != 10 {
		t.Errorf("quantity = %d, want 10", leg.Quantity)
	}
	if leg.Instrument.Symbol != "AAPL" {
		t.Errorf("symbol = %q, want AAPL", leg.Instrument.Symbol)
	}
}

func TestSchwabBrokerPlaceGTTReturnsError(t *testing.T) {
	client, _ := buildBrokerTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	b := NewBroker(client, "hash123")

	_, err := b.PlaceGTT(broker.Order{})
	if err == nil {
		t.Fatal("PlaceGTT should return an error for US markets")
	}
}

func TestSchwabBrokerIsMock(t *testing.T) {
	client, _ := buildBrokerTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	b := NewBroker(client, "hash123")

	if b.IsMock() {
		t.Error("SchwabBroker.IsMock() should return false")
	}
}

func TestExtractOrderIDFromLocation(t *testing.T) {
	tests := []struct {
		location string
		want     string
	}{
		{"/trader/v1/accounts/hash123/orders/12345678", "12345678"},
		{"", ""},
		{"/orders/ABC-DEF", "ABC-DEF"},
	}
	for _, tt := range tests {
		got := extractOrderIDFromLocation(tt.location)
		if got != tt.want {
			t.Errorf("extractOrderIDFromLocation(%q) = %q, want %q", tt.location, got, tt.want)
		}
	}
}

func TestSchwabBrokerInterfaceCompliance(t *testing.T) {
	// Compile-time check is in broker.go (var _ broker.Broker = ...).
	// This test confirms it at runtime too.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(AccountResponse{SecuritiesAccount: SecuritiesAccount{}})
	})
	client, _ := buildBrokerTestClient(t, handler)
	var _ broker.Broker = NewBroker(client, "hash")

	// Also verify the unused context import doesn't cause issues
	_ = context.Background()
}
