// Package marketcal is a pure, zero-import market-calendar leaf: it computes
// end-of-day (EOD) settlement timing for a market given only a timezone and a
// daily settlement-cutoff hour.
//
// It is the single source of truth for "when did the last EOD settle?",
// "is this cached data fresh?", "what trading date does this timestamp map to?"
// and "when will the next EOD file be available?". Both live markets are
// supported:
//
//   - NSE  (India legacy) — Asia/Kolkata, 21:00 IST cutoff.
//   - NYSE (US)           — America/New_York, 16:00 ET regular close.
//
// Design notes:
//   - This package imports only the standard library (time, strings). It sits
//     at the very bottom of the package layering (below the L0 leaves) so that
//     marketdata, cache, and selectiontracker can all consume it downward
//     instead of each duplicating the settlement math. See
//     .kiro/steering/architecture.md and devtools/internal/layers/layers.go.
//   - The calculation is calendar-only: it handles weekends but NOT exchange
//     holidays. Holiday-calendar awareness (NYSE/NSE holiday lists) is a
//     deliberate future extension; today a holiday is treated as a normal
//     trading day. This matches the pre-existing behavior it replaces.
//   - Times are resolved via time.LoadLocation (DST-aware) with a fixed-offset
//     fallback if the tz database is unavailable. India observes no DST so its
//     behavior is identical to the previous FixedZone("IST", 5h30m) code; the
//     US path is DST-correct via America/New_York.
package marketcal

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

// Clock describes a market's daily EOD settlement rule: the local timezone the
// market settles in, and the local hour (0–23) at which a trading day's EOD is
// considered settled. A trading day is "settled" once local time reaches
// CutoffHour:00 on that day.
type Clock struct {
	Loc        *time.Location
	CutoffHour int
	IsHoliday  func(t time.Time) bool
}

const defaultNSEHolidaysPath = "config/nse_holidays.json"

var (
	nseHolidaysMu sync.RWMutex
	nseHolidays   = make(map[string]string)
)

func init() {
	// Dynamically load from config/nse_holidays.json, checking root and parent paths (for tests).
	candidates := []string{
		defaultNSEHolidaysPath,
		"../../" + defaultNSEHolidaysPath,
		"../" + defaultNSEHolidaysPath,
	}
	for _, path := range candidates {
		if err := LoadNSEHolidaysFromFile(path); err == nil {
			break
		}
	}
}

// LoadNSEHolidaysFromFile loads holidays from a JSON file path and merges them into the active holiday set.
func LoadNSEHolidaysFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var parsed map[string]string
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	nseHolidaysMu.Lock()
	defer nseHolidaysMu.Unlock()
	for k, v := range parsed {
		nseHolidays[k] = v
	}
	return nil
}

// SetNSEHolidays replaces or sets the active NSE trading holidays map.
func SetNSEHolidays(holidays map[string]string) {
	nseHolidaysMu.Lock()
	defer nseHolidaysMu.Unlock()
	nseHolidays = make(map[string]string, len(holidays))
	for k, v := range holidays {
		nseHolidays[k] = v
	}
}

// IsNSEHoliday reports whether the given date (in IST) is an NSE trading holiday.
func IsNSEHoliday(t time.Time) bool {
	ist := mustLoad("Asia/Kolkata", 5*3600+30*60)
	dateKey := t.In(ist).Format("2006-01-02")
	nseHolidaysMu.RLock()
	defer nseHolidaysMu.RUnlock()
	_, ok := nseHolidays[dateKey]
	return ok
}

func mustLoad(name string, fallbackOffsetSec int) *time.Location {
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	// Fallback keeps the package usable even if the tz database is missing.
	// Abbreviation is derived from the IANA name's last segment for readability.
	abbrev := name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		abbrev = name[i+1:]
	}
	return time.FixedZone(abbrev, fallbackOffsetSec)
}

// NSE is the India (NSE) settlement clock: Asia/Kolkata, 21:00 IST cutoff, holiday-aware.
var NSE = Clock{
	Loc:        mustLoad("Asia/Kolkata", 5*3600+30*60),
	CutoffHour: 21,
	IsHoliday:  IsNSEHoliday,
}

// NYSE is the US settlement clock: America/New_York, 16:00 ET regular close.
var NYSE = Clock{Loc: mustLoad("America/New_York", -5*3600), CutoffHour: 16}

// ClockForTicker returns the settlement clock for a ticker based on its market
// prefix. US:/NYSE:/NASDAQ: → NYSE; everything else (NSE:, unprefixed) → NSE.
// The classification is intentionally a plain prefix check so this leaf needs
// no dependency on the broker/datafetcher packages.
func ClockForTicker(ticker string) Clock {
	if strings.HasPrefix(ticker, "US:") ||
		strings.HasPrefix(ticker, "NYSE:") ||
		strings.HasPrefix(ticker, "NASDAQ:") {
		return NYSE
	}
	return NSE
}

// atCutoff builds a timestamp at CutoffHour:00:00 on the calendar date of d,
// in the clock's location.
func (c Clock) atCutoff(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), c.CutoffHour, 0, 0, 0, c.Loc)
}

func (c Clock) isTradingDay(d time.Time) bool {
	if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		return false
	}
	if c.IsHoliday != nil && c.IsHoliday(d) {
		return false
	}
	return true
}

// LastSettledEOD returns the timestamp of the most recent completed EOD
// settlement cutoff at or before t.
//
// The rule (expressed once, weekend- and holiday-aware): interpret t in the market's
// timezone; if it is before today's cutoff hour, today has not settled yet so
// step back a day; then walk back over any non-trading days (weekends and holidays)
// to the most recent trading day. The result is that trading day's cutoff timestamp.
func (c Clock) LastSettledEOD(t time.Time) time.Time {
	local := t.In(c.Loc)

	// If we haven't reached today's cutoff yet, today isn't settled.
	if local.Hour() < c.CutoffHour {
		local = local.AddDate(0, 0, -1)
	}
	// Walk back over non-trading days (weekends and exchange holidays).
	for !c.isTradingDay(local) {
		local = local.AddDate(0, 0, -1)
	}
	return c.atCutoff(local)
}

// IsFreshEOD reports whether data fetched at fetchedAt already includes the most
// recent settled EOD relative to now — i.e. fetchedAt is at or after the last
// settlement cutoff.
func (c Clock) IsFreshEOD(fetchedAt, now time.Time) bool {
	return !fetchedAt.Before(c.LastSettledEOD(now))
}

// SettlementDate returns the trading date (midnight in the market's timezone) of
// the most recent settled EOD at or before t. This is the "as-of" trading date.
func (c Clock) SettlementDate(t time.Time) time.Time {
	cutoff := c.LastSettledEOD(t)
	return time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, c.Loc)
}

// NextEODAvailable returns the timestamp at which the next EOD file will become
// available (the next settlement cutoff strictly after the current one),
// skipping weekends and holidays. If t is before today's cutoff on an active
// trading day, that is today's cutoff; otherwise it searches forward.
func (c Clock) NextEODAvailable(t time.Time) time.Time {
	local := t.In(c.Loc)

	// Candidate is today's cutoff if we're still before it on an active trading day;
	// otherwise advance to the next calendar day and search forward.
	if local.Hour() >= c.CutoffHour || !c.isTradingDay(local) {
		local = local.AddDate(0, 0, 1)
	}
	for !c.isTradingDay(local) {
		local = local.AddDate(0, 0, 1)
	}
	return c.atCutoff(local)
}
