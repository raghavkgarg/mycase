# Configuration Directory Inventory

Everything the binary reads at startup lives under `config/`, and it is deliberately small:
two YAML files carry all tunable behaviour, a pair of Schwab credential files hold secrets,
and a `reference/` tree holds market-specific reference data. The directory is read-only at
runtime — the engine never writes back into it (runtime state goes under `data/`, covered in
[Data Directory Inventory](2-70-data-inventory.md)).

`config/` is resolved relative to a single home root, so the same binary finds its config
whether run from the project tree, from `dist/`, or symlinked into `/usr/local/bin`.
Precedence, implemented in `pkg/config/paths.go` (`Home`): `$MYCASE_HOME`, then
binary-relative (walk up from the executable to a directory that contains `config/`), then
the current working directory. `$MYCASE_CONFIG_DIR` overrides just the config directory.

---

## 1. The directory

```
config/
├── defaults.yaml        # master config: markets, strategies, gates, system settings
├── pipeline.yaml        # profile-driven end-to-end pipeline definitions
├── schwab.json          # [SECRET, git-ignored] Schwab app credentials + OAuth tokens
├── schwab_token.json    # [SECRET, git-ignored] cached Schwab OAuth token
├── configSchwab.json    # committed template for schwab.json (placeholder values)
└── reference/
    ├── india/           # csvlinks, governance, management_alerts, sector_tam, themes
    └── us/              # csvlinks
```

`config/schwab.json`, `config/schwab_token.json`, and `config/config.json` are listed in
`.gitignore` and must never be committed; `config/configSchwab.json` is the committed
template with placeholder keys.

---

## 2. `defaults.yaml` — the master config

One commented YAML file loaded by `pkg/config/defaults_yaml.go` and consumed across
`pkg/stockpicker`, `pkg/marketcal`, and the `cmd/` layer. It is organized around a
**dual-country** model: a top-level `active_market` selects the default country for
zero-flag commands, and each market carries its own strategies, calendar, and defaults.

The top-level shape:

- **`active_market`** — `india` or `us`; the market zero-flag CLI commands resolve against.
- **`system`** — operational plumbing: `logging` (dir, level, retention — see
  [Logging & Observability](3-10-logging.md)), `raw_capture` (retention and size ceilings for
  the raw archive), and `scheduler` (EOD/drift/rebalance toggles and timing).
- **`markets`** — the substance, keyed by country (`india`, `us`). Each market defines its
  broker, currency, exchange, benchmark, timezone, close time, default index/strategy/golden
  copy, and — under `strategies` — the per-strategy screening rules and scoring weights.

Every strategy's tunables live under `markets.<country>.strategies.<name>`: `multibagger`,
`earlymb`, `value`, `fairprice`, `us_quality_momentum`, and the baseline `standard`. So the
[Fair Price](1-35-strategy-fair-price.md) model weights, the [Value](1-30-strategy-value.md)
anti-trap thresholds, and the Early Multibagger gates are all edited in this one file, scoped
to the market they apply to, rather than scattered across per-strategy JSON files.

---

## 3. `pipeline.yaml` — profile-driven execution

Loaded by `pkg/config/pipeline.go` and consumed by `cmd/pipeline.go`. Rather than one flat
pipeline, it defines named **profiles** — `india-multibagger`, `india-earlymb`,
`us-momentum` — each fixing an index set, golden-copy path, strategy, sizing, hysteresis and
cooldown parameters, sector caps, and broker. `default_profile` selects one when `mycase
pipeline` runs with no flags, and any field is overridable at the command line (`--index`,
`--strategy`, `--golden`, …).

Each profile drives the same end-to-end sequence: `sync` (prices, delivery, fundamentals into
`mycase.db`), `pick` (factor scoring and Stage-1 gates), `rebalance` (hysteresis matching
against the active golden copy), `performance` (benchmark attribution), and `backup`
(timestamped allocation snapshots). A set of top-level keys mirrors a profile as a
backward-compatible fallback.

---

## 4. Schwab credentials

The US path authenticates against the Schwab Trader API using two secret files, both
git-ignored: `config/schwab.json` (app `client_id`, `client_secret`, `callback_url`) and
`config/schwab_token.json` (the cached OAuth access/refresh tokens written by the auth flow).
`config/configSchwab.json` is the committed placeholder template — copy it to `schwab.json`
and fill in real app keys. The `us-momentum` pipeline profile points at both paths explicitly.
Secrets are referenced by key name and never logged by value.

---

## 5. `reference/`

Market-specific reference data that changes rarely and is checked in, split by country:

- **`reference/india/`** — `csvlinks.json` (NSE constituent download URLs), plus the
  Indian-equity domain knowledge that enriches scoring and research: `governance.json`,
  `management_alerts.json`, `sector_tam.json`, and `themes.json`.
- **`reference/us/`** — `csvlinks.json` for US index constituents.
