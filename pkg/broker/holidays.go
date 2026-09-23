package broker

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"log/slog"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/config"
)

// HolidayProvider yields the trading-holiday dates for an exchange, formatted
// "2006-01-02" in that exchange's local timezone. It is the pluggable source of
// the holiday set that broker.TradingClock attaches to the marketcal.Clock.
//
// Defined here (pkg/broker, L1) because broker is the consumer: it owns the
// clock assembly (TradingClock/TradingClockForMarket). "Interfaces are defined by
// their consumer" (see .kiro/steering/architecture.md) — config (a zero-import
// leaf) and marketcal (the pure floor) must not grow this abstraction.
type HolidayProvider interface {
	// Holidays returns the holiday dates for an exchange ("NYSE", "NSE"), or an
	// empty slice when none are configured. It must never error the caller into a
	// broken clock — an unavailable source degrades to weekend-only trading-day
	// logic, matching config.LoadHolidays' philosophy.
	Holidays(exchange string) []string
}

// FileHolidayProvider reads holidays from config/holidays.json (the default). It
// wraps config.LoadHolidays, so a missing/malformed file degrades to an empty
// holiday set (weekend-only trading days) rather than an error.
type FileHolidayProvider struct {
	// Path is the holidays.json path. Empty → config.Path("holidays.json").
	Path string
}

// Holidays returns the file-configured holidays for an exchange.
func (p FileHolidayProvider) Holidays(exchange string) []string {
	path := p.Path
	if path == "" {
		path = config.Path("holidays.json")
	}
	return config.LoadHolidays(path).For(exchange)
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
// exchange. It is the seeding path for the DB provider — e.g. a one-time import
// of config/holidays.json into the table. A nil provider/handle is a no-op.
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

// Holiday-source selection constants.
const (
	holidaySourceFile = "file"
	holidaySourceDB   = "db"
	holidaySourceEnv  = "MYCASE_HOLIDAY_SOURCE"
)

// selectHolidayProvider chooses the holiday source using the precedence
// flag > env > config > default (default "file"), mirroring rawstore.ResolveRetention.
//
// The "db" source is only honored when the cache singleton is open; when it is
// not (e.g. a lightweight command that never opened the DB), it falls back to the
// file provider so a clock is always assembled. This keeps the graceful-degrade
// contract: an unavailable source never yields a broken (order-blocking) clock.
//
// override is the resolved --holiday-source flag value ("" when unset).
func selectHolidayProvider(override string) HolidayProvider {
	source := holidaySourceFile // default
	if cfgSrc := config.LoadUserDefaults(config.Path("defaults.json")).HolidaySource; cfgSrc != "" {
		source = strings.ToLower(strings.TrimSpace(cfgSrc)) // config
	}
	if env := strings.TrimSpace(os.Getenv(holidaySourceEnv)); env != "" {
		source = strings.ToLower(env) // env
	}
	if o := strings.TrimSpace(override); o != "" {
		source = strings.ToLower(o) // flag / explicit override
	}

	if source == holidaySourceDB {
		if c := cache.GetDB(); c != nil {
			return NewDBHolidayProvider(c.Conn())
		}
		slog.Warn("holidays.db_source_unavailable", "fallback", "file",
			"note", "holiday_source=db but cache DB is not open")
	}
	return FileHolidayProvider{}
}
