package themereturn

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
)

// DBTrade models a raw fill from portfolio.db.
type DBTrade struct {
	TradeID            string
	AccountID          string
	Symbol             string
	Exchange           string
	TradeType          string // "BUY", "SELL"
	Quantity           int
	Price              float64
	TradeDate          time.Time
	OrderExecutionTime time.Time
	TotalCharges       float64
}

// DBClosedLot models a realized FIFO lot match.
type DBClosedLot struct {
	MatchID      string
	AccountID    string
	Symbol       string
	BuyDate      time.Time
	SellDate     time.Time
	Quantity     int
	BuyPrice     float64
	SellPrice    float64
	RealizedGain float64
}

// DBDividend models a cash dividend credit.
type DBDividend struct {
	DividendID     string
	AccountID      string
	Symbol         string
	RecordDate     time.Time
	AmountPerShare float64
	TotalAmount    float64
}

// DBBenchmarkQuote models a dated closing price for index benchmarking.
type DBBenchmarkQuote struct {
	BenchmarkName string
	TradeDate     time.Time
	ClosePrice    float64
}

// DB provides read-only access to portfolio.db.
type DB struct {
	db     *sql.DB
	dbPath string
}

// ResolveDBPath determines the active location of portfolio.db.
func ResolveDBPath(explicitPath string) (string, error) {
	candidates := []string{
		explicitPath,
		os.Getenv("PORTFOLIO_DB"),
		"../myportfolio/data/portfolio.db",
		"/Users/raghavgarg/Projects/myGo/myportfolio/data/portfolio.db",
		"data/portfolio.db",
	}

	for _, p := range candidates {
		if strings.TrimSpace(p) == "" {
			continue
		}
		clean := filepath.Clean(p)
		if info, err := os.Stat(clean); err == nil && !info.IsDir() {
			return clean, nil
		}
	}

	return "", fmt.Errorf("portfolio.db not found; please specify --portfolio-db or set $PORTFOLIO_DB")
}

// OpenDB opens a connection to portfolio.db in read-only mode.
func OpenDB(explicitPath string) (*DB, error) {
	path, err := ResolveDBPath(explicitPath)
	if err != nil {
		return nil, err
	}

	connStr := path + "?access_mode=read_only"
	db, err := sql.Open("duckdb", connStr)
	if err != nil {
		return nil, fmt.Errorf("opening portfolio.db read-only: %w", err)
	}

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)

	return &DB{
		db:     db,
		dbPath: path,
	}, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// Path returns the resolved database path.
func (d *DB) Path() string {
	return d.dbPath
}

// QueryTrades retrieves all execution fills for given symbols and account.
func (d *DB) QueryTrades(accountID string, symbols []string) ([]*DBTrade, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(symbols))
	args := make([]any, 0, len(symbols)+1)
	if accountID != "" {
		args = append(args, accountID)
	}
	for i, s := range symbols {
		placeholders[i] = "?"
		args = append(args, s)
	}

	query := `
		SELECT 
			trade_id, account_id, symbol, exchange, trade_type, quantity, price, 
			trade_date, order_execution_time, total_charges
		FROM trades
		WHERE `
	if accountID != "" {
		query += "account_id = ? AND "
	}
	query += fmt.Sprintf("symbol IN (%s) ORDER BY order_execution_time ASC", strings.Join(placeholders, ","))

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying trades: %w", err)
	}
	defer rows.Close()

	var trades []*DBTrade
	for rows.Next() {
		var t DBTrade
		if err := rows.Scan(
			&t.TradeID, &t.AccountID, &t.Symbol, &t.Exchange, &t.TradeType,
			&t.Quantity, &t.Price, &t.TradeDate, &t.OrderExecutionTime, &t.TotalCharges,
		); err != nil {
			return nil, fmt.Errorf("scanning trade: %w", err)
		}
		trades = append(trades, &t)
	}

	return trades, rows.Err()
}

// QueryClosedLots retrieves realized FIFO matches for given symbols.
func (d *DB) QueryClosedLots(accountID string, symbols []string) ([]*DBClosedLot, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(symbols))
	args := make([]any, 0, len(symbols)+1)
	if accountID != "" {
		args = append(args, accountID)
	}
	for i, s := range symbols {
		placeholders[i] = "?"
		args = append(args, s)
	}

	query := `
		SELECT 
			match_id, account_id, symbol, buy_date, sell_date, quantity, 
			buy_price, sell_price, realized_gain
		FROM closed_lots
		WHERE `
	if accountID != "" {
		query += "account_id = ? AND "
	}
	query += fmt.Sprintf("symbol IN (%s) ORDER BY sell_date ASC", strings.Join(placeholders, ","))

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying closed_lots: %w", err)
	}
	defer rows.Close()

	var lots []*DBClosedLot
	for rows.Next() {
		var cl DBClosedLot
		if err := rows.Scan(
			&cl.MatchID, &cl.AccountID, &cl.Symbol, &cl.BuyDate, &cl.SellDate,
			&cl.Quantity, &cl.BuyPrice, &cl.SellPrice, &cl.RealizedGain,
		); err != nil {
			return nil, fmt.Errorf("scanning closed lot: %w", err)
		}
		lots = append(lots, &cl)
	}

	return lots, rows.Err()
}

// QueryDividends retrieves cash dividend credits for given symbols.
func (d *DB) QueryDividends(accountID string, symbols []string) ([]*DBDividend, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(symbols))
	args := make([]any, 0, len(symbols)+1)
	if accountID != "" {
		args = append(args, accountID)
	}
	for i, s := range symbols {
		placeholders[i] = "?"
		args = append(args, s)
	}

	query := `
		SELECT 
			dividend_id, account_id, symbol, record_date, amount_per_share, total_amount
		FROM dividends
		WHERE `
	if accountID != "" {
		query += "account_id = ? AND "
	}
	query += fmt.Sprintf("symbol IN (%s) ORDER BY record_date ASC", strings.Join(placeholders, ","))

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying dividends: %w", err)
	}
	defer rows.Close()

	var divs []*DBDividend
	for rows.Next() {
		var dv DBDividend
		if err := rows.Scan(
			&dv.DividendID, &dv.AccountID, &dv.Symbol, &dv.RecordDate,
			&dv.AmountPerShare, &dv.TotalAmount,
		); err != nil {
			return nil, fmt.Errorf("scanning dividend: %w", err)
		}
		divs = append(divs, &dv)
	}

	return divs, rows.Err()
}

// QueryBenchmarkQuotes retrieves dated quotes for index comparison.
func (d *DB) QueryBenchmarkQuotes(benchmarkName string, fromDate, toDate time.Time) ([]*DBBenchmarkQuote, error) {
	query := `
		SELECT benchmark_name, trade_date, close_price
		FROM benchmark_quotes
		WHERE benchmark_name = ? AND trade_date >= ? AND trade_date <= ?
		ORDER BY trade_date ASC
	`
	rows, err := d.db.Query(query, benchmarkName, fromDate, toDate)
	if err != nil {
		return nil, fmt.Errorf("querying benchmark_quotes: %w", err)
	}
	defer rows.Close()

	var quotes []*DBBenchmarkQuote
	for rows.Next() {
		var q DBBenchmarkQuote
		if err := rows.Scan(&q.BenchmarkName, &q.TradeDate, &q.ClosePrice); err != nil {
			return nil, fmt.Errorf("scanning benchmark quote: %w", err)
		}
		quotes = append(quotes, &q)
	}

	return quotes, rows.Err()
}
