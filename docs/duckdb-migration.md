# DuckDB Migration — Intermediate Pipeline Data

**Status**: Planned  
**Depends on**: Existing `pkg/cache/` DuckDB infrastructure  
**Blocked by**: Nothing — can proceed independently of TLH / Performance Attribution

---

## Problem

The pipeline passes data between stages via CSV files in `data/candidates/`. This works but is:

- **Fragile**: half-written CSV on crash mid-pipeline → corrupt state
- **Opaque**: can't easily query "what did the pipeline propose on 2026-04-15?"
- **Messy**: accumulates stale files that need manual cleanup
- **Unauditable**: no timestamps, no run IDs, no diff between runs

The selection tracker (`pkg/selectiontracker/`) reads its own previous `.txt` report output to extract driver metrics for cross-run comparison — brittle text parsing of a human-readable format.

---

## Current Data Flow (file-based)

```
pick (per-index)
  → data/candidates/index_picks/{index}_{method}.csv

pipeline combine
  → data/candidates/temp/combine_{name}.csv (deleted after)

pipeline re-score
  → data/candidates/proposals/{date}_{name}_{method}.csv

pipeline prune
  → data/candidates/proposals/{date}_{name}_{method}_optim.csv

merge golden
  → data/{name}.csv (golden copy, human-editable)

selection tracker
  → report/{name}_{method}/executions/{date}_01_selection_reasons.txt
  ← reads previous run's _01_selection_reasons.txt for deltas
```

---

## What Moves to DuckDB

### Tier 1: Pure machine-to-machine intermediates (move immediately)

| Current artifact | New table | Rationale |
|-----------------|-----------|-----------|
| `data/candidates/index_picks/*.csv` | `index_picks` | Ephemeral per-index results, combined then discarded |
| `data/candidates/proposals/*.csv` | `proposals` | Pipeline stage handoff — the "draft" |
| `data/candidates/proposals/*_optim.csv` | `proposals` (with `stage='optimized'`) | Final candidate set before golden merge |
| `data/candidates/temp/combine_*.csv` | Eliminated entirely | In-memory join in DuckDB |
| Selection tracker cross-run state | `selections` | Replaces brittle text parsing with a query |

### Tier 2: Move after SwiftUI editing UI exists

| Current artifact | New table | Rationale |
|-----------------|-----------|-----------|
| `data/{name}.csv` (golden copy) | `golden_portfolio` | Currently must be a file for manual editing. Once SwiftUI provides an editing UI, the file constraint disappears |

### Stays as files (human artifacts)

| Artifact | Why it stays |
|----------|-------------|
| `report/…/03_portfolio_report.txt` | Human consumption — opened in editor |
| `report/…/simulations/*_monitoring.txt` | Human consumption — opened in editor |
| `data/backups/*.csv` | Manual disaster recovery — `cp` back if needed |
| Performance command output | Stdout only, no persistence needed |

---

## Schema

All tables below live in the same DuckDB database as `prices`/`fundamentals`/`cache_meta` (currently at `data/mycase_cache.db`).

### `pipeline_runs`

Master table for tracking pipeline executions.

```sql
CREATE TABLE pipeline_runs (
    run_id      VARCHAR PRIMARY KEY,   -- UUID or timestamp-based
    started_at  TIMESTAMP NOT NULL,
    completed_at TIMESTAMP,
    status      VARCHAR NOT NULL,      -- 'running', 'completed', 'failed', 'cancelled'
    portfolio   VARCHAR NOT NULL,      -- e.g. 'microsmall', 'us_sp500'
    method      VARCHAR NOT NULL,      -- e.g. 'multibagger', 'us_quality_momentum'
    config_json VARCHAR                -- snapshot of pipeline.yaml params for this run
);
```

### `index_picks`

Per-index scored candidates from a single pipeline run.

```sql
CREATE TABLE index_picks (
    run_id      VARCHAR NOT NULL REFERENCES pipeline_runs(run_id),
    index_name  VARCHAR NOT NULL,      -- e.g. 'microcap250', 'sp500'
    ticker      VARCHAR NOT NULL,
    score       DOUBLE,
    rank        INTEGER,
    weight      DOUBLE,                -- initial weight before optimization
    sector      VARCHAR,
    PRIMARY KEY (run_id, index_name, ticker)
);
```

### `proposals`

Combined/optimized candidate sets flowing through pipeline stages.

```sql
CREATE TABLE proposals (
    run_id      VARCHAR NOT NULL REFERENCES pipeline_runs(run_id),
    stage       VARCHAR NOT NULL,      -- 'draft', 'optimized', 'final'
    ticker      VARCHAR NOT NULL,
    weight      DOUBLE NOT NULL,
    score       DOUBLE,
    rank        INTEGER,
    sector      VARCHAR,
    PRIMARY KEY (run_id, stage, ticker)
);
```

### `selections`

Final portfolio selections with driver metrics — replaces cross-run `.txt` parsing.

```sql
CREATE TABLE selections (
    run_id          VARCHAR NOT NULL REFERENCES pipeline_runs(run_id),
    ticker          VARCHAR NOT NULL,
    weight          DOUBLE NOT NULL,
    score           DOUBLE,
    rank            INTEGER,
    -- Driver metrics (for cross-run comparison)
    ttm_growth      DOUBLE,
    revenue_cagr    DOUBLE,
    dso_delta       DOUBLE,
    rsi             DOUBLE,
    momentum_1y     DOUBLE,
    fcf_yield       DOUBLE,
    roic            DOUBLE,
    -- Selection context
    action          VARCHAR,           -- 'new', 'retained', 'removed'
    prev_rank       INTEGER,           -- rank in previous run (NULL if new)
    prev_weight     DOUBLE,            -- weight in previous run (NULL if new)
    PRIMARY KEY (run_id, ticker)
);
```

### `golden_portfolio` (Tier 2 — after SwiftUI)

```sql
CREATE TABLE golden_portfolio (
    portfolio   VARCHAR NOT NULL,
    ticker      VARCHAR NOT NULL,
    weight      DOUBLE NOT NULL,
    sector      VARCHAR,
    qty         INTEGER,
    avg_cost    DOUBLE,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (portfolio, ticker)
);
```

---

## Implementation Plan

### Phase A: Schema + Migration Infrastructure

1. Add new tables to `pkg/cache/db.go` `ensureSchema()` — same pattern as existing tables
2. Add `RunID` generation (timestamp-based: `run_20260815_143022`)
3. Add `InsertRun`, `CompleteRun`, `FailRun` methods to cache

**Effort**: 1 day

### Phase B: Pipeline Writes to DuckDB

Replace file writes in pipeline stages with DB inserts:

1. `cmd/pipeline.go` / `pkg/autopilot/autopilot.go`: create `pipeline_runs` row at start
2. Per-index pick step: write scored candidates to `index_picks` instead of CSV
3. Combine step: DuckDB query (`SELECT ... FROM index_picks WHERE run_id = ?`) replaces reading CSVs + combining
4. Prune step: write to `proposals` with `stage='optimized'` instead of `_optim.csv`
5. Selection tracker: write to `selections` instead of `.txt`, read previous run via query

**Effort**: 3–4 days

### Phase C: Pipeline Reads from DuckDB

Replace file reads in pipeline stages with DB queries:

1. Combine step reads from `index_picks` table (eliminates temp file entirely)
2. Prune step reads draft from `proposals` table
3. Golden merge reads final from `proposals WHERE stage='final'`
4. Selection tracker reads previous metrics from `selections WHERE run_id = (previous)`
5. Report command reads from `selections` for rationale enrichment

**Effort**: 2–3 days

### Phase D: Cleanup + CLI Tooling

1. `mycase pipeline history` — list past runs with dates, status, stock counts
2. `mycase pipeline diff <run1> <run2>` — show what changed between runs
3. `mycase pipeline show <run_id>` — show proposals/selections for a specific run
4. Remove file-writing code paths (keep `--export-csv` flag for manual export if needed)
5. Remove `data/candidates/` directory from active use (archive existing files)

**Effort**: 2–3 days

---

## Benefits

| Benefit | How |
|---------|-----|
| **Atomic writes** | No half-written CSVs on crash — transaction rollback |
| **Run history** | `SELECT * FROM pipeline_runs ORDER BY started_at DESC` |
| **Cross-run diffs** | `SELECT ... FROM selections WHERE run_id IN (?, ?) PIVOT ...` |
| **No file cleanup** | `DELETE FROM proposals WHERE started_at < now() - interval '90 days'` |
| **Combine is a query** | No temp file, no read-write-delete dance |
| **Selection tracker** | `SELECT prev.rank, curr.rank FROM selections curr JOIN selections prev ON ...` |
| **Pipeline resume** | If pipeline crashes mid-way, partial results survive in DB — resume from last stage |

---

## What Does NOT Move

- **Golden copy (for now)**: The human-editing workflow requires a file. Moves in Tier 2 after SwiftUI.
- **Human-readable reports**: `.txt` files opened in editor stay as files.
- **Backups**: These exist for manual `cp`-based recovery — they are files by design.
- **Config files**: `pipeline.yaml`, `mfs.json`, etc. — these are version-controlled, not runtime data.

---

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| DuckDB schema migration on upgrade | `ensureSchema()` uses `CREATE TABLE IF NOT EXISTS` — additive only, no ALTER |
| DB corruption | DuckDB WAL mode + existing backup strategy applies. Pipeline runs are reproducible (re-run from cache) |
| Debugging harder than reading a CSV | `mycase pipeline show <run_id>` provides formatted output. `--export-csv` flag for tooling |
| Breaking existing pipeline users | Keep `--legacy-csv` flag that writes files in parallel during transition period |

---

## Timeline

| Phase | Effort | Dependency |
|-------|--------|-----------|
| A: Schema | 1 day | None |
| B: Write path | 3–4 days | Phase A |
| C: Read path | 2–3 days | Phase B |
| D: CLI + cleanup | 2–3 days | Phase C |
| **Total** | **~10 days** | |

Can be done incrementally — each phase is independently deployable. Phase B can write to both DB and CSV in parallel (belt-and-suspenders) until Phase C is proven.
