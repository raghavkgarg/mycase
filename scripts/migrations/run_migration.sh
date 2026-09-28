#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# Script: run_migration.sh
# Description: Execute DuckDB migrations with safety checkpointing and validation
# Usage: ./scripts/migrations/run_migration.sh [path/to/mycase.db]
# ============================================================================

DB_PATH="${1:-data/mycase.db}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SQL_FILE="${SCRIPT_DIR}/001_emb_reconciliation.sql"

if [ ! -f "$DB_PATH" ]; then
    echo "❌ Error: Database file not found at $DB_PATH" >&2
    exit 1
fi

if [ ! -f "$SQL_FILE" ]; then
    echo "❌ Error: Migration SQL file not found at $SQL_FILE" >&2
    exit 1
fi

echo "======================================================================"
echo "Starting EMB Reconciliation Migration on: $DB_PATH"
echo "======================================================================"

# 1. Flush WAL via CHECKPOINT
echo "1. Flushing WAL checkpoint..."
duckdb "$DB_PATH" "CHECKPOINT;"

# 2. Physical Backup
BACKUP_PATH="${DB_PATH}.backup_$(date +%Y%m%d_%H%M%S)"
echo "2. Creating physical backup: $BACKUP_PATH"
cp "$DB_PATH" "$BACKUP_PATH"
shasum -a 256 "$BACKUP_PATH" > "${BACKUP_PATH}.sha256"

# Verify backup can be opened read-only
duckdb -readonly "$BACKUP_PATH" "SELECT count(*) FROM pit_runs;" > /dev/null
echo "   ✓ Backup verified successfully."

# 3. Execute Migration
echo "3. Executing SQL migration: $SQL_FILE..."
duckdb "$DB_PATH" < "$SQL_FILE"

# 4. Strict Validation Checks
echo "4. Running post-migration integrity assertions..."

NULL_POLICIES=$(duckdb "$DB_PATH" -noheader -list "SELECT count(*) FROM pit_runs WHERE selection_policy IS NULL;" | tr -d '[:space:]')
if [ "$NULL_POLICIES" != "0" ]; then
    echo "❌ Integrity check failed: $NULL_POLICIES runs have NULL selection_policy!" >&2
    exit 1
fi

NULL_HOLDINGS_REC=$(duckdb "$DB_PATH" -noheader -list "SELECT count(*) FROM pit_runs WHERE holdings_recorded IS NULL;" | tr -d '[:space:]')
if [ "$NULL_HOLDINGS_REC" != "0" ]; then
    echo "❌ Integrity check failed: $NULL_HOLDINGS_REC runs have NULL holdings_recorded!" >&2
    exit 1
fi

NULL_OUTCOMES=$(duckdb "$DB_PATH" -noheader -list "SELECT count(*) FROM pit_candidate_scores WHERE passed_stage1 = true AND method = 'earlymb' AND outcome IS NULL;" | tr -d '[:space:]')
if [ "$NULL_OUTCOMES" != "0" ]; then
    echo "❌ Integrity check failed: $NULL_OUTCOMES Stage-1 survivors have NULL outcome!" >&2
    exit 1
fi

echo "======================================================================"
echo "✓ Migration completed successfully with zero integrity violations."
echo "======================================================================"
