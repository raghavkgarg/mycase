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
	"strings"
	"time"
)

// Clock describes a market's daily EOD settlement rule: the local timezone the
// market settles in, and the local hour (0–23) at which a trading day's EOD is
// considered settled. A trading day is "settled" once local time reaches
// CutoffHour:00 on that day.
type Clock struct {
	Loc        *time.Location
	CutoffHour int
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

// NSE is the India (NSE) settlement clock: Asia/Kolkata, 21:00 IST cutoff.
// This preserves the exact behavior of the legacy marketdata IST helpers.
var NSE = Clock{Loc: mustLoad("Asia/Kolkata", 5*3600+30*60), CutoffHour: 21}

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

// LastSettledEOD returns the timestamp of the most recent completed EOD
// settlement cutoff at or before t.
//
// The rule (expressed once, weekend-aware): interpret t in the market's
// timezone; if it is before today's cutoff hour, today has not settled yet so
// step back a day; then walk back over any weekend days (no settlement on
// Saturday/Sunday) to the most recent weekday. The result is that weekday's
// cutoff timestamp.
func (c Clock) LastSettledEOD(t time.Time) time.Time {
	local := t.In(c.Loc)

	// If we haven't reached today's cutoff yet, today isn't settled.
	if local.Hour() < c.CutoffHour {
		local = local.AddDate(0, 0, -1)
	}
	// Walk back over weekend days (markets don't settle Sat/Sun).
	for local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
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
// skipping weekends. If t is before today's cutoff on a weekday, that is today's
// cutoff; otherwise it is the next trading day's cutoff.
func (c Clock) NextEODAvailable(t time.Time) time.Time {
	local := t.In(c.Loc)

	// Candidate is today's cutoff if we're still before it on a weekday;
	// otherwise advance to the next calendar day and search forward.
	if local.Hour() >= c.CutoffHour {
		local = local.AddDate(0, 0, 1)
	}
	for local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
		local = local.AddDate(0, 0, 1)
	}
	return c.atCutoff(local)
}
