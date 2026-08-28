package tax

import (
	"testing"

	"github.com/raghavkgarg/mycase/pkg/broker"
)

func TestTaxOptimizeOrders_SequencesLossSellsFirst(t *testing.T) {
	openLots := map[string][]Lot{
		// AAPL held at 200, will be sold at a loss (price 150).
		"US:AAPL": {{ID: "a1", Ticker: "US:AAPL", Quantity: 10, CostPerShare: 200, AcquiredAt: date(2024, 1, 1)}},
		// XOM held at 50, sold at a gain (price 100).
		"US:XOM": {{ID: "x1", Ticker: "US:XOM", Quantity: 10, CostPerShare: 50, AcquiredAt: date(2024, 1, 1)}},
	}
	orders := []broker.Order{
		{TradingSymbol: "NVDA", Exchange: "US", TransactionType: "BUY", Quantity: 5, Price: 300},
		{TradingSymbol: "XOM", Exchange: "US", TransactionType: "SELL", Quantity: 10, Price: 100},  // gain
		{TradingSymbol: "AAPL", Exchange: "US", TransactionType: "SELL", Quantity: 10, Price: 150}, // loss
	}
	prices := map[string]float64{"US:AAPL": 150, "US:XOM": 100, "US:NVDA": 300}

	plan := TaxOptimizeOrders(orders, prices, SequenceParams{OpenLots: openLots})

	if len(plan.Orders) != 3 {
		t.Fatalf("expected 3 orders, got %d", len(plan.Orders))
	}
	// Order[0] must be the loss sell (AAPL), then gain sell (XOM), then buy (NVDA).
	if plan.Orders[0].TradingSymbol != "AAPL" {
		t.Errorf("expected AAPL loss-sell first, got %s", plan.Orders[0].TradingSymbol)
	}
	if plan.Orders[1].TradingSymbol != "XOM" {
		t.Errorf("expected XOM gain-sell second, got %s", plan.Orders[1].TradingSymbol)
	}
	if plan.Orders[2].TradingSymbol != "NVDA" {
		t.Errorf("expected NVDA buy last, got %s", plan.Orders[2].TradingSymbol)
	}
	if len(plan.HarvestSells) != 1 || plan.HarvestSells[0] != "US:AAPL" {
		t.Errorf("expected AAPL as harvest sell, got %v", plan.HarvestSells)
	}
	if plan.EstTaxSaving <= 0 {
		t.Errorf("expected positive est tax saving, got %.2f", plan.EstTaxSaving)
	}
}

func TestTaxOptimizeOrders_DetectsWashSaleInBatch(t *testing.T) {
	openLots := map[string][]Lot{
		"US:AAPL": {{ID: "a1", Ticker: "US:AAPL", Quantity: 10, CostPerShare: 200, AcquiredAt: date(2024, 1, 1)}},
	}
	// Sell AAPL at a loss AND buy AAPL in the same batch → wash sale.
	orders := []broker.Order{
		{TradingSymbol: "AAPL", Exchange: "US", TransactionType: "SELL", Quantity: 5, Price: 150},
		{TradingSymbol: "AAPL", Exchange: "US", TransactionType: "BUY", Quantity: 5, Price: 150},
	}
	prices := map[string]float64{"US:AAPL": 150}

	plan := TaxOptimizeOrders(orders, prices, SequenceParams{OpenLots: openLots})
	if len(plan.WashSaleWarnings) == 0 {
		t.Error("expected a wash-sale warning for buying AAPL sold at a loss")
	}
}

func TestTaxOptimizeOrders_NoLossNoHarvest(t *testing.T) {
	openLots := map[string][]Lot{
		"US:XOM": {{ID: "x1", Ticker: "US:XOM", Quantity: 10, CostPerShare: 50, AcquiredAt: date(2024, 1, 1)}},
	}
	orders := []broker.Order{
		{TradingSymbol: "XOM", Exchange: "US", TransactionType: "SELL", Quantity: 10, Price: 100}, // gain
	}
	prices := map[string]float64{"US:XOM": 100}

	plan := TaxOptimizeOrders(orders, prices, SequenceParams{OpenLots: openLots})
	if len(plan.HarvestSells) != 0 {
		t.Errorf("expected no harvest sells for a gain, got %v", plan.HarvestSells)
	}
	if plan.EstTaxSaving != 0 {
		t.Errorf("expected zero saving, got %.2f", plan.EstTaxSaving)
	}
}
