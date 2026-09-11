package edgar

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// factsDDL caches the raw companyfacts.json blob per CIK. Owned by edgar (not
// pkg/cache) and created lazily via cache.Conn().
const factsDDL = `
CREATE TABLE IF NOT EXISTS edgar_facts (
    cik        VARCHAR PRIMARY KEY,
    raw_json   VARCHAR NOT NULL,
    fetched_at BIGINT  NOT NULL
);`

// companyFactsPath is the data.sec.gov path template for a company's full facts.
func companyFactsPath(cik string) string {
	return "/api/xbrl/companyfacts/CIK" + cik + ".json"
}

// companyFacts is the subset of companyfacts.json we parse: us-gaap concepts,
// each carrying units → facts. We keep it permissive (RawMessage) and let the
// concept mapper extract typed values, since filers vary widely.
type companyFacts struct {
	CIK    int64  `json:"cik"`
	Entity string `json:"entityName"`
	Facts  struct {
		USGAAP map[string]conceptData `json:"us-gaap"`
	} `json:"facts"`
}

// conceptData holds one us-gaap concept's units → fact rows.
type conceptData struct {
	Label string                 `json:"label"`
	Units map[string][]factValue `json:"units"`
}

// factValue is a single reported datapoint for a concept/unit.
type factValue struct {
	Start string  `json:"start"` // period start (empty for instant/balance-sheet facts)
	End   string  `json:"end"`   // period end (YYYY-MM-DD)
	FP    string  `json:"fp"`    // fiscal period: "FY", "Q1".."Q4"
	Form  string  `json:"form"`  // "10-K", "10-Q", ...
	Filed string  `json:"filed"` // filing date (YYYY-MM-DD)
	Frame string  `json:"frame"` // e.g. "CY2024" / "CY2024Q4I" (present only on framed facts)
	Val   float64 `json:"val"`
	FY    int     `json:"fy"` // fiscal year
}

// fetchFacts returns a company's parsed facts for a CIK, serving from the
// DuckDB cache when the blob is within factsTTL, otherwise re-fetching. Returns
// (nil, false, nil) when EDGAR has no facts for the CIK (404) — a graceful skip.
func (c *Client) fetchFacts(ctx context.Context, cik string) (*companyFacts, bool, error) {
	// Try cache first.
	if raw, ok := c.cachedFacts(ctx, cik); ok {
		cf, err := parseFacts(raw)
		if err == nil {
			return cf, true, nil
		}
		// Corrupt cached blob: fall through and re-fetch.
		c.logger.WarnContext(ctx, "edgar.facts_cache_parse_failed", "cik", cik, "err", err)
	}

	body, err := c.getJSON(ctx, c.dataBase+companyFactsPath(cik))
	if errors.Is(err, errNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	cf, err := parseFacts(body)
	if err != nil {
		return nil, false, fmt.Errorf("edgar: parse facts CIK%s: %w", cik, err)
	}
	c.storeFacts(ctx, cik, body)
	c.logger.DebugContext(ctx, "edgar.facts_fetched", "cik", cik,
		"concepts", len(cf.Facts.USGAAP))
	return cf, true, nil
}

// cachedFacts returns the raw blob for a CIK if present and fresh.
func (c *Client) cachedFacts(ctx context.Context, cik string) ([]byte, bool) {
	if c.cache == nil {
		return nil, false
	}
	db := c.cache.Conn()
	if _, err := db.ExecContext(ctx, factsDDL); err != nil {
		c.logger.WarnContext(ctx, "edgar.facts_table_create_failed", "err", err)
		return nil, false
	}
	var raw string
	var fetchedAt int64
	err := db.QueryRowContext(ctx,
		`SELECT raw_json, fetched_at FROM edgar_facts WHERE cik = ?`, cik,
	).Scan(&raw, &fetchedAt)
	if err == sql.ErrNoRows {
		return nil, false
	}
	if err != nil {
		c.logger.WarnContext(ctx, "edgar.facts_cache_read_failed", "cik", cik, "err", err)
		return nil, false
	}
	if time.Since(time.Unix(fetchedAt, 0)) >= c.factsTTL {
		return nil, false // stale
	}
	return []byte(raw), true
}

// storeFacts upserts the raw companyfacts blob for a CIK. Best-effort: a cache
// write failure is logged but never fails the fetch.
func (c *Client) storeFacts(ctx context.Context, cik string, body []byte) {
	if c.cache == nil {
		return
	}
	db := c.cache.Conn()
	if _, err := db.ExecContext(ctx, factsDDL); err != nil {
		c.logger.WarnContext(ctx, "edgar.facts_table_create_failed", "err", err)
		return
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO edgar_facts (cik, raw_json, fetched_at) VALUES (?, ?, ?)
		ON CONFLICT (cik) DO UPDATE SET raw_json = EXCLUDED.raw_json, fetched_at = EXCLUDED.fetched_at`,
		cik, string(body), time.Now().Unix(),
	); err != nil {
		c.logger.WarnContext(ctx, "edgar.facts_cache_write_failed", "cik", cik, "err", err)
	}
}

// parseFacts unmarshals a companyfacts.json blob.
func parseFacts(body []byte) (*companyFacts, error) {
	var cf companyFacts
	if err := json.Unmarshal(body, &cf); err != nil {
		return nil, err
	}
	return &cf, nil
}
