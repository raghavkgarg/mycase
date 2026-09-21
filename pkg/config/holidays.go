package config

import (
	"encoding/json"
	"os"
)

// Holidays holds exchange trading holidays keyed by exchange name ("NSE",
// "NYSE"), each a list of dates formatted "2006-01-02" in that exchange's local
// timezone. It is loaded from config/holidays.json.
//
// This loader deliberately returns raw string lists rather than assembled
// marketcal.Clock values: pkg/config is a designated zero-import leaf and must
// not import marketcal. A higher layer that legally imports both (the scheduler,
// daemon, or autopilot wiring) attaches these dates to a clock via
// marketcal.NYSE.WithHolidays(holidays.For("NYSE")...).
type Holidays struct {
	// Exchanges maps an exchange name to its holiday dates ("2006-01-02").
	Exchanges map[string][]string `json:"exchanges"`
}

// For returns the holiday dates for an exchange, or nil if none are configured.
func (h Holidays) For(exchange string) []string {
	return h.Exchanges[exchange]
}

// LoadHolidays reads config/holidays.json and returns the configured exchange
// holidays. A missing or malformed file yields an empty set (no holidays), which
// degrades gracefully to weekend-only trading-day logic — never an error that
// would block scheduling.
func LoadHolidays(filename string) Holidays {
	h := Holidays{Exchanges: map[string][]string{}}
	file, err := os.Open(filename)
	if err != nil {
		return h
	}
	defer file.Close()
	_ = json.NewDecoder(file).Decode(&h)
	if h.Exchanges == nil {
		h.Exchanges = map[string][]string{}
	}
	return h
}
