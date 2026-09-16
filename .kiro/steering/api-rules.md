# External API Usage Rules

## Schwab Trader API

### Rate Limits
- **Hard cap**: 120 requests/minute (enforced by a `golang.org/x/time/rate` token bucket in `pkg/broker/schwab/client.go`: `rate.Limit(2)`/sec, burst 120; `executeRequest` calls `limiter.Wait(ctx)` before every send)
- **Order limit**: 120 per app registration
- **Auth tokens**: Access token expires in 30 minutes, refresh token in 7 days
- **Fundamentals**: fetched one ticker at a time via `/instruments?symbol=X&projection=fundamental`

### Usage Discipline
- **Never burst**: Space requests evenly. The code rate-limiter handles this, but when testing manually or adding new endpoints, respect the 120/min ceiling.
- **Cache aggressively**: DuckDB cache (`pkg/cache/`) stores prices and fundamentals. Always check cache before hitting the API. Price cache expires daily (IST); fundamentals cache expires after 24h.
- **Save raw responses during development**: Automated via `pkg/rawcapture` (the zero-import hook) + `pkg/rawstore` (the filesystem sink). Capture is **ON by default** — every 2xx API body is archived flat to `data/raw/<source>__<endpoint>__<symbol>__<YYYYMMDD-HHMMSS>.json` (both the Schwab and Yahoo client chokepoints are hooked; token/auth responses are never captured). Opt out with `MYCASE_CAPTURE=0` (or `false`/`no`/`off`). Analyze the archived JSON offline. To rerun a whole command against recorded responses with **zero live calls**, set `MYCASE_REPLAY=1` — each chokepoint serves the newest matching archive as a synthetic 200 and skips the network, token, and rate limiter (replay also suppresses capture, so replayed bodies aren't re-archived). Do NOT repeatedly call the same endpoint from different angles — fetch once, inspect/replay locally.
- **Batch where possible**: Use the multi-symbol `/quotes?symbols=A,B,C` endpoint instead of individual calls when fetching prices.
- **No unnecessary re-fetches**: If a command fails mid-way (e.g., scoring crashes after fundamentals are fetched), the cached fundamentals survive. Fix the bug and re-run — the cache will serve warm data.

### Testing Rules
- Test against production (sandbox is unreliable and returns static data)
- The `pick` command is **read-only** — no risk of accidental trades
- Order execution requires explicit `--live` flag + confirmation prompt
- When developing new API integrations, start with 1-2 tickers to validate response shape, then scale up

### Token Safety
- `config/schwab.json` — app credentials (client_id, client_secret). **Never commit.**
- `config/schwab_token.json` — OAuth tokens. **Never commit.**
- Both are in `.gitignore`.

## Yahoo Finance (Fallback)
- No API key required (public endpoints)
- Rate limit: ~2000 requests/hour (unofficial, aggressive bursts get 429s)
- Paced by a process-wide `golang.org/x/time/rate` token bucket in `pkg/yfinance` (10 req/s, burst 20; gated in `executeYFinanceRequest` + the fundamentals timeseries call; overridable via `yfinance.SetRateLimiter`)
- 15-worker concurrent pool for historical prices (`pkg/stockpicker/loader.go`) bounds *concurrency*; the limiter bounds aggregate *rate* across all pools
- Cache applies identically — always check DuckDB first

## General Principles
1. **Fetch once, analyze many times** — save responses locally for debugging
2. **Respect rate limits as a budget** — 120/min is not "try to hit 120/min", it's a ceiling
3. **Cache is truth during a session** — if data is fresh today, don't re-fetch
4. **Fail gracefully** — if an API call fails for one ticker, skip it and continue (never abort the full run)
5. **Log API errors, don't panic** — downstream scoring handles missing data with imputation or exclusion
