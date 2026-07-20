package optimizer

import (
	"testing"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/costs"
)

func TestFilterMicroTransactions_LargeOrderKept(t *testing.T) {
	// 1000 shares × ₹1000 BUY: trade=₹1,000,000; cost≈₹1151 (0.115%) < 0.5% → kept
	orders := []broker.Order{
		{TradingSymbol: "TCS", Exchange: "NSE", TransactionType: "BUY", Quantity: 1000, Price: 1000},
	}
	quotes := map[string]float64{"NSE:TCS": 1000}
	kept, filtered := FilterMicroTransactions(orders, quotes, costs.DefaultZerodha, 0.005)
	if len(kept) != 1 || len(filtered) != 0 {
		t.Errorf("large order should be kept: kept=%d filtered=%d", len(kept), len(filtered))
	}
}

func TestFilterMicroTransactions_MicroOrderFiltered(t *testing.T) {
	// 1 share × ₹50 SELL: DP=₹15.93 alone makes ratio ≈ 32% >> 0.5% → filtered
	orders := []broker.Order{
		{TradingSymbol: "XYZ", Exchange: "NSE", TransactionType: "SELL", Quantity: 1, Price: 50},
	}
	quotes := map[string]float64{"NSE:XYZ": 50}
	kept, filtered := FilterMicroTransactions(orders, quotes, costs.DefaultZerodha, 0.005)
	if len(kept) != 0 || len(filtered) != 1 {
		t.Errorf("micro order should be filtered: kept=%d filtered=%d", len(kept), len(filtered))
	}
}

func TestFilterMicroTransactions_ZeroThresholdDisabled(t *testing.T) {
	// Zero threshold → filter is disabled; all orders returned as kept
	orders := []broker.Order{
		{TradingSymbol: "XYZ", Exchange: "NSE", TransactionType: "SELL", Quantity: 1, Price: 50},
		{TradingSymbol: "TCS", Exchange: "NSE", TransactionType: "BUY", Quantity: 1000, Price: 1000},
	}
	kept, filtered := FilterMicroTransactions(orders, nil, costs.DefaultZerodha, 0)
	if len(kept) != 2 || len(filtered) != 0 {
		t.Errorf("zero threshold: want all kept, got kept=%d filtered=%d", len(kept), len(filtered))
	}
}

func TestFilterMicroTransactions_MixedOrders(t *testing.T) {
	orders := []broker.Order{
		{TradingSymbol: "TCS", Exchange: "NSE", TransactionType: "BUY", Quantity: 500, Price: 3000},  // large → kept
		{TradingSymbol: "TINY", Exchange: "NSE", TransactionType: "SELL", Quantity: 1, Price: 10},    // micro → filtered
		{TradingSymbol: "INFY", Exchange: "NSE", TransactionType: "BUY", Quantity: 100, Price: 1500}, // medium → kept
	}
	quotes := map[string]float64{
		"NSE:TCS":  3000,
		"NSE:TINY": 10,
		"NSE:INFY": 1500,
	}
	kept, filtered := FilterMicroTransactions(orders, quotes, costs.DefaultZerodha, 0.005)
	if len(kept) != 2 {
		t.Errorf("want 2 kept, got %d", len(kept))
	}
	if len(filtered) != 1 || filtered[0].TradingSymbol != "TINY" {
		t.Errorf("want TINY filtered, got %v", filtered)
	}
}

func TestFilterMicroTransactions_EmptyInput(t *testing.T) {
	kept, filtered := FilterMicroTransactions(nil, nil, costs.DefaultZerodha, 0.005)
	if len(kept) != 0 || len(filtered) != 0 {
		t.Errorf("empty input: want both nil, got kept=%d filtered=%d", len(kept), len(filtered))
	}
}

func TestDetectExits_Basic(t *testing.T) {
	golden := map[string]float64{
		"NSE:TCS":   0.40,
		"NSE:INFY":  0.30,
		"NSE:WIPRO": 0.30,
	}
	// WIPRO is no longer in new selection → should be an exit
	newKeys := []string{"NSE:TCS", "NSE:INFY"}
	exits := DetectExits(golden, newKeys)
	if len(exits) != 1 || exits[0] != "NSE:WIPRO" {
		t.Errorf("exits = %v, want [NSE:WIPRO]", exits)
	}
}

func TestDetectExits_ZeroWeightNotExit(t *testing.T) {
	// WIPRO has weight 0 in golden → already exited, not flagged again
	golden := map[string]float64{
		"NSE:TCS":   0.50,
		"NSE:WIPRO": 0.0,
	}
	exits := DetectExits(golden, []string{"NSE:TCS"})
	if len(exits) != 0 {
		t.Errorf("zero-weight ticker should not appear in exits: %v", exits)
	}
}
