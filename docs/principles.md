# Mycase — Architecture Principles

**Status**: SKELETON — bullet points only, to be fleshed out next session.
**Purpose**: Codify the durable architectural principles the system is built on, so future changes (and merges from divergent branches, like the EBM integration) can be evaluated against a stable rubric rather than ad-hoc judgment.
**Related**: `docs/architecture.md` (current design + D-decisions), `.kiro/steering/architecture.md` (enforced layering), `docs/datasources.md` (data-source design), `docs/refactor.md` (refactor history).

---

## 1. Layering & Dependencies

- Imports go strictly downward; a package imports only lower layers, never same/higher.
- Shared DTOs live in zero-import leaf packages (`marketdata`, `broker/types`) — define OR consume, not both low in the stack.
- Interfaces are defined by their consumer; the low-level implementer satisfies structurally, never imports the consumer.
- Domains own their persistence (take a `*sql.DB` via `cache.Conn()`, define their own tables); `cache` never imports a domain.
- New package → must be placed in `scripts/checkdeps` layer map deliberately; guard fails on unlisted packages.
- Designated leaves never acquire an internal import (`marketdata`, `broker/types`, `cache`, `config`, `costs`, `render`, `market`, `logging`, `alert`).
- (TODO) when to introduce a new package vs. extend an existing one.
- (TODO) how to break a would-be cycle (the pithistory→stockpicker case: persistence at composition root).

## 2. IO & Side Effects

- Two output channels, never conflated: user output (stdout via `pkg/render`/`fmt`) vs. logs (slog → stderr + JSON file).
- Command *results* are the product → stdout; everything diagnostic → slog.
- No side effects hidden in low-level packages; IO/orchestration lives at L4+ and the composition root (`cmd/`, `main.go`).
- No hardcoded machine/user-specific paths in committed code (cf. rejected `SyncAccessTokenToAllConfigs`).
- Golden-copy CSVs never overwritten programmatically except via explicit merge command.
- Secrets referenced by key name, never logged by value; credential files gitignored.
- (TODO) filesystem layout conventions (data/, report/, config/ read-only at runtime).
- (TODO) network IO: rate limits as a budget, cache-first, save-raw-for-debug.

## 3. Configuration Management

- Precedence is explicit and one-directional: flag > env > config file > built-in default (cf. logging config wiring).
- User preferences (`config/defaults.json`) provide convenience defaults; explicit flags always override — config never forces behavior.
- Config files are read-only at runtime; the system never writes back to its own config as a side effect.
- Config structs are additive/backward-compatible: new fields default to zero-value and don't break existing files (cf. EBM `HardFilters` fields, `AllowCashOnSectorCapExhaustion`).
- Strategy/behavior config (`mfs.json`, `pipeline.yaml`) is version-controlled; secrets/tokens are separate, gitignored files referenced by key name.
- Method/name aliasing handled at the config-load boundary, not scattered through call sites (cf. `earlymb`→`early_multibagger`).
- Config loading is a leaf concern (`pkg/config`, zero internal imports); it parses, it doesn't orchestrate.
- Absent/malformed optional config degrades to zero-value defaults, never panics (cf. `LoadUserDefaults`).
- (TODO) config schema documentation + validation (where do we validate ranges/required fields?).
- (TODO) per-market vs. per-strategy vs. per-user config boundaries — which file owns what.
- (TODO) migration/versioning story for config schema changes over time.
- (TODO) relationship between config files and the DuckDB-stored state (what belongs in each).

## 4. Data Sources & Integration

- Source each data type from the most authoritative provider that can supply it (prices→broker/exchange, fundamentals→SEC EDGAR, sector→classification standard).
- Aggregators (Yahoo) are fallback, not source-of-record; demote, don't eliminate.
- Routing selects provider; fundamentals may be *composed/merged* across sources rather than single-sourced.
- Fallback chains are explicit and logged — a degraded run (Schwab→Yahoo) must be observable, never silent.
- Record provenance (which source produced a value) so numbers are auditable and one source can be invalidated selectively.
- Ticker-prefix convention (`US:`/`NSE:`/`BSE:`) drives routing; DTOs are market-agnostic.
- New source = new package satisfying a consumer-defined `*Source` interface (`PriceSource`, `FundamentalsSource`, `SectorSource`).
- (TODO) how to add a provider without touching consumers.
- (TODO) cache freshness policy per data type (intraday prices vs. quarterly filings).

## 5. Algorithms & Strategies

- Strategy = scoring + hard filters + selection; each pluggable by `--method` dispatch in one place (`stockpicker.RunWithResult`).
- Hard filters exclude entirely (not low-score); scoring normalizes within the candidate set.
- Algorithms are pure/deterministic over their inputs; data-fetching is injected (`DataFetcher`), not called directly.
- Transparency / no black boxes: every score, filter, weight explainable from output (design constraint from vision).
- New strategy = new Score/SelectTopN/Normalize trio + a dispatch branch + config block; no changes to IO or data layers.
- Legacy strategies (India multibagger/value) coexist with active (US quality-momentum, EBM) — kept, not deleted, but clearly marked.
- (TODO) factor-weight config conventions (`config/mfs.json` per strategy).
- (TODO) where derived metrics live (structured `DriverMetrics` vs. formatted strings).

## 6. Pipelines & Orchestration

- Pipeline stages share one process (one DB conn, one market-data session, one broker client); no subprocess chaining.
- Stages communicate via DuckDB tables + structured results (`PickResult`), not by re-parsing text output.
- Intermediate state is atomic, queryable, run-tracked (`pipeline_runs`, `proposals` stages: draft/optimized/final).
- Investor-in-the-loop for execution: system proposes, investor confirms, orders fire; never auto-execute without opt-in.
- Non-interactive path (autopilot) shares inner functions with interactive path; no duplicated logic.
- Point-in-time snapshots capture *why* a decision was made, for run-to-run diffing and audit.
- (TODO) pipeline pause/resume + proposal lifecycle (proposed → executed reconcile).
- (TODO) scheduling boundary (launchd/OS owns lifecycle, not in-process loops for long intervals).

## 7. Testing & Verification

- Build + layering guard + tests must stay green on every change (`make build`, `make check-deps`, `make test`).
- Table-driven, stdlib `testing`, hand-written mocks; interfaces are the seams. No mocking framework.
- Pure-logic packages: high unit coverage; IO glue: covered by E2E, not chased for %.
- Integration tests (network/creds/external scripts) skip gracefully when the environment is absent — never hard-fail the suite.
- Verify against the actual success criteria, not just "exited 0".
- (TODO) coverage targets per layer.
- (TODO) E2E scenario list + tags.

## 8. Change & Merge Discipline

- Distinguish algorithmic changes (bring) from architectural changes (evaluate against principles; our architecture wins on conflict).
- Union where both sides added disjoint things; take-theirs where we never touched a file; hand-merge only true conflicts.
- Relocate incoming changes to the architecturally-correct home (e.g. logic our refactor moved out of `cmd/` into `pkg/`).
- Keep unused-but-wanted code (kiteclient) rather than delete, when explicitly requested.
- Commit in safe checkpoints; keep a pushed restore point before risky integration.
- (TODO) when to cherry-pick vs. merge vs. rebase given branch divergence.

---

## 9. Self-Evaluation: Current Solution vs. Principles

Rough scoring after the EBM integration (`integration/main-ebm`). To be detailed next session.

| Area | Status | Notes |
|------|--------|-------|
| Layering | 🟢 Strong | R16 guard enforced; new pkgs placed deliberately; pithistory cycle avoided |
| IO / side effects | 🟢 Good | slog/render split; rejected machine-specific token-sync. TODO: audit remaining `fmt` diagnostics |
| Config management | 🟢 Good | flag>env>file>default precedence; additive structs; read-only at runtime. TODO: no schema validation |
| Data sources | 🟡 Partial | Principles documented (datasources.md) but Phase 10 unbuilt; 7 router-bypass paths remain; Yahoo still primary for many US paths |
| Algorithms | 🟢 Good | Clean `--method` dispatch; injected DataFetcher; EBM slotted in without touching IO. TODO: rsi/momentum still persist zero |
| Pipelines | 🟢 Good | DuckDB-backed, run-tracked, proposal lifecycle closed; PIT snapshots added |
| Testing | 🟢 Good | build+check-deps+test green; integration tests now skip gracefully. TODO: coverage gaps (stockpicker historically low) |
| Merge discipline | 🟢 Applied | This session: algorithm-in / architecture-preserved worked cleanly |

## 10. Improvements Identified (this branch)

- (TODO) Phase 10 data-source resilience is the biggest gap vs. §3 principles — the 7 router-bypass paths + Yahoo-primary + no provenance column.
- (TODO) `rsi`/`momentum_1y` persist zero in selections (Phase 8 follow-up) — violates "explainable from output" partially.
- (TODO) evaluate whether EBM's PIT snapshot + our `selections` table overlap/duplicate (two audit trails).
- (TODO) confirm no remaining direct-yfinance calls introduced by the EBM merge that bypass the router principle.
- (TODO) decide kiteclient's fate (kept for now; is it dead code or a real fallback?).
- (TODO) `printer.go` — main's delivery-column display was dropped (option b); revisit if the EBM UI wants it on the render layer.

---

## Next Session

- Flesh out each `(TODO)` bullet into prose with concrete file references.
- Turn §8 into a proper scored assessment with evidence.
- Promote the durable principles into `.kiro/steering/` if they should be enforced/auto-included, and cross-link from `architecture.md`.
- Decide which §9 improvements are worth doing on this branch before merge vs. deferring to roadmap phases.
