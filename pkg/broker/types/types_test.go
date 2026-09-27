package types

import (
	"encoding/json"
	"testing"
)

func TestHolding_JSONSerialization(t *testing.T) {
	h := Holding{
		TradingSymbol: "NSE:RELIANCE",
		Exchange:      "NSE",
		Quantity:      50,
		T1Quantity:    10,
		T2Quantity:    0,
		AveragePrice:  2500.50,
		LastPrice:     2650.00,
		PnL:           7475.00,
		PnLPct:        5.98,
	}

	data, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("failed to marshal Holding: %v", err)
	}

	var h2 Holding
	if err := json.Unmarshal(data, &h2); err != nil {
		t.Fatalf("failed to unmarshal Holding: %v", err)
	}

	if h2.TradingSymbol != h.TradingSymbol || h2.Quantity != h.Quantity || h2.LastPrice != h.LastPrice {
		t.Errorf("unmarshaled Holding mismatch: got %+v, want %+v", h2, h)
	}
}

func TestOrder_Fields(t *testing.T) {
	ord := Order{
		TradingSymbol:   "NSE:TCS",
		Exchange:        "NSE",
		TransactionType: "BUY",
		OrderType:       "LIMIT",
		Product:         "CNC",
		Quantity:        20,
		Price:           3500.0,
		Ltp:             3495.0,
		TriggerPrice:    3490.0,
	}

	if ord.TransactionType != "BUY" || ord.Quantity != 20 || ord.Price != 3500.0 {
		t.Errorf("Order fields mismatch: %+v", ord)
	}
}

func TestOrderResult_Fields(t *testing.T) {
	res := OrderResult{
		OrderID:   "ORD12345",
		TriggerID: 9876,
	}

	if res.OrderID != "ORD12345" || res.TriggerID != 9876 {
		t.Errorf("OrderResult mismatch: %+v", res)
	}
}

func TestMarketConfig_Fields(t *testing.T) {
	mcIndia := MarketConfig{
		Benchmark: "^NSEI",
		Exchange:  "NSE",
		Currency:  "₹",
		Timezone:  "Asia/Kolkata",
		Market:    "india",
		CloseHour: 15,
		CloseMin:  30,
	}

	if mcIndia.Exchange != "NSE" || mcIndia.CloseHour != 15 || mcIndia.CloseMin != 30 {
		t.Errorf("MarketConfig India mismatch: %+v", mcIndia)
	}

	mcUS := MarketConfig{
		Benchmark: "^GSPC",
		Exchange:  "US",
		Currency:  "$",
		Timezone:  "America/New_York",
		Market:    "us",
		CloseHour: 16,
		CloseMin:  0,
	}

	if mcUS.Exchange != "US" || mcUS.CloseHour != 16 || mcUS.CloseMin != 0 {
		t.Errorf("MarketConfig US mismatch: %+v", mcUS)
	}
}
