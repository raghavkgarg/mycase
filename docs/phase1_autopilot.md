# Phase 1: Quarterly Autopilot Pipeline — Implementation Plan

**Branch**: `feature/schwab-integration`  
**Estimated effort**: ~1 week  
**Dependencies**: None (all building blocks exist)

---

## Table of Contents

1. [Problem Statement](#1-problem-statement)
2. [Design Decisions](#2-design-decisions)
3. [Component Breakdown](#3-component-breakdown)
4. [Implementation Steps](#4-implementation-steps)
5. [Config Schema](#5-config-schema)
6. [Testing Plan](#6-testing-plan)

---

## 1. Problem Statement

The current `mycase pipeline` command is fully interactive — it prompts the user at every step (edit proposal? update golden copy? enter capital? open report? authenticate? execute?). This makes it impossible to run on a schedule.

The goal is to add a **non-interactive autopilot mode** that:
1. Runs the pick → optimize → report steps without user input
2. Generates a rebalance proposal (orders + cost breakdown)
3. Sends a Telegram/Discord alert summarizing the proposal
4. **Stops and waits** for the investor to confirm via the web dashboard
5. On confirmation, executes the basket orders

The system must never auto-execute without explicit opt-in. The default behavior is: **propose → notify → wait → confirm → execute**.

---

## 2. Design Decisions

### D1 — Separate subcommand, not a flag on `pipeline`

The interactive pipeline has ~15 user prompts, file editing pauses, and report-opening commands. Adding `--non-interactive` to skip all of them is fragile — every new prompt needs a skip condition.

Instead: `mycase autopilot run` is a clean, purpose-built non-interactive pipeline. It shares the internal `runPickWithOpts`, `runReportWithParams`, etc. but never calls `reader.ReadString`.

The existing `mycase pipeline` stays as-is for the investor who wants to run things manually and inspect at each step.

### D2 — Scheduling via launchd/cron, not an in-process loop

The drift daemon uses an in-process loop (sleep until 15:45 IST, check, repeat). This works for daily checks but is wrong for quarterly runs:
- Process must survive months between runs (memory leak risk, OS updates, reboots)
- Quarterly schedule is calendar-based (1st trading day of quarter), not time-interval

Better: `mycase autopilot install` writes a launchd `StartCalendarInterval` plist that fires on specific days. The autopilot process runs once, does its work, exits. No long-lived process needed.

Fallback: `mycase autopilot run` can also be invoked manually or via cron by the user.

### D3 — Proposal state persisted to disk

After the autopilot runs pick → optimize → report, it writes a **proposal file** (`data/autopilot/pending_proposal.json`) containing:
- Proposed orders (ticker, action, qty, price, value)
- Cost breakdown
- Tax warnings
- Golden copy diff (entries, exits)
- Timestamp and expiry (proposal valid for 7 days)

The web dashboard reads this file to show the `/rebalance` confirmation page. The Telegram alert includes a summary and directs the user to the dashboard.

This decouples the pipeline run from the confirmation step — they don't need to happen in the same process.

### D4 — Confirmation via existing web dashboard endpoint

The dashboard already has:
- `GET /api/portfolio/{name}/orders` — shows proposed orders
- `POST /api/portfolio/{name}/execute` — places orders live

For autopilot, we add:
- `GET /api/autopilot/proposal` — returns the pending proposal JSON
- `POST /api/autopilot/confirm` — validates the proposal hasn't expired, then executes
- `POST /api/autopilot/dismiss` — marks the proposal as dismissed (no execution)

The existing `<order-preview>` Web Component is extended to also render from a proposal file (not just live computation).

### D5 — Alert payload includes actionable summary

The Telegram/Discord alert for autopilot is richer than the drift alert:

```
📊 Quarterly Rebalance Ready

Portfolio: microsmall_multibagger
Date: 2026-10-01

Changes:
  + NSE:NEWSTOCK (new entry, rank #3)
  + NSE:ANOTHER (new entry, rank #7)
  - NSE:OLDSTOCK (exit, fell to rank #28)

Orders: 3 buys, 1 sell
Estimated cost: ₹847 (STT + DP)
Tax impact: 1 STCG sell (₹2,400 gain → ₹480 tax)

👉 Review & confirm: http://localhost:8080/#/rebalance
⏳ Proposal expires: 2026-10-08
```

### D6 — Trading day calendar (simple approach)

"First trading day of the quarter" requires knowing NSE holidays. For v1:
- Schedule launchd to fire on the 2nd of Jan/Apr/Jul/Oct (covers most cases since 1st is often a holiday)
- The autopilot run checks if today is a trading day by attempting to fetch a quote. If the market is closed (no data), it reschedules to tomorrow (re-fires next day via a simple retry mechanism)

Future: maintain a holiday calendar from NSE's published schedule.

---

## 3. Component Breakdown

### New files

| File | Purpose |
|------|---------|
| `cmd/autopilot.go` | CLI subcommand: `mycase autopilot {run, install, uninstall, status, dismiss}` |
| `pkg/autopilot/autopilot.go` | Core logic: non-interactive pipeline execution |
| `pkg/autopilot/proposal.go` | Proposal generation, persistence, loading, expiry validation |
| `pkg/autopilot/schedule.go` | Trading day detection, next-run calculation |
| `pkg/autopilot/alert.go` | Rich alert formatting for Telegram/Discord |

### Modified files

| File | Change |
|------|--------|
| `pkg/config/pipeline.go` | Add `ScheduleConfig` struct + YAML parsing |
| `config/pipeline.yaml` | Add `schedule:` section |
| `pkg/server/routes.go` | Add `/api/autopilot/*` routes |
| `pkg/server/handlers.go` | Add `handleAutopilotProposal`, `handleAutopilotConfirm`, `handleAutopilotDismiss` |
| `pkg/server/static/js/components/order-preview.js` | Extend to render from proposal file |
| `main.go` | Register `AutopilotCommand` |

### Reused (no changes)

| Component | How it's reused |
|-----------|----------------|
| `runPickWithOpts()` | Called by autopilot for stock selection |
| `runReportWithParams()` | Called by autopilot for report generation |
| `csvloader.MergeGoldenCopy()` | Called by autopilot for golden copy update |
| `csvloader.PrintComparisonReport()` | Called by autopilot for diff |
| `pkg/alert/` | Telegram/Discord sending |
| `pkg/broker/` | Order execution on confirm |
| `pkg/optimizer.FilterMicroTransactions()` | Order filtering |
| `pipelineCopyFile()` | Golden copy backup |

---

## 4. Implementation Steps

### Step 1: `pkg/config/pipeline.go` — Add ScheduleConfig

```go
type ScheduleConfig struct {
    Frequency       string   `yaml:"frequency"`         // "quarterly", "monthly", "drift-triggered"
    Day             string   `yaml:"day"`               // "first_trading_day", "last_trading_day", "2", "15"
    Notify          []string `yaml:"notify"`            // ["telegram", "discord"]
    AutoExecute     bool     `yaml:"auto_execute"`      // false = require confirmation (default)
    DriftTriggerPct float64  `yaml:"drift_trigger_pct"` // mid-cycle drift trigger (0 = disabled)
    ProposalTTLDays int      `yaml:"proposal_ttl_days"` // days before proposal expires (default 7)
}
```

Add `Schedule ScheduleConfig` to `PipelineConfig`. Parse from the `schedule:` YAML key.

### Step 2: `pkg/autopilot/proposal.go` — Proposal data model

```go
type Proposal struct {
    ID            string          `json:"id"`             // UUID
    CreatedAt     time.Time       `json:"created_at"`
    ExpiresAt     time.Time       `json:"expires_at"`
    Status        string          `json:"status"`         // "pending", "confirmed", "dismissed", "expired"
    Portfolio     string          `json:"portfolio"`      // golden copy path
    Strategy      string          `json:"strategy"`
    
    // Changes
    Entries       []StockChange   `json:"entries"`        // new additions
    Exits         []StockChange   `json:"exits"`          // removals
    WeightChanges []WeightDelta   `json:"weight_changes"` // reweighted stocks
    
    // Orders
    Orders        []ProposedOrder `json:"orders"`
    FilteredOut   []ProposedOrder `json:"filtered_out"`   // micro-transaction filtered
    
    // Summary
    TotalBuyValue  float64        `json:"total_buy_value"`
    TotalSellValue float64        `json:"total_sell_value"`
    EstimatedCost  float64        `json:"estimated_cost"`
    TaxWarnings    []string       `json:"tax_warnings"`
    
    // Reporting
    ReportPath     string         `json:"report_path"`
    SelectionPath  string         `json:"selection_path"`
}

type ProposedOrder struct {
    Ticker    string  `json:"ticker"`
    Action    string  `json:"action"`    // "BUY" or "SELL"
    Quantity  int     `json:"quantity"`
    LimitPrice float64 `json:"limit_price"`
    Value     float64 `json:"value"`
}

type StockChange struct {
    Ticker string  `json:"ticker"`
    Rank   int     `json:"rank"`
    Score  float64 `json:"score"`
    Reason string  `json:"reason"`
}

type WeightDelta struct {
    Ticker    string  `json:"ticker"`
    OldWeight float64 `json:"old_weight"`
    NewWeight float64 `json:"new_weight"`
}
```

Functions:
- `SaveProposal(p *Proposal) error` — writes to `data/autopilot/pending_proposal.json`
- `LoadProposal() (*Proposal, error)` — reads + validates expiry
- `DismissProposal() error` — sets status to "dismissed"
- `ConfirmProposal() error` — sets status to "confirmed"

### Step 3: `pkg/autopilot/autopilot.go` — Non-interactive pipeline

The core function:

```go
func Run(ctx context.Context, cfg config.PipelineConfig) (*Proposal, error)
```

Steps (mirrors `cmd/pipeline.go` but non-interactive):
1. Clean stale cache files
2. For each source (index/file): `runPickWithOpts(ctx, opts)` — no prompts
3. If multiple sources: combine → pick top N+5 → prune to top N (no manual edit pause)
4. Backup golden copy → `csvloader.MergeGoldenCopy()` — no confirmation prompt
5. Generate report via `runReportWithParams()`
6. Compute orders: load golden copy, get quotes, calculate target quantities, diff against holdings
7. Filter micro-transactions
8. Build `Proposal` struct with all data
9. Save proposal to disk
10. Return proposal (caller sends alert)

Key difference from interactive pipeline: **no performance simulation step** (it's backward-looking and not needed for the rebalance decision) and **no monitoring step** (the drift daemon handles that continuously).

### Step 4: `pkg/autopilot/alert.go` — Rich alert formatting

```go
func FormatProposalAlert(p *Proposal) alert.Alert
```

Builds the rich Telegram message (Markdown format) with:
- Portfolio name and date
- Entries/exits list
- Order count and cost summary
- Dashboard link
- Expiry date

### Step 5: `pkg/autopilot/schedule.go` — Trading day logic

```go
func NextRunDate(cfg config.ScheduleConfig) time.Time
func IsTradingDay(ctx context.Context) bool
```

v1 trading day check: attempt `yfinance.FetchHistoricalData(ctx, "NSE:NIFTY50", "1d")`. If the latest candle is today, market is open. If not, it's a holiday.

Quarter starts: Jan 1, Apr 1, Jul 1, Oct 1. The schedule fires on the 2nd; if not a trading day, try 3rd, 4th, etc. (max 5 attempts before giving up and alerting).

### Step 6: `cmd/autopilot.go` — CLI subcommand

```
mycase autopilot run         Run the non-interactive pipeline now, generate proposal, send alert
mycase autopilot install     Install launchd plist for scheduled quarterly runs
mycase autopilot uninstall   Remove the scheduled service
mycase autopilot status      Show last run info + pending proposal status
mycase autopilot dismiss     Dismiss the pending proposal without executing
```

The `install` subcommand generates a launchd plist with `StartCalendarInterval` entries for quarterly execution:

```xml
<key>StartCalendarInterval</key>
<array>
    <dict><key>Month</key><integer>1</integer><key>Day</key><integer>2</integer><key>Hour</key><integer>10</integer></dict>
    <dict><key>Month</key><integer>4</integer><key>Day</key><integer>2</integer><key>Hour</key><integer>10</integer></dict>
    <dict><key>Month</key><integer>7</integer><key>Day</key><integer>2</integer><key>Hour</key><integer>10</integer></dict>
    <dict><key>Month</key><integer>10</integer><key>Day</key><integer>2</integer><key>Hour</key><integer>10</integer></dict>
</array>
```

Fires at 10:00 IST on the 2nd of each quarter month. If it's not a trading day, the autopilot detects this and writes a retry marker; the drift daemon (which runs daily at 15:45) picks up the retry and re-invokes the autopilot next trading day.

### Step 7: Server endpoints — Proposal API

Three new handlers in `pkg/server/handlers.go`:

```go
// GET /api/autopilot/proposal
func (s *Server) handleAutopilotProposal(w http.ResponseWriter, r *http.Request)

// POST /api/autopilot/confirm  
func (s *Server) handleAutopilotConfirm(w http.ResponseWriter, r *http.Request)

// POST /api/autopilot/dismiss
func (s *Server) handleAutopilotDismiss(w http.ResponseWriter, r *http.Request)
```

`handleAutopilotConfirm`:
1. Load proposal, validate status == "pending" and not expired
2. For each order in proposal: `s.broker.PlaceOrder("regular", order)` with 200ms throttle
3. Record successes/failures
4. Update proposal status to "confirmed"
5. Send confirmation alert: "✅ Rebalance executed. 4/4 orders placed successfully."

### Step 8: Frontend — Proposal-aware rebalance view

Extend `<order-preview>` component:
1. On mount, check `GET /api/autopilot/proposal`
2. If a pending proposal exists: render its orders (not live-computed orders)
3. Show "Confirm Rebalance" button (calls `POST /api/autopilot/confirm`)
4. Show "Dismiss" button (calls `POST /api/autopilot/dismiss`)
5. Show expiry countdown
6. If no proposal: fall back to current behavior (live order computation)

---

## 5. Config Schema

Full `schedule:` section added to `config/pipeline.yaml`:

```yaml
schedule:
  frequency: quarterly            # quarterly | monthly | drift-triggered
  day: first_trading_day          # first_trading_day | last_trading_day | 2 | 15 (day of month)
  notify: [telegram]              # channels to alert on proposal ready
  auto_execute: false             # DANGEROUS: skip confirmation and execute immediately
  drift_trigger_pct: 15           # mid-quarter drift % that triggers early rebalance (0 = disabled)
  proposal_ttl_days: 7            # days before unconfirmed proposal expires
```

Defaults (if `schedule:` section is absent):
- frequency: quarterly
- day: first_trading_day
- notify: [] (no alerts, proposal only saved to disk)
- auto_execute: false
- drift_trigger_pct: 0 (disabled)
- proposal_ttl_days: 7

---

## 6. Testing Plan

### Unit tests

| Package | Tests |
|---------|-------|
| `pkg/autopilot/proposal.go` | Save/load round-trip, expiry validation, status transitions |
| `pkg/autopilot/schedule.go` | Next quarter date calculation, trading day detection with mock |
| `pkg/autopilot/alert.go` | Alert formatting with various proposal shapes (0 exits, many entries, etc.) |
| `pkg/config/pipeline.go` | ScheduleConfig parsing from YAML (defaults, all fields set, missing section) |

### Integration tests

| Test | What it validates |
|------|-------------------|
| `autopilot run` with mock broker | Full pipeline runs without prompts, proposal file written, alert formatted |
| `autopilot confirm` via API | Proposal loaded, orders placed via mock broker, status updated |
| `autopilot dismiss` via API | Proposal status set to dismissed, no orders placed |
| Expired proposal rejection | Confirm on expired proposal returns 410 Gone |

### Manual verification

```bash
# 1. Run autopilot (mock mode — no live broker needed)
mycase autopilot run

# 2. Check proposal was generated
cat data/autopilot/pending_proposal.json | jless

# 3. Check status
mycase autopilot status

# 4. Start dashboard and confirm via UI
mycase serve &
open http://localhost:8080/#/rebalance

# 5. Or dismiss via CLI
mycase autopilot dismiss
```

---

## Implementation Order

| Step | File(s) | Effort |
|------|---------|--------|
| 1 | `pkg/config/pipeline.go` | 0.5 day |
| 2 | `pkg/autopilot/proposal.go` | 0.5 day |
| 3 | `pkg/autopilot/autopilot.go` | 1.5 days (core logic, extracting from pipeline.go) |
| 4 | `pkg/autopilot/alert.go` | 0.5 day |
| 5 | `pkg/autopilot/schedule.go` | 0.5 day |
| 6 | `cmd/autopilot.go` | 1 day (CLI + launchd plist generation) |
| 7 | `pkg/server/` (handlers + routes) | 0.5 day |
| 8 | Frontend (order-preview.js extension) | 0.5 day |
| 9 | Tests | 1 day |

**Total**: ~7 days

---

## Open Questions (to resolve during implementation)

1. **Should autopilot also run the performance simulation?** Current plan says no (it's backward-looking). But it might be useful in the alert as context ("portfolio is up 12% since last rebalance").

2. **Drift-triggered mid-quarter rebalance**: Should this produce a full proposal (same as quarterly) or a lighter "drift correction" (only reweight, no re-pick)? Recommendation: lighter — only rebalance weights to target, don't re-run stock selection.

3. **Multiple portfolios**: The current pipeline handles one golden copy at a time. If the investor has both `microsmall.csv` and `nifty50_value.csv`, should autopilot run both? Recommendation: yes — iterate over all entries in `golden_copy_path` array, generate one proposal per portfolio, one combined alert.

4. **Auth token freshness**: Zerodha tokens expire daily. If the investor confirms 3 days after the proposal, the token is stale. Solution: the confirm endpoint must prompt for re-auth if the token is expired (or the alert should say "authenticate before confirming").
