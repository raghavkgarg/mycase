-- holiday.sql — one-time LOCAL bootstrap for the `holidays` table in mycase.db.
--
-- WHAT THIS IS: a convenience snapshot of the currently-known NYSE + NSE full-day
-- closures (2026–2027), so a fresh checkout/machine can populate its local DB in
-- one command instead of hand-entering dates. Run it against your local cache DB:
--
--     duckdb data/mycase.db < holiday.sql
--
-- WHAT THIS IS NOT: it is NOT the authoritative source, and NO application code
-- reads this file. The single source of truth is the `holidays` table itself
-- (data/mycase.db is git-ignored, so each machine seeds its own). The authoritative
-- calendar is whatever each exchange publishes (NYSE hours-calendars page; the NSE
-- annual trading-holiday circular) — see docs/18-runbook.md for the yearly refresh
-- workflow. Do NOT treat editing this file as "updating the holidays": update the
-- table (via the duckdb CLI against the official calendar) and, if you like, refresh
-- this snapshot afterwards. Re-running is safe (ON CONFLICT DO NOTHING).
--
-- Conventions: full-day closures only (early-close half-days are omitted — the
-- market is open); weekend-falling holidays omitted (weekends are already
-- non-trading); dates are YYYY-MM-DD in the exchange's local timezone.

CREATE TABLE IF NOT EXISTS holidays (
    exchange VARCHAR NOT NULL,
    date     VARCHAR NOT NULL,
    PRIMARY KEY (exchange, date)
);

INSERT INTO holidays (exchange, date) VALUES
    -- NYSE 2026
    ('NYSE', '2026-01-01'),
    ('NYSE', '2026-01-19'),
    ('NYSE', '2026-02-16'),
    ('NYSE', '2026-04-03'),
    ('NYSE', '2026-05-25'),
    ('NYSE', '2026-06-19'),
    ('NYSE', '2026-07-03'),
    ('NYSE', '2026-09-07'),
    ('NYSE', '2026-11-26'),
    ('NYSE', '2026-12-25'),
    -- NYSE 2027
    ('NYSE', '2027-01-01'),
    ('NYSE', '2027-01-18'),
    ('NYSE', '2027-02-15'),
    ('NYSE', '2027-03-26'),
    ('NYSE', '2027-05-31'),
    ('NYSE', '2027-06-18'),
    ('NYSE', '2027-07-05'),
    ('NYSE', '2027-09-06'),
    ('NYSE', '2027-11-25'),
    ('NYSE', '2027-12-24'),
    -- NSE 2026
    ('NSE', '2026-01-26'),
    ('NSE', '2026-03-03'),
    ('NSE', '2026-03-26'),
    ('NSE', '2026-03-31'),
    ('NSE', '2026-04-03'),
    ('NSE', '2026-04-14'),
    ('NSE', '2026-05-01'),
    ('NSE', '2026-05-28'),
    ('NSE', '2026-06-26'),
    ('NSE', '2026-09-14'),
    ('NSE', '2026-10-02'),
    ('NSE', '2026-10-20'),
    ('NSE', '2026-11-10'),
    ('NSE', '2026-11-24'),
    ('NSE', '2026-12-25')
ON CONFLICT DO NOTHING;
