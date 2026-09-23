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

// Holidays returns the DB-configured holidays for an exchange. On any error (no
// handle, schema failure, query failure) it logs and returns nil so the clock
// degrades to weekend-only rather than propagating a fatal error.
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
	return dates
}

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
