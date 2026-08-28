package cache

import (
	"context"
	"database/sql"
	"time"

	"github.com/raghavkgarg/mycase/pkg/tax"
)

// InsertTransactions bulk-inserts normalized buy/sell transactions. Idempotent
// on txn_id, so re-importing the same Schwab history is safe.
func (c *Cache) InsertTransactions(ctx context.Context, txns []tax.Transaction) error {
	if len(txns) == 0 {
		return nil
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, t := range txns {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tax_transactions (txn_id, ticker, txn_type, quantity, price, fees, traded_at, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (txn_id) DO UPDATE SET
				ticker = EXCLUDED.ticker, txn_type = EXCLUDED.txn_type,
				quantity = EXCLUDED.quantity, price = EXCLUDED.price,
				fees = EXCLUDED.fees, traded_at = EXCLUDED.traded_at,
				source = EXCLUDED.source`,
			t.ID, t.Ticker, string(t.Type), t.Quantity, t.Price, t.Fees, t.TradedAt.Unix(), "schwab_txn",
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetTransactions returns all stored transactions ordered chronologically.
func (c *Cache) GetTransactions(ctx context.Context) ([]tax.Transaction, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT txn_id, ticker, txn_type, quantity, price, fees, traded_at
		FROM tax_transactions
		ORDER BY traded_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txns []tax.Transaction
	for rows.Next() {
		var t tax.Transaction
		var typ string
		var fees sql.NullFloat64
		var tradedAt int64
		if err := rows.Scan(&t.ID, &t.Ticker, &typ, &t.Quantity, &t.Price, &fees, &tradedAt); err != nil {
			return nil, err
		}
		t.Type = tax.TxnType(typ)
		if fees.Valid {
			t.Fees = fees.Float64
		}
		t.TradedAt = time.Unix(tradedAt, 0)
		txns = append(txns, t)
	}
	return txns, rows.Err()
}

// ReplaceOpenLots overwrites the tax_lots table with the given open lots. Lots
// are derived state (recomputed from transactions via FIFO), so a full replace
// keeps them consistent after each import.
func (c *Cache) ReplaceOpenLots(ctx context.Context, openLots map[string][]tax.Lot) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM tax_lots`); err != nil {
		return err
	}

	for _, lots := range openLots {
		for _, l := range lots {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO tax_lots (lot_id, ticker, quantity, cost_per_share, acquired_at, source)
				VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT (lot_id) DO UPDATE SET
					quantity = EXCLUDED.quantity, cost_per_share = EXCLUDED.cost_per_share,
					acquired_at = EXCLUDED.acquired_at, source = EXCLUDED.source`,
				l.ID, l.Ticker, l.Quantity, l.CostPerShare, l.AcquiredAt.Unix(), l.Source,
			); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// GetOpenLots returns all open lots grouped by ticker, oldest-first.
func (c *Cache) GetOpenLots(ctx context.Context) (map[string][]tax.Lot, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT lot_id, ticker, quantity, cost_per_share, acquired_at, source
		FROM tax_lots
		ORDER BY ticker, acquired_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]tax.Lot)
	for rows.Next() {
		var l tax.Lot
		var acquiredAt int64
		var source sql.NullString
		if err := rows.Scan(&l.ID, &l.Ticker, &l.Quantity, &l.CostPerShare, &acquiredAt, &source); err != nil {
			return nil, err
		}
		l.AcquiredAt = time.Unix(acquiredAt, 0)
		if source.Valid {
			l.Source = source.String
		}
		out[l.Ticker] = append(out[l.Ticker], l)
	}
	return out, rows.Err()
}

// ReplaceRealizedGains overwrites the realized_gains table. Like lots, realized
// gains are derived from the transaction history.
func (c *Cache) ReplaceRealizedGains(ctx context.Context, gains []tax.RealizedGain) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM realized_gains`); err != nil {
		return err
	}

	for _, g := range gains {
		var acquiredAt sql.NullInt64
		if !g.AcquiredAt.IsZero() {
			acquiredAt = sql.NullInt64{Int64: g.AcquiredAt.Unix(), Valid: true}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO realized_gains (txn_id, lot_id, ticker, quantity, proceeds, cost_basis, gain, acquired_at, sold_at, holding_days, long_term)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (txn_id, lot_id) DO UPDATE SET
				ticker = EXCLUDED.ticker, quantity = EXCLUDED.quantity,
				proceeds = EXCLUDED.proceeds, cost_basis = EXCLUDED.cost_basis,
				gain = EXCLUDED.gain, acquired_at = EXCLUDED.acquired_at,
				sold_at = EXCLUDED.sold_at, holding_days = EXCLUDED.holding_days,
				long_term = EXCLUDED.long_term`,
			g.TransactionID, g.LotID, g.Ticker, g.Quantity, g.Proceeds, g.CostBasis, g.Gain,
			acquiredAt, g.SoldAt.Unix(), g.HoldingDays, g.LongTerm,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetRealizedGains returns realized gains, optionally filtered to those sold
// on/after `since` (pass a zero time for all).
func (c *Cache) GetRealizedGains(ctx context.Context, since time.Time) ([]tax.RealizedGain, error) {
	query := `
		SELECT txn_id, lot_id, ticker, quantity, proceeds, cost_basis, gain, acquired_at, sold_at, holding_days, long_term
		FROM realized_gains`
	args := []any{}
	if !since.IsZero() {
		query += ` WHERE sold_at >= ?`
		args = append(args, since.Unix())
	}
	query += ` ORDER BY sold_at ASC`

	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var gains []tax.RealizedGain
	for rows.Next() {
		var g tax.RealizedGain
		var acquiredAt sql.NullInt64
		var soldAt int64
		var holdingDays sql.NullInt64
		var longTerm sql.NullBool
		if err := rows.Scan(&g.TransactionID, &g.LotID, &g.Ticker, &g.Quantity, &g.Proceeds,
			&g.CostBasis, &g.Gain, &acquiredAt, &soldAt, &holdingDays, &longTerm); err != nil {
			return nil, err
		}
		if acquiredAt.Valid {
			g.AcquiredAt = time.Unix(acquiredAt.Int64, 0)
		}
		g.SoldAt = time.Unix(soldAt, 0)
		if holdingDays.Valid {
			g.HoldingDays = int(holdingDays.Int64)
		}
		if longTerm.Valid {
			g.LongTerm = longTerm.Bool
		}
		gains = append(gains, g)
	}
	return gains, rows.Err()
}

// LatestBuyDates returns the most recent BUY date per ticker, for wash-sale
// detection.
func (c *Cache) LatestBuyDates(ctx context.Context) (map[string]time.Time, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT ticker, MAX(traded_at)
		FROM tax_transactions
		WHERE txn_type = 'BUY'
		GROUP BY ticker`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]time.Time)
	for rows.Next() {
		var ticker string
		var maxTraded int64
		if err := rows.Scan(&ticker, &maxTraded); err != nil {
			return nil, err
		}
		out[ticker] = time.Unix(maxTraded, 0)
	}
	return out, rows.Err()
}
