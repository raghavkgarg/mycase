package broker

import "github.com/raghavkgarg/mycase/pkg/broker/types"

// The broker-agnostic DTOs live in pkg/broker/types (a zero-import leaf) so that
// type-only consumers need not pull in config/costs (R16 P3). These aliases keep
// existing broker.Holding / broker.Order / broker.OrderResult call sites and the
// Broker interface unchanged.
type (
	// Holding is a single stock holding across all settlement buckets.
	Holding = types.Holding
	// Order is a single equity delivery order.
	Order = types.Order
	// OrderResult holds identifiers returned after placing an order.
	OrderResult = types.OrderResult
)

// Broker abstracts broker-specific operations for market data and order placement.
type Broker interface {
	// GetHoldings returns holdings merged with today's CNC positions (T+1, T+2).
	GetHoldings() ([]Holding, error)
	// GetQuotes returns last traded price for each instrument key (e.g. "NSE:TCS").
	GetQuotes(keys []string) (map[string]float64, error)
	// PlaceOrder places a regular or AMO order. variety is "regular" or "amo".
	PlaceOrder(variety string, order Order) (OrderResult, error)
	// PlaceGTT places a Good Till Triggered order.
	PlaceGTT(order Order) (OrderResult, error)
	// IsMock reports whether this broker is operating in dry-run/mock mode.
	IsMock() bool
}
