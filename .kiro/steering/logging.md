# Logging Conventions (slog)

Structured logging is built on stdlib `log/slog` via the `pkg/logging` package
(see `docs/refactor.md` Phase R14). Follow these rules when adding or converting
diagnostic output.

## The two-channel rule (most important)

There are two distinct output channels. Never conflate them:

| Channel | Purpose | Mechanism | Example |
|---------|---------|-----------|---------|
| **User output** | Deliberate CLI results the investor reads | `pkg/render` (tables, KV, sections) + direct `fmt` to **stdout** | Holdings table, harvest candidates, pipeline diff |
| **Logs** | Diagnostic / operational trace | `slog` → **stderr** (text) + JSON file | "fetched 47 quotes in 320ms", "Schwab 401, refreshing token", "drift check fired" |

- Command *results* stay on **stdout** via `fmt`/`pkg/render` — they are the product.
- Everything diagnostic goes to **`slog`** (stderr + file), and must never pollute stdout.
- A user piping `mycase holdings` into a script must still get clean tabular stdout.

## Classify before converting

Every `fmt.Print*` call site is one of:

1. **User-facing result** → leave on stdout (or migrate to `pkg/render`). *No slog.*
2. **Diagnostic / operational** ("Warning: ...", daemon status, API retry, counts, durations) → convert to `slog` at the appropriate level.
3. **Interactive UX** (e.g. "🔐 Opening browser...") → keep on stdout if genuinely interactive; use `slog.Info` if it's really operational status.

Do not blind find-replace. Classify each site.

## Levels

- **Debug** — per-item fetch/score internals, HTTP request/response bodies-of-metadata, DB ops.
- **Info** — stage transitions, counts, durations, scheduled events.
- **Warn** — recoverable: skipped ticker, cache-miss fallback, API retry, alert-send failure.
- **Error** — an operation failed but the process continues. **Never `Fatal`/`os.Exit`** — commands return `error` to `urfave/cli`.

## How to log

- **Use `slog.SetDefault` package-level calls with context**: `slog.InfoContext(ctx, ...)`, `slog.WarnContext(ctx, ...)`, `slog.ErrorContext(ctx, ...)`. `main.go`'s `Before` hook sets the default logger and attaches a `req_id` to `ctx`, so context-aware calls get end-to-end tracing for free.
- **Always thread `ctx`** so the `req_id` attaches. Prefer the `*Context` variants over bare `slog.Info`.
- **Structured attrs, not formatted strings**:
  - Good: `slog.InfoContext(ctx, "quotes.fetched", "count", n, "ms", d.Milliseconds())`
  - Bad: `slog.Info(fmt.Sprintf("fetched %d quotes", n))`
- **Dotted event names** as the message: `"daemon.check_completed"`, `"nav.ticker_skipped"`, `"http.response"`. Keep the human detail in attrs.
- **Hot paths / long-running components** (daemon, fetchers) may take an explicit `*slog.Logger` field for testability (see `pkg/attribution.Tracker`); default to `slog.Default()` when nil.

## Never log

- Full API responses, request/response bodies, or credentials.
- Reference secrets by **key name**, never value (e.g. log `"token_source", "env"`, not the token).
- Log counts, symbols, status codes, and durations instead of payloads.

## Helpers (`pkg/logging`)

- `logging.Timer(ctx, log, "pick.score", ...)()` — defer to emit an operation's duration.
- `logging.LogRequest` / `logging.LogResponse` — outbound HTTP; response level scales with status (>=500 Error, >=400 Warn, else Info). Use these on Schwab / datafetcher / yfinance paths — this is how the API rule "Log API errors, don't panic" becomes enforceable.
- `logging.LogDBOp` — DB operation timing (Debug).
- `logging.GenerateReqID` / `WithReqID` / `ReqID` — req_id lifecycle (wired in `main.go`).

## Config & flags

Logging is configured via `config/defaults.json` (`logging` block), env, and global CLI flags. Precedence: **flag > env > config > default**.

- Flags: `--log-level {debug|info|warn|error}`, `--log-dir`, `--quiet` (stderr off, file only), `--verbose` (= `--log-level debug`).
- Env: `MYCASE_LOG_LEVEL`, `MYCASE_LOG_DIR`.
- Defaults: level `info`, dir `data/logs`, file on, 14-day retention.
