# EDGAR Facts Reference

**Purpose**: catalogue the SEC EDGAR XBRL fact universe as it relates to Mycase.
This is a *reference for product evolution* — the DuckDB `edgar_facts` cache stores
only the compact subset we extract today (see [What we store](#what-we-store-today)),
but the raw `companyfacts.json` carries ~500–1500 us-gaap concepts per company. When
the strategy grows to need a new factor, this doc says which fact backs it and which
candidate XBRL tags to add to the concept mapper.

**Updated**: 2026-09-18

---

## Background: the companyfacts document

Source: `https://data.sec.gov/api/xbrl/companyfacts/CIK{10-digit}.json`
(client: `pkg/edgar`; mandatory `User-Agent`, ~10 req/s limiter, cached).

Shape (only the parts we parse; the mapper is deliberately permissive):

```jsonc
{
  "cik": 320193,
  "entityName": "Apple Inc.",
  "facts": {
    "dei":     { /* a couple of entity-level concepts */ },
    "us-gaap": {
      "NetCashProvidedByUsedInOperatingActivities": {
        "label": "...", "description": "...",
        "units": {
          "USD": [
            { "start": "2023-10-01", "end": "2024-09-28", "val": 118254000000,
              "accn": "...", "fy": 2024, "fp": "FY", "form": "10-K",
              "filed": "2024-11-01" },
            /* ...quarterly + annual rows across all filed years... */
          ]
        }
      },
      /* ...hundreds more concepts... */
    }
  }
}
```

Key facts about the shape (drive the mapper design in `pkg/edgar/concepts.go`):

- **Concepts are keyed by us-gaap tag name**, and *filers vary the tag they use for
  the same economic quantity*. So every logical concept needs an **ordered candidate
  tag list**, taking the first tag that carries usable facts. (See the capex saga:
  Visa/Qualcomm report capex under `PaymentsToAcquireProductiveAssets`, not the
  classic `PaymentsToAcquirePropertyPlantAndEquipment`.)
- **A tag can be present but empty / non-annual.** The mapper must *fall through*
  past a present-but-empty candidate to a later populated one (not lock onto the
  first key that merely exists).
- **Units**: absolute currency concepts live under the `"USD"` unit key; per-share
  under `"USD/shares"`; share counts under `"shares"`. We read `USD` for statement
  values.
- **Annual selection**: a full-year figure is `fp == "FY"` on an annual form
  (`10-K`, `10-K/A`, `10-KT`, `20-F`, `40-F`). Dedupe by period-end, keeping the
  most-recently-`filed` row (restatements win).
- **Data-quality traps** (learned the hard way, see roadmap Phase 11):
  - SEC's `company_tickers.json` can map a ticker to a **non-filing shell CIK** with
    0 statement facts (e.g. `XOM` → CIK 2115436 vs the real filer 34088). Handled by
    `cikOverrides` in `pkg/edgar/cik.go`.
  - Recent spinoffs/IPOs (HONA, FDXF, Q, GEHC…) legitimately have thin/no 10-K
    history — no fix, they simply don't qualify until they file.

---

## What we store today

The DuckDB `edgar_facts` cache stores the **extracted** facts, not the raw blob
(the raw bodies are archived to `data/raw/` with their own retention — the DB need
not duplicate ~4 MB/company to serve ~8 KB of useful numbers; measured 437×–2115×
smaller). `mapFacts` (`pkg/edgar/concepts.go`) populates this subset of
`marketdata.Fundamentals`:

| Field | Kind | Candidate us-gaap tags (ordered) | Used by |
|-------|------|----------------------------------|---------|
| `OperatingCashflow` | point (latest) | `NetCashProvidedByUsedInOperatingActivities`, `…ContinuingOperations` | earnings-quality (CFO/NI), FCF |
| `NetIncome` | point (latest) | `NetIncomeLoss`, `ProfitLoss` | earnings-quality, ROE derivation |
| `FreeCashflow` | derived (annual OCF − annual capex) | — | **FCF hard filter**, FCF-yield factor |
| `AnnualOperatingIncome` | annual series | `OperatingIncomeLoss` | ROIC (EBIT/invested capital), interest coverage |
| `AnnualTotalAssets` | annual series | `Assets` | ROIC, CROIC |
| `AnnualCurrentLiabilities` | annual series | `LiabilitiesCurrent` | ROIC, CROIC (invested capital) |
| `AnnualRevenue` | annual series | `RevenueFromContractWithCustomerExcludingAssessedTax`, `Revenues`, `SalesRevenueNet` | growth, CAGR |
| `AnnualGrossProfit` | annual series | `GrossProfit` | gross-margin trajectory |
| `AnnualNetPPE` | annual series | `PropertyPlantAndEquipmentNet` | asset-turnover, capex inflection |
| `AnnualAccountsReceivable` | annual series | `AccountsReceivableNetCurrent` | DSO / working-capital sentry |
| `AnnualCapEx` | annual series | `PaymentsToAcquirePropertyPlantAndEquipment`, `PaymentsToAcquireProductiveAssets`, `PaymentsForCapitalImprovements`, `…PropertyPlantAndEquipmentAndIntangibleAssets`, `…OtherProductiveAssets`, `PaymentsToAcquireOilAndGasProperty`, `PaymentsToExploreAndDevelopOilAndGasProperties` | FCF, CROIC, capex inflection |
| `AnnualInterestExpense` | annual series | `InterestExpense`, `InterestExpenseDebt` | interest-coverage gate |
| `entity_name` | metadata | `entityName` (top-level) | provenance/triage (catches shell-CIK mismatches) |

> Consequence of storing extracted (not raw) facts: adding a **new** concept to the
> mapper requires a cache **refresh** (re-fetch from EDGAR) rather than re-parsing
> the cached blob. That's acceptable because `data/raw/` archives full bodies for
> offline re-derivation. Bump `edgar_facts.schema_version` when the extracted set
> changes so stale-shape rows are treated as a miss.

---

## Candidate facts for future factors (not yet extracted)

Coverage below is from a 3-company spot check (AAPL, JPM, NVDA); treat it as
"generally present" not a guarantee. Add the candidate tags to `concepts.go` and a
field to `marketdata.Fundamentals` + `mapFacts` when the factor lands.

### Balance-sheet depth (quality, leverage, liquidity)

| Concept | Candidate tags | Coverage | Factor it unlocks |
|---------|----------------|----------|-------------------|
| Stockholders' equity | `StockholdersEquity`, `StockholdersEquityIncludingPortionAttributableToNoncontrollingInterest` | 3/3 | **Authoritative ROE** (NI / equity) — replaces Schwab's ROE, fixes the negative-book-equity nonsense (MAS/MCK) with a real denominator; book value |
| Current assets | `AssetsCurrent` | 2/3 | Current ratio, quick ratio (liquidity) |
| Cash & equivalents | `CashAndCashEquivalentsAtCarryingValue` | 3/3 | Net debt, cash-adjusted EV, quick ratio |
| Long-term debt | `LongTermDebtNoncurrent`, `LongTermDebt` | 3/3 | Net-debt/EBITDA, real debt-to-equity (vs Schwab's ratio) |
| Inventory | `InventoryNet` | 2/3 | Inventory turnover, cash-conversion cycle |
| Goodwill | `Goodwill` | 3/3 | Tangible book value, acquisition-heaviness flag |

### Cash-flow statement (capital allocation, shareholder yield)

| Concept | Candidate tags | Coverage | Factor it unlocks |
|---------|----------------|----------|-------------------|
| Financing cash flow | `NetCashProvidedByUsedInFinancingActivities` | 3/3 | Capital-allocation discipline |
| Investing cash flow | `NetCashProvidedByUsedInInvestingActivities` | 3/3 | Reinvestment intensity |
| Dividends paid | `PaymentsOfDividendsCommonStock`, `PaymentsOfDividends` | 3/3 | **Authoritative shareholder yield** (vs the FCF-proxy buyback estimate today) |
| Share repurchases | `PaymentsForRepurchaseOfCommonStock` | 3/3 | **Buyback yield** — the other half of shareholder yield |
| Depreciation & amortization | `DepreciationDepletionAndAmortization`, `DepreciationAmortizationAndAccretionNet` | 2/3 | EBITDA (→ EV/EBITDA, net-debt/EBITDA) |

### Income-statement depth (growth, margins, valuation)

| Concept | Candidate tags | Coverage | Factor it unlocks |
|---------|----------------|----------|-------------------|
| Diluted EPS | `EarningsPerShareDiluted` | 3/3 | EPS growth, PEG from filings (vs Schwab's) |
| Shares outstanding | `CommonStockSharesOutstanding`, `WeightedAverageNumberOfDilutedSharesOutstanding` | 3/3 | Share-count trend (dilution/buyback), per-share metrics without Schwab |
| R&D expense | `ResearchAndDevelopmentExpense` | 2/3 | R&D intensity (quality/moat proxy for tech/pharma) |

### Notes on candidate reliability

- **Coverage varies by sector.** Banks (JPM) legitimately lack `GrossProfit`,
  `OperatingIncomeLoss`, `LiabilitiesCurrent`, capex — do not treat absence as an
  error; gate sector-appropriately (cf. the Financials/REIT FCF exemption).
- **`TotalDebt` has no single canonical tag** — most filers report long-term +
  short-term separately; compute it, don't look for one tag.
- Always validate a new concept's candidate list against **both** a financial and a
  non-financial filer before trusting it (the capex bug was a one-filer assumption).

---

## Where this lives in code

- Concept mapping + candidate tag lists: `pkg/edgar/concepts.go` (`mapFacts`,
  `annualSeries`, `latestValue`, `latestAnnual`).
- Fetch + cache: `pkg/edgar/facts.go` (`fetchFacts`, `edgar_facts` DDL).
- CIK resolution + overrides: `pkg/edgar/cik.go`.
- Merge with Schwab base + provenance: `pkg/datafetcher/merger.go`.
- Shared DTO: `marketdata.Fundamentals` (leaf) — add fields here first.
