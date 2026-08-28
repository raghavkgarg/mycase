# DuckDB Migration — Intermediate Pipeline Data

**Status**: Planned — Phase 7 in [roadmap](roadmap.md)  
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

> **Note (naming):** the doc refers to `ensureSchema()`, but the implemented method is `initSchema` in `pkg/cache/db.go`. It execs one additive `CREATE TABLE IF NOT EXISTS` DDL block.

### Tax tables (Phase 4 — shipped)

Phase 4 (Tax-Loss Harvesting) added three tables to the same DuckDB file, following the conventions here (BIGINT epoch timestamps, DOUBLE money, composite PKs, no FK constraints): `tax_transactions` (source of truth, idempotent on Schwab `activityId`), `tax_lots`, and `realized_gains` (both derived — full-replace projections rebuilt from transactions on each import). Methods live in `pkg/cache/tax.go`. See `docs/architecture.md` D11 and `docs/runbook.md` §7b.

---

## Implementation Plan

### Phase A: Schema + Migration Infrastructure ✅ Done

1. ✅ Added new tables to `pkg/cache/db.go` `ensureSchema()` — `pipeline_runs`, `index_picks`, `proposals`, `selections`
2. ✅ `RunID` generation (timestamp-based: `run_20260826_143022`)
3. ✅ `InsertRun`, `CompleteRun`, `FailRun`, `GetRun`, `ListRuns`, `LatestRun` methods
4. ✅ `InsertIndexPicks`, `InsertProposals`, `InsertSelections`, query methods
5. ✅ 22 unit tests passing

**Effort**: 1 day → completed

### Phase B: Pipeline Writes to DuckDB ✅ Done

Write to DB alongside existing CSV writes (belt-and-suspenders):

1. ✅ `cmd/pipeline.go` / `pkg/autopilot/autopilot.go`: create `pipeline_runs` row at start
2. ✅ Per-index pick step: write scored candidates to `index_picks` alongside CSV
3. ✅ Combine/prune steps: write to `proposals` with `stage='draft'`/`'optimized'` alongside CSV
4. ✅ `stockpicker.RunWithResult` returns `PickResult` struct (selectedKeys, weights, scores, sectors)
5. ✅ Run marked completed on success, failed on early exit (deferred)

**Effort**: 3–4 days → completed

### Phase C: Pipeline Reads from DuckDB ✅ Done (C1 + C2)

Replace file reads in pipeline stages with DB queries. Based on analysis of actual read sites:

#### What each CSV read site actually extracts

| # | Read Site | File Pattern | Columns Used | Can Replace? |
|---|-----------|-------------|-------------|-------------|
| 1 | `CombineMultipleCSVs` | `index_picks/*.csv` | `ticker` only | ✅ → `SELECT DISTINCT ticker FROM index_picks WHERE run_id = ?` |
| 2 | `loadLocalCSVConstituents` (combined) | `temp/combine_*.csv` | `ticker` only | ✅ → same query as #1 (combine+read is one step) |
| 3 | `loadLocalCSVConstituents` (proposal) | `proposals/*.csv` | `ticker` only | ✅ → `SELECT ticker FROM proposals WHERE run_id = ? AND stage = 'draft'` |
| 4 | `ReadCSVWeights` (golden, for diff) | golden copy | `ticker` + `weight` | ❌ Golden copy stays as file (Tier 2) |
| 5 | `LoadBasketCSV` (for orders) | golden copy | `ticker` + `weight` | ❌ Golden copy stays as file |
| 6 | `MergeGoldenCopy` | proposals + golden | both | ❌ Golden copy stays as file; source could be DB |
| 7 | `SaveReport` (prev report) | `*_01_selection_reasons.txt` | ticker + drivers | ✅ → `SELECT * FROM selections WHERE run_id = (prev)` |
| 8 | `parseSelectionReport` | `*_01_selection_reasons.txt` | ticker + score + rank | ✅ → same as #7 |

#### Implementation steps (ordered by risk, safest first)

**C1: Eliminate the combine temp file** (lowest risk)

The combine step (`CombineMultipleCSVs`) reads N index-pick CSVs, extracts only ticker names,
deduplicates them, and writes a temp CSV. Then `loadLocalCSVConstituents` reads that temp CSV
to get the ticker list. Both steps together just produce `[]string` (unique tickers from all indices).

Replace with:
```go
// In autopilot.go / pipeline.go, after all index picks are in DB:
allPicks, _ := db.GetAllIndexPicks(ctx, runID)
var tickers []string
seen := map[string]bool{}
for _, p := range allPicks {
    if !seen[p.Ticker] {
        seen[p.Ticker] = true
        tickers = append(tickers, p.Ticker)
    }
}
// Pass tickers directly to stockpicker instead of writing/reading a file
```

**Blocker**: `stockpicker.Run` / `runPickWithOpts` only accepts `opts.FilePath` or `opts.IndexName`.
Need to add an `opts.Tickers []string` field that `LoadConstituents` can use directly (bypass file I/O).

**C2: Eliminate the proposal re-read for pruning** (low risk)

Same pattern as C1 — the prune step reads the draft proposal CSV only to extract ticker names.
After C1, we'd pass `opts.Tickers = draftResult.SelectedKeys` directly (already in memory from
`RunWithResult`). No DB query needed — it's already returned by the previous step.

**C3: Selection tracker reads from DB** (medium risk)

`selectiontracker.SaveReport` currently parses its own previous `.txt` output to extract
`prevDriversMap[ticker] = "TTM Growth: 25%, CAGR 3Y: 18%, ..."`. This is brittle text parsing.

Replace with:
```go
prevSelections, err := db.GetPreviousSelections(ctx, portfolio, method)
// Build prevDriversMap from Selection.TTMGrowth, RevenueCagr, ROIC, etc.
```

**Blocker**: The driver metric string format is ad-hoc and method-specific. Need to ensure
the `selections` table stores all the metrics that the report formatter needs. Current schema
has: ttm_growth, revenue_cagr, dso_delta, rsi, momentum_1y, fcf_yield, roic. May need to add
more fields for US strategy (shareholder_yield, earnings_quality, low_volatility).

**C4: MergeGoldenCopy reads source from DB** (low risk, optional)

Currently reads the optimized proposal CSV. Could instead read from
`proposals WHERE run_id = ? AND stage = 'optimized'`. But since the golden copy itself stays as
a file, and `MergeGoldenCopy` does file→file merge, this saves one file read but still needs the
golden file read. Low priority — defer.

#### What does NOT change in Phase C

- **Golden copy stays as a file** — human-editable, manual tweaks between pipeline steps
- **Human-readable reports stay as files** — `.txt` files opened in editor
- **CSV writes continue in parallel** — the `--legacy-csv` flag writes CSV alongside DB reads
  (remove in a later cleanup pass once confidence is high)
- **`runPickWithOpts` in cmd/pick.go** — the interactive CLI pick command stays file-based
  (it's not part of the automated pipeline)

#### Effort estimate

| Step | Effort | Risk | Status |
|------|--------|------|--------|
| C1: Add `opts.Tickers` to stockpicker + wire in autopilot/pipeline | 1 day | Low | ✅ Done |
| C2: Pass draft result directly to prune step | 0.5 day | Low | ✅ Done |
| C3: Selection tracker reads from DB | 1–2 days | Medium | ⬜ Deferred |
| C4: MergeGoldenCopy from DB (optional) | 0.5 day | Low | ⬜ Deferred |

**Recommended order**: C1 → C2 → C3. Skip C4 until golden copy moves to DB (Tier 2).

**Note**: `cmd/pipeline.go` prune step intentionally kept file-based — user may manually edit the
proposal CSV between draft and prune (interactive workflow).

**Effort**: C1+C2 completed. C3 deferred (medium risk, needs schema review).

### Phase D: Cleanup + CLI Tooling ✅ Done

1. ✅ `mycase pipeline history` — list past runs with dates, status, stock counts
2. ✅ `mycase pipeline diff <run1> <run2>` — show what changed between runs
3. ✅ `mycase pipeline show <run_id>` — show proposals/selections for a specific run
4. Remove file-writing code paths (keep `--export-csv` flag for manual export if needed) — defer to after Phase C
5. Remove `data/candidates/` directory from active use (archive existing files) — defer to after Phase C

**Effort**: 2–3 days → completed in Phase 7D

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
