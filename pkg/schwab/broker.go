package schwab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/raghavkgarg/mycase/pkg/broker"
)

// SchwabBroker implements broker.Broker for US equity trading via the Schwab Trader API.
type SchwabBroker struct {
	client      *Client
	accountHash string // encrypted account ID used in API paths
}

// NewBroker creates a SchwabBroker. accountHash is the encrypted account identifier
// from the Schwab accounts endpoint (obtained during auth or from saved token).
func NewBroker(client *Client, accountHash string) *SchwabBroker {
	return &SchwabBroker{
		client:      client,
		accountHash: accountHash,
	}
}

// IsMock reports that this is a live broker.
func (b *SchwabBroker) IsMock() bool { return false }

// GetHoldings retrieves equity positions from the Schwab account and maps them
// to the common broker.Holding type.
func (b *SchwabBroker) GetHoldings() ([]broker.Holding, error) {
	ctx := context.Background()
	path := fmt.Sprintf("/accounts/%s?fields=positions", b.accountHash)

	resp, err := b.client.GetTrader(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("schwab GetHoldings: %w", err)
	}

	var acctResp AccountResponse
	if err := DecodeJSON(resp, &acctResp); err != nil {
		return nil, fmt.Errorf("schwab GetHoldings decode: %w", err)
	}

	var holdings []broker.Holding
	for _, pos := range acctResp.SecuritiesAccount.Positions {
		// Only include equity positions
		if pos.Instrument.AssetType != "EQUITY" {
			continue
		}

		qty := int(pos.LongQuantity) - int(pos.ShortQuantity)
		if qty <= 0 {
			continue
		}

		// Compute last price from market value / quantity
		lastPrice := 0.0
		if qty > 0 {
			lastPrice = pos.MarketValue / float64(qty)
		}

		pnl := pos.MarketValue - (pos.AveragePrice * float64(qty))
		invested := pos.AveragePrice * float64(qty)
		pnlPct := 0.0
		if invested > 0 {
			pnlPct = (pnl / invested) * 100.0
		}

		holdings = append(holdings, broker.Holding{
			TradingSymbol: pos.Instrument.Symbol,
			Exchange:      "US",
			Quantity:      qty,
			T1Quantity:    0, // US is T+1; no separate bucket
			T2Quantity:    0,
			AveragePrice:  pos.AveragePrice,
			LastPrice:     lastPrice,
			PnL:           pnl,
			PnLPct:        pnlPct,
		})
	}

	return holdings, nil
}

// GetQuotes retrieves real-time quotes for the given instrument keys.
// Keys should be in "US:AAPL" format.
func (b *SchwabBroker) GetQuotes(keys []string) (map[string]float64, error) {
	ctx := context.Background()
	return b.client.FetchQuotes(ctx, keys)
}

// PlaceOrder places a US equity order via the Schwab Trader API.
// variety is ignored for Schwab (always regular session); it's kept for interface compatibility.
func (b *SchwabBroker) PlaceOrder(_ string, order broker.Order) (broker.OrderResult, error) {
	ctx := context.Background()

	// Map broker.Order to Schwab order payload
	schwabOrder := SchwabOrder{
		OrderType:         mapOrderType(order.OrderType),
		Session:           "NORMAL",
		Duration:          "DAY",
		OrderStrategyType: "SINGLE",
		OrderLegCollection: []OrderLeg{
			{
				Instruction: mapInstruction(order.TransactionType),
				Quantity:    order.Quantity,
				Instrument: OrderInstrument{
					Symbol:    order.TradingSymbol,
					AssetType: "EQUITY",
				},
			},
		},
	}

	// For LIMIT orders, add the price
	if schwabOrder.OrderType == "LIMIT" {
		schwabOrder.Price = order.Price
	}

	payload, err := json.Marshal(schwabOrder)
	if err != nil {
		return broker.OrderResult{}, fmt.Errorf("schwab PlaceOrder marshal: %w", err)
	}

	path := fmt.Sprintf("/accounts/%s/orders", b.accountHash)
	resp, err := b.client.PostTrader(ctx, path, bytes.NewReader(payload))
	if err != nil {
		return broker.OrderResult{}, fmt.Errorf("schwab PlaceOrder: %w", err)
	}
	defer resp.Body.Close()

	// Schwab returns 201 Created with the order ID in the Location header
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return broker.OrderResult{}, fmt.Errorf("schwab PlaceOrder: unexpected status %d", resp.StatusCode)
	}

	orderID := extractOrderIDFromLocation(resp.Header.Get("Location"))
	return broker.OrderResult{OrderID: orderID}, nil
}

// PlaceGTT is not supported for US markets. Use PlaceOrder with GTC duration instead.
func (b *SchwabBroker) PlaceGTT(_ broker.Order) (broker.OrderResult, error) {
	return broker.OrderResult{}, fmt.Errorf("GTT orders are not supported for US markets — use a regular GTC (Good Till Cancel) order instead")
}

// mapOrderType converts the internal order type to Schwab's enum.
func mapOrderType(orderType string) string {
	switch strings.ToUpper(orderType) {
	case "LIMIT":
		return "LIMIT"
	case "MARKET":
		return "MARKET"
	default:
		return "LIMIT" // default to LIMIT for safety
	}
}

// mapInstruction converts "BUY"/"SELL" to Schwab instruction.
func mapInstruction(txnType string) string {
	switch strings.ToUpper(txnType) {
	case "BUY":
		return "BUY"
	case "SELL":
		return "SELL"
	default:
		return txnType
	}
}

// extractOrderIDFromLocation parses the order ID from the Location header.
// Format: /trader/v1/accounts/{hash}/orders/{orderId}
func extractOrderIDFromLocation(location string) string {
	if location == "" {
		return ""
	}
	parts := strings.Split(location, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

// Compile-time interface compliance check.
var _ broker.Broker = (*SchwabBroker)(nil)
