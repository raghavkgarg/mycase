package datafetcher

import (
	"context"
	"testing"

	"github.com/raghavkgarg/mycase/pkg/broker"
)

type mockBrokerWithHoldings struct {
	broker.MockBroker
}

func (m *mockBrokerWithHoldings) GetHoldings() ([]broker.Holding, error) {
	return []broker.Holding{
		{TradingSymbol: "TARAIL", LastPrice: 42.50, Quantity: 10},
		{TradingSymbol: "RELIANCE", LastPrice: 2800.00, Quantity: 5},
	}, nil
}

func TestFetchMarketData_HoldingFallback(t *testing.T) {
	b := &mockBrokerWithHoldings{}
	basketKeys := []string{"NSE:TARAIL"}

	// When using mock broker, GetQuotes returns prices
	quotes, holdings, holdingDetails, err := FetchMarketData(context.Background(), b, basketKeys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if quotes["NSE:TARAIL"] <= 0 {
		t.Errorf("expected positive price for NSE:TARAIL, got %f", quotes["NSE:TARAIL"])
	}

	if holdings["TARAIL"] != 10 {
		t.Errorf("expected 10 quantity for TARAIL, got %d", holdings["TARAIL"])
	}

	if holdingDetails["TARAIL"].Quantity != 10 {
		t.Errorf("expected 10 quantity in holdingDetails for TARAIL, got %d", holdingDetails["TARAIL"].Quantity)
	}
}
