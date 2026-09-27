# Data & Report Subsystem Architecture & File Inventory

This document provides a comprehensive, file-by-file and directory-by-directory audit of the **`data/`** and **`report/`** subsystems in `mycase`. It explains:
1. **What each file/folder is** and why it exists.
2. **Provenance**: Where it came from (which CLI command, background job, scraper, or user action generated it).
3. **Consumers**: Which Go packages or Python scripts read from it.
4. **Status**: `REQUIRED`, `TRANSIENT`, `ACTIVE`, or `ARCHIVED`.
5. **Recreation / Recovery Guide**: If deleted or lost, exactly how to regenerate, re-source, or recover it.

---

## 1. Directory Tree Overview

```
data/                                   # Quantitative Data & Analytical Engine
├── mycase.db                           # [CRITICAL] Consolidated DuckDB master analytical database
│
├── aitheme.csv                         # [REQUIRED] Golden theme copy (Theme AI Advice)
├── earlymb_live.csv                    # [REQUIRED] Golden theme copy (Early Multibagger Live Satellite)
├── hydrogen.csv                        # [REQUIRED] Golden theme copy (Theme Hydrogen Nuclear)
├── microsmall.csv                      # [REQUIRED] Golden theme copy (Theme Microsmall Core)
├── modularmicro.csv                    # [REQUIRED] Golden theme copy (Theme Micro Advice)
├── myall.csv                           # [REQUIRED] Golden theme copy (Theme KK Advise)
├── us_microsmall.csv                   # [REQUIRED] Golden portfolio copy (US S&P 500 / Quality Momentum)
│
├── state/                              # [REQUIRED] 1. Runtime Process & Service State
│   ├── daemon_state.json               # Drift daemon last-check outcome, drift score, and alert count
│   ├── daemon.pid                      # PID lockfile written during daemon.RunLoop execution
│   └── scheduler_state.json            # Scheduler completion tracking & consecutive failure streaks
│
├── cache/                              # [REQUIRED] 2. Unified Caching (No hidden .cache)
│   ├── prices/                         # Intraday Yahoo Finance price bar JSONs (transient L1 HTTP cache)
│   ├── delivery/                       # 3-month NSE deliverable volume history JSONs per ticker
│   └── customer_concentration.json     # Scanned Ind AS 108 segment customer concentration disclosures
│
├── candidates/                         # [ACTIVE] 3. All Strategy & Candidate Outputs
│   ├── proposals/                      # Proposed baskets from rebalance runs (historical audit trail)
│   ├── index_picks/                    # Scored picks per index and active incubator watchlists (*_incubator.csv)
│   └── snapshots/                      # (From pit_snapshots) Latest daily candidate factor score JSON snapshots
│
├── universe/                           # [REQUIRED] 4. Canonical Constituent Rosters (From universe_snapshots)
│   ├── NIFTY50.csv                     # Seeds index_constituents in mycase.db
│   ├── microcap250.csv                 # Seeds index_constituents in mycase.db
│   ├── smallcap250.csv                 # Seeds index_constituents in mycase.db
│   ├── microcap250_smallcap250.csv     # Seeds index_constituents in mycase.db
│   └── microcap250,smallcap250_*.csv   # Immutable dated rosters for survivorship-free calibration
│
├── raw/                                # [ACTIVE] 5. Raw API Wire Response Archive (mycase raw)
│   └── <source>__<endpoint>__*.json    # Wire captures from YFinance, Schwab, and SEC EDGAR (bounded to 512 MB)
│
├── logs/                               # [ACTIVE] 6. Operational Diagnostics & Execution Logs
│   ├── mycase-YYYY-MM-DD.jsonl         # Daily structured JSONL application logs
│   ├── scheduler-runs.log              # Run-now and scheduler trigger execution summaries
│   └── scheduler.log                   # launchd / systemd timer stdout and stderr diagnostic log
│
└── backups/                            # [REQUIRED] 7. Safety Snapshots, Cooldown Registry & Archives
    ├── microsmall/                     # Historical rebalance backups (bk_YYYYMMDD_HHMMSS.csv) - Cooldown registry
    ├── us_microsmall/                  # Historical rebalance backups for US portfolio
    ├── legacy_config/                  # Archived pre-consolidation JSON configuration files
    ├── archive_pre_cleanup_20260911.tar.gz  # Archived retired databases and dead July universes
    ├── archive_pre_migration_20260914.tar.gz# Compressed pre-migration DuckDB snapshots (100 MB -> 29 MB)
    └── archive_pit_snapshots_20260925.tar.gz# Compressed historical daily PIT snapshots

report/                                 # Qualitative Research & Human Audit Hub
├── annual_reports/                     # [QUALITATIVE RESEARCH] Downloaded company annual report PDFs (246 MB)
│   └── *.pdf                           # Target PDFs scanned by scripts/check_customer_concentration.py
├── <theme>_<strategy>/
│   ├── executions/                     # Portfolio rebalance audit & selection reasons (*.txt)
│   ├── research/                       # Automated Scuttlebutt qualitative reports (*_scuttlebutt.txt)
│   └── simulations/                    # Stop-loss and monitoring simulations (*.txt)
└── ...

execution/                              # Live Broker Execution & Operational State (Zerodha / Schwab)
├── india/
│   ├── orders/                         # Successfully placed live orders (Order_*.txt)
│   ├── errors/                         # Failed order logs & retry JSON payloads for `mycase retry`
│   └── holdings/                       # Real-time broker account holdings snapshots (holding_*.txt)
└── us/
    ├── orders/
    ├── errors/
    └── holdings/
```

---

## 2. Granular Inventory: Files & Directories

### 2.1 Database Files

#### `data/mycase.db` (~69.5 MB)
* **Status**: **CRITICAL / REQUIRED**
* **What it is**: The unified, master DuckDB analytical database for the entire project. It contains all 14 tables and views:
  - **Market data**: `prices`, `fundamentals`, `cache_meta`, `holidays`
  - **Research & PIT**: `pit_runs`, `pit_candidate_scores`, `v_pit_candidate_scores`, `v_pit_runs`, `index_constituents`
  - **Pipeline & Portfolios**: `pipeline_runs`, `index_picks`, `proposals`, `selections`
  - **Themes**: `theme_rebalances`, `theme_history`
* **Provenance**: Initialized and maintained by `pkg/themedb`, `pkg/cache`, and `pkg/pithistory`. Consolidated from legacy `cache.db` and `pit_history.db`.
* **Consumers**: Core CLI and packages (`main.go`, `cmd/*`, `pkg/cache`, `pkg/pithistory`, `pkg/themedb`, `pkg/server`, `pkg/themereturn`).
* **Recreation / Recovery**:
  - Automatically initialized on startup if missing.
  - Price & fundamental tables are rebuilt via `mycase db update` or `mycase cache warm`.
  - PIT scores are repopulated by running `mycase pit update`.
  - Theme rebalances are synced from proposal files via `mycase theme sync`.

---

### 2.2 Active Theme & Portfolio CSVs

These CSV files represent the active golden copy of holdings and weights for configured investment themes:

| File | Status | Configured Theme / Strategy | Target Weight | Consumers |
|---|---|---|---|---|
| `data/microsmall.csv` | **REQUIRED** | Theme Microsmall (Microcap 250 + Smallcap 250) | 20% | `cmd/pipeline.go`, `pkg/themereturn`, `pkg/server` |
| `data/aitheme.csv` | **REQUIRED** | Theme AI Advice | 20% | `cmd/pipeline.go`, `pkg/themereturn`, `pkg/server` |
| `data/modularmicro.csv` | **REQUIRED** | Theme Micro Advice | 30% | `cmd/pipeline.go`, `pkg/themereturn`, `pkg/server` |
| `data/myall.csv` | **REQUIRED** | Theme KK Advise | 30% | `cmd/pipeline.go`, `pkg/themereturn`, `pkg/server` |
| `data/hydrogen.csv` | **REQUIRED** | Theme Hydrogen Nuclear | 0% (satellite) | `config/themes.json`, `pkg/themedb` |
| `data/earlymb_live.csv` | **REQUIRED** | Early Multibagger Live Satellite Basket | Satellite | `config/pipeline.yaml`, `pkg/stockpicker` |
| `data/us_microsmall.csv` | **REQUIRED** | US S&P 500 Quality Momentum Portfolio | US Account | `config/pipeline.yaml`, `pkg/stockpicker` |

* **Rebalance Safety Guarantee**: Every time a theme (such as `microsmall.csv`) is updated via `mycase pipeline` or rebalance execution, a timestamped backup is automatically written to `data/backups/<theme>/bk_YYYYMMDD_HHMMSS.csv` before modification.
* **Recreation / Recovery**:
  - Historical snapshots of every past version are preserved in `data/backups/<theme>/`.
  - The exact constituent roster and weights are also stored in DuckDB table `theme_history`. You can reconstruct any theme CSV by running `mycase theme show <theme>` or copying the latest backup from `data/backups/<theme>/`.

---

### 2.3 Runtime State Subsystem (`data/state/`)

Isolates process and service runtime state files away from analytical datasets:

| File | Status | Purpose | Consumer |
|---|---|---|---|
| `data/state/daemon_state.json` | **REQUIRED** | Persists drift check timestamp, drift magnitude, and alert dispatch count. | `pkg/daemon`, `mycase daemon status` |
| `data/state/daemon.pid` | **TRANSIENT** | Process ID lockfile created during `daemon.RunLoop` execution. | `pkg/daemon`, `mycase daemon stop` |
| `data/state/scheduler_state.json` | **REQUIRED** | Records the last trading day each cadence completed (`drift`, `eod`, `rebalance`) and consecutive failure counts. Prevents double-running on the same day across machine reboots. | `pkg/scheduler`, `mycase scheduler status` |

* **Provenance**: Managed autonomously by `pkg/daemon/daemon.go` and `pkg/scheduler/state.go`.
* **Backward Compatibility**: Both packages check `data/state/` first and fall back seamlessly to legacy paths in `data/` if old state exists.

---

### 2.4 Unified Caching Subsystem (`data/cache/`)

Consolidates all disk caches into a single visible tree:

| Subpath | Status | Purpose | Consumer / Script |
|---|---|---|---|
| `data/cache/prices/` | **TRANSIENT** | L1 Yahoo Finance intraday price bar JSONs (`prices_NSE_*.json`). Auto-purged daily by pipeline. | `pkg/yfinance/prices.go`, `cmd/pipeline.go` |
| `data/cache/delivery/` | **REQUIRED** | 3-month NSE deliverable volume history JSONs per ticker. Prevents rate-limits during Pillar 4 volume scoring. | `scripts/fetch_nse_data.py`, `pkg/stockpicker` |
| `data/cache/customer_concentration.json` | **REQUIRED** | Cached Ind AS 108 customer concentration scan disclosures. | `scripts/check_customer_concentration.py` |

---

### 2.5 Candidate & Strategy Outputs (`data/candidates/`)

Consolidates candidate watchlists, rebalance proposals, and scoring snapshots:

| Subpath | Status | Purpose | Consumer |
|---|---|---|---|
| `data/candidates/proposals/` | **ACTIVE** | Proposed rebalance baskets (`YYYYMMDD_microsmall_multibagger_optim.csv`). Used by `mycase theme sync` to backfill rebalances. | `cmd/pipeline.go`, `pkg/themedb` |
| `data/candidates/index_picks/` | **ACTIVE** | Scored picks per index and active incubator watchlists (`<index>_<method>_incubator.csv`). | `cmd/pick.go`, `pkg/stockpicker` |
| `data/candidates/snapshots/` | **ACTIVE** | Point-in-Time factor score snapshots per strategy and index. Read by `LoadPreviousSnapshot` for daily candidate diff reporting (`[Candidate Score & Roster Diff]`). | `pkg/stockpicker/snapshot.go` |

---

### 2.6 Canonical Constituent Rosters (`data/universe/`)

Renamed from `data/universe_snapshots/` to reflect its role as the **Canonical Master Universe**:

| File | Status | Purpose | Consumer |
|---|---|---|---|
| `data/universe/niftytotalmarket.csv` | **CRITICAL** | Enriched official NSE constituent roster (Company Name, Industry, Symbol, Series, ISIN Code). Serves as the authoritative local air-gapped mirror for Nifty Total Market runs. | `pkg/stockpicker/loader.go` |
| `data/universe/NIFTY50.csv` | **CRITICAL** | Canonical constituent roster for Nifty 50. Seeds `index_constituents` table in `mycase.db`. | `pkg/pithistory/db.go` |
| `data/universe/microcap250.csv` | **CRITICAL** | Canonical constituent roster for Nifty Microcap 250. Seeds `index_constituents` table in `mycase.db`. | `pkg/pithistory/db.go` |
| `data/universe/smallcap250.csv` | **CRITICAL** | Canonical constituent roster for Nifty Smallcap 250. Seeds `index_constituents` table in `mycase.db`. | `pkg/pithistory/db.go` |
| `data/universe/microcap250_smallcap250.csv` | **CRITICAL** | Combined Micro+Small constituent roster. Seeds `index_constituents` table in `mycase.db`. | `pkg/pithistory/db.go` |
| `data/universe/microcap250,smallcap250_*.csv` | **CRITICAL** | Dated rosters used by `pkg/universe/resolver.go` (`GetConstituentsForDate`) during rolling IC calibration to eliminate survivorship bias. | `pkg/universe/resolver.go` |

---

### 2.7 API Wire Capture Subsystem (`data/raw/`) (~350 MB, 12,440 files)

* **Status**: **ACTIVE (API Response Archive)**
* **What it is**: Raw wire JSON responses captured at API client chokepoints (`schwab.Client.executeRequest`, `executeYFinanceRequest`) via `pkg/rawcapture`.
* **Offline Replay**: Setting `MYCASE_REPLAY=1` serves the newest matching archived body as a synthetic 200, bypassing the network, token fetch, and rate limiters entirely.
* **Retention Controls**: Bounded by age ceiling (14 days default) and size ceiling (512 MB default). Pruned via `mycase raw prune`.

---

### 2.8 Qualitative Research & Annual Reports (`report/annual_reports/`) (246 MB, 35 PDFs)

* **Status**: **QUALITATIVE RESEARCH INPUTS**
* **Location**: Moved from `data/annual_reports/` to `report/annual_reports/` to separate qualitative research materials from the quantitative data engine.
* **Files**: Downloaded annual report PDFs (`NETWEB.pdf`, `ACUTAAS.pdf`, `CHENNPETRO.pdf`, etc.).
* **Consumers**: Scanned by `scripts/check_customer_concentration.py` to extract customer concentration disclosures.

---

### 2.9 Operational Diagnostics (`data/logs/`) (~600 KB)

* **Status**: **ACTIVE DIAGNOSTIC LOGS**
* **Files**:
  - `mycase-YYYY-MM-DD.jsonl`: Daily structured JSONL application logs.
  - `scheduler-runs.log`: Execution summaries for automated scheduler runs.
  - `scheduler.log`: Standard output and standard error stream for the background scheduler timer (launchd / systemd).

---

### 2.10 Backups & Cooldown Registry (`data/backups/`) (~45 MB)

* **Status**: **REQUIRED (Cooldown Registry & Forensic Audit)**
* **Contents**:
  - `microsmall/` (41 files): Preserved rebalance backups enforcing the 30-day cooldown window via `pkg/stockpicker/cooldown.go`.
  - `us_microsmall/`: US portfolio rebalance backups.
  - `legacy_config/`: Historical configuration backups before YAML unification.
  - `archive_pre_cleanup_20260911.tar.gz` (15 MB): Retired databases and legacy universes.
  - `archive_pre_migration_20260914.tar.gz` (29 MB): Compressed pre-migration DuckDB snapshots.
  - `archive_pit_snapshots_20260925.tar.gz` (1.2 MB): Compressed historical daily factor snapshots.

---

## 3. Comprehensive Summary: The 8 Pillars

| Pillar | Subpath | Size | Status | Primary Function |
|---|---|---|---|---|
| **Master Database** | `data/mycase.db` | ~69.5 MB | **CRITICAL** | Master analytical storage engine |
| **Golden Portfolios** | `data/*.csv` (7 files) | < 30 KB | **CRITICAL** | Authoritative theme holdings |
| **Runtime State** | `data/state/` | < 10 KB | **REQUIRED** | Daemon and scheduler process state |
| **Unified Caches** | `data/cache/` | ~16 MB | **REQUIRED** | Prices, volume delivery, concentration |
| **Candidate Outputs** | `data/candidates/` | ~3 MB | **ACTIVE** | Proposals, index picks, and PIT snapshots |
| **Universe Rosters** | `data/universe/` | ~70 KB | **CRITICAL** | Canonical & enriched index constituent rosters |
| **Wire Captures** | `data/raw/` | ~350 MB | **ACTIVE** | API wire response archive |
| **Diagnostic Logs** | `data/logs/` | ~600 KB | **ACTIVE** | Structured JSONL logs and timer logs |
| **Research Hub** | `report/annual_reports/` | 246 MB | **RESEARCH** | Corporate annual report PDFs for scuttlebutt |

---

## 4. Verification & Health Check Results

| Check / Verification | Target | Result | Status |
|---|---|---|---|
| **Root Cleanliness** | `ls -la data/` | Only `mycase.db` and active golden theme CSVs reside at root | **PASSED** |
| **Codebase Build** | `go build ./...` | Compiled cleanly with 0 errors | **PASSED** |
| **Unit Test Suites** | `go test ./...` | All packages passed without regression | **PASSED** |
| **Daemon State Resolution** | `go run . daemon status` | Successfully read state from `data/state/daemon_state.json` | **PASSED** |
| **Scheduler State Resolution** | `go run . scheduler status` | Successfully read state from `data/state/scheduler_state.json` | **PASSED** |
| **Universe Constituent Seeding** | `go run . pit stats --index microcap250` | Dynamic SQL view resolved from `data/universe/` | **PASSED** |
| **Customer Concentration Scanner** | `python3 scripts/check_customer_concentration.py ACUTAAS` | Resolved PDF from `report/annual_reports/` | **PASSED** |
| **Rebalance Backup Protection** | `data/backups/microsmall/` | 41 historical rebalance backups preserved for cooldown enforcement | **PASSED** |
