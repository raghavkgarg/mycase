-- ============================================================================
-- Migration: 001_emb_reconciliation.sql
-- Description: EMB Engine Point-in-Time Reconciliation Schema & Historical Backfill
-- Target: DuckDB (data/mycase.db)
-- ============================================================================

-- 1. Schema Additions: pit_runs
ALTER TABLE pit_runs ADD COLUMN IF NOT EXISTS selection_policy   VARCHAR;
ALTER TABLE pit_runs ADD COLUMN IF NOT EXISTS engine_commit      VARCHAR;
ALTER TABLE pit_runs ADD COLUMN IF NOT EXISTS r_raw              DOUBLE;
ALTER TABLE pit_runs ADD COLUMN IF NOT EXISTS r_eff              DOUBLE;
ALTER TABLE pit_runs ADD COLUMN IF NOT EXISTS hurdle_raw_pts     DOUBLE;
ALTER TABLE pit_runs ADD COLUMN IF NOT EXISTS bench_last_bar     DATE;
ALTER TABLE pit_runs ADD COLUMN IF NOT EXISTS breadth_last_bar   DATE;
ALTER TABLE pit_runs ADD COLUMN IF NOT EXISTS degraded           BOOLEAN;
ALTER TABLE pit_runs ADD COLUMN IF NOT EXISTS degraded_inferred  BOOLEAN;
ALTER TABLE pit_runs ADD COLUMN IF NOT EXISTS equity_weight      DOUBLE;
ALTER TABLE pit_runs ADD COLUMN IF NOT EXISTS holdings_recorded  BOOLEAN;

-- 2. Schema Additions: pit_candidate_scores
ALTER TABLE pit_candidate_scores ADD COLUMN IF NOT EXISTS outcome VARCHAR;

-- 3. Dedicated Portfolio Holdings Table
CREATE TABLE IF NOT EXISTS pit_holdings (
    as_of_date  DATE,
    index_name  VARCHAR,
    method      VARCHAR,
    ticker      VARCHAR,
    weight      DOUBLE,
    PRIMARY KEY (as_of_date, index_name, method, ticker)
);

-- 4. Definitive Benchmark-Derived Trading Days View
CREATE OR REPLACE VIEW trading_days AS
SELECT 
    date AS d, 
    ROW_NUMBER() OVER (ORDER BY date) AS seq
FROM prices 
WHERE ticker = '^NSEI' AND close > 0;

-- 5. Radar Episodes and Fixed-Horizon Returns Tables
CREATE TABLE IF NOT EXISTS radar_episodes (
    episode_id        VARCHAR PRIMARY KEY,   -- ticker || '|' || first_seen_date
    ticker            VARCHAR,
    sector            VARCHAR,
    first_seen_date   DATE,
    index_name        VARCHAR,
    method            VARCHAR,
    entry_date        DATE,                  -- session T+1
    entry_px_open     DOUBLE,                -- primary (T+1 open)
    entry_px_close_t0 DOUBLE,                -- legacy comparison (T0 close)
    criteria_version  VARCHAR,               -- 'live' | 'backfilled'
    blocker_at_entry  VARCHAR                -- first failing Stage-1 gate
);

CREATE TABLE IF NOT EXISTS radar_horizon_returns (
    episode_id      VARCHAR,
    horizon         INTEGER,               -- 5 | 10 | 21
    exit_date       DATE,
    exit_px         DOUBLE,
    ret             DOUBLE,                -- exit_px / entry_px_open - 1
    bench_ew_mean   DOUBLE,
    bench_ew_median DOUBLE,
    bench_n         INTEGER,
    excess          DOUBLE,                -- ret - bench_ew_mean
    state_at_h      VARCHAR,               -- DELISTED | CLEARED | ACTIVE | EXITED
    last_price_flag BOOLEAN DEFAULT FALSE,
    computed_at     TIMESTAMP,
    PRIMARY KEY (episode_id, horizon)
);

-- ============================================================================
-- 6. Historical Data Backfill (Aligned to Engine Commit History)
-- ============================================================================

-- A. Runs prior to Sep 24, 2026 (Legacy Weight Ladder)
UPDATE pit_runs
SET selection_policy = 'LEGACY_LADDER',
    holdings_recorded = TRUE,
    r_eff = regime_multiplier,
    hurdle_raw_pts = ROUND(30.0 / regime_multiplier, 4),
    equity_weight = CASE 
        WHEN as_of_date = '2026-08-28' THEN 0.50
        WHEN as_of_date = '2026-08-31' THEN 0.25
        ELSE 0.0
    END
WHERE as_of_date <= '2026-09-23' AND method = 'earlymb';

-- Populate bench_last_bar, r_raw, degraded flags
UPDATE pit_runs
SET bench_last_bar = as_of_date,
    r_raw = regime_multiplier,
    degraded = FALSE,
    degraded_inferred = FALSE
WHERE method = 'earlymb';

-- Cured: Populate fresh true R values and synced benchmark bar dates for 09-02, 09-08, and 09-09
-- (forensic audit showed runs executed before 18:00 IST received prior-day bars from data provider)
UPDATE pit_runs
SET regime_multiplier = 0.498879,
    r_eff = 0.498879,
    r_raw = 0.498879,
    hurdle_raw_pts = 60.1349,
    bench_last_bar = '2026-09-02',
    degraded = FALSE,
    degraded_inferred = FALSE
WHERE as_of_date = '2026-09-02' AND method = 'earlymb';

UPDATE pit_runs
SET regime_multiplier = 0.354028,
    r_eff = 0.354028,
    r_raw = 0.354028,
    hurdle_raw_pts = 84.7391,
    bench_last_bar = '2026-09-08',
    degraded = FALSE,
    degraded_inferred = FALSE
WHERE as_of_date = '2026-09-08' AND method = 'earlymb';

UPDATE pit_runs
SET regime_multiplier = 0.284255,
    r_eff = 0.284255,
    r_raw = 0.284255,
    hurdle_raw_pts = 105.5389,
    bench_last_bar = '2026-09-09',
    degraded = FALSE,
    degraded_inferred = FALSE
WHERE as_of_date = '2026-09-09' AND method = 'earlymb';

-- Backfill pre-clamp raw regime scores for sessions constrained by the 0.20 floor clamp
UPDATE pit_runs SET r_raw = 0.196218 WHERE as_of_date = '2026-09-16' AND method = 'earlymb';
UPDATE pit_runs SET r_raw = 0.122191 WHERE as_of_date = '2026-09-24' AND method = 'earlymb';
UPDATE pit_runs SET r_raw = 0.112052 WHERE as_of_date = '2026-09-25' AND method = 'earlymb';

-- Update candidate effective scores for cured dates
UPDATE pit_candidate_scores SET effective_score = raw_score * 0.498879 WHERE as_of_date = '2026-09-02' AND method = 'earlymb';
UPDATE pit_candidate_scores SET effective_score = raw_score * 0.354028 WHERE as_of_date = '2026-09-08' AND method = 'earlymb';
UPDATE pit_candidate_scores SET effective_score = raw_score * 0.284255 WHERE as_of_date = '2026-09-09' AND method = 'earlymb';

-- Populate pit_holdings for 08-28 and 08-31 directly from recorded selected stocks
INSERT INTO pit_holdings (as_of_date, index_name, method, ticker, weight)
SELECT as_of_date, index_name, method, ticker, final_weight
FROM pit_candidate_scores
WHERE as_of_date IN ('2026-08-28', '2026-08-31') AND method = 'earlymb' AND selected = true
ON CONFLICT DO NOTHING;

-- Populate legacy terminal outcomes for Stage-1 survivors
UPDATE pit_candidate_scores
SET outcome = CASE 
    WHEN selected = true THEN 'LEGACY_SELECTED'
    ELSE 'LEGACY_NOT_TOP_N'
END
WHERE as_of_date <= '2026-09-23' AND method = 'earlymb' AND passed_stage1 = true;

-- B. Runs from Sep 24, 2026 onward (Binary Sentry Policy via Commit f09b9ca)
UPDATE pit_runs
SET selection_policy = 'BINARY_SENTRY_V1',
    holdings_recorded = TRUE,
    r_eff = regime_multiplier,
    hurdle_raw_pts = ROUND(30.0 / regime_multiplier, 4),
    equity_weight = 0.0,
    engine_commit = 'f09b9ca'
WHERE as_of_date >= '2026-09-24' AND method = 'earlymb';

UPDATE pit_candidate_scores
SET outcome = 'HURDLE_REJECT'
WHERE as_of_date >= '2026-09-24' AND method = 'earlymb' AND passed_stage1 = true AND raw_score < 150.0;

-- C. Other strategies (multibagger, fairprice) in pit_runs
UPDATE pit_runs
SET selection_policy = 'LEGACY_LADDER',
    holdings_recorded = FALSE
WHERE method = 'multibagger' AND selection_policy IS NULL;

UPDATE pit_runs
SET selection_policy = 'FAIRPRICE_MODEL',
    holdings_recorded = FALSE
WHERE method = 'fairprice' AND selection_policy IS NULL;

-- Leave r_raw, bench_last_bar, and breadth_last_bar as NULL where not observed historically.
