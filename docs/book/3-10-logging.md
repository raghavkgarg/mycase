# Logging & Observability

Two pieces of cross-cutting machinery make a mostly-headless quarterly system
debuggable: **structured logging** (`pkg/logging`, built on stdlib `log/slog`) and the
**raw-response archive** (`pkg/rawcapture` + `pkg/rawstore`, surfaced by `mycase raw`).
The first tells you *what the system did*; the second lets you see *exactly what an
external API returned*, offline, after the fact.

The enforced day-to-day logging conventions (levels, event naming, what never to log)
live in `.kiro/steering/logging.md` — this chapter is the design behind them.

---

## 1. Structured logging (`pkg/logging`)

### The two-channel rule

The system has two distinct output channels that must never be conflated:

| Channel | Purpose | Mechanism | Example |
|---------|---------|-----------|---------|
| **User output** | Deliberate CLI results the investor reads | `pkg/render` (tables, KV, sections) + `fmt` to **stdout** | Holdings table, harvest candidates, pipeline diff |
| **Logs** | Diagnostic / operational trace | `slog` → **stderr** (text) + JSON file | "fetched 47 quotes in 320ms", "Schwab 401, refreshing token", "drift check fired" |

Command *results* are the product and stay on **stdout**. Everything diagnostic goes to
**`slog`** (stderr + a JSON file) and never pollutes stdout — so a user piping `mycase
holdings` into a script still gets clean tabular output.

### Fanout handler

`pkg/logging` provides a fanout handler that writes each record two ways: human-readable
text to **stderr** and machine-readable JSON to a **daily rotating file**
(`mycase-YYYY-MM-DD.jsonl`), each channel level-gated independently. Log files are
auto-cleaned after a retention window (default 14 days).

### Request tracing

Every command invocation gets a unique `req_id` (`GenerateReqID` → `WithReqID`), attached
to `context.Context` in `main.go`'s `Before` hook. Sub-operations (API calls, DB ops,
fetch batches) log with the parent `req_id`, so a whole run traces end-to-end. `main.go`
also calls `slog.SetDefault`, so any package can emit `slog.InfoContext(ctx, ...)` without
threading a `*Logger` — while hot paths / long-running components (the daemon, the
attribution tracker) may take an explicit `*slog.Logger` field for testability, defaulting
to `slog.Default()` when nil.

### Helpers

- `logging.Timer(ctx, log, "pick.score", ...)()` — deferred, emits an operation's duration.
- `logging.LogRequest` / `logging.LogResponse` — outbound HTTP; response level scales with
  status (≥500 Error, ≥400 Warn, else Info). Wired into the Schwab / yfinance / datafetcher
  paths — this is how the API rule "log API errors, don't panic" is enforced.
- `logging.LogDBOp` — DB operation timing (Debug).

### Package layout & configuration

```
pkg/logging/
├── logging.go   # Setup, Config, Logger wrapper, fanout handler
├── context.go   # WithReqID, ReqID, GenerateReqID, child logger
└── helpers.go   # Timer, LogRequest, LogResponse, LogDBOp
```

Configured via the `logging` block in `config/defaults.json`, overridable by env and
global CLI flags with precedence **flag > env > config > default**:

- Flags: `--log-level {debug|info|warn|error}`, `--log-dir`, `--quiet` (file only, no
  stderr), `--verbose` (= `--log-level debug`).
- Env: `MYCASE_LOG_LEVEL`, `MYCASE_LOG_DIR`.
- Defaults: level `info`, dir `data/logs`, file on, 14-day retention.

The choice of stdlib `slog` (over zap/logrus) and the absence of distributed tracing are
deliberate — this is a single-binary local tool, and minimal dependencies is a project
constraint.

---

## 2. Raw-response archive (`pkg/rawcapture` + `pkg/rawstore`)

### Why it exists

The motivating failure: Schwab returned HTTP **200** hundreds of times but a report came
out empty, and the response bodies had been thrown away — so there was no way to tell a
**data issue** (Schwab genuinely returned thin/zero fields) from a **code issue** (the
mapper mis-parsed a full body). Transport was never in doubt; the lost evidence was.

The archive enforces the API discipline rule *"fetch once, analyze offline"*: every
successful external API body is saved to disk so it can be inspected — and, if needed,
replayed — without re-hitting the network.

Three independent activities, in order of value:

1. **Capture** — record a response as it arrives.
2. **Triage** — read and understand recorded responses (the primary, everyday need).
3. **Replay** — re-serve recorded responses to a whole run with zero live calls (a distant
   third; it only pays off after triage has localized a bug to *code*).

### Capture is on by default

Capture defaults **on**, because the failure mode is evidence loss — the run that
surprises you is the run you didn't think to arm. It is suppressed only by an explicit
falsy `MYCASE_CAPTURE` (`0`/`false`/`no`/`off`) or by replay mode (replayed bytes are
never re-archived). Sampling is deliberately *not* done: at ~500 tickers a quarter the
volume is single-digit MB, and sampling would drop exactly the one weird ticker triage
needs. Growth is bounded by **retention**, not by declining to capture.

On a 2xx, the capturing client buffers the body, writes the raw bytes flat to
`data/raw/<source>__<endpoint>__<symbol>__<YYYYMMDD-HHMMSS>.json`, and returns a fresh
reader over the same bytes so callers are unchanged. Non-2xx bodies (including 401) and
OAuth token flows are never archived, so credentials never touch the archive.

### Retention

Two independent ceilings bound the archive; a file is pruned if it violates *either*:

- **Age** — `raw.retain_days` (default 14).
- **Total size** — `raw.max_size_mb` (default 512), oldest-first eviction until under cap.

Both live in the `raw` block of `config/defaults.json`, env-overridable
(`MYCASE_RAW_RETAIN_DAYS` / `MYCASE_RAW_MAX_SIZE_MB`) and flag-overridable
(flag > env > config > default). Pruning is best-effort (unreadable/foreign files are
skipped, never fatal), runs inline at process exit, and is also available as
`mycase raw prune`. The policy is deliberately "keep everything recent, prune only the
boring old middle" — false-keep costs disk; false-delete costs an unreproducible bug.

### Triage — `mycase raw`

Schema-blind commands that work for *every* source (Schwab, Yahoo, and any future source)
the day it captures, because they parse only the filename convention:

- `raw ls` — list the archive as columns (source, endpoint, symbol, when, size),
  newest-first, with `--source` / `--endpoint` / `--symbol` substring filters and `-n`.
- `raw show [query]` — pretty-print the newest matching capture (JSON indented, else
  verbatim). `query` matches the symbol first, then the whole filename.
- `raw path [query]` — print just the path, for piping (`jless "$(mycase raw path AAPL)"`).

On top of the schema-blind spine sit **per-source field inspectors** that show *raw wire
value vs. what production mapped*. `mycase raw inspect [symbol]` parses the newest archived
Schwab `/instruments` body exactly as production does, reruns the real mapper, and renders
each raw field beside the value it produced — plus a cross-check that names any wire key
the struct silently dropped. This is the tool that localizes a mapping bug: because the
mapper never zeros a nonzero input, a mapped `0` means either the wire genuinely sent `0`
(a data issue) or the value arrived under an undeclared key (a code issue), and the
inspector disambiguates the two. An inspector lives *with its source* and reuses the
production parser, so it stays truthful and extending to a new source touches only that
source.

### Replay

With `MYCASE_REPLAY` set, each API-client chokepoint calls the archive *first*; on a hit it
returns the newest matching archive body as a synthetic 200 and short-circuits the network,
token fetch, and rate limiter entirely. This can rerun a whole `pick`/pipeline run against
recorded responses with zero live calls. In practice capture + triage has carried the
debugging load and replay has seen little use — its long-term keep-or-cut status is tracked
in the Roadmap.

### Layering

The split is a deliberate dependency inversion so the capture hook can sit low enough to be
called from inside the API clients while retention/config/naming policy sits high:

- **`pkg/rawcapture`** (L0 leaf, zero imports) — declares the `Sink` interface
  (`Write`/`Open`), a settable sink var + `SetSink`, and thin `Capture`/`Replay`
  delegators. It holds no filenames, directories, or config, and is an inert no-op until a
  sink is wired (correct for tests and library use). Because it is import-free it can be
  called from the `yfinance` and `broker/schwab` client chokepoints.
- **`pkg/rawstore`** (L4) — implements `Sink`; owns the data dir (via `config`), the
  `<source>__<endpoint>__<symbol>__<stamp>.json` filename convention (`Filename` /
  `ParseFilename` / `List` / `ResolvePath`), run identity, and retention.
- **`main.go`'s `Before` hook** constructs the store and calls `rawcapture.SetSink(...)`.
  Only the composition root names `rawstore`; nothing below L4 references it, so the
  strictly-downward import rule (Ch. 4) holds.

This is the same shape as the persistence pattern elsewhere — a consumer-defined interface
in a leaf, with the concrete store owned higher up.
