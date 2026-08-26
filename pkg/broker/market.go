package broker

import (
	"strings"

	"github.com/raghavkgarg/mycase/pkg/config"
)

const defaultsPath = "config/defaults.json"

// MarketConfig provides market-specific defaults derived from config/defaults.json.
type MarketConfig struct {
	Benchmark string // "^GSPC" for US, "^NSEI" for India
	Exchange  string // "US" or "NSE"
	Currency  string // "$" or "₹"
	CloseHour int    // 16 (US ET) or 15 (India IST)
	CloseMin  int    // 0 (US) or 30 (India)
	Timezone  string // "America/New_York" or "Asia/Kolkata"
	Market    string // "us" or "india"
}

// LoadMarketConfig returns market-specific configuration based on config/defaults.json.
func LoadMarketConfig() MarketConfig {
	defaults := config.LoadUserDefaults(defaultsPath)
	return MarketConfigForName(defaults.Market)
}

// MarketConfigForName returns the MarketConfig for a given market name.
func MarketConfigForName(market string) MarketConfig {
	switch strings.ToLower(market) {
	case "us":
		return MarketConfig{
			Benchmark: "^GSPC",
			Exchange:  "US",
			Currency:  "$",
			CloseHour: 16,
			CloseMin:  0,
			Timezone:  "America/New_York",
			Market:    "us",
		}
	default: // "india" or ""
		return MarketConfig{
			Benchmark: "^NSEI",
			Exchange:  "NSE",
			Currency:  "₹",
			CloseHour: 15,
			CloseMin:  30,
			Timezone:  "Asia/Kolkata",
			Market:    "india",
		}
	}
}

// ExchangeFromTicker derives the exchange from a prefixed ticker string.
// "NSE:TCS" → "NSE", "BSE:500325" → "BSE", "US:AAPL" → "US", "AAPL" → default exchange.
func ExchangeFromTicker(ticker string, defaultExchange string) string {
	if idx := strings.Index(ticker, ":"); idx > 0 {
		return ticker[:idx]
	}
	return defaultExchange
}

// SymbolFromTicker strips the exchange prefix from a ticker.
// "NSE:TCS" → "TCS", "US:AAPL" → "AAPL", "AAPL" → "AAPL".
func SymbolFromTicker(ticker string) string {
	if idx := strings.Index(ticker, ":"); idx > 0 {
		return ticker[idx+1:]
	}
	return ticker
}

// DeliveryProduct returns the order product type for the given exchange.
// India (NSE/BSE) uses "CNC" for delivery; US has no equivalent (empty string).
func DeliveryProduct(exchange string) string {
	switch strings.ToUpper(exchange) {
	case "NSE", "BSE":
		return "CNC"
	default:
		return ""
	}
}

// IsUSBroker returns true if the broker name implies US market.
func IsUSBroker(brokerName string) bool {
	return brokerName == "schwab"
}

// BrokerName returns the configured broker name from defaults.
func BrokerName() string {
	defaults := config.LoadUserDefaults(defaultsPath)
	if defaults.Broker == "" {
		return "zerodha"
	}
	return defaults.Broker
}
