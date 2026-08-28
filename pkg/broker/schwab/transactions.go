package schwab

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/tax"
)

// --- Transaction Types ---
//
// Schwab's GET /trader/v1/accounts/{hash}/transactions returns an array of
// transaction records. For equity trades (type == "TRADE"), the trade legs are
// in transferItems: each item has an instrument, an amount (signed quantity),
// a price, and a cost. Fees appear as separate transferItems without an
// instrument, or as records with feeType set.

// SchwabTransaction is one record from the transactions endpoint.
type SchwabTransaction struct {
	ActivityID    int64                `json:"activityId"`
	Time          string               `json:"time"` // ISO-8601, e.g. "2026-03-14T14:30:00+0000"
	Type          string               `json:"type"` // "TRADE", "DIVIDEND_OR_INTEREST", ...
	Status        string               `json:"status"`
	NetAmount     float64              `json:"netAmount"`
	TransferItems []SchwabTransferItem `json:"transferItems"`
}

// SchwabTransferItem is one leg of a transaction.
type SchwabTransferItem struct {
	Instrument SchwabTxnInstrument `json:"instrument"`
	Amount     float64             `json:"amount"`  // signed share quantity (+ buy, - sell)
	Price      float64             `json:"price"`   // per-share price
	Cost       float64             `json:"cost"`    // total cost for this leg
	FeeType    string              `json:"feeType"` // set for fee legs (e.g. "COMMISSION", "SEC_FEE")
}

// SchwabTxnInstrument identifies the security in a transfer item.
type SchwabTxnInstrument struct {
	AssetType string `json:"assetType"` // "EQUITY", "CURRENCY", ...
	Symbol    string `json:"symbol"`
	CUSIP     string `json:"cusip"`
}

// FetchTransactions retrieves TRADE transactions for the account between
// `from` and `to`. Schwab limits the window to ~1 year per request, so callers
// spanning longer periods should chunk the range.
//
// Per the API rules: this fetches once and returns raw records; callers should
// cache the normalized result and avoid repeated calls.
func (c *Client) FetchTransactions(ctx context.Context, accountHash string, from, to time.Time) ([]SchwabTransaction, error) {
	params := url.Values{
		"startDate": {from.UTC().Format("2006-01-02T15:04:05.000Z")},
		"endDate":   {to.UTC().Format("2006-01-02T15:04:05.000Z")},
		"types":     {"TRADE"},
	}

	path := fmt.Sprintf("/accounts/%s/transactions?%s", accountHash, params.Encode())
	resp, err := c.GetTrader(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("schwab transactions: %w", err)
	}

	var txns []SchwabTransaction
	if err := DecodeJSON(resp, &txns); err != nil {
		return nil, err
	}
	return txns, nil
}

// parseSchwabTime parses Schwab's ISO-8601 timestamps, tolerating both the
// "+0000" and "Z" zone formats.
func parseSchwabTime(s string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02T15:04:05-0700",
		time.RFC3339,
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05.000Z07:00",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time format: %q", s)
}

// normalizeSymbol converts a raw Schwab equity symbol into the "US:" prefixed
// form used across the codebase.
func normalizeSymbol(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if IsUSTicker(raw) {
		return raw
	}
	return "US:" + raw
}

// NormalizeTransactions flattens raw Schwab TRADE records into the
// broker-agnostic tax.Transaction form used by the FIFO engine. Each record's
// equity transfer item becomes one BUY or SELL (sign of Amount decides), with
// fees summed from the record's fee legs. Non-equity or malformed records are
// skipped.
func NormalizeTransactions(raw []SchwabTransaction) []tax.Transaction {
	var out []tax.Transaction

	for _, r := range raw {
		if !strings.EqualFold(r.Type, "TRADE") {
			continue
		}

		tradedAt, err := parseSchwabTime(r.Time)
		if err != nil {
			continue
		}

		// Separate the equity leg from fee legs.
		var equityItem *SchwabTransferItem
		var fees float64
		for i := range r.TransferItems {
			item := &r.TransferItems[i]
			if item.FeeType != "" || strings.EqualFold(item.Instrument.AssetType, "CURRENCY") {
				// Fee legs carry a negative cost; accumulate the magnitude.
				fees += absFloat(item.Cost)
				continue
			}
			if strings.EqualFold(item.Instrument.AssetType, "EQUITY") && item.Amount != 0 {
				equityItem = item
			}
		}

		if equityItem == nil {
			continue
		}

		symbol := normalizeSymbol(equityItem.Instrument.Symbol)
		if symbol == "" {
			continue
		}

		qty := equityItem.Amount
		txnType := tax.TxnBuy
		if qty < 0 {
			txnType = tax.TxnSell
			qty = -qty
		}

		out = append(out, tax.Transaction{
			ID:       fmt.Sprintf("schwab_%d", r.ActivityID),
			Ticker:   symbol,
			Type:     txnType,
			Quantity: qty,
			Price:    equityItem.Price,
			Fees:     fees,
			TradedAt: tradedAt,
		})
	}

	return out
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
