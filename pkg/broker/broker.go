package broker

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
