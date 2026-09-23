package broker

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"log/slog"

	"github.com/raghavkgarg/mycase/pkg/cache"
)

// HolidayProvider yields the trading-holiday dates for an exchange, formatted
// "2006-01-02" in that exchange's local timezone. It is the pluggable source of
// the holiday set that broker.TradingClock attaches to the marketcal.Clock.
//
// Defined here (pkg/broker, L1) because broker is the consumer: it owns the
// clock assembly (TradingClock/TradingClockForMarket). "Interfaces are defined by
// their consumer" (see .kiro/steering/architecture.md) — marketcal (the pure
// floor) must not grow this abstraction.
type HolidayProvider interface {
	// Holidays returns the holiday dates for an exchange ("NYSE", "NSE"), or an
	// empty slice when none are configured. It must never error the caller into a
	// broken clock — an unavailable source degrades to weekend-only trading-day
	// logic.
	Holidays(exchange string) []string
}

// holidaysDDL creates the per-exchange holiday table this domain owns. Following
// the domains-own-their-tables rule (like attribution.Store / tax.Store), the
// table lives here and is created via cache.Conn()'s *sql.DB, never by pkg/cache.
const holidaysDDL = `
CREATE TABLE IF NOT EXISTS holidays (
    exchange VARCHAR NOT NULL,
    date     VARCHAR NOT NULL,
    PRIMARY KEY (exchange, date)
);`

// DBHolidayProvider reads holidays from the `holidays` table in the DuckDB cache
// (data/mycase.db). It owns its table (create + query) via a *sql.DB handle from
// cache.Conn(), keeping the dependency direction domain → cache.
//
// A nil *sql.DB (persistence disabled — the cache singleton is not open) makes
// Holidays return nil, so the selection logic can fall back to the file provider
// without breaking scheduling. This mirrors attribution/tax's nil-Store no-op.
type DBHolidayProvider struct {
	db      *sql.DB
	ensured bool
}

// NewDBHolidayProvider builds a DB-backed provider over the given handle. A nil
// db yields a provider whose Holidays always returns nil (persistence disabled).
func NewDBHolidayProvider(db *sql.DB) *DBHolidayProvider {
	if db == nil {
		return &DBHolidayProvider{}
	}
	return &DBHolidayProvider{db: db}
}

// ensureSchema lazily creates the holidays table on first use.
func (p *DBHolidayProvider) ensureSchema(ctx context.Context) error {
	if p.ensured {
		return nil
	}
	if _, err := p.db.ExecContext(ctx, holidaysDDL); err != nil {
		return fmt.Errorf("create holidays table: %w", err)
	}
	p.ensured = true
	return nil
}

// knownExchanges are the exchanges the system actively schedules against. A
// zero-row result for one of these is almost certainly an unseeded table (a real
// exchange always has holidays in a year), so Holidays warns actionably rather
// than silently degrading — closing the "forgot to seed" silent-failure hole.
var knownExchanges = map[string]bool{"NYSE": true, "NSE": true}

// Holidays returns the DB-configured holidays for an exchange. On any error (no
// handle, schema failure, query failure) it logs and returns nil so the clock
// degrades to weekend-only rather than propagating a fatal error.
//
// A zero-row result for a *known* exchange (NYSE/NSE) is treated as a likely
// unseeded table and logged at WARN with a seed hint — an empty holiday calendar
// is never legitimate for a real exchange, so it must be observable rather than
// silent. It still returns an empty set (weekend-only) so the clock is never
// broken; loudness here is visibility, not a hard failure.
func (p *DBHolidayProvider) Holidays(exchange string) []string {
	if p == nil || p.db == nil {
		return nil
	}
	ctx := context.Background()
	if err := p.ensureSchema(ctx); err != nil {
		slog.WarnContext(ctx, "holidays.db_schema_failed", "err", err, "fallback", "weekend_only")
		return nil
	}
	rows, err := p.db.QueryContext(ctx, `SELECT date FROM holidays WHERE exchange = ? ORDER BY date`, exchange)
	if err != nil {
		slog.WarnContext(ctx, "holidays.db_query_failed", "err", err, "exchange", exchange, "fallback", "weekend_only")
		return nil
	}
	defer rows.Close()

	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			slog.WarnContext(ctx, "holidays.db_scan_failed", "err", err, "exchange", exchange)
			return nil
		}
		if d = strings.TrimSpace(d); d != "" {
			dates = append(dates, d)
		}
	}
	if err := rows.Err(); err != nil {
		slog.WarnContext(ctx, "holidays.db_rows_failed", "err", err, "exchange", exchange)
		return nil
	}
	if len(dates) == 0 && knownExchanges[strings.ToUpper(exchange)] {
		slog.WarnContext(ctx, "holidays.empty_calendar",
			"exchange", exchange,
			"impact", "trading-day logic is WEEKEND-ONLY until seeded",
			"fix", "seed the holidays table (duckdb data/mycase.db < holiday.sql) — see docs/18-runbook.md")
	}
	return dates
}

// Count returns the number of holiday rows for an exchange (-1 on any error or
// nil handle, so callers can distinguish "0 rows" from "couldn't check"). Used by
// `mycase holidays status` and the scheduler's empty-calendar check.
func (p *DBHolidayProvider) Count(ctx context.Context, exchange string) int {
	if p == nil || p.db == nil {
		return -1
	}
	if err := p.ensureSchema(ctx); err != nil {
		return -1
	}
	var n int
	if err := p.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM holidays WHERE exchange = ?`, exchange).Scan(&n); err != nil {
		return -1
	}
	return n
}

// HolidayStat summarizes the seeded holidays for one exchange, for the read-only
// `mycase holidays status` report.
type HolidayStat struct {
	Exchange string
	Count    int
	MinDate  string
	MaxDate  string
}

// HolidayStatus returns per-exchange row counts + date range from the holidays
// table, ordered by exchange. A nil handle yields nil. It is read-only.
func (p *DBHolidayProvider) HolidayStatus(ctx context.Context) ([]HolidayStat, error) {
	if p == nil || p.db == nil {
		return nil, nil
	}
	if err := p.ensureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT exchange, COUNT(*), MIN(date), MAX(date) FROM holidays GROUP BY exchange ORDER BY exchange`)
	if err != nil {
		return nil, fmt.Errorf("querying holiday status: %w", err)
	}
	defer rows.Close()

	var stats []HolidayStat
	for rows.Next() {
		var s HolidayStat
		var minD, maxD sql.NullString
		if err := rows.Scan(&s.Exchange, &s.Count, &minD, &maxD); err != nil {
			return nil, fmt.Errorf("scanning holiday status: %w", err)
		}
		s.MinDate = minD.String
		s.MaxDate = maxD.String
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// KnownExchanges returns the exchanges the system schedules against, so callers
// (e.g. `holidays status`) can report a "NOT SEEDED" line for a known exchange
// that has no rows at all (and thus no GROUP BY row above).
func KnownExchanges() []string { return []string{"NSE", "NYSE"} }

// UpsertHolidays inserts (or ignores duplicates of) the given dates for an
// exchange — the sanctioned write primitive for landing operator-sourced holiday
// data into the table (and for round-tripping it in tests). The authoritative
// calendar comes from each exchange in whatever format it publishes (CSV,
// circular, HTML), so seeding is an operator step (direct SQL via the duckdb CLI,
// or their own tooling), documented in the runbook — the product deliberately
// ships no importer that would enshrine one intermediate format as authoritative.
// A nil provider/handle is a no-op.
func (p *DBHolidayProvider) UpsertHolidays(ctx context.Context, exchange string, dates []string) error {
	if p == nil || p.db == nil {
		return nil
	}
	if err := p.ensureSchema(ctx); err != nil {
		return err
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin holidays tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // committed on success; rollback is the no-op cleanup

	for _, d := range dates {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO holidays (exchange, date) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			exchange, d); err != nil {
			return fmt.Errorf("insert holiday %s %s: %w", exchange, d, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit holidays tx: %w", err)
	}
	return nil
}

// selectHolidayProvider returns the holiday source. Holidays live in the
// `holidays` table of the DuckDB cache (data/mycase.db) — the single source of
// truth, seeded and maintained operationally (see docs/18-runbook.md). When the
// cache singleton is not open (e.g. a lightweight command that never opened the
// DB) or the table is empty, the DB provider yields no holidays and the clock
// degrades to weekend-only, so a clock is always assembled and order-placing
// paths are never blocked by a missing calendar.
func selectHolidayProvider() HolidayProvider {
	if c := cache.GetDB(); c != nil {
		return NewDBHolidayProvider(c.Conn())
	}
	slog.Warn("holidays.db_unavailable", "fallback", "weekend_only",
		"note", "cache DB is not open; holiday calendar unavailable this invocation")
	return NewDBHolidayProvider(nil)
}

// HolidayStore returns the DB-backed holiday provider over the active cache
// connection, or nil when the cache DB is not open. Exported for the composition
// root (`mycase holidays status`) to read/report the table without re-deriving
// the cache wiring. Returns the concrete type so callers can reach Count/
// HolidayStatus/UpsertHolidays, not just the read-only interface.
func HolidayStore() *DBHolidayProvider {
	if c := cache.GetDB(); c != nil {
		return NewDBHolidayProvider(c.Conn())
	}
	return nil
}
