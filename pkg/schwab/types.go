package schwab

// --- Price History Types ---

// CandleList is the response from GET /marketdata/v1/pricehistory.
type CandleList struct {
	Symbol  string   `json:"symbol"`
	Empty   bool     `json:"empty"`
	Candles []Candle `json:"candles"`
}

// Candle is a single OHLCV candle from Schwab price history.
type Candle struct {
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Close    float64 `json:"close"`
	Volume   float64 `json:"volume"`
	Datetime int64   `json:"datetime"` // Unix milliseconds
}

// --- Quote Types ---

// QuoteResponse is the response from GET /marketdata/v1/quotes.
// Keys are symbols, values are quote data.
type QuoteResponse map[string]QuoteData

// QuoteData holds quote information for a single symbol.
type QuoteData struct {
	AssetMainType string      `json:"assetMainType"` // "EQUITY", "ETF", etc.
	Symbol        string      `json:"symbol"`
	Quote         QuoteDetail `json:"quote"`
}

// QuoteDetail holds the nested quote fields.
type QuoteDetail struct {
	LastPrice              float64 `json:"lastPrice"`
	OpenPrice              float64 `json:"openPrice"`
	HighPrice              float64 `json:"highPrice"`
	LowPrice               float64 `json:"lowPrice"`
	ClosePrice             float64 `json:"closePrice"`
	NetChange              float64 `json:"netChange"`
	NetPercentChange       float64 `json:"netPercentChangeInDouble"`
	TotalVolume            int64   `json:"totalVolume"`
	Mark                   float64 `json:"mark"`
	FiftyTwoWeekHigh       float64 `json:"52WeekHigh"`
	FiftyTwoWeekLow        float64 `json:"52WeekLow"`
	RegularMarketLastPrice float64 `json:"regularMarketLastPrice"`
}

// --- Instrument/Fundamentals Types ---

// InstrumentResponse is the response from GET /marketdata/v1/instruments.
type InstrumentResponse struct {
	Instruments []Instrument `json:"instruments"`
}

// Instrument holds instrument details including fundamental data.
type Instrument struct {
	Symbol      string       `json:"symbol"`
	Description string       `json:"description"`
	Exchange    string       `json:"exchange"`
	AssetType   string       `json:"assetType"`
	Fundamental *Fundamental `json:"fundamental,omitempty"`
}

// Fundamental holds fundamental data from Schwab's instrument search.
type Fundamental struct {
	Symbol               string  `json:"symbol"`
	MarketCap            float64 `json:"marketCap"` // in millions
	PeRatio              float64 `json:"peRatio"`
	PegRatio             float64 `json:"pegRatio"`
	PbRatio              float64 `json:"pbRatio"`
	DivYield             float64 `json:"divYield"`
	ReturnOnEquity       float64 `json:"returnOnEquity"`
	ReturnOnAssets       float64 `json:"returnOnAssets"`
	OperatingMarginTTM   float64 `json:"operatingMarginTTM"`
	NetProfitMarginTTM   float64 `json:"netProfitMarginTTM"`
	GrossMarginTTM       float64 `json:"grossMarginTTM"`
	QuickRatio           float64 `json:"quickRatio"`
	CurrentRatio         float64 `json:"currentRatio"`
	DebtToCapital        float64 `json:"debtToCapital"`
	TotalDebtToEquity    float64 `json:"totalDebtToEquity"`
	EpsTTM               float64 `json:"epsTTM"`
	RevenueTTM           float64 `json:"revenueTTM"` // total revenue TTM
	Vol10DayAvg          float64 `json:"vol10DayAvg"`
	Vol3MonthAvg         float64 `json:"vol3MonthAvg"`
	Beta                 float64 `json:"beta"`
	SharesOutstanding    float64 `json:"sharesOutstanding"`
	BookValuePerShare    float64 `json:"bookValuePerShare"`
	FreeCashFlowPerShare float64 `json:"freeCashFlowPerShare"`
}

// --- Account/Position Types ---

// AccountResponse is the response from GET /trader/v1/accounts/{hash}?fields=positions.
type AccountResponse struct {
	SecuritiesAccount SecuritiesAccount `json:"securitiesAccount"`
}

// SecuritiesAccount holds account details.
type SecuritiesAccount struct {
	Type      string     `json:"type"`
	AccountID string     `json:"accountId"`
	Positions []Position `json:"positions"`
}

// Position holds a single position in the account.
type Position struct {
	LongQuantity  float64            `json:"longQuantity"`
	ShortQuantity float64            `json:"shortQuantity"`
	AveragePrice  float64            `json:"averagePrice"`
	MarketValue   float64            `json:"marketValue"`
	Instrument    PositionInstrument `json:"instrument"`
}

// PositionInstrument identifies the instrument in a position.
type PositionInstrument struct {
	AssetType string `json:"assetType"`
	Symbol    string `json:"symbol"`
	CUSIP     string `json:"cusip"`
}

// --- Order Types ---

// SchwabOrder is the order payload for POST /trader/v1/accounts/{hash}/orders.
type SchwabOrder struct {
	OrderType          string     `json:"orderType"`         // "LIMIT", "MARKET"
	Session            string     `json:"session"`           // "NORMAL"
	Duration           string     `json:"duration"`          // "DAY", "GOOD_TILL_CANCEL"
	Price              float64    `json:"price,omitempty"`   // required for LIMIT orders
	OrderStrategyType  string     `json:"orderStrategyType"` // "SINGLE"
	OrderLegCollection []OrderLeg `json:"orderLegCollection"`
}

// OrderLeg is a single leg in a Schwab order.
type OrderLeg struct {
	Instruction string          `json:"instruction"` // "BUY", "SELL"
	Quantity    int             `json:"quantity"`
	Instrument  OrderInstrument `json:"instrument"`
}

// OrderInstrument identifies what is being traded.
type OrderInstrument struct {
	Symbol    string `json:"symbol"`
	AssetType string `json:"assetType"` // "EQUITY"
}

// OrderResponse is returned after placing an order (empty body, order ID in Location header).
type OrderResponse struct {
	OrderID string
}
