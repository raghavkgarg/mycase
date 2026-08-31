// Package types holds the broker-agnostic data-transfer objects (DTOs) shared
// across the codebase: Holding, Order, OrderResult, and MarketConfig.
//
// It is a leaf package with zero internal imports. Extracting these types out of
// pkg/broker (which also wires config + costs behind the Broker interface) lets
// type-only consumers — tax, printer, optimizer, attribution — depend on just
// the DTOs without transitively dragging in config/costs (R16 problem P3).
//
// pkg/broker re-exports these via type aliases (broker.Holding = types.Holding,
// etc.) so existing call sites and the Broker interface are unchanged.
package types

// Holding is a single stock holding across all settlement buckets.
type Holding struct {
	TradingSymbol string
	Exchange      string
	Quantity      int
	T1Quantity    int
	T2Quantity    int
	AveragePrice  float64
	LastPrice     float64
	PnL           float64
	PnLPct        float64
}

// Order is a single equity delivery order.
type Order struct {
	TradingSymbol   string
	Exchange        string
	TransactionType string // "BUY" or "SELL"
	Quantity        int
	OrderType       string  // "LIMIT", "MARKET"
	Product         string  // "CNC"
	Price           float64 // limit price; for GTT orders this is the GTT limit price
	Ltp             float64 // raw last traded price
	TriggerPrice    float64 // for GTT orders only; zero for regular/AMO
}

// OrderResult holds identifiers returned after placing an order.
type OrderResult struct {
	OrderID   string // non-empty for regular/AMO orders
	TriggerID int    // non-zero for GTT orders
}

// MarketConfig provides market-specific defaults derived from config/defaults.json.
// The values are populated by broker.LoadMarketConfig / broker.MarketConfigForName;
// the type lives here so packages that only need to read the config need not import
// the heavier broker package.
type MarketConfig struct {
	Benchmark string // "^GSPC" for US, "^NSEI" for India
	Exchange  string // "US" or "NSE"
	Currency  string // "$" or "₹"
	CloseHour int    // 16 (US ET) or 15 (India IST)
	CloseMin  int    // 0 (US) or 30 (India)
	Timezone  string // "America/New_York" or "Asia/Kolkata"
	Market    string // "us" or "india"
}
