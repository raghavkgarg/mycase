package edgar

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// cikMapDDL is the ticker→CIK lookup table. Owned by this package (edgar), not
// by pkg/cache — created lazily via cache.Conn() so cache stays a leaf.
const cikMapDDL = `
CREATE TABLE IF NOT EXISTS edgar_cik_map (
    ticker      VARCHAR PRIMARY KEY,
    cik         VARCHAR NOT NULL,
    title       VARCHAR,
    refreshed_at BIGINT NOT NULL
);`

// companyTickersURL path (relative to www base). The file is a JSON object keyed
// by arbitrary index strings, each value {cik_str, ticker, title}.
const companyTickersPath = "/files/company_tickers.json"

// companyTicker is one entry in company_tickers.json.
type companyTicker struct {
	Ticker string `json:"ticker"`
	Title  string `json:"title"`
	CIK    int64  `json:"cik_str"`
}

// cikOverrides maps tickers whose CIK in SEC's company_tickers.json (and the
// constituents CSV) points at a non-filing shell/holding entity rather than the
// operating company that files the 10-Ks we need. Verified against
// data.sec.gov/api/xbrl/companyfacts: the shell CIK returns HTTP 200 but has
// zero FY 10-K statement facts (no operating cash flow / capex), so EDGAR
// enrichment silently no-ops and the ticker degrades to Schwab-only, tripping
// the positive-FCF hard filter for an otherwise obvious quality name.
//
// Keep this list small and evidence-based — each entry is a documented data
// bug in the upstream ticker file, not a convenience alias. CIKs are the numeric
// form (padded on use). Re-verify if SEC corrects the source file.
var cikOverrides = map[string]int64{
	// SEC lists XOM under a 2024-registered "Exxon Mobil Corporation" shell
	// (CIK 2115436, 0 FY facts); the real filer since 1957 is CIK 34088.
	"XOM": 34088,
}

// CIK returns the 10-digit zero-padded CIK for a ticker (e.g. "AAPL" →
// "0000320193"). It consults the DuckDB cache first (refreshing the whole map
// when stale or empty), then looks up the ticker. Returns ("", false, nil) when
// the ticker is unknown to EDGAR — an expected "no data" condition, not an error.
func (c *Client) CIK(ctx context.Context, ticker string) (string, bool, error) {
	sym := strings.ToUpper(strings.TrimSpace(stripUSPrefix(ticker)))
	if sym == "" {
		return "", false, nil
	}

	// A known-bad upstream mapping wins over the fetched map (see cikOverrides).
	if cik, ok := cikOverrides[sym]; ok {
		return padCIK(cik), true, nil
	}

	if err := c.ensureCIKMap(ctx); err != nil {
		return "", false, err
	}

	// No cache (nil DB): fetch once and search in-memory.
	if c.cache == nil {
		m, err := c.fetchCIKMap(ctx)
		if err != nil {
			return "", false, err
		}
		if e, ok := m[sym]; ok {
			return padCIK(e.CIK), true, nil
		}
		return "", false, nil
	}

	var cik string
	err := c.cache.Conn().QueryRowContext(ctx,
		`SELECT cik FROM edgar_cik_map WHERE ticker = ?`, sym,
	).Scan(&cik)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("edgar: query cik for %s: %w", sym, err)
	}
	return cik, true, nil
}

// ensureCIKMap makes sure the cached CIK map exists and is fresh. It refreshes
// (re-fetches company_tickers.json and repopulates the table) when the map is
// empty or older than cikTTL. A no-op when there is no cache.
func (c *Client) ensureCIKMap(ctx context.Context) error {
	if c.cache == nil {
		return nil
	}
	db := c.cache.Conn()
	if _, err := db.ExecContext(ctx, cikMapDDL); err != nil {
		return fmt.Errorf("edgar: create cik map table: %w", err)
	}

	var count int
	var newestRefresh sql.NullInt64
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*), MAX(refreshed_at) FROM edgar_cik_map`,
	).Scan(&count, &newestRefresh); err != nil {
		return fmt.Errorf("edgar: cik map freshness check: %w", err)
	}
	if count > 0 && newestRefresh.Valid {
		age := time.Since(time.Unix(newestRefresh.Int64, 0))
		if age < c.cikTTL {
			return nil // fresh
		}
	}

	m, err := c.fetchCIKMap(ctx)
	if err != nil {
		// If we have a stale-but-usable map, keep serving it rather than failing.
		if count > 0 {
			c.logger.WarnContext(ctx, "edgar.cik_map_refresh_failed",
				"err", err, "action", "serving_stale")
			return nil
		}
		return err
	}
	return c.storeCIKMap(ctx, m)
}

// fetchCIKMap downloads and parses company_tickers.json into a symbol→entry map.
func (c *Client) fetchCIKMap(ctx context.Context) (map[string]companyTicker, error) {
	body, err := c.getJSON(ctx, c.wwwBase+companyTickersPath)
	if err != nil {
		return nil, fmt.Errorf("edgar: fetch company_tickers: %w", err)
	}
	// The file is an object keyed by index strings ("0","1",...) → entry.
	var raw map[string]companyTicker
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("edgar: parse company_tickers: %w", err)
	}
	out := make(map[string]companyTicker, len(raw))
	for _, e := range raw {
		sym := strings.ToUpper(strings.TrimSpace(e.Ticker))
		if sym == "" || e.CIK == 0 {
			continue
		}
		out[sym] = e
	}
	c.logger.InfoContext(ctx, "edgar.cik_map_refreshed", "count", len(out))
	return out, nil
}

// storeCIKMap replaces the cached CIK map in a single transaction.
func (c *Client) storeCIKMap(ctx context.Context, m map[string]companyTicker) error {
	db := c.cache.Conn()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("edgar: begin cik tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM edgar_cik_map`); err != nil {
		return fmt.Errorf("edgar: clear cik map: %w", err)
	}
	now := time.Now().Unix()
	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR REPLACE INTO edgar_cik_map (ticker, cik, title, refreshed_at) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("edgar: prepare cik insert: %w", err)
	}
	defer stmt.Close()
	for sym, e := range m {
		if _, err := stmt.ExecContext(ctx, sym, padCIK(e.CIK), e.Title, now); err != nil {
			return fmt.Errorf("edgar: insert cik %s: %w", sym, err)
		}
	}
	return tx.Commit()
}

// padCIK formats a numeric CIK as the 10-digit zero-padded string EDGAR's
// data.sec.gov endpoints require (e.g. 320193 → "0000320193").
func padCIK(cik int64) string {
	return fmt.Sprintf("%010d", cik)
}
