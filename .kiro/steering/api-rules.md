# External API Usage Rules

## Schwab Trader API

### Rate Limits
- **Hard cap**: 120 requests/minute (enforced by client-side sliding window)
- **Order limit**: 120 per app registration
- **Auth tokens**: Access token expires in 30 minutes, refresh token in 7 days
- **Fundamentals**: fetched one ticker at a time via `/instruments?symbol=X&projection=fundamental`

### Usage Discipline
- **Never burst**: Space requests evenly. The code rate-limiter handles this, but when testing manually or adding new endpoints, respect the 120/min ceiling.
- **Cache aggressively**: DuckDB cache (`pkg/cache/`) stores prices and fundamentals. Always check cache before hitting the API. Price cache expires daily (IST); fundamentals cache expires after 24h.
- **Save raw responses during development**: When triaging issues or exploring new endpoints, save the full JSON response to a local file (e.g., `data/debug/schwab_response_YYYYMMDD.json`) and analyze offline. Do NOT repeatedly call the same endpoint from different angles — fetch once, inspect locally.
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
- 15-worker concurrent pool for historical prices (`pkg/stockpicker/loader.go`)
- Cache applies identically — always check DuckDB first

## General Principles
1. **Fetch once, analyze many times** — save responses locally for debugging
2. **Respect rate limits as a budget** — 120/min is not "try to hit 120/min", it's a ceiling
3. **Cache is truth during a session** — if data is fresh today, don't re-fetch
4. **Fail gracefully** — if an API call fails for one ticker, skip it and continue (never abort the full run)
5. **Log API errors, don't panic** — downstream scoring handles missing data with imputation or exclusion
