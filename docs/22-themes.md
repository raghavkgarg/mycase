# Theme Subsystem — Lifecycle DB + Exact Returns (India-Path)

> **Part of the India-Path.** Themes belong to the multi-market design and the
> Zerodha/`portfolio.db` integration — the India market path, distinct from the active US
> focus.
> This is the consolidated reference for the two theme concerns — the lifecycle
> database (`theme_rebalances`/`theme_history` in `data/mycase.db`) and the
> exact-return engine (`pkg/themereturn`). Historical implementation-plan detail lives
> in git history.

## 1. Domain separation

| Concern | Owner | Store |
|---------|-------|-------|
| Broker execution & FIFO tax lots | `myportfolio` (sibling project) | `../myportfolio/data/portfolio.db` |
| Strategy, theme rebalancing, lifecycle history | `mycase` | `data/mycase.db` |

`mycase` reads `portfolio.db` **read-only** (`?access_mode=read_only`) for return math,
avoiding lock contention with the trading engine.

## 2. Theme lifecycle tables (`data/mycase.db`)

Owned by `pkg/themedb` (per the "domains own their persistence" rule).

```sql
-- Discrete rebalance events (v1, v2, … per theme)
CREATE TABLE IF NOT EXISTS theme_rebalances (
    id             VARCHAR PRIMARY KEY,
    theme          VARCHAR NOT NULL,
    version        INTEGER NOT NULL,
    rebalance_date DATE NOT NULL,
    run_id         VARCHAR,
    source         VARCHAR NOT NULL,
    turnover       DOUBLE,
    created_at     TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Point-in-time constituent roster + turnover action per version
-- action ∈ NEW | REMOVED | INCREASED | DECREASED | UNCHANGED
CREATE TABLE IF NOT EXISTS theme_history (
    id             VARCHAR PRIMARY KEY,
    theme          VARCHAR NOT NULL,
    version        INTEGER NOT NULL,
    symbol         VARCHAR NOT NULL,
    weight         DOUBLE NOT NULL,
    action         VARCHAR NOT NULL,
    prior_weight   DOUBLE,
    rebalance_date DATE NOT NULL,
    created_at     TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

## 3. Exact-return engine (`pkg/themereturn`)

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

`portfolio.db` resolves from: `--portfolio-db` flag → `$PORTFOLIO_DB` →
`../myportfolio/data/portfolio.db` → `data/portfolio.db`.

## 4. CLI

| Action | Command |
|--------|---------|
| Exact theme return | `mycase returns --theme microsmall --live` |
| Full lifecycle (incl. exited positions) | `mycase returns --theme microsmall --live --lifecycle --detail` |
| All configured themes | `mycase returns --all --live` |
| JSON output | `mycase returns --theme microsmall --format json` |
| Live holdings + return banner | `mycase holdings --live` |
| Theme roster | `mycase theme show <theme>` |
| Theme rebalance history | `mycase theme history <theme>` |
| Backfill rebalances from proposal CSVs / golden copies | `mycase theme sync` |

`returns` flags: `--theme/-t`, `--file/-f`, `--live/-l`, `--account/-a` (default
`CBR420`), `--db`, `--lifecycle`, `--detail/-d`, `--benchmark/-b` (default
`NIFTY50_TRI`), `--format` (`table`|`json`).

## 5. Daily sync

Post-market theme sync runs as **stage 3 of the daily EOD update** (`mycase db update`,
aliases `eod`/`daily`), and is scheduled automatically by the autonomous scheduler
(`mycase scheduler` — see `docs/18-runbook.md` §11), which owns the daily EOD cadence with
holiday-aware skipping. The theme sync itself lives in `pkg/eod` stage 3
(`SyncThemeFromProposals` per configured theme). The former machine-specific
`scripts/daily_sync.sh` shell script has been retired — its weekend/holiday guard is now
`marketcal.IsTradingDay` + `config/holidays.json`, and its `mycase db update` call is the
scheduler's EOD cadence.
