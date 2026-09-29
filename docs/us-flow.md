# US Market Pipeline Execution & Data Ingestion Flow

This document details the end-to-end data ingestion, scoring, and persistence lifecycle when executing the US Market pipeline (`us-momentum`).

```mermaid
flowchart TD
    %% CLI Trigger & Flag Gotcha
    subgraph CLI ["1. Invocation & Profile Resolution"]
        direction TB
        CMD_WRONG["mycase pipeline --market us"] -.->|Matches Suffix/Exact? No| FALLBACK["Fallback: india-multibagger<br/>(Zerodha + microsmall.csv)"]
        CMD_OK["mycase pipeline --profile us-momentum"] -->|Exact Match in config/pipeline.go| US_PROFILE["Profile: us-momentum<br/>(Strategy: us_quality_momentum)"]
    end

    %% Universe Ingestion
    subgraph UNIVERSE ["2. Constituent Universe Ingestion (S&P 500)"]
        direction TB
        CONF_CSV["config/reference/us/csvlinks.json"] -->|Public GitHub URL| LOADER["pkg/stockpicker/loader.go"]
        LOADER -->|Download & Cache| DISK_CSV["data/universe/sp500.csv<br/>(Fallback: data/cache/constituents/sp500.csv)"]
        DISK_CSV --> PARSE["Parse CSV: Extract GICS Sectors<br/>+ Prefix Tickers with 'US:' (e.g. US:AAPL)"]
    end

    %% Market Prices & Quotes
    subgraph PRICES ["3. Price & OHLCV Ingestion (datafetcher.Router)"]
        direction TB
        ROUTER{"datafetcher.Router<br/>(usPrimaryWithYahooFallback)"}
        SCHWAB_AUTH["Schwab Auth<br/>(config/schwab.json & schwab_token.json)"] --> ROUTER
        
        ROUTER -->|Primary: Available| SCHWAB_PRICE["Charles Schwab API<br/>• Real-time/EOD Quotes<br/>• Historical OHLCV Bars"]
        ROUTER -->|Missing Auth / Endpoint Failure| YFIN_PRICE["Yahoo Finance API<br/>(Secondary / Fallback)"]
    end

    %% Fundamentals & EDGAR Enrichment
    subgraph FUNDAMENTALS ["4. Fundamentals & Quality Metrics"]
        direction TB
        CHECK_CACHE{"Valid Same-Day<br/>DuckDB Cache?"}
        
        CHECK_CACHE -->|Yes| HIT_CACHE["Load cached fundamentals directly<br/>(Zero external API calls)"]
        
        CHECK_CACHE -->|No: Fetch External| ROUTE_FUND{"Schwab Fundamentals<br/>Available?"}
        
        ROUTE_FUND -->|Yes| SCHWAB_FUND["Schwab API:<br/>PE, PB, Margin, ROE, Betas"]
        ROUTE_FUND -->|No| YFIN_FUND["Yahoo Finance Fallback"]
        
        SCHWAB_FUND --> EDGAR_FETCH{"SEC EDGAR API<br/>(XBRL Facts via defaults.yaml)"}
        YFIN_FUND --> EDGAR_FETCH
        
        EDGAR_FETCH -->|Success| EDGAR_OK["Pull 10-K / 10-Q XBRL Facts:<br/>• Sloan Accruals (Quality)<br/>• Authoritative FCF (OCF - CapEx)"]
        EDGAR_FETCH -->|Rate-Limited / Down| EDGAR_FAIL["Degrade Gracefully:<br/>Keep base ratios without EDGAR"]
    end

    %% Storage & Caching
    subgraph CACHE ["5. Storage, Snapshots & Attribution"]
        direction TB
        DUCK_CACHE[("data/mycase.duckdb")]
        EDGAR_OK --> MERGE["Merged Fundamental Record"]
        EDGAR_FAIL --> MERGE
        MERGE -->|Persist| DUCK_CACHE
        
        DUCK_CACHE --- CIK_TTL["CIK TTL: 7 Days<br/>XBRL Facts TTL: 80 Days"]
        
        BENCHMARK["Fetch Benchmark: S&P 500 (^GSPC)<br/>(Beta Attribution & Rel Strength)"]
        GOLDEN["Compare data/us_portfolio.csv<br/>(Hysteresis / Prevent Churn)"]
        
        AUDIT["Log Run:<br/>• pipeline_runs<br/>• pipeline_index_picks<br/>• pipeline_proposals<br/>• PIT Snapshots (pithistory)"]
    end

    %% Cross-Subsystem Connections
    US_PROFILE --> LOADER
    PARSE --> ROUTER
    PARSE --> CHECK_CACHE
    SCHWAB_PRICE --> BENCHMARK
    YFIN_PRICE --> BENCHMARK
    HIT_CACHE --> BENCHMARK
    MERGE --> BENCHMARK
    BENCHMARK --> GOLDEN
    GOLDEN --> AUDIT

    %% Styling
    classDef warn fill:#fff0f0,stroke:#d93025,stroke-width:1.5px,color:#900;
    classDef pass fill:#f0fbf0,stroke:#1e8e3e,stroke-width:1.5px,color:#080;
    classDef storage fill:#f8f9fa,stroke:#5f6368,stroke-width:1.5px;
    
    class CMD_WRONG,FALLBACK warn;
    class CMD_OK,US_PROFILE pass;
    class DUCK_CACHE,DISK_CSV storage;
```

---

## Stage Breakdown

| Stage | Component / Source | Details & Fallbacks |
|---|---|---|
| **1. Profile Resolution** | [cmd/pipeline.go](file:///Users/raghavgarg/Projects/myGo/mycase/cmd/pipeline.go) | Run with `--profile us-momentum`. `--market us` does not suffix-match `us-momentum` and falls back to `india-multibagger`. |
| **2. Constituents** | [pkg/stockpicker/loader.go](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/loader.go) | Fetches S&P 500 CSV via [config/reference/us/csvlinks.json](file:///Users/raghavgarg/Projects/myGo/mycase/config/reference/us/csvlinks.json). Caches to `data/universe/sp500.csv` and tags tickers as `US:<SYMBOL>`. |
| **3. Quotes & Prices** | [pkg/datafetcher/router.go](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/datafetcher/router.go) | Primary: Charles Schwab API (`config/schwab.json`, `config/schwab_token.json`). Degrades to Yahoo Finance if unauthenticated or down. |
| **4. Fundamentals** | Schwab + SEC EDGAR XBRL | Primary ratios from Schwab. Authoritative cash flows (OCF, CapEx, Sloan Accruals) pulled from SEC EDGAR. Cached to avoid redundant calls. |
| **5. Audit & State** | DuckDB (`data/mycase.duckdb`) | Stores runs, index picks, proposals, benchmark attribution (`^GSPC`), and evaluates drift vs `data/us_portfolio.csv`. |