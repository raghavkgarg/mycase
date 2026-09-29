# Themes

> **Indian-equity subsystem.** Themes are tied to the Zerodha/`portfolio.db` integration and
> serve the Indian-equity universe.
>
> The consolidated reference for the two theme concerns — the lifecycle database
> (`theme_rebalances`/`theme_history` in `data/mycase.db`), configuration (`config/reference/india/themes.json`), and the exact-return engine
> (`pkg/themereturn` in `mycase` and `pkg/theme` in `myportfolio`).

## 1. Domain separation

| Concern | Owner | Store |
|---------|-------|-------|
| Broker execution & FIFO tax lots | `myportfolio` (sibling project) | `../myportfolio/data/portfolio.db` |
| Strategy, theme rebalancing, lifecycle history | `mycase` | `data/mycase.db` |
| Theme Configuration (reference mapping) | `mycase` | `config/reference/india/themes.json` |

`mycase` and `myportfolio` read each other's databases in **read-only** mode (`?access_mode=read_only`) for return calculations and cross-theme attribution, avoiding lock contention with the trading engine.

## 2. Theme lifecycle tables (`data/mycase.db`)

Owned by `pkg/themedb` (per the "domains own their persistence" rule).

```sql
-- Discrete rebalance events (v1, v2, … per theme)
CREATE TABLE IF NOT EXISTS theme_rebalances (
    theme_name          VARCHAR NOT NULL,
    version             INTEGER NOT NULL,
    effective_date      DATE NOT NULL,
    created_at          TIMESTAMP NOT NULL,
    executed_at         TIMESTAMP,
    run_id              VARCHAR,
    total_nav           DOUBLE,
    cash_weight         DOUBLE DEFAULT 0.0,
    turnover_pct        DOUBLE,
    benchmark_price     DOUBLE,
    status              VARCHAR DEFAULT 'COMMITTED',
    notes               VARCHAR,
    PRIMARY KEY (theme_name, version)
);

-- Point-in-time constituent roster + turnover action per version
-- action ∈ NEW_ENTRY | REWEIGHT | EXITED | UNCHANGED
CREATE TABLE IF NOT EXISTS theme_history (
    theme_name          VARCHAR NOT NULL,
    version             INTEGER NOT NULL,
    symbol              VARCHAR NOT NULL,
    isin                VARCHAR,
    action              VARCHAR NOT NULL,
    cycle_number        INTEGER DEFAULT 1,
    target_weight       DOUBLE NOT NULL,
    prev_weight         DOUBLE DEFAULT 0.0,
    actual_weight       DOUBLE,
    target_shares       INTEGER,
    delta_shares        INTEGER,
    executed_shares     INTEGER,
    decision_price      DOUBLE,
    execution_avg_price DOUBLE,
    rank                INTEGER,
    composite_score     DOUBLE,
    exit_category       VARCHAR,
    exit_reason         VARCHAR,
    execution_status    VARCHAR DEFAULT 'FILLED',
    PRIMARY KEY (theme_name, version, symbol)
);
```

### Status Lifecycle & Pipeline Guard
- **`PROPOSED`**: Written during `mycase pipeline` candidate merge (`RecordPipelineRebalance`). Captures intended candidate shifts, `executed_at = NULL`, `status = 'PROPOSED'`, and `theme_history.execution_status = 'PROPOSED'`. If the user tweaks the golden CSV or aborts before execution, subsequent pipeline runs idempotently replace the proposed draft without creating phantom version gaps.
- **`COMMITTED`**: Promoted upon confirmed broker order execution (`RecordBasketRebalance`), recording actual execution timestamps (`executed_at`), fill prices, and confirmed broker share quantities.
- **`ROLLED_BACK`**: Optional status for invalidating unexecuted or superseded historical versions.
- **Active Isolation Guard**: All active constituent queries (`GetActiveHoldings`, `GetActiveConstituentShares`, `GetLatestVersion`, and `myportfolio`'s resolver) strictly filter `status = 'COMMITTED'`. If pipeline proposals are skipped or unexecuted, they remain completely isolated and never pollute active portfolio holdings, shared holdings reconciliation, or return calculations.

### Omitted Exits & Audit Continuity
When executing a basket via `RecordBasketRebalance`, if any stock that had an active allocation (`target_weight > 0`) in the latest committed version is dropped or omitted from the incoming basket keys, the engine automatically:
1. Detects the omission against the previous committed holdings (`GetActiveHoldings`).
2. Inserts an explicit `EXITED` row in `theme_history` with `target_weight = 0.0`, `prev_weight`, `target_shares = 0`, `delta_shares = -currQty`, and `execution_status = 'FILLED'`.
This guarantees that full historical turnover and exit tracking are preserved even if zero-weight tickers are removed from the basket CSV.

### Forensic Audit & Rollback Runbook
If an unexecuted rebalance was prematurely recorded as `COMMITTED` (e.g. from legacy pipeline merges before broker confirmation):
1. **Audit the Timeline**:
   ```bash
   mycase theme history <theme>
   ```
   Check if any version has `Status = COMMITTED` but lacks live execution (`executed_at IS NULL` or 0 shares executed while Demat still holds the old stocks).
2. **Rollback to Previous Committed Version**:
   To realign DuckDB with real Demat holdings without losing data:
   ```sql
   -- Create backup first: cp data/mycase.db data/mycase.db.bk_$(date +%Y%m%d_%H%M%S)
   DELETE FROM theme_history WHERE theme_name = 'microsmall' AND version > <last_real_version>;
   DELETE FROM theme_rebalances WHERE theme_name = 'microsmall' AND version > <last_real_version>;
   ```
3. **Verify Active Roster**:
   ```bash
   mycase theme show <theme>
   ```

## 3. Configuration & Constituent Resolution

Theme definitions are configured in `config/reference/india/themes.json` (with backward-compatible lookup in `config/themes.json`):

```json
[
  {
    "name": "Theme Microsmall",
    "prefix": "My MicroSmall",
    "csv_path": "data/microsmall.csv",
    "target_weight": 0.20
  }
]
```

### Dual Resolution Hierarchy
When evaluating a theme's return or attribution:
1. **Primary Source (`mycase.db`)**: Constituent symbols, target weights, and historical exits are queried directly from `theme_history` joining the latest committed version in `theme_rebalances`.
2. **Fallback Source (Golden Copy CSV)**: If a theme has no recorded versions in DuckDB yet, constituents and weights are parsed from the specified `csv_path` (e.g. `data/microsmall.csv`).
3. **Cross-Theme Deduplication**: If multiple themes claim the same ticker in a flat account, historical tenure (count of committed versions in `theme_history`) arbitrates primary attribution.

## 4. Exact-return engine (`mycase/pkg/themereturn` & `myportfolio/pkg/theme`)

Static `holdings --live` P&L ignores cash-flow timing, rebalancing exits, and
dividends. The return engine computes a **triple-return framework** (CFA/GIPS-aligned)
against `portfolio.db` trades, closed lots, dividends, and benchmark quotes:

- **HPR** (holding-period / absolute return): cash-on-cash total return including
  unrealized P&L, realized gains, and lifetime dividends; annualized by
  `(1+HPR)^(365.25/holdingDays) − 1`.
- **MWR / XIRR**: root `r` of `Σ Cᵢ/(1+r)^((tᵢ−t₀)/365.25) = 0` (buys negative; sells,
  dividends, terminal value positive). Newton–Raphson with bracketed-bisection fallback
  (`xirr.go`).
- **TWR** (Modified Dietz): `(V_final − V₀ − ΣFᵢ) / (V₀ + ΣWᵢFᵢ)`, `Wᵢ=(T−tᵢ)/T` — strips
  external-flow skew.
- **Alpha**: `TWR_theme − TWR_benchmark` (default `NIFTY50_TRI`).

## 5. CLI

Both `mycase` and `myportfolio` provide zero-config auto-discovery for `themes.json` (checking local and sibling `config/reference/india/themes.json`) and `mycase.db`.

| Tool | Action | Command |
|------|--------|---------|
| `mycase` | Exact theme return | `mycase returns --theme microsmall --live` |
| `mycase` | Full lifecycle (incl. exited positions) | `mycase returns --theme microsmall --live --lifecycle --detail` |
| `mycase` | All configured themes | `mycase returns --all --live` |
| `mycase` | JSON output | `mycase returns --theme microsmall --format json` |
| `mycase` | Live holdings + return banner | `mycase holdings --live` |
| `mycase` | Theme roster | `mycase theme show <theme>` |
| `mycase` | Theme rebalance history | `mycase theme history <theme>` |
| `mycase` | Backfill rebalances from proposal CSVs | `mycase theme sync` |
| `myportfolio` | Single theme drilldown (active & exits, fees) | `myportfolio perf --account CBR420 --theme microsmall --live` |
| `myportfolio` | Strategy & theme attribution matrix | `myportfolio perf --account CBR420 --themes --live` |

Optional override flags:
- `--theme-config`: Explicit path to `themes.json` (defaults to auto-discovering `config/reference/india/themes.json`).
- `--mycase-db`: Explicit path to `data/mycase.db`.

## 6. Daily sync

Post-market theme sync runs as **stage 3 of the daily EOD update** (`mycase db update`,
aliases `eod`/`daily`), and is scheduled automatically by the autonomous scheduler
(`mycase scheduler` — see `docs/1-80-runbook.md` §11), which owns the daily EOD cadence with
holiday-aware skipping. The theme sync itself lives in `pkg/eod` stage 3
(`SyncThemeFromProposals` per configured theme). The former machine-specific
`scripts/daily_sync.sh` shell script has been retired — its weekend/holiday guard is now
`marketcal.IsTradingDay` + `config/holidays.json`, and its `mycase db update` call is the
scheduler's EOD cadence.

## 7. Cross-Theme Shared Holdings & Rebalance Isolation

In Indian trading setups, all strategies and themes execute within a **single flat Demat account** (e.g. Zerodha Kite). The broker API returns account-wide aggregate quantities without sub-portfolio tags. When multiple themes share instruments (such as cash/liquid ETFs like `NSE:LIQUIDCASE` or overlapping equities like `NSE:NETWEB`), naïve rebalancing risks:
1. **Value Inflation**: Counting another theme's units towards the current theme's capital.
2. **Accidental Liquidation**: Selling another theme's active position when the current theme exits that stock.

### Upfront Audit Table
When `mycase basket` runs on any theme in `themes.json`, it performs an upfront cross-theme audit before prompting:

```text
══════════════════════════════════════════════════════════════════════════════
                   CROSS-THEME SHARED HOLDINGS AUDIT                          
══════════════════════════════════════════════════════════════════════════════
The following instruments in this basket are also held by other themes:

  Ticker           | Demat Qty | Claimed Value | Claiming Themes
  ----------------------------------------------------------------------------
  NSE:LIQUIDCASE   | 496       |    ₹57,595.52 | Theme Micro Advice (modularmicro)
  NSE:NETWEB       | 2         |     ₹9,097.00 | Theme Microsmall (microsmall)
  ----------------------------------------------------------------------------
  Total Gross Demat Value:       ₹167,326.14
  Less Other Themes' Claims:    -₹66,692.52 (LIQUIDCASE, NETWEB)
  ----------------------------------------------------------------------------
  Theme AI Advice Net Baseline:  ₹100,633.62
══════════════════════════════════════════════════════════════════════════════
```

### Dual-Handling Strategy
For each overlapping ticker, the engine applies specific safety logic:
- **Active Target (`target_weight > 0.0`)**: 
  - **Option A (Isolated / Dedicated, Default)**: Computes target allocation strictly against the theme's net baseline (excluding other themes' units), generating fresh buy orders without touching the other theme's units.
  - **Option B (Shared Pool)**: Treats other themes' units in Demat as shared buffer, buying only any residual shortfall (or 0).
- **Exit Target (`target_weight == 0.0`)**:
  - If another theme in `themes.json` actively holds this stock, the engine automatically **protects** the other theme by defaulting to **0 shares sold** (`Option A`), rather than dumping the Demat position.
  - The investor can explicitly override with `Option B` if liquidation is desired.

### Persistence & CLI Automation
- **DuckDB State Tracking**: Upon successful order execution, the theme's confirmed constituent allocations are committed to `theme_rebalances` and `theme_history` in `data/mycase.db`, making future rebalances automatically aware of the split.
- **CLI Overrides**:
  - `--shares "LIQUIDCASE:318,NETWEB:0"`: Specify explicit share quantities without interactive prompts.
  - `--yes` / `-y`: Automatically accept the safe default (Option A) across all shared tickers.
