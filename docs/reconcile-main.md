# Reconcile origin/main → integration/ebm-upstream

**Status**: ✅ DONE — merged on `integration/reconcile-main`, all gates green
**Created**: September 8, 2026
**Branch strategy**: work on disposable `integration/reconcile-main` cut from `integration/ebm-upstream`

## Outcome (what actually happened)

Merged `origin/main`'s 3 post-divergence commits under the dormant-code doctrine.
mergiraf (Go-aware merge driver) auto-resolved the hardest Go collisions
(`scoring.go`, `types.go`, `selectiontracker/tracker.go`) as correct **union**
merges — main's anti-churn cooldown API and the EBM Phase-8 driver additions
coexist. 7 live cmd/config conflicts hand-resolved. Resurrected `portfolio`
(L1), `themereturn` (L3), `kiteauth` (L0), `stockpicker/cooldown.go` as dormant,
layer-legal packages.

Two live-path decisions (confirmed with the user):
- **1(a) — cooldown wired LIVE** into `stockpicker.RunWithResult` for all 5
  methods (value/multibagger/earlymb/USQM/standard). Added
  `SelectTopNUSQMWithCooldown`, `--cooldown-days`/`--cooldown-bypass-rank`
  flags, `LoadRecentExits`. `ApplySafetyFilters` now leniences existing holdings.
- **2(b) — theme-return kept DORMANT**: reachable only via `cmd/returns.go`, not
  the live `holdings` view (banner wiring removed from `cmd/holdings.go`).

One layering fix required: `portfolio` first landed at L2 but is consumed by
L2 packages (`optimizer`, `broker/zerodha`); root cause was its `Holding` alias
importing `pkg/broker` (L1). Repointed the alias to the `broker/types` (L0)
leaf → `portfolio` imports only L0 → correctly L1.

Verification: `make check-deps` (layering intact), `make cleanup` (exit 0,
idempotent), `make test` (all packages green, incl. the funnel-conservation
invariant that now sums `CooldownBlocked`). Deferred debt recorded in
`docs/refactor.md`.

---


## 1. Why this exists

The two branches diverged after the old `origin/main` (`e2cfbf1`):

- `integration/ebm-upstream` (HEAD `5f65f85`) carries ~60 commits — the entire
  feature history: R16 layering, Phase 4 tax, Phase 5 attribution, Phase 7
  DuckDB, Phase 8/9 lifecycle, R14/R18 slog, Phase 10a/10b, EBM integration,
  depsgraph tooling.
- `origin/main` moved ahead by **3 new commits** the integration branch does
  not have.

Nothing of the integration branch is upstream yet. The goal is to bring main's
3 new commits in **without discarding features and without breaking the R16
layering**.

### The 3 new commits on origin/main

| Commit | Title | Size | Nature |
|--------|-------|------|--------|
| `327b39d` | New Theme Return and Softer Criterion for Existing Holding | 2,672 lines | Rebuilds `pkg/themereturn` (~1,900 LOC) + a **softer-criterion scoring change** (~750 LOC) in live files |
| `02c5e49` | Few Bugs | 1,618 lines | themereturn tweaks + re-adds kiteauth/portfolio/auto_login.py/India docs + real bug fixes (auth, basket, config, datafetcher, optimizer/rebalance) |
| `5606487` | PIT Display Bugs | 166 lines | Focused clean fixes: auth, config, yfinance, cache/fundamentals |

---

## 2. The doctrine: dormant code, not dead code

**Principle** (user's framing): keep architectural clarity, but bring *all*
features in — even ones the US path never calls — refactored to obey the L0–L6
layering. They become **dormant** (well-formed, self-contained, layer-legal
packages that simply aren't wired into the active US pipeline), never **dead**
(cycle-magnet cruft working against the architecture). As a product these flows
may be reused elsewhere later.

**Rules the dormant code must obey:**
- Passes `make check-deps` — correct layer in `layers.go`, imports only
  downward, leaves stay import-free.
- Imported only by `cmd/*` (composition root, not layer-checked) or a legacy
  entry point — **never** by active US-path packages.
- Shared DTOs still flow through leaves (`marketdata`, `broker/types`) — do not
  reintroduce define-and-import cycle-magnets.
- `slog` two-channel rule applies eventually (main's themereturn still uses
  `fmt.Print*` — acceptable as *recorded* deferred debt, not a blocker).

---

## 3. Conflict surface (from `git merge-tree`)

A blind `git merge main` yields **~10 real content conflicts**; much else
auto-merges clean (main.go, config.go, cache/fundamentals.go, daemon/drift.go,
datafetcher.go, yfinance/* — so the PIT/misc bug fixes largely come free).

**True content conflicts (hand-resolve):**
- `cmd/auth.go`, `cmd/basket.go`, `cmd/holdings.go`, `cmd/optimize.go`, `cmd/pick.go` — wiring
- `config/pipeline.yaml`, `pkg/config/pipeline.go` — config schema drift
- `pkg/printer/printer.go` — heavily rewritten on ebm (612-line diff) vs main touch
- `pkg/selectiontracker/tracker.go` — softer-criterion lands here vs rewrite
- `pkg/stockpicker/scoring.go` + `types.go` — **core collision**: softer-criterion vs EBM/slog rewrite

**Deleted-on-ebm / modified-on-main (git asks delete-vs-keep → answer: KEEP = resurrect):**
- `pkg/portfolio/portfolio.go` (+ test)
- `pkg/themereturn/*` (db, engine, matcher, models, printer, xirr + test)
- `pkg/kiteauth/*` (autologin, totp + test)
- `pkg/stockpicker/cooldown.go` (+ test)
- `pkg/alert/email.go`
- `cmd/returns.go`
- `docs/multibagger.md`, `docs/value.md`, `docs/ThemeBasedReturn.md`
- `scripts/auto_login.py`

---

## 4. Layer placement for resurrected packages

Confirmed by inspecting main's import blocks. All fit cleanly:

| Package | Internal imports (on main) | Target layer | layers.go key |
|---------|---------------------------|--------------|---------------|
| `kiteauth` | *none* | **L0 (leaf)** | `"kiteauth": 0` |
| `portfolio` | `broker` (L1) | **L2** | `"portfolio": 2` |
| `themereturn` | `config` (L0), `csvloader` (L0), `portfolio` (L2) | **L3** | `"themereturn": 3` |
| `cooldown` | (folds back into `stockpicker` L3) | — | no new key |
| `cmd/returns.go` | zerodha, config, themereturn | n/a | composition root, not layer-checked |

Chain: `kiteauth`(L0) → `portfolio`(L2) → `themereturn`(L3), wired only via
`cmd/returns.go`. No back-edges, no leaf pollution.

> NOTE: verify `kiteauth` truly has zero internal imports after resurrection
> (main showed none). If it imports `kiteclient`, it becomes L2, not L0.

---

## 5. Execution order

1. **Write this doc** ✅ (task #1)
2. **Tooling**: enable `git rerere`; use `difftastic`/`mergiraf` if available,
   else install; confirm `ast-grep` for structural porting. (task #2)
3. **Branch**: `git switch -c integration/reconcile-main`. (task #3)
4. **Merge**: `git merge main`; triage each conflict by doctrine:
   - live US path = ebm version + port main's genuine fixes
   - dormant India/theme = take main's version wholesale
   - deleted-vs-modified = keep (resurrect). (task #4)
5. **Resurrect** deleted feature files from main. (task #5)
6. **Register** in `devtools/internal/layers/layers.go`; run `make check-deps`. (task #6)
7. **Hand-port** the softer-criterion scoring into EBM-rewritten
   `scoring.go`/`selectiontracker.go` — the one non-mechanical piece. (task #7)
8. **Reconcile** remaining live cmd/config collisions. (task #8)
9. **Verify**: `make check-deps` → `make cleanup` → `make test` all green. (task #9)
10. **Record debt** (themereturn on fmt.Print* not slog) in `docs/refactor.md`;
    commit. (task #10)

---

## 6. The one real judgment call

The **softer-criterion for existing holdings** (`327b39d`) is the only piece
that changes *live* behavior and cannot be made dormant. It relaxes
scoring/filter thresholds for stocks already held (reduces churn — aligns with
the hysteresis philosophy). It touches `scoring.go`, `filters.go`,
`selectiontracker/tracker.go`, `types.go`, and re-adds `cooldown.go` — all files
the EBM/slog work rewrote. Must be **hand-ported**, understanding both sides,
not clobbered either direction.

---

## 7. Effort estimate

~1–1.5 days total (NOT another multi-day slog):
- ~½ day: dormant-package resurrection + layer registration (mechanical)
- ~½–1 day: hand-reconcile the ~5 live-collision files (softer-criterion is the
  only one needing real thought)
- verification with `make cleanup` + `make test`

The dormant-code strategy converts themereturn (~2,000 LOC) from a "redesign"
problem into a "file copy + one layers.go edit" problem.
