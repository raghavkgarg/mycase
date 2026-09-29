# Report Subsystem Architecture & File Inventory (`report/`)

This document provides a comprehensive, file-by-file and directory-by-directory audit of the **`report/`** subsystem in `mycase`. It explains:
1. **What each report is** and why it exists.
2. **Directory Taxonomy & Standard Structure**: The standardized `<target>_<strategy>/` hierarchy and its three subdirectories (`executions/`, `research/`, `simulations/`), plus the central `annual_reports/` qualitative library.
3. **Provenance**: Where each artifact originates (which CLI command, pipeline phase, or background routine generated it).
4. **Consumers**: Which Go packages, Python scripts, or human audit workflows read it.
5. **Status**: `CRITICAL`, `ACTIVE`, `RESEARCH`, or `SIMULATION`.
6. **Recreation, Recovery & Retention Guide**: How reports are regenerated, how cooldown hysteresis depends on execution reports, and safe pruning guidelines.

---

## 1. Directory Tree Overview

```
report/                                         # Qualitative Research & Human Audit Hub (~250 MB)
├── annual_reports/                             # [RESEARCH INPUTS] Downloaded primary company annual report PDFs (246 MB)
│   ├── ACUTAAS.pdf                             # Scanned for Ind AS 108 segment customer concentration disclosures
│   ├── CHENNPETRO.pdf
│   ├── MCX.pdf
│   ├── NETWEB.pdf
│   └── ... (33 PDFs total)
│
├── niftytotalmarket_multibagger/               # [ACTIVE] Nifty Total Market with Multibagger Growth Preset (1.3 MB)
│   ├── executions/                             # Formal rebalance runs & audit logs
│   │   ├── YYYYMMDD_01_selection_reasons.txt   # [CRITICAL] Full pass/fail filter & selection diagnostic report
│   │   ├── YYYYMMDD_02_comparison.txt          # [ACTIVE] Incumbent golden copy vs proposed rebalance delta
│   │   └── YYYYMMDD_03_portfolio_report.txt    # [ACTIVE] 5-Factor fundamental & technical thesis per holding
│   ├── research/                               # Qualitative deep dives & intrinsic valuation
│   │   └── YYYYMMDD_scuttlebutt.txt            # [ACTIVE] 16-Point automated Scuttlebutt & MOS valuation
│   └── simulations/                            # Point-in-time policy simulations
│       └── YYYYMMDD_HHMMSS_monitoring.txt      # [SIMULATION] Simulated portfolio churn, returns & alpha efficiency
│
├── niftytotalmarket_earlymb/                   # [ACTIVE] Nifty Total Market with Early Multibagger Preset (1.6 MB)
│   ├── executions/                             # Selection reasons, rebalance comparisons, and portfolio reports
│   └── research/                               # Scuttlebutt reports with VCP tightness, RVOL, and delivery checks
│
├── niftytotalmarket_fairprice/                 # [ACTIVE] Nifty Total Market with Intrinsic Value Ensemble (56 KB)
│   ├── executions/                             # Margin-of-safety rankings and selection rationale
│   └── research/                               # Comprehensive DCF, EPV, Graham, Rel, and PEG model breakdowns
│
├── microsmall_multibagger/                     # [ACTIVE] Theme Microsmall Core Portfolio (1.4 MB)
│   ├── executions/                             # Rebalance audits, comparison tables, and holding explanation reports
│   ├── research/                               # Scuttlebutt qualitative files
│   └── simulations/                            # Historical monitoring policy backtests
│
├── microsmall_earlymb/                         # [ACTIVE] Theme Microsmall with Early Multibagger Preset (8 KB)
│   └── executions/                             # Rebalance comparison and portfolio reports
│
├── smallcap250_multibagger/                    # [ACTIVE] Nifty Smallcap 250 with Multibagger Preset (84 KB)
│   ├── executions/                             # Smallcap selection reasons
│   └── research/                               # Smallcap qualitative scuttlebutt reports
│
├── sp500_multibagger/                          # [ACTIVE] US S&P 500 Multibagger Model (208 KB)
│   ├── executions/                             # US selection reasons, comparisons, and portfolio reports
│   ├── research/                               # SEC EDGAR qualitative checks & Scuttlebutt notes
│   └── simulations/                            # US portfolio monitoring simulations
│
├── sp500_earlymb/                              # [ACTIVE] US S&P 500 Institutional Accumulation Model (92 KB)
│   └── executions/                             # US Stage-1 breakout selection reasons
│
├── us_microsmall_multibagger/                  # [ACTIVE] US S&P 500 Quality Momentum Portfolio Hub (48 KB)
│   ├── executions/                             # Comparison and portfolio reports
│   └── simulations/                            # US portfolio policy simulations
│
├── portfolio_earlymb/                          # [ACTIVE] Portfolio Early Multibagger Execution Hub (4 KB)
│   └── executions/                             # Execution reports
│
└── live_earlymb/                               # [ACTIVE] Live Satellite Early Multibagger Basket Hub (4 KB)
    └── executions/                             # Live satellite portfolio explanation reports
```

---

## 2. Architectural Separation & Directory Taxonomy

### 2.1 Separation of Quantitative Data vs. Qualitative Audit
In the `mycase` platform architecture, runtime responsibilities are cleanly bifurcated:
- **`data/` (Quantitative Engine & State)**: Contains binary relational databases ([`data/mycase.db`](file:///Users/raghavgarg/Projects/myGo/mycase/data/mycase.db)), machine-readable candidate CSVs ([`data/candidates/`](file:///Users/raghavgarg/Projects/myGo/mycase/data/candidates)), runtime process state ([`data/state/`](file:///Users/raghavgarg/Projects/myGo/mycase/data/state)), and raw API wire captures ([`data/raw/`](file:///Users/raghavgarg/Projects/myGo/mycase/data/raw)).
- **`report/` (Qualitative Research & Human Audit Hub)**: Contains human-readable text documents and corporate PDF filings. It serves as the primary auditable paper trail explaining **why** positions were entered, weighted, exited, or rejected.

### 2.2 Standardized Directory Taxonomy
Every strategy run organizes its output under a standardized root directory:
```
report/<target>_<strategy>/
```
Where:
- `<target>`: The constituent universe or golden portfolio name (e.g., `niftytotalmarket`, `microsmall`, `smallcap250`, `sp500`, `live`).
- `<strategy>`: The strategy preset or scoring methodology (e.g., `multibagger`, `earlymb`, `fairprice`, `balanced`).

Within each target-strategy hub, three subdirectories isolate distinct analytical phases:
1. **`executions/`**: Rebalance records and selection decisions. Files follow the strict sequencing prefix `YYYYMMDD_01_`, `YYYYMMDD_02_`, and `YYYYMMDD_03_`.
2. **`research/`**: Deep qualitative analysis, 5-model intrinsic valuation breakdowns, earnings calendars, and scuttlebutt flags (`YYYYMMDD_scuttlebutt.txt`).
3. **`simulations/`**: Algorithmic policy backtests, churn modeling, and stop-loss monitoring runs (`YYYYMMDD_HHMMSS_monitoring.txt`).

In addition, **`report/annual_reports/`** acts as a centralized library of corporate PDF filings for qualitative Ind AS 108 forensic scans.

---

## 3. Granular Report Inventory & Specifications

### 3.1 Execution Reports Subsystem (`executions/`)

Execution reports provide the auditable paper trail for every portfolio adjustment. They are generated in sequential stages during pipeline execution.

```
       [ Stage 1: Pick ]
               │
               ▼
   YYYYMMDD_01_selection_reasons.txt   ──► Read by pkg/stockpicker/cooldown.go
               │                             (Enforces 30-Day Anti-Churn Cooldown)
               ▼
     [ Stage 2: Rebalance ]
               │
               ▼
      YYYYMMDD_02_comparison.txt        ──► Presented to operator before golden update
               │
               ▼
      [ Stage 3: Report ]
               │
               ▼
   YYYYMMDD_03_portfolio_report.txt    ──► Full fundamental & technical holding thesis
```

#### 1. `YYYYMMDD_01_selection_reasons.txt`
* **Status**: **CRITICAL / ACTIVE AUDIT TRAIL**
* **File Format**: ASCII formatted text table and diagnostics
* **Size**: ~15 KB – 80 KB per run (depends on pool size)
* **Provenance**: Generated by [`pkg/selectiontracker.Tracker.SaveReport`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/selectiontracker/tracker.go#L322) during:
  - `mycase pick` ([`cmd/pick.go`](file:///Users/raghavgarg/Projects/myGo/mycase/cmd/pick.go#L244))
  - `mycase pipeline` ([`cmd/pipeline.go`](file:///Users/raghavgarg/Projects/myGo/mycase/cmd/pipeline.go#L718))
* **Contents**:
  1. **Metadata Header**: Target index, strategy preset, timestamp `Based on:` (e.g., `2026-09-25 21:00:00 IST`), and generated timestamp.
  2. **Waterfall Summary**: Initial constituent count, Stage 1 Safety Filter pass/fail counts, Macro Regime score cutoffs, Sector Cap exclusions, Rank limit eliminations, and Final Selected count.
  3. **Selected Stocks Table**: Ticker, Sector, Raw Score, Effective Score, Raw Rank, Weight Decided, Result Prev -> Coming, and Selection Reason.
  4. **Removed Active Holdings (Exits) Table**: Tickers evicted from the portfolio, accompanied by explicit exit triggers (e.g., *Level-3 Sentry trend rupture*, *Price < 0.95x 200-SMA*, *Peak drawdown exceeds -20%*, or *Rank deterioration*).
  5. **Rejected New Candidates Table**: Candidates rejected due to hysteresis cooldowns, minimum entry quality score hurdles, or sector concentration caps.
  6. **Candidate Diagnostics**: Exhaustive per-stock breakdown showing why every constituent in the universe passed or failed each filter.
* **Consumers**:
  - **Programmatic Consumer**: [`pkg/stockpicker/cooldown.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/cooldown.go#L33-L47). The function `GetExitDatesFromHistoricalReports()` uses `filepath.Glob("report/<goldenBase>_*/executions/*_01_selection_reasons.txt")` and `extractExitsFromReport()` to identify historical exit dates within the last 30 days. This enforces the anti-churn cooldown gate unless the stock qualifies for a high-conviction rank bypass.
  - **CLI Viewer**: [`cmd/pick.go`](file:///Users/raghavgarg/Projects/myGo/mycase/cmd/pick.go#L244) loads and displays cached selection reason reports when re-running without network access.
  - **Portfolio Manager / Auditor**: Discretionary validation before executing live orders.
* **Recreation / Recovery**:
  - Re-run `mycase pick --index <index> --method <method>` or `mycase pipeline`.

#### 2. `YYYYMMDD_02_comparison.txt`
* **Status**: **ACTIVE (REBALANCE DELTA AUDIT)**
* **File Format**: ASCII formatted delta table
* **Size**: ~1.5 KB – 3 KB per run
* **Provenance**: Generated by [`pkg/csvloader.PrintComparisonReport`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/csvloader/pipeline_csv.go#L268) during `mycase pipeline` (Rebalance phase) and [`pkg/autopilot`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/autopilot/autopilot.go#L376).
* **Contents**:
  1. **Portfolio State Summary**: Active script counts and average weights for both the incumbent golden copy and the incoming candidate proposal.
  2. **Action Breakdown**: Counts of New Additions, Removals, Increased weights, Reduced weights, and Unchanged allocations.
  3. **Position-by-Position Comparison Table**: Symbol, Previous Weight %, New Weight %, Action (`Add Action`, `Remove Action`, `Increased weight`, `Reduced weight`, `No Change`), and rank rationale.
* **Consumers**:
  - **Pipeline Interactive Operator**: Printed to stdout during `mycase pipeline` step execution. Prompts the operator with an interactive prompt before overwriting the golden portfolio CSV.
  - **Audit Log**: Permanent record of portfolio turnover and rebalance deltas.
* **Recreation / Recovery**:
  - Re-run `mycase pipeline` or compare any timestamped backup in [`data/backups/`](file:///Users/raghavgarg/Projects/myGo/mycase/data/backups) against the corresponding proposal in [`data/candidates/proposals/`](file:///Users/raghavgarg/Projects/myGo/mycase/data/candidates/proposals).

#### 3. `YYYYMMDD_03_portfolio_report.txt`
* **Status**: **ACTIVE (FUNDAMENTAL THESIS REPORT)**
* **File Format**: Formatted narrative investment memorandum
* **Size**: ~10 KB – 30 KB per run
* **Provenance**: Generated by [`cmd/report.go`](file:///Users/raghavgarg/Projects/myGo/mycase/cmd/report.go#L24) (`mycase report`), [`pkg/autopilot.GeneratePortfolioExplanationReport`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/autopilot/autopilot.go#L376), and [`cmd/pipeline.go`](file:///Users/raghavgarg/Projects/myGo/mycase/cmd/pipeline.go#L585).
* **Contents**:
  1. **Portfolio Overview Table**: Ticker, TTM Revenue Growth %, 3-Year CAGR %, DSO (Latest / Prior), RSI-14, Institutional Stake %, and Final Portfolio Weight.
  2. **Granular 5-Pillar Investment Narrative** (per active holding):
     - **Sales Growth Trajectory**: Identifies accelerating revenue versus multi-year CAGR benchmarks.
     - **Asset Turnover & CapEx Inflection**: Scans for rising sales efficiency and CapEx expansion or stabilization.
     - **Working Capital Efficiency**: Days Sales Outstanding (DSO) trend and cash conversion speed.
     - **Institutional Sponsorship**: Professional smart-money backing and promoter equity health.
     - **Technical Stage Analysis**: Stan Weinstein Stage 2 markup confirmation, 200-SMA distance, RSI momentum, and volume breakout flags.
  3. **Cash Defense State**: If zero equities pass regime filters, outputs `100% CASH DEFENSE (0 Equities Selected)` with regime preservation rationale.
* **Consumers**:
  - Human review, investment committee documentation, and post-rebalance health audits.
* **Recreation / Recovery**:
  - Run `mycase report -f data/<portfolio>.csv -m <strategy>` at any time.

---

### 3.2 Qualitative Research Subsystem (`research/`)

#### `YYYYMMDD_scuttlebutt.txt`
* **Status**: **ACTIVE (QUALITATIVE SCUTTLEBUTT & MOS AUDIT)**
* **File Format**: Multi-section qualitative profile
* **Size**: ~25 KB – 40 KB per candidate basket
* **Provenance**: Generated by [`pkg/stockpicker.PrintScuttlebutt`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/io.go#L373) during `mycase pick` and `mycase pipeline`.
* **Contents**:
  Contains an automated 16-point qualitative Scuttlebutt & Live Market validation checklist for every candidate stock:
  1. **[Intrinsic Valuation & MOS]**: Fair Price (₹), Current Market Price (CMP), Upside %, Margin of Safety (MOS) Band (Pessimistic, Base, Optimistic), Valuation Verdict (`DEEPLY_UNDERVALUED`, `FAIRLY_VALUED`, `DEEPLY_OVERVALUED`), Confidence %, and Model Coefficient of Variation (CV).
  2. **[Valuation Model Breakdown]**: Individual price targets across DCF, Earnings Power Value (EPV), Graham Number, Relative Multiple, and PEG models.
  3. **[Live NSE Result Schedule]**: Previous earnings report date and scheduled upcoming earnings date.
  4. **[Live NSE Delivery Vol %]**: Deliverable accumulation percentage from the last trading day.
  5. **[Shareholding Snapshot]**: Institutional %, Promoter/Insiders %, and Promoter Pledged %.
  6. **[Fundamental Traction]**: TTM Sales Growth, 3Y Sales CAGR, ROCE (latest and 3-year average), and Days Sales Outstanding (DSO).
  7. **[Cash Generation & Quality]**: Operating Cash Flow (CFO), Free Cash Flow (FCF), CFO/PAT cash conversion ratio, and Cash Return on Invested Capital (CROIC).
  8. **[Valuation & Growth Pricing]**: Trailing P/E, Forward P/E, and Price-to-Book (P/B).
  9. **[Operating Margin Trajectory]**: Latest Operating Margin (OPM %) and year-over-year basis point expansion.
  10. **[Balance Sheet & Reinvestment]**: Debt-to-Equity ratio, CapEx expansion YoY %, and Reinvestment Rate.
  11. **[Earnings Growth Consistency]**: Ratio of profitable growth cycles over the last 3 years.
  12. **[Auditor Opinion Status]**: Scans corporate announcements for auditor qualifications, disclaimers, or adverse opinions.
  13. **[Live Transcript Highlights]**: Natural language scans for order book updates, guidance notes, or capacity expansion.
  14. **[Sector TAM Trajectory]**: Industry TAM growth CAGR and thematic tailwinds loaded from [`config/defaults.yaml`](file:///Users/raghavgarg/Projects/myGo/mycase/config/defaults.yaml).
  15. **[Management Stability Check]**: Flags resignation announcements for CFO, Statutory Auditor, or Key Management Personnel (KMP).
  16. **[Related Party Trans. Check]**: Scans corporate disclosures for non-arm's-length Related Party Transaction alerts.
  17. **[Customer Concentration Check]**: Flags customer revenue concentration disclosures extracted from annual report PDFs.
* **Consumers**:
  - Fundamental investors, qualitative risk committees, and discretionary pre-trade sanity checks.
* **Recreation / Recovery**:
  - Run `mycase pick --index <index> --method <strategy>`.

---

### 3.3 Simulation Subsystem (`simulations/`)

#### `YYYYMMDD_HHMMSS_monitoring.txt`
* **Status**: **SIMULATION / HISTORICAL POLICY BACKTEST**
* **File Format**: ASCII table and simulated performance ledger
* **Size**: ~3.5 KB – 5 KB per simulation run
* **Provenance**: Generated by [`cmd/monitor.go`](file:///Users/raghavgarg/Projects/myGo/mycase/cmd/monitor.go#L273) (`mycase monitor`) and `mycase pipeline` (Monitoring step).
* **Contents**:
  1. **Simulation Parameters**: Portfolio input file, policy preset (`conservative`, `moderate`, `aggressive`), simulated timeframe (e.g., 1-year historical backtest or purchase date), and data mode (`Live` vs `High-Fidelity Mock Fallback`).
  2. **Holding-by-Holding Policy Evaluation Table**:
     - 3-Year CAGR %
     - TTM Revenue Growth %
     - DSO Delta % (Working capital deterioration)
     - Capital Stall Severity (`None`, `Mild`, `Moderate`, `Severe`)
     - Data Source (`Live` or `Mock`)
     - Policy Verdict: `✅ KEEP HOLD` or `⚠️ AUTO EXIT`
  3. **Simulated Portfolio Performance Metrics**:
     - **Simulated Churn Rate**: Turnover percentage induced by policy exit rules.
     - **Alpha Efficiency**: Ratio of excess return to trading friction.
     - **Portfolio Return**: Cumulative return over the simulated period.
     - **Benchmark Return**: Comparison against index benchmark.
     - **Excess Return ($\alpha$)**: Net annualized alpha generated over benchmark.
* **Consumers**:
  - Quantitative researchers tuning stop-loss and exit policy parameters in [`config/defaults.yaml`](file:///Users/raghavgarg/Projects/myGo/mycase/config/defaults.yaml).
* **Recreation / Recovery**:
  - Run `mycase monitor -f data/<portfolio>.csv -s <strategy> --policy moderate`.

---

### 3.4 Qualitative Primary Source Archive (`report/annual_reports/`)

* **Status**: **QUALITATIVE RESEARCH INPUTS (PRIMARY FILING ARCHIVE)**
* **Disk Footprint**: ~246 MB (33 active PDFs)
* **Provenance**: Official corporate Annual Report filings downloaded directly from NSE, BSE, or corporate investor relations portals.
* **Role in Pipeline**:
  - Serves as the primary source material for forensic accounting.
  - The script [`scripts/check_customer_concentration.py`](file:///Users/raghavgarg/Projects/myGo/mycase/scripts/check_customer_concentration.py#L140) extracts Ind AS 108 Segment Reporting disclosures to determine if single customers account for $\ge 10\%$ of total corporate revenues.
  - Extracted disclosures are cached in [`data/cache/customer_concentration.json`](file:///Users/raghavgarg/Projects/myGo/mycase/data/cache/customer_concentration.json) and rendered in section 16 of `YYYYMMDD_scuttlebutt.txt`.

#### Library Manifest

| Symbol | Company Name / Filing | File Size | Ind AS 108 Status |
|---|---|---|---|
| `AARTIDRUGS.pdf` | Aarti Drugs Ltd. | 4.5 MB | Covered |
| `ACUTAAS.pdf` | Acutaas Chemicals Ltd. | 9.7 MB | Covered |
| `ARVIND.pdf` | Arvind Ltd. | 20.0 MB | Covered |
| `BALKRISIND.pdf` | Balkrishna Industries Ltd. | 5.9 MB | Covered |
| `CCL.pdf` | CCL Products (India) Ltd. | 4.4 MB | Covered |
| `CHALET.pdf` | Chalet Hotels Ltd. | 4.2 MB | Covered |
| `CHENNPETRO.pdf` | Chennai Petroleum Corporation Ltd. | 25.0 MB | Covered |
| `DATAPATTNS.pdf` | Data Patterns (India) Ltd. | 3.0 MB | Covered |
| `DIVISLAB.pdf` | Divi's Laboratories Ltd. | 14.0 MB | Covered |
| `ECLERX.pdf` | eClerx Services Ltd. | 11.0 MB | Covered |
| `ENGINERSIN.pdf` | Engineers India Ltd. | 4.4 MB | Covered |
| `GOKULAGRO.pdf` | Gokul Agro Resources Ltd. | 11.0 MB | Covered |
| `HINDCOPPER.pdf` | Hindustan Copper Ltd. | 1.9 MB | Covered |
| `IPCALAB.pdf` | IPCA Laboratories Ltd. | 1.8 MB | Covered |
| `JAMNAAUTO.pdf` | Jamna Auto Industries Ltd. | 4.8 MB | Covered |
| `JINDALSAW.pdf` | Jindal Saw Ltd. | 13.0 MB | Covered |
| `LAURUSLABS.pdf` | Laurus Labs Ltd. | 7.5 MB | Covered |
| `LTFOODS.pdf` | LT Foods Ltd. | 12.0 MB | Covered |
| `MANORAMA.pdf` | Manorama Industries Ltd. | 7.8 MB | Covered |
| `MCX.pdf` | Multi Commodity Exchange of India Ltd. | 8.1 MB | Covered |
| `METROPOLIS.pdf` | Metropolis Healthcare Ltd. | 5.7 MB | Covered |
| `NAVINFLUOR.pdf` | Navin Fluorine International Ltd. | 2.8 MB | Covered |
| `NETWEB.pdf` | Netweb Technologies India Ltd. | 6.7 MB | Covered |
| `SARDAEN.pdf` | Sarda Energy & Minerals Ltd. | 5.9 MB | Covered |
| `SHAILY.pdf` | Shaily Engineering Plastics Ltd. | 3.6 MB | Covered |
| `SHILPAMED.pdf` | Shilpa Medicare Ltd. | 7.5 MB | Covered |
| `SIEMENS.pdf` | Siemens Ltd. | 12.0 MB | Covered |
| `SMLMAH.pdf` | SML Isuzu Ltd. | 7.3 MB | Covered |
| `SUMICHEM.pdf` | Sumitomo Chemical India Ltd. | 3.9 MB | Covered |
| `THYROCARE.pdf` | Thyrocare Technologies Ltd. | 3.3 MB | Covered |
| `TIPSMUSIC.pdf` | Tips Music Ltd. | 2.3 MB | Covered |
| `WABAG.pdf` | VA Tech Wabag Ltd. | 8.1 MB | Covered |
| `ZFCVINDIA.pdf` | ZF Commercial Vehicle Control Systems Ltd. | 3.1 MB | Covered |

---

## 4. Active Target & Strategy Execution Hubs

The `report/` root contains 11 dedicated strategy hubs:

| Directory | Strategy Preset | Underlying Universe / Portfolio | File Count | Disk Size | Primary Scope |
|---|---|---|---|---|---|
| [`report/niftytotalmarket_multibagger/`](file:///Users/raghavgarg/Projects/myGo/mycase/report/niftytotalmarket_multibagger) | `multibagger` | Nifty Total Market (750 stocks) | 32 files | 1.3 MB | Production growth stock screening & audit |
| [`report/niftytotalmarket_earlymb/`](file:///Users/raghavgarg/Projects/myGo/mycase/report/niftytotalmarket_earlymb) | `earlymb` | Nifty Total Market (750 stocks) | 27 files | 1.6 MB | Institutional accumulation & VCP breakouts |
| [`report/niftytotalmarket_fairprice/`](file:///Users/raghavgarg/Projects/myGo/mycase/report/niftytotalmarket_fairprice) | `fairprice` | Nifty Total Market (750 stocks) | 2 files | 56 KB | 5-Model Intrinsic Valuation & MOS ranks |
| [`report/microsmall_multibagger/`](file:///Users/raghavgarg/Projects/myGo/mycase/report/microsmall_multibagger) | `multibagger` | Theme Microsmall Golden Portfolio | 114 files | 1.4 MB | Historical rebalance comparisons & audits |
| [`report/microsmall_earlymb/`](file:///Users/raghavgarg/Projects/myGo/mycase/report/microsmall_earlymb) | `earlymb` | Theme Microsmall Golden Portfolio | 2 files | 8 KB | Early multibagger rebalance testing |
| [`report/smallcap250_multibagger/`](file:///Users/raghavgarg/Projects/myGo/mycase/report/smallcap250_multibagger) | `multibagger` | Nifty Smallcap 250 | 4 files | 84 KB | Smallcap growth candidate tracking |
| [`report/sp500_multibagger/`](file:///Users/raghavgarg/Projects/myGo/mycase/report/sp500_multibagger) | `multibagger` | US S&P 500 | 11 files | 208 KB | US large-cap fundamentals & EDGAR checks |
| [`report/sp500_earlymb/`](file:///Users/raghavgarg/Projects/myGo/mycase/report/sp500_earlymb) | `earlymb` | US S&P 500 | 2 files | 92 KB | US institutional breakout candidates |
| [`report/us_microsmall_multibagger/`](file:///Users/raghavgarg/Projects/myGo/mycase/report/us_microsmall_multibagger) | `us_quality_momentum` | US Microsmall Portfolio | 4 files | 48 KB | US quality momentum rebalance audit |
| [`report/portfolio_earlymb/`](file:///Users/raghavgarg/Projects/myGo/mycase/report/portfolio_earlymb) | `earlymb` | Portfolio Early Multibagger | 1 file | 4 KB | Active portfolio explanation snapshot |
| [`report/live_earlymb/`](file:///Users/raghavgarg/Projects/myGo/mycase/report/live_earlymb) | `earlymb` | Live Satellite Early Multibagger | 1 file | 4 KB | Real-time live satellite rebalance snapshot |

---

## 5. Cross-Subsystem Interactions & Dependencies

The `report/` tree is not an isolated output sink; several core Go packages and Python routines maintain two-way contracts with it:

### 5.1 The Cooldown Memory Loop (`pkg/stockpicker/cooldown.go`)
```
 ┌────────────────────────────────────────────────────────┐
 │ mycase pick / pipeline                                 │
 └──────────────────────────┬─────────────────────────────┘
                            │ (Writes)
                            ▼
 ┌────────────────────────────────────────────────────────┐
 │ report/<target>_*/executions/*_01_selection_reasons.txt│
 └──────────────────────────┬─────────────────────────────┘
                            │ (Reads historical exits)
                            ▼
 ┌────────────────────────────────────────────────────────┐
 │ pkg/stockpicker/cooldown.go:                           │
 │   GetExitDatesFromHistoricalReports()                  │
 │   -> Enforces 30-Day Anti-Churn Cooldown Gate          │
 └────────────────────────────────────────────────────────┘
```
1. When a stock is evicted during rebalancing, the exit reason is written to `REMOVED ACTIVE HOLDINGS (EXITS)` in `*_01_selection_reasons.txt`.
2. On subsequent pipeline runs, [`pkg/stockpicker/cooldown.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/cooldown.go) parses past reports to discover recent exits within the 30-day window (`cutoffDays = 30`).
3. Re-entry is strictly blocked unless the candidate's rank satisfies the bypass threshold (`cooldown_bypass_rank = 3` or `5`).
4. **Resilience**: If reports are deleted, [`pkg/stockpicker/cooldown.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/cooldown.go#L49-L89) falls back to comparing dated backups in [`data/backups/<theme>/bk_*.csv`](file:///Users/raghavgarg/Projects/myGo/mycase/data/backups).

### 5.2 The Ind AS 108 Qualitative Pipeline
```
 ┌────────────────────────────────────────────────────────┐
 │ report/annual_reports/*.pdf                            │
 └──────────────────────────┬─────────────────────────────┘
                            │ (Extracts customer concentration)
                            ▼
 ┌────────────────────────────────────────────────────────┐
 │ scripts/check_customer_concentration.py                │
 └──────────────────────────┬─────────────────────────────┘
                            │ (Caches disclosures)
                            ▼
 ┌────────────────────────────────────────────────────────┐
 │ data/cache/customer_concentration.json                 │
 └──────────────────────────┬─────────────────────────────┘
                            │ (Enriches qualitative section 16)
                            ▼
 ┌────────────────────────────────────────────────────────┐
 │ report/<target>_<strategy>/research/*_scuttlebutt.txt  │
 └────────────────────────────────────────────────────────┘
```

### 5.3 Golden Copy Rebalance Verification
1. [`cmd/pipeline.go`](file:///Users/raghavgarg/Projects/myGo/mycase/cmd/pipeline.go#L532) generates `YYYYMMDD_02_comparison.txt` by evaluating the delta between active golden holdings ([`data/microsmall.csv`](file:///Users/raghavgarg/Projects/myGo/mycase/data/microsmall.csv)) and the optimized proposal ([`data/candidates/proposals/*_optim.csv`](file:///Users/raghavgarg/Projects/myGo/mycase/data/candidates/proposals)).
2. The user is prompted to inspect the comparison report before approving the update.
3. Upon approval, a backup is archived in [`data/backups/microsmall/`](file:///Users/raghavgarg/Projects/myGo/mycase/data/backups/microsmall), the golden CSV is updated, and the transition is recorded as `PROPOSED` in DuckDB table `theme_rebalances` (and promoted to `COMMITTED` upon confirmed broker basket execution).

---

## 6. Comprehensive Summary Table

| Category | Artifact Path Pattern | Typical Size | Generating Package / Command | Downstream Consumers | Status | Primary Function |
|---|---|---|---|---|---|---|
| **Selection Diagnostics** | `report/*_*/executions/*_01_selection_reasons.txt` | 15–80 KB | `pkg/selectiontracker`, `mycase pick` | `pkg/stockpicker/cooldown.go`, Human PM | **CRITICAL** | Filter waterfall & cooldown audit |
| **Rebalance Delta** | `report/*_*/executions/*_02_comparison.txt` | 1.5–3 KB | `pkg/csvloader`, `mycase pipeline` | Terminal display, PM review | **ACTIVE** | Golden copy vs candidate diff |
| **Portfolio Thesis** | `report/*_*/executions/*_03_portfolio_report.txt` | 10–30 KB | `pkg/autopilot`, `mycase report` | Investment memorandum, PM | **ACTIVE** | 5-factor fundamental writeup |
| **Qualitative Scuttlebutt**| `report/*_*/research/*_scuttlebutt.txt` | 25–40 KB | `pkg/stockpicker/io.go`, `mycase pick` | Human committee, Risk team | **ACTIVE** | 16-point qualitative & MOS check |
| **Policy Simulation** | `report/*_*/simulations/*_monitoring.txt` | 3.5–5 KB | `cmd/monitor.go`, `mycase monitor` | Quantitative researchers | **SIMULATION** | Stop-loss & churn backtest |
| **Annual Report Library** | `report/annual_reports/*.pdf` | 1.8–25 MB | NSE/BSE corporate filings | `check_customer_concentration.py` | **RESEARCH** | Ind AS 108 forensic customer disclosures |

---

## 7. Housekeeping, Retention & Pruning Guide

### 7.1 Retention Policy
- **Execution Reports (`executions/`)**:
  - `*_01_selection_reasons.txt`: Retain at least **30 to 90 days** to ensure the 30-day anti-churn cooldown window has authoritative exit provenance. Because text files average ~70 KB, retaining the full multi-year history requires less than 10 MB total.
  - `*_02_comparison.txt` & `*_03_portfolio_report.txt`: Extremely lightweight (~20 KB total); permanent archival is recommended for historical attribution.
- **Research Reports (`research/`)**:
  - Permanent retention recommended for forensic review of qualitative assumptions and intrinsic valuation model drift.
- **Simulation Reports (`simulations/`)**:
  - Simulation files older than 90 days may be pruned safely as they represent point-in-time experimental backtests.
- **Annual Reports (`annual_reports/`)**:
  - Retain indefinitely as long as disk storage permits (~246 MB for 33 companies). If disk space is constrained, PDFs can be moved to external cold storage or re-downloaded on demand.

### 7.2 Directory Hygiene & Maintenance
- **System Artifacts**: Remove any macOS Finder `.DS_Store` metadata files:
  ```bash
  find report -name ".DS_Store" -delete
  ```
- **Safe Dry-Run Pruning (Simulations older than 90 days)**:
  ```bash
  find report/*/simulations -name "*_monitoring.txt" -mtime +90 -print
  ```

---

## 8. Verification & Health Check Results

| Check / Verification | Target | Result | Status |
|---|---|---|---|
| **Root Cleanliness** | `find report -maxdepth 1` | Only `<target>_<strategy>/` hubs and `annual_reports/` reside at root | **PASSED** |
| **Cooldown Report Resolution** | `pkg/stockpicker/cooldown_test.go` | Parsed historical `01_selection_reasons.txt` exits cleanly | **PASSED** |
| **Comparison Report Generation** | `pkg/csvloader/comparison_test.go` | Generated valid comparison table under `report/` | **PASSED** |
| **Customer Concentration Scanner** | `python3 scripts/check_customer_concentration.py ACUTAAS` | Resolved PDF from `report/annual_reports/` and extracted Ind AS 108 data | **PASSED** |
| **Portfolio Report Command** | `go run . report -f data/microsmall.csv -m multibagger` | Generated formatted `03_portfolio_report.txt` | **PASSED** |
| **Go Test Suite** | `go test ./pkg/csvloader ./pkg/stockpicker ./pkg/selectiontracker` | All tests passed with 0 failures | **PASSED** |
