package edgar

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/raghavkgarg/mycase/pkg/marketdata"
)

// factsSchemaVersion is bumped whenever the set of fields mapFacts extracts
// changes. Cached rows written under an older version are treated as a miss and
// re-derived (from EDGAR — the raw blob is no longer retained in the DB), so a
// mapper change can't silently serve a stale-shape record. See
// docs/09-edgar-facts-reference.md.
const factsSchemaVersion = 1

// factsDDL caches the COMPACT, extracted facts per CIK — the mapped
// marketdata.Fundamentals subset EDGAR supplies (see mapFacts), not the raw
// multi-MB companyfacts.json. The raw bodies are archived to data/raw/ with
// their own retention; the DB need not duplicate ~4 MB/company to serve ~8 KB of
// useful numbers (measured 437×–2115× smaller). Owned by edgar (not pkg/cache),
// created lazily via cache.Conn().
const factsDDL = `
CREATE TABLE IF NOT EXISTS edgar_facts (
    cik            VARCHAR PRIMARY KEY,
    facts_json     VARCHAR NOT NULL,
    entity_name    VARCHAR,
    schema_version INTEGER NOT NULL,
    fetched_at     BIGINT  NOT NULL
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

// fetchFacts returns the extracted (mapped) fundamentals for a CIK, serving the
// compact blob from the DuckDB cache when it is within factsTTL and matches the
// current schema version, otherwise re-fetching companyfacts.json and re-mapping.
// Returns (·, false, nil) when EDGAR has no facts for the CIK (404) — a graceful
// skip.
func (c *Client) fetchFacts(ctx context.Context, cik string) (marketdata.Fundamentals, bool, error) {
	if f, ok := c.cachedFacts(ctx, cik); ok {
		return f, true, nil
	}

	body, err := c.getJSON(ctx, c.dataBase+companyFactsPath(cik))
	if errors.Is(err, errNotFound) {
		return marketdata.Fundamentals{}, false, nil
	}
	if err != nil {
		return marketdata.Fundamentals{}, false, err
	}

	cf, err := parseFacts(body)
	if err != nil {
		return marketdata.Fundamentals{}, false, fmt.Errorf("edgar: parse facts CIK%s: %w", cik, err)
	}
	f := mapFacts(cf)
	c.storeFacts(ctx, cik, f, cf.Entity)
	c.logger.DebugContext(ctx, "edgar.facts_fetched", "cik", cik,
		"concepts", len(cf.Facts.USGAAP), "entity", cf.Entity)
	return f, true, nil
}

// cachedFacts returns the extracted fundamentals for a CIK if a fresh row of the
// current schema version is present.
func (c *Client) cachedFacts(ctx context.Context, cik string) (marketdata.Fundamentals, bool) {
	if c.cache == nil {
		return marketdata.Fundamentals{}, false
	}
	db := c.cache.Conn()
	if _, err := db.ExecContext(ctx, factsDDL); err != nil {
		c.logger.WarnContext(ctx, "edgar.facts_table_create_failed", "err", err)
		return marketdata.Fundamentals{}, false
	}
	var raw string
	var schemaVer int
	var fetchedAt int64
	err := db.QueryRowContext(ctx,
		`SELECT facts_json, schema_version, fetched_at FROM edgar_facts WHERE cik = ?`, cik,
	).Scan(&raw, &schemaVer, &fetchedAt)
	if err == sql.ErrNoRows {
		return marketdata.Fundamentals{}, false
	}
	if err != nil {
		c.logger.WarnContext(ctx, "edgar.facts_cache_read_failed", "cik", cik, "err", err)
		return marketdata.Fundamentals{}, false
	}
	if schemaVer != factsSchemaVersion {
		return marketdata.Fundamentals{}, false // extracted-field set changed → re-derive
	}
	if time.Since(time.Unix(fetchedAt, 0)) >= c.factsTTL {
		return marketdata.Fundamentals{}, false // stale
	}
	var f marketdata.Fundamentals
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		c.logger.WarnContext(ctx, "edgar.facts_cache_decode_failed", "cik", cik, "err", err)
		return marketdata.Fundamentals{}, false
	}
	return f, true
}

// storeFacts upserts the compact extracted facts for a CIK. Best-effort: a cache
// write failure is logged but never fails the fetch.
func (c *Client) storeFacts(ctx context.Context, cik string, f marketdata.Fundamentals, entity string) {
	if c.cache == nil {
		return
	}
	db := c.cache.Conn()
	if _, err := db.ExecContext(ctx, factsDDL); err != nil {
		c.logger.WarnContext(ctx, "edgar.facts_table_create_failed", "err", err)
		return
	}
	data, err := json.Marshal(f)
	if err != nil {
		c.logger.WarnContext(ctx, "edgar.facts_marshal_failed", "cik", cik, "err", err)
		return
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO edgar_facts (cik, facts_json, entity_name, schema_version, fetched_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (cik) DO UPDATE SET facts_json = EXCLUDED.facts_json, entity_name = EXCLUDED.entity_name, schema_version = EXCLUDED.schema_version, fetched_at = EXCLUDED.fetched_at`,
		cik, string(data), entity, factsSchemaVersion, time.Now().Unix(),
	); err != nil {
		c.logger.WarnContext(ctx, "edgar.facts_cache_write_failed", "cik", cik, "err", err)
	}
}

// marshalFacts encodes the extracted fundamentals to the compact JSON stored in
// edgar_facts.facts_json. Shared by storeFacts and the blob migration.
func marshalFacts(f marketdata.Fundamentals) (string, error) {
	data, err := json.Marshal(f)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// parseFacts unmarshals a companyfacts.json blob.
func parseFacts(body []byte) (*companyFacts, error) {
	var cf companyFacts
	if err := json.Unmarshal(body, &cf); err != nil {
		return nil, err
	}
	return &cf, nil
}
