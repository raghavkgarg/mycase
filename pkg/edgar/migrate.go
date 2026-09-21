package edgar

import (
	"context"
	"database/sql"
	"fmt"
)

// FactsMigrationResult reports the outcome of MigrateFactsBlobs.
type FactsMigrationResult struct {
	AlreadyCompact bool // true when the table was already the compact schema (no-op)
	Scanned        int  // old raw_json rows read
	Migrated       int  // rows re-derived and written to the compact table
	Skipped        int  // rows whose raw blob failed to parse (dropped; re-fetchable)
}

// MigrateFactsBlobs converts a legacy edgar_facts table (columns
// cik, raw_json, fetched_at — the full multi-MB companyfacts blob per CIK) to the
// compact schema (cik, facts_json, entity_name, schema_version, fetched_at) by
// re-deriving the extracted fundamentals from each stored blob IN PLACE. It makes
// NO network calls — the existing raw blobs are re-parsed and re-mapped locally,
// so no EDGAR re-fetch or rate-limit budget is spent.
//
// It is idempotent: if the table is already the compact schema it returns
// {AlreadyCompact: true} without touching anything. Rows whose blob no longer
// parses are skipped (they remain re-fetchable from EDGAR on next use).
//
// NOTE: this rewrites the table rows but does NOT reclaim the on-disk pages the
// old blobs occupied — DuckDB frees them logically but the file keeps its size.
// The caller reclaims space by rewriting the database file (see the CLI command,
// which COPYs to a fresh file).
func (c *Client) MigrateFactsBlobs(ctx context.Context) (FactsMigrationResult, error) {
	var res FactsMigrationResult
	if c.cache == nil {
		return res, fmt.Errorf("edgar: MigrateFactsBlobs requires a cache")
	}
	db := c.cache.Conn()

	hasRaw, err := columnExists(ctx, db, "edgar_facts", "raw_json")
	if err != nil {
		return res, err
	}
	if !hasRaw {
		// Either the table doesn't exist yet or it's already compact. Ensure the
		// compact table exists and report a no-op.
		if _, err := db.ExecContext(ctx, factsDDL); err != nil {
			return res, fmt.Errorf("edgar: ensure compact table: %w", err)
		}
		res.AlreadyCompact = true
		return res, nil
	}

	// Read every legacy row first (a single pass; blobs are large but we process
	// one at a time and only keep the compact form).
	rows, err := db.QueryContext(ctx, `SELECT cik, raw_json, fetched_at FROM edgar_facts`)
	if err != nil {
		return res, fmt.Errorf("edgar: read legacy facts: %w", err)
	}
	type compactRow struct {
		cik       string
		factsJSON string
		entity    string
		fetchedAt int64
	}
	var compact []compactRow
	for rows.Next() {
		var cik, raw string
		var fetchedAt int64
		if err := rows.Scan(&cik, &raw, &fetchedAt); err != nil {
			rows.Close()
			return res, fmt.Errorf("edgar: scan legacy row: %w", err)
		}
		res.Scanned++
		cf, perr := parseFacts([]byte(raw))
		if perr != nil {
			res.Skipped++
			continue
		}
		f := mapFacts(cf)
		data, merr := marshalFacts(f)
		if merr != nil {
			res.Skipped++
			continue
		}
		compact = append(compact, compactRow{cik: cik, factsJSON: data, entity: cf.Entity, fetchedAt: fetchedAt})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return res, fmt.Errorf("edgar: iterate legacy rows: %w", err)
	}
	rows.Close()

	// Swap the table in a single transaction: drop the legacy table, create the
	// compact one, bulk-insert the re-derived rows (preserving each row's original
	// fetched_at so freshness/TTL is unchanged).
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("edgar: begin migration tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DROP TABLE edgar_facts`); err != nil {
		return res, fmt.Errorf("edgar: drop legacy table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, factsDDL); err != nil {
		return res, fmt.Errorf("edgar: create compact table: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO edgar_facts (cik, facts_json, entity_name, schema_version, fetched_at)
		VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return res, fmt.Errorf("edgar: prepare insert: %w", err)
	}
	defer stmt.Close()
	for _, r := range compact {
		if _, err := stmt.ExecContext(ctx, r.cik, r.factsJSON, r.entity, factsSchemaVersion, r.fetchedAt); err != nil {
			return res, fmt.Errorf("edgar: insert compact row %s: %w", r.cik, err)
		}
		res.Migrated++
	}
	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("edgar: commit migration: %w", err)
	}
	return res, nil
}

// columnExists reports whether a table has a given column, tolerating a missing
// table (returns false, nil).
func columnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT column_name FROM information_schema.columns WHERE table_name = ?`, table)
	if err != nil {
		return false, fmt.Errorf("edgar: introspect %s columns: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
