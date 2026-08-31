# Package Architecture & Layering

The `pkg/` graph is organized into layers. **Imports must always go strictly
downward** — a package may import only packages at a lower layer, never the same
layer or higher. This is enforced by `scripts/checkdeps` (run via `make check-deps`,
and part of `make cleanup`). Go rejects import *cycles* at compile time; this guard
additionally preserves the *direction* and *leaf-ness* that phase R16 established
(see `docs/refactor.md`).

## The core rule: define OR consume, don't do both low in the stack

The cycle-magnets R16 removed all shared one trait: a low-level package that both
**defined** widely-shared types **and** **imported** heavy dependencies. The fix,
and the rule going forward:

- **Shared DTOs live in zero-import leaf packages.** If a type is used across
  package boundaries, it belongs in a leaf (`marketdata`, `broker/types`), not in
  a package that also wires behavior/config.
- **Interfaces are defined by their consumer.** The high-level package that needs
  an abstraction defines the interface; the low-level implementer satisfies it
  structurally and must NOT import the consumer to prove it (e.g. `datafetcher`
  does not import `stockpicker`). If you want a compile-time assert, put it in a
  package that legitimately imports both.
- **Domains own their persistence.** A domain package that needs storage takes a
  `*sql.DB` handle (via `cache.Conn()`) and defines its own tables + Insert/Get
  methods (`attribution.Store`, `tax.Store`). `pkg/cache` must never import a
  domain package.

## Layers (as enforced by scripts/checkdeps)

| Layer | Packages | Role |
|-------|----------|------|
| **L0 — leaves** | `alert`, `broker/types`, `cache`, `config`, `costs`, `csvloader`, `excel`, `logging`, `market`, `marketdata`, `render`, `selectiontracker` | Zero internal imports. Pure types, config, generic stores, rendering primitives. |
| **L1 — stores/impls** | `broker`, `tax`, `yfinance` | Thin layers over leaves. |
| **L2 — domains/data** | `backtest`, `broker/schwab`, `broker/zerodha`, `monitoring`, `optimizer` | Strategy math, broker clients, data providers. |
| **L3 — higher domains** | `attribution`, `datafetcher`, `printer`, `stockpicker` | Compose L0–L2. |
| **L4 — orchestration/IO** | `daemon`, `executor` | Long-running / order placement. |
| **L5 — top composition** | `autopilot` | Wires the pipeline. |
| **L6 — server** | `server` | Embeds autopilot + most domains. |

`cmd/*` and `main.go` sit above all of `pkg/` (the composition root) and are not
layer-checked.

## Designated leaves (never acquire an internal import)

`marketdata`, `broker/types`, `cache`, `config`, `costs`, `render`, `market`,
`logging`, `alert`. `scripts/checkdeps` fails hard if any of these imports another
internal package.

## Adding or moving a package

1. Add it to the `layers` map in `scripts/checkdeps/main.go` at the correct layer
   (the check fails on any unlisted `pkg/` package, forcing a deliberate placement).
2. If it only needs shared *types*, import the leaf (`marketdata` / `broker/types`),
   not the heavy package that re-exports them.
3. Run `make check-deps`. If it reports a layer violation, the dependency direction
   is wrong — rethink placement rather than bumping a layer number to silence it.

## Type re-export aliases

`pkg/broker` and `pkg/yfinance` re-export their leaf-owned DTOs via type aliases
(`broker.Holding = types.Holding`, `yfinance.HistoricalData = marketdata.HistoricalData`)
so existing call sites are unchanged. New code that needs only the types should
import the leaf directly; the aliases exist for backward-compatibility and for
call sites that also use the package's behavior.
