# Data Directory Architecture & File Inventory (`data/`)

This document provides a comprehensive, file-by-file and directory-by-directory audit of the `data/` folder in `mycase`. It explains:
1. **What each file/folder is** and why it exists.
2. **Provenance**: Where it came from (which CLI command, background job, scraper, or user action generated it).
3. **Consumers**: Which Go packages or Python scripts read from it.
4. **Status**: `REQUIRED`, `TRANSIENT`, `PARTIALLY REDUNDANT`, or `OBSOLETE / SAFE TO DELETE`.
5. **Recreation / Recovery Guide**: If deleted or lost, exactly how to regenerate, re-source, or recover it.

---

## 1. Directory Tree Overview

```
data/
├── mycase.db                           # [REQUIRED] Consolidated DuckDB master database
├── cache.db                            # [OBSOLETE] Legacy cache database (migrated to mycase.db)
├── pit_history.db                      # [OBSOLETE] Legacy PIT database (migrated to mycase.db)
│
├── aitheme.csv                         # [REQUIRED] Golden theme copy (Theme AI Advice)
├── microsmall.csv                      # [REQUIRED] Golden theme copy (Theme Microsmall)
├── modularmicro.csv                    # [REQUIRED] Golden theme copy (Theme Micro Advice)
├── myall.csv                           # [REQUIRED] Golden theme copy (Theme KK Advise)
├── hydrogen.csv                        # [REQUIRED] Golden theme copy (Theme Hydrogen Nuclear - moved from candidates)
│
├── midcap150.csv                       # [OBSOLETE] Stale universe snapshot from July 2026
├── midsmallmicro.csv                   # [OBSOLETE] Stale universe snapshot from July 2026
├── niftytotalmarket.csv                # [OBSOLETE] Transient rebalance output from Sep 9, 2026
├── qtum.xlsx                           # [OBSOLETE] Stale US quantum ETF sample file from July 2026
├── sp500.csv                           # [OBSOLETE] Stale US SP500 test basket from July 2026
├── .DS_Store                           # [OBSOLETE] macOS Finder metadata
│
├── universe_snapshots/                 # [REQUIRED] Canonical index constituent rosters & backtest snapshots
│   ├── NIFTY50.csv                     # Seeds index_constituents in mycase.db
│   ├── microcap250.csv                 # Seeds index_constituents in mycase.db
│   ├── smallcap250.csv                 # Seeds index_constituents in mycase.db
│   ├── microcap250_smallcap250.csv     # Seeds index_constituents in mycase.db
│   └── microcap250,smallcap250_*.csv   # Immutable dated rosters for survivorship-free calibration
│
├── cache/                              # [REQUIRED] Scraper disk caches
│   └── delivery/                       # 3-month NSE delivery volume history JSONs per ticker
│
├── .cache/                             # [TRANSIENT] L1 HTTP cache for Yahoo Finance API responses
│   └── prices_*.json                   # Same-day price bars to avoid repeated HTTP calls
│
├── backups/                            # [REQUIRED] Safety snapshots & cooldown registry
│   ├── microsmall/                     # Historical rebalance backups of microsmall.csv
│   ├── niftytotalmarket/               # Historical rebalance backups of niftytotalmarket
│   └── {midcap150,nifty50,qtum,sp500}/ # [OBSOLETE] Backups of abandoned July universes
│
├── candidates/                         # [HYBRID] Strategy outputs and candidate proposals
│   ├── proposals/                      # Proposed baskets from rebalance runs (historical audit trail)
│   ├── index_picks/                    # Scored picks & incubator watchlists (*_incubator.csv)
│   ├── microcap250_multibagger.csv     # [OBSOLETE] Static test output from July
│   ├── small250_multibagger.csv        # [OBSOLETE] Static test output from July
│   └── hydrogen.csv                    # Moved to data/hydrogen.csv
│
├── pit_snapshots/                      # [PRUNABLE] Daily JSON dumps of candidate factor scores
│   └── {index}_{method}_{date}.json    # Only latest snapshot is read for daily diff reports
│
├── annual_reports/                     # [QUALITATIVE RESEARCH] PDF annual reports for scuttlebutt
│   └── *.pdf                           # Scanned by check_customer_concentration.py
│
└── logs/                               # [ACTIVE] Runtime JSONL structured logs
    └── mycase-YYYY-MM-DD.jsonl         # Daily stdout/stderr application logs
```

---

## 2. Granular Inventory: Files & Directories

### 2.1 Database Files

#### `data/mycase.db` (34.1 MB)
* **Status**: **CRITICAL / REQUIRED**
* **What it is**: The unified, master DuckDB analytical database for the entire project. It contains all 14 tables and views:
  - Market data: `prices`, `fundamentals`, `cache_meta`
  - Research & PIT: `pit_runs`, `pit_candidate_scores`, `v_pit_candidate_scores`, `v_pit_runs`, `index_constituents`
  - Pipeline & Portfolios: `pipeline_runs`, `index_picks`, `proposals`, `selections`
  - Themes: `theme_rebalances`, `theme_history`
* **Provenance**: Initialized by `pkg/themedb`, `pkg/cache`, and `pkg/pithistory`. Consolidated from `cache.db` and `pit_history.db`.
* **Consumers**: Almost every package in `mycase` (`main.go`, `cmd/*`, `pkg/cache`, `pkg/pithistory`, `pkg/themedb`, `pkg/server`, `pkg/themereturn`).
* **Recreation / Recovery**:
  - Automatically re-created on startup if missing.
  - Price & fundamental tables are rebuilt via `mycase db update` or `mycase cache warm`.
  - PIT scores are repopulated by running `mycase pit update`.
  - Theme rebalances are synced from proposal files via `mycase theme sync`.

#### `data/cache.db` (49.2 MB)
* **Status**: **OBSOLETE / SAFE TO DELETE**
* **What it is**: The old standalone cache database used prior to database consolidation.
* **Why it is redundant**: 100% of its contents (`prices`, `fundamentals`, `cache_meta`, `pipeline_runs`, `index_picks`, `proposals`, `selections`) have been migrated into `data/mycase.db`. All Go code and server routes have been redirected to `mycase.db`.
* **Recreation**: Not needed. All data already exists inside `data/mycase.db`.

#### `data/pit_history.db` (4.4 MB)
* **Status**: **OBSOLETE / SAFE TO DELETE**
* **What it is**: The old standalone point-in-time research database.
* **Why it is redundant**: All `pit_runs` and `pit_candidate_scores` have been migrated into `data/mycase.db`. `pkg/pithistory/db.go` now defaults strictly to `data/mycase.db`.
* **Recreation**: Not needed. All data already exists inside `data/mycase.db`.

---

### 2.2 Active Theme CSVs

These CSV files represent the active golden copy of holdings and weights for your configured investment themes in `config/themes.json`.

| File | Status | Configured Theme Name | Target Weight | Consumers |
|---|---|---|---|---|
| `data/microsmall.csv` | **REQUIRED** | Theme Microsmall | 20% | `cmd/pipeline.go`, `pkg/themereturn`, `pkg/server` |
| `data/aitheme.csv` | **REQUIRED** | Theme AI Advice | 20% | `cmd/pipeline.go`, `pkg/themereturn`, `pkg/server` |
| `data/modularmicro.csv` | **REQUIRED** | Theme Micro Advice | 30% | `cmd/pipeline.go`, `pkg/themereturn`, `pkg/server` |
| `data/myall.csv` | **REQUIRED** | Theme KK Advise | 30% | `cmd/pipeline.go`, `pkg/themereturn`, `pkg/server` |
| `data/hydrogen.csv` | **REQUIRED** | Theme Hydrogen Nuclear | 0% (satellite) | `config/themes.json` |

* **Provenance**: Authored by user or generated via `mycase pick ... -o data/<theme>.csv` and finalized upon trade execution.
* **Recreation / Recovery**:
  - Backups of every past version are stored in `data/backups/<theme>/bk_YYYYMMDD_HHMMSS.csv`.
  - The exact constituent roster and weights are also stored in DuckDB table `theme_history`. If deleted, you can reconstruct any theme CSV by running `mycase theme show <theme>` or copying the latest backup from `data/backups/<theme>/`.

---

### 2.3 Stale / Dead Universe Files (In `data/` Root)

These files are experimental remnants from July/early September 2026 and are no longer referenced in active workflows:

| File | Size | Original Purpose | Why Redundant | Recreation / Origin |
|---|---|---|---|---|
| `data/sp500.csv` | 299 B | Test 20-stock US S&P 500 basket from July 2026. | Commented out in `config/pipeline.yaml`. US pipeline is inactive. | Recreated by `mycase pick --index sp500`. |
| `data/qtum.xlsx` | 12.1 KB | Sample Defiance Quantum ETF constituent sheet. | Used for testing Excel parsing (`pkg/stockpicker/loader.go`). | Downloadable from ETF provider website or testing fixtures. |
| `data/midcap150.csv` | 339 B | One-off pick snapshot from July 20, 2026. | Commented out in `config/pipeline.yaml`. MidCap 150 is not an active theme. | Recreated by `mycase pick --index midcap150`. |
| `data/midsmallmicro.csv` | 508 B | Combined universe snapshot from July 20, 2026. | Commented out in `config/pipeline.yaml`. | Recreated by `mycase pick --index midsmallmicro`. |
| `data/niftytotalmarket.csv` | 393 B | Transient output from Sep 9 test rebalance. | Not a theme golden copy. Complete holding weights already preserved in `data/backups/niftytotalmarket/bk_20260909_151725.csv` and DuckDB. | Recreated by `mycase pick --index niftytotalmarket`. |

* **Status**: **OBSOLETE / SAFE TO DELETE** (will be backed up in `archive_pre_cleanup_20260911.tar.gz`).

---

### 2.4 `data/universe_snapshots/` (12 KB, 5 files)

* **Status**: **CRITICAL / REQUIRED**
* **Files**:
  - `NIFTY50.csv`
  - `microcap250.csv`
  - `smallcap250.csv`
  - `microcap250_smallcap250.csv`
  - `microcap250,smallcap250_20260826.csv`
* **What it is & Why it exists**:
  - These files store the official index constituent lists from NSE (e.g., the 250 tickers in Nifty Microcap 250).
  - `pkg/pithistory/db.go` reads `NIFTY50.csv`, `microcap250.csv`, `smallcap250.csv`, and `microcap250_smallcap250.csv` during schema setup to seed the `index_constituents` table in `mycase.db`. This table defines which tickers belong to which sub-index, allowing the dynamic SQL view `v_pit_candidate_scores` to slice `niftytotalmarket` without duplicating score data.
  - Dated files like `microcap250,smallcap250_20260826.csv` are read by `pkg/universe/resolver.go` (`GetConstituentsForDate`) during historical rolling IC calibration to eliminate survivorship bias.
* **Provenance**:
  - Downloaded from NSE India official index constituent reports (or created via `mycase calibrate --save-snapshot`).
* **Recreation / Recovery**:
  - If deleted, run `mycase calibrate --index microcap250 --save-snapshot` or re-download official index CSVs from NSE India and place them in this folder.

---

### 2.5 Tradebook Files (`data/Tradebook/`) — Transferred to `myportfolio`

* **Status**: **TRANSFERRED TO EXTERNAL PROJECT (`myportfolio`)**
* **Previous Files**:
  - `orderBook_Equity_1788521644969.csv`
  - `tradebook-CBR420-EQ.csv`
* **Architecture Clarification**:
  - Raw broker tradebook parsing, lot accounting, and tax computations have been factored out into the dedicated **`myportfolio`** project (`/Users/raghavgarg/Projects/myGo/myportfolio`).
  - `mycase` no longer maintains duplicate raw broker exports in its local `data/` directory.
  - When historical fill verification or returns tracking is required, `mycase` connects directly to `myportfolio`'s consolidated database (`../myportfolio/data/portfolio.db` via `pkg/themereturn/db.go`).
* **Recovery / Location**:
  - Maintained under `/Users/raghavgarg/Projects/myGo/myportfolio/data/`.
  - Fresh exports can always be downloaded from Zerodha Console (**Reports** → **Tradebook**).

---

### 2.6 `data/cache/delivery/` (10 KB, 22 JSON files)

* **Status**: **REQUIRED (Scraper Cache)**
* **Files**: `AVALON.json`, `CASTROLIND.json`, `CHENNPETRO.json`, etc.
* **What it is & Why it exists**:
  - Stores up to 3 months of daily deliverable trading volume percentages for individual NSE stocks.
  - Used in Pillar 4 institutional accumulation scoring:
    $$\Delta \text{Delivery} = \text{SMA}_{5\text{D}}(\text{Delivery \%}) - \text{SMA}_{20\text{D}}(\text{Delivery \%})$$
  - NSE limits and throttles historical volume queries. Maintaining this JSON cache allows `scripts/fetch_nse_data.py` to only fetch new closed trading days and append them incrementally, preventing HTTP 429 / 403 blocks.
* **Provenance**: Automatically fetched and updated by `scripts/fetch_nse_data.py` during market data updates.
* **Recreation / Recovery**:
  - If deleted, the Python scraper will automatically recreate `data/cache/delivery/` and refetch history from NSE the next time `mycase db update` or `scripts/fetch_nse_data.py --mode delivery_data` runs.

---

### 2.7 `data/.cache/` (Hidden Directory, 6.07 MB, 547 JSON files)

* **Status**: **TRANSIENT (L1 HTTP Response Cache)**
* **Files**: `prices_NSE_<TICKER>_1y_<YYYY-MM-DD>.json`
* **What it is & Why it exists**:
  - Caches raw JSON responses from Yahoo Finance on disk.
  - When running `mycase pick` or `mycase returns` multiple times on the same day, `pkg/yfinance/prices.go` reads from these JSON files rather than repeating identical HTTP requests to Yahoo Finance.
  - `cmd/pipeline.go` automatically purges cache files from prior days upon startup.
* **Provenance**: Auto-generated by `pkg/yfinance/prices.go`.
* **Recreation / Recovery**:
  - Completely self-healing. If deleted, Yahoo Finance requests will fetch fresh data over the network and write new JSON cache files automatically. All persistent prices are already committed into DuckDB `prices` table.

---

### 2.8 `data/backups/` (20 KB, 52 files across 7 directories)

* **Status**: **REQUIRED (microsmall & niftytotalmarket) / OBSOLETE (dead universes)**
* **Subdirectories**:
  - `microsmall/` (33 files): Backups of `data/microsmall.csv` created automatically before every rebalance overwrite (`bk_YYYYMMDD_HHMMSS.csv`). **KEEP**. Used by `pkg/stockpicker/cooldown.go` to ensure exited stocks cannot be repurchased within the cooldown window (e.g. 30 days).
  - `niftytotalmarket/` (8 files): Backups of rebalance experiments on the Total Market universe. **KEEP**.
  - `midcap150/`, `nifty50/`, `qtum/`, `sp500/`, `midsmallmicro/`: Backups of abandoned July universes. **DELETE** (safe to clean).
* **Provenance**: Auto-generated by `pkg/stockpicker/io.go` whenever a portfolio CSV is updated.
* **Recreation / Recovery**:
  - Auto-generated during rebalancing. Cannot be recreated from thin air once deleted, but older rebalances are also archived in DuckDB table `theme_rebalances`.

---

### 2.9 `data/candidates/` (40 KB, 74 files)

* **Status**: **HYBRID (Keep proposals & active watchlists; prune test files)**
* **Contents**:
  1. **`data/candidates/proposals/` (48 files)**:
     - Proposed rebalance baskets (`YYYYMMDD_microsmall_multibagger_optim.csv`).
     - **Keep**: Serves as a historical paper trail. Used by `mycase theme sync` to backfill rebalances into `theme_rebalances`.
  2. **`data/candidates/index_picks/` (22 files)**:
     - Scored picks per index and incubator watchlists (`<index>_<method>_incubator.csv`).
     - **Prune**: Intermediate pick files from July are obsolete (now in DuckDB `index_picks`). Keep only the active `*_incubator.csv` watchlists.
  3. **Root files**:
     - `microcap250_multibagger.csv` & `small250_multibagger.csv`: **DELETE** (stale ad-hoc runs).
     - `hydrogen.csv`: **MOVE to `data/hydrogen.csv`** (so `config/themes.json` finds it).
* **Provenance**: Generated by `mycase pick` and `mycase pipeline`.
* **Recreation / Recovery**:
  - Intermediate picks are recreated by running `mycase pick --index <name> --method <method>`.
  - Proposals are recreated during pipeline optimization runs.

---

### 2.10 `data/pit_snapshots/` (6.55 MB, 31 JSON files)

* **Status**: **PARTIALLY REDUNDANT (Prune older snapshots)**
* **Files**: `<index>_<method>_<date>.json` (e.g. `niftytotalmarket_earlymb_2026-09-10.json`).
* **What it is & Why it exists**:
  - Point-in-time snapshot of every candidate's 4-pillar scores, regime multipliers, and weights for a single run.
  - `pkg/stockpicker/run.go` uses `LoadPreviousSnapshot` to load **only the single latest snapshot** to display the daily candidate diff report (`[Candidate Score & Roster Diff]`).
* **Why 30 files are redundant**:
  - Older snapshots (> 1 run old) are never read again.
  - Complete historical factor scores and weights for all dates are now permanently preserved in DuckDB `pit_candidate_scores`.
* **Recommendation**: Retain the latest 1 snapshot per index/method; delete older snapshots to reclaim **~5.5 MB**.
* **Recreation / Recovery**:
  - A new snapshot is automatically written on every `mycase pick` or `mycase pit update` run via `SaveRunSnapshot`.

---

### 2.11 `data/annual_reports/` (237.55 MB, 32 PDF files)

* **Status**: **QUALITATIVE RESEARCH / USER DECISION**
* **Files**: `ARVIND.pdf`, `CHENNPETRO.pdf`, `DIVISLAB.pdf`, `SIEMENS.pdf`, etc.
* **What it is & Why it exists**:
  - Downloaded annual report PDFs for portfolio companies.
  - Scanned by `scripts/check_customer_concentration.py` to extract customer concentration disclosures (e.g. single customer > 10% of revenue) to populate `config/customer_concentration.json`.
* **Provenance**: Manually downloaded from company investor relations or BSE/NSE disclosures.
* **Options**:
  - *Keep*: Retain in `data/annual_reports/` if you actively run scuttlebutt scans.
  - *Archive*: Compress into `data/backups/annual_reports.tar.gz` (~180 MB) to declutter the folder.
* **Recreation / Recovery**:
  - Can be re-downloaded from NSE India or corporate investor relations portals.

---

### 2.12 `data/logs/` (90 KB, 4 files)

* **Status**: **ACTIVE OPERATIONAL LOGS**
* **Files**: `mycase-YYYY-MM-DD.jsonl`
* **What it is & Why it exists**:
  - Daily structured JSONL application logs configured via `pkg/logging/logging.go`.
  - Captures execution timings, cache hits/misses, and pipeline audit events.
* **Recreation / Recovery**:
  - Automatically created on the fly as CLI commands run. Older logs can be pruned after 30 days.

---

## 3. Comprehensive Summary: What to Keep vs Delete

| Item | Current Size | Action | Reason |
|---|---|---|---|
| `data/mycase.db` | 34.06 MB | **KEEP** | Primary unified DuckDB database. |
| `data/cache.db` | 49.16 MB | **DELETE** | 100% migrated into `mycase.db`. |
| `data/pit_history.db` | 4.36 MB | **DELETE** | 100% migrated into `mycase.db`. |
| Active Theme CSVs (`microsmall`, `aitheme`, `modularmicro`, `myall`) | ~1 KB | **KEEP** | Golden copies for portfolio rebalancing. |
| `data/candidates/hydrogen.csv` | 36 B | **MOVE** | Move to `data/hydrogen.csv` to match `config/themes.json`. |
| Stale Universes (`sp500.csv`, `qtum.xlsx`, `midcap150.csv`, `midsmallmicro.csv`, `niftytotalmarket.csv`) | ~14 KB | **DELETE** | Dead experiments from July/Sep. |
| `data/universe_snapshots/` | 12 KB | **KEEP** | Seeds `index_constituents` table and bias-free backtesting. |
| `data/Tradebook/` | 0.13 MB | **MOVED** | Transferred to `myportfolio` (portfolio.db). Deleted from `mycase`. |
| `data/cache/delivery/` | 10 KB | **KEEP** | Prevents NSE rate limits during volume delta scoring. |
| `data/.cache/` | 6.07 MB | **AUTO-MANAGE** | Transient HTTP cache, auto-purged daily by pipeline. |
| `data/backups/microsmall/` & `niftytotalmarket/` | 20 KB | **KEEP** | Required for cooldown enforcement and recovery. |
| Stale Backups (`midcap150`, `nifty50`, `qtum`, `sp500`, `midsmallmicro`) | ~5 KB | **DELETE** | Remnants of abandoned universes. |
| `data/candidates/proposals/` | 25 KB | **KEEP** | Paper audit trail of historical rebalance proposals. |
| `data/candidates/index_picks/` | 10 KB | **PRUNE** | Keep active `*_incubator.csv`; delete old July pick CSVs. |
| Ad-hoc candidates (`microcap250_multibagger.csv`, `small250_multibagger.csv`) | ~1 KB | **DELETE** | Stale test files from July. |
| `data/pit_snapshots/` | 6.55 MB | **PRUNE** | Keep only latest snapshot per index (~1 MB); delete 30 old files (-5.5 MB). |
| `data/annual_reports/` | 237.55 MB | **USER DECISION** | Keep if running scuttlebutt analysis; or archive to tarball. |
| `data/logs/` | 0.09 MB | **KEEP** | Active daily execution diagnostics. |
| `.DS_Store` across folders | ~28 KB | **DELETE** | Unnecessary macOS metadata. |

---

## 4. Pre-Cleanup Safety Protocol

Before any file is deleted, a complete timestamped tarball will be saved:
```bash
tar -czf data/backups/archive_pre_cleanup_20260911.tar.gz \
    data/cache.db \
    data/pit_history.db \
    data/midcap150.csv \
    data/midsmallmicro.csv \
    data/sp500.csv \
    data/qtum.xlsx \
    data/niftytotalmarket.csv \
    data/candidates/microcap250_multibagger.csv \
    data/candidates/small250_multibagger.csv \
    data/backups/midcap150 \
    data/backups/midsmallmicro \
    data/backups/nifty50 \
    data/backups/qtum \
    data/backups/sp500
```
This guarantees that any item can be restored instantly with a single extraction command:
```bash
tar -xzf data/backups/archive_pre_cleanup_20260911.tar.gz
```

---

## 5. Executed Cleanup & Migration Walkthrough (September 11, 2026)

### 5.1 Actions Executed
1. **Safety Backup Created**:
   - Packaged and verified `data/backups/archive_pre_cleanup_20260911.tar.gz` (15 MB compressed).
   - Confirmed archive integrity with `tar -tzf`.
2. **Removed Legacy Databases (-53.52 MB)**:
   - `data/cache.db` (49.16 MB) — Permanently deleted.
   - `data/pit_history.db` (4.36 MB) — Permanently deleted.
   - All 14 tables and views are active in `data/mycase.db`.
3. **Pruned Historical PIT Snapshots (-4.99 MB)**:
   - Removed 23 older daily JSON files from `data/pit_snapshots/`.
   - Retained the **single latest snapshot** per strategy/index for daily diff reporting:
     - `NIFTY50_earlymb_2026-08-28.json`
     - `microcap250_multibagger_2026-09-11.json`
     - `microcap250_smallcap250_earlymb_2026-08-28.json`
     - `microsmall_multibagger_2026-09-11.json`
     - `niftytotalmarket_earlymb_2026-09-10.json`
     - `niftytotalmarket_multibagger_2026-09-09.json`
     - `small250_multibagger_2026-09-11.json`
     - `sp500_earlymb_2026-08-28.json`
4. **Deleted Obsolete Root Universes & Stale Tests**:
   - Deleted root files: `sp500.csv`, `qtum.xlsx`, `midcap150.csv`, `midsmallmicro.csv`, `niftytotalmarket.csv`.
   - Deleted dead candidate tests: `data/candidates/microcap250_multibagger.csv`, `data/candidates/small250_multibagger.csv`.
   - Deleted obsolete July index picks: `qtum_multibagger.csv`, `midcap150_multibagger.csv`, `sp500_*.csv`, `nifty50_*.csv`, `nifty200_value.csv`.
   - Removed abandoned backup directories: `data/backups/{midcap150,midsmallmicro,nifty50,qtum,sp500}/`.
   - Cleaned all `.DS_Store` metadata files across `data/`.
5. **Reconciled Hydrogen Theme**:
   - Moved `data/candidates/hydrogen.csv` to `data/hydrogen.csv` so it matches `config/themes.json`.
6. **Code String Updates**:
   - Updated startup error messages from `data/cache.db` to `data/mycase.db` in `cmd/cache.go`, `cmd/pipeline_history.go`, `cmd/pipeline_diff.go`, and `cmd/pipeline_show.go`.
7. **Transferred Tradebook to `myportfolio`**:
   - `data/Tradebook/` removed from `mycase/data/` as broker trade ingestion is now managed in `myportfolio` (`../myportfolio/data/portfolio.db`).

### 5.2 Before vs After Comparison

| Metric | Before Cleanup | After Cleanup | Net Change |
|---|---|---|---|
| **Root Database Files** | 3 (`mycase.db`, `cache.db`, `pit_history.db`) | 1 (`mycase.db`) | -2 files (-53.52 MB) |
| **Root Theme CSVs** | 4 active + 5 stale/dead files | 5 active golden themes | Aligned with `config/themes.json` |
| **PIT Snapshots** | 31 files (6.55 MB) | 8 files (1.56 MB) | -23 files (-4.99 MB) |
| **Backup Subdirectories** | 7 folders | 2 folders (`microsmall`, `niftytotalmarket`) + 1 tarball | Cleaned 5 dead folders |
| **Index Picks Directory** | 22 files (polluted with July tests) | 13 clean active watchlists & picks | -9 stale test files |

### 5.3 Health Checks & Verification Results

| Check / Verification | Target | Result | Status |
|---|---|---|---|
| **Codebase Build** | `go build ./...` | Compiled with 0 errors | **PASSED** |
| **Unit Test Suites** | `go test ./pkg/... ./cmd/...` | All test suites passing | **PASSED** |
| **Database Integrity** | `go run . db stats` | 14 tables/views online, 868,776 records | **PASSED** |
| **Active Theme Resolution** | `go run . theme show microsmall` | Correctly rendered 20 holdings [v22] | **PASSED** |
| **Dynamic Sub-Index PIT** | `go run . pit stats --index microcap250` | Dynamic SQL view computed stats | **PASSED** |
| **Master Universe PIT** | `go run . pit stats --index niftytotalmarket` | Master table computed stats | **PASSED** |

