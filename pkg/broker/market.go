package broker

import (
	"strings"

	"github.com/raghavkgarg/mycase/pkg/broker/types"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/marketcal"
)

// MarketConfig is the market-defaults DTO; it lives in pkg/broker/types (a
// zero-import leaf, R16 P3). This alias keeps broker.MarketConfig call sites
// unchanged. The Load/For-name constructors below populate it from config.
type MarketConfig = types.MarketConfig

// LoadMarketConfig returns market-specific configuration based on config/defaults.json.
func LoadMarketConfig() MarketConfig {
	defaults := config.LoadUserDefaults(config.Path("defaults.json"))
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

// marketExchangeName maps a MarketConfig.Market to the exchange key used in
// config/holidays.json and the marketcal clock selection.
func marketExchangeName(market string) string {
	if strings.EqualFold(market, "us") {
		return "NYSE"
	}
	return "NSE"
}

// TradingClock returns the holiday-aware settlement clock for the active market
// (from config/defaults.json). It selects the NYSE clock for the US market and
// NSE otherwise, then attaches exchange holidays loaded from
// config/holidays.json. This is the single authority every scheduler/daemon path
// should consult for "is the market open today?" (clock.IsTradingDay) and EOD
// settlement timing — replacing the daemon's every-calendar-day loop and the
// autopilot's live-price probe.
//
// broker (L1) is the natural home: it already owns MarketConfig + config access,
// and both the daemon (L4) and autopilot (L5) import it, so the holiday-clock
// assembly lives in one place rather than being duplicated per consumer.
func TradingClock() marketcal.Clock {
	return TradingClockForMarket(LoadMarketConfig().Market)
}

// TradingClockForMarket is TradingClock for an explicit market name ("us" /
// "india"), so callers that already know the market avoid re-reading defaults.
func TradingClockForMarket(market string) marketcal.Clock {
	exchange := marketExchangeName(market)
	base := marketcal.NSE
	if exchange == "NYSE" {
		base = marketcal.NYSE
	}
	holidays := config.LoadHolidays(config.Path("holidays.json"))
	return base.WithHolidays(holidays.For(exchange)...)
}

// BrokerName returns the configured broker name from defaults.
func BrokerName() string {
	defaults := config.LoadUserDefaults(config.Path("defaults.json"))
	if defaults.Broker == "" {
		return "zerodha"
	}
	return defaults.Broker
}
