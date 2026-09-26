# Storage & Pipeline Persistence

The pipeline keeps its intermediate state — runs, per-index picks, proposals, and final
selections — in **DuckDB** (`pkg/cache`), not in loose CSV files. A pipeline run creates a
row in `pipeline_runs`, each stage writes its output to a stage table keyed by that run's
id, and `mycase pipeline history|show|diff` read the state back. This makes runs atomic,
queryable, diffable, and self-cleaning, and replaces the earlier brittle practice of the
selection tracker re-parsing its own `.txt` report output to compare runs.

All tables live in a single DuckDB database at `data/mycase.db`, alongside the
`prices` / `fundamentals` / `cache_meta` cache tables. The schema is created by
`initSchema` in `pkg/cache/db.go`, which execs one additive `CREATE TABLE IF NOT EXISTS`
DDL block — schema changes are additive only (no `ALTER`), so opening an older database
just adds any missing tables.

---

## What lives in the database

| Data | Table | Role |
|------|-------|------|
| Pipeline run master record | `pipeline_runs` | One row per execution: status, portfolio, method, config snapshot |
| Per-index scored candidates | `index_picks` | Ephemeral per-index results, combined then discarded |
| Pipeline-stage handoffs | `proposals` | The `draft` → `optimized` → `final` candidate sets |
| Final selections + driver metrics | `selections` | Cross-run comparison source (replaces `.txt` parsing) |
| Tax lots / transactions / realized gains | `tax_transactions`, `tax_lots`, `realized_gains` | Owned by `pkg/tax` (see below) |

### What stays as files (human artifacts)

- **Golden copy** (`data/{name}.csv`) — the human-editable portfolio; edited by hand
  between pipeline steps, so it stays a file for now (see roadmap for the eventual move
  to the DB behind an editing UI).
- **Human-readable reports** (`report/…/*.txt`) — opened in an editor.
- **Backups** (`data/backups/*.csv`) — exist for manual `cp`-based recovery.
- **Config files** (`pipeline.yaml`, `mfs.json`, …) — version-controlled, not runtime data.

---

## Schema

### `pipeline_runs`

Master table for tracking pipeline executions.

```sql
CREATE TABLE pipeline_runs (
    run_id      VARCHAR PRIMARY KEY,   -- timestamp-based, e.g. run_20260826_143022
    started_at  TIMESTAMP NOT NULL,
    completed_at TIMESTAMP,
    status      VARCHAR NOT NULL,      -- 'running', 'completed', 'failed', 'cancelled'
    portfolio   VARCHAR NOT NULL,      -- e.g. 'microsmall', 'us_sp500'
    method      VARCHAR NOT NULL,      -- e.g. 'multibagger', 'us_quality_momentum'
    config_json VARCHAR                -- snapshot of the resolved pipeline config for this run
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

Combined/optimized candidate sets flowing through pipeline stages. The `stage` column
distinguishes the `draft` set (post-combine), the `optimized` set (post-weighting/caps),
and the `final` set (the confirmed basket, reconciled against executed fills).

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

Final portfolio selections with driver metrics, so cross-run comparison is a query rather
than text parsing.

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
    source          VARCHAR,           -- data provenance tag: 'schwab+edgar'/'schwab'/'edgar'/'yahoo' (Phase 10d)
    PRIMARY KEY (run_id, ticker)
);
```

### Tax tables

Tax-loss harvesting adds three tables to the same DuckDB file, following the conventions
here (BIGINT epoch timestamps, DOUBLE money, composite PKs, no FK constraints):
`tax_transactions` (source of truth, idempotent on Schwab `activityId`), `tax_lots`, and
`realized_gains` (both derived — full-replace projections rebuilt from transactions on each
import). These tables are **owned by `pkg/tax`** (its `Store` defines the DDL and access
methods via a `*sql.DB` handle from `cache.Conn()`), keeping the dependency direction
domain → cache. See `docs/04-architecture.md` D11 and `docs/18-runbook.md` §7b.

---

## How the pipeline uses it

A run threads through the tables as it executes:

1. **Start** — `InsertRun` creates a `pipeline_runs` row with status `running` and a JSON
   snapshot of the resolved config.
2. **Per-index picks** — each scored index writes to `index_picks`.
3. **Combine** — instead of writing and re-reading a temp CSV, the combine step is a query:
   `GetAllIndexPicks(runID)` returns the union, deduplicated in memory. The ticker list is
   passed directly to the stock picker (`opts.Tickers`), no file round-trip.
4. **Draft / optimize** — the candidate sets are written to `proposals` at `stage='draft'`
   and `stage='optimized'`.
5. **Selections** — the final selection set plus driver metrics is written to `selections`;
   cross-run driver comparison reads it back via `GetPreviousSelections`.
6. **Final** — a confirmed proposal's placed BUY orders are written back as `stage='final'`,
   later reconciled against actual executed fills.
7. **Complete / fail** — the run is marked `completed` on success or `failed` on early exit.

The interactive `mycase pick` command and the `pipeline` prune step remain file-based by
design, because the operator may hand-edit the proposal CSV between stages.

## Why the database (vs. CSV files)

| Benefit | How |
|---------|-----|
| **Atomic writes** | No half-written CSVs on crash — transaction rollback |
| **Run history** | `SELECT * FROM pipeline_runs ORDER BY started_at DESC` |
| **Cross-run diffs** | join `selections` on `run_id` for two runs |
| **No file cleanup** | prune old rows by `started_at` instead of sweeping stale files |
| **Combine is a query** | no temp file, no read-write-delete dance |
| **Selection tracking** | join `selections` prev/curr instead of parsing `.txt` reports |
| **Resumable** | partial results survive a mid-run crash in the DB |

## Reliability

- **Schema evolution**: `initSchema` is additive-only (`CREATE TABLE IF NOT EXISTS`), so
  upgrades never need a migration step.
- **Corruption**: DuckDB's WAL plus the existing backup strategy applies; a run is
  reproducible by re-running against the warm cache.
- **Debuggability**: `mycase pipeline show <run_id>` renders a run's proposals/selections;
  an `--export-csv` path exists for external tooling.
