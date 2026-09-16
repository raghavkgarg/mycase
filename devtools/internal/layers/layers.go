// Package layers is the single source of truth for the R16 package layering
// used by both devtools/checkdeps (enforcement) and devtools/depsgraph
// (visualization).
//
// The layering rule: a pkg/ package may import only packages at a STRICTLY
// LOWER layer. Layer 0 packages are leaves; a subset (MustBeLeaf) must have
// zero internal imports. See docs/refactor.md R16 and
// .kiro/steering/architecture.md.
//
// When adding or re-tiering a package, edit Layers here — both the checker and
// the graph pick it up automatically.
package layers

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ModulePrefix is the module import path prefix, trimmed to produce short
// package names (e.g. "broker/types").
const ModulePrefix = "github.com/raghavkgarg/mycase/"

// Layers maps each pkg/ package (short path, e.g. "broker/types") to its layer.
// A package may only import packages at a strictly lower layer. Layer 0
// packages are leaves and must have zero internal imports.
//
// The checker fails on any pkg/ package missing from this map, so new packages
// must be placed deliberately.
var Layers = map[string]int{
	// L-1 — the absolute floor: a pure, zero-import algorithmic leaf that even the
	// L0 leaves may import downward. marketcal holds market-calendar / EOD-settlement
	// time math (stdlib-only) so marketdata, cache, and selectiontracker consume one
	// implementation instead of each duplicating it. It is the sole MustBeLeaf member
	// among this group; its consumers are permitted exactly this one downward import.
	"marketcal": -1,

	// L0 — leaves: zero internal imports (except the permitted downward marketcal import).
	"alert":            0,
	"broker/types":     0,
	"cache":            0,
	"config":           0,
	"costs":            0,
	"csvloader":        0,
	"excel":            0,
	"kiteauth":         0, // (India legacy) Kite auto-login/TOTP — zero internal imports; dormant, wired only via cmd
	"logging":          0,
	"market":           0,
	"marketdata":       0,
	"marketfmt":        0, // market-aware currency/magnitude formatting (₹ Cr/L, $ K/M/B/T) — pure, zero-import
	"rawcapture":       0, // raw API response archive for offline replay/triage — pure, zero-import (env-gated)
	"render":           0,
	"selectiontracker": 0,
	"universe":         0,

	// L1 — stores / low-level impls over leaves.
	"broker":     1, // broker/types, config, costs
	"edgar":      1, // cache, marketdata — SEC EDGAR fundamentals client; owns its CIK-map + facts tables via cache.Conn()
	"kiteclient": 1, // config (Zerodha/Kite low-level client, India legacy)
	"portfolio":  1, // broker/types (India legacy) — dormant; string utils + Holding alias, consumed by optimizer/zerodha/themereturn
	"tax":        1, // broker/types
	"themedb":    1, // cache — theme rebalance/history store; owns its DuckDB tables via cache.Conn()
	"yfinance":   1, // cache, marketdata

	// L2 — domains + data routing.
	"backtest":       2, // yfinance
	"broker/schwab":  2, // broker, marketdata, tax
	"broker/zerodha": 2, // broker, config
	"monitoring":     2, // yfinance
	"optimizer":      2, // broker/types, costs, market, marketdata

	// L3 — higher-level domains.
	"attribution": 3, // backtest, cache, marketdata
	"datafetcher": 3, // broker, broker/schwab, edgar, yfinance
	"printer":     3, // broker/types, market, optimizer, portfolio, render
	"stockpicker": 3, // config, csvloader, excel, optimizer, selectiontracker, yfinance
	"themereturn": 3, // config, csvloader, portfolio, themedb (India legacy) — dormant; wired only via cmd/returns

	// L4 — orchestration / IO.
	"daemon":     4, // alert, broker, config, csvloader
	"executor":   4, // broker, config, market, printer, render, yfinance
	"pithistory": 4, // stockpicker
	"rawstore":   4, // config, rawcapture — filesystem impl of rawcapture.Sink; owns data dir + archive filename convention

	// L5 — top composition (below cmd/main, which live outside pkg/).
	"autopilot": 5,

	// L6 — server embeds autopilot + most domains.
	"server": 6,
}

// MustBeLeaf lists packages that must never acquire ANY internal import — the
// true zero-import floor. marketcal is the pure algorithmic leaf that the former
// leaves (marketdata, cache, market) now import downward for EOD settlement math;
// those three are therefore no longer zero-import, but remain L0 and may import
// ONLY marketcal (enforced by the strictly-downward layer check). broker/types,
// config, costs, render, logging, alert remain pure DTO/config/primitive leaves.
var MustBeLeaf = map[string]bool{
	"marketcal":    true,
	"broker/types": true,
	"config":       true,
	"costs":        true,
	"render":       true,
	"logging":      true,
	"alert":        true,
	"rawcapture":   true,
}

// Pkg is the subset of `go list -json` output both commands consume.
type Pkg struct {
	ImportPath string
	Imports    []string
}

// Short strips the module prefix and a leading "pkg/" to yield the short
// package name used as the map key (e.g. "broker/types").
func Short(importPath string) string {
	s := strings.TrimPrefix(importPath, ModulePrefix)
	return strings.TrimPrefix(s, "pkg/")
}

// IsInternal reports whether an import path is one of this module's packages.
func IsInternal(importPath string) bool {
	return strings.HasPrefix(importPath, ModulePrefix)
}

// List returns the module's pkg/ packages with their direct imports, via
// `go list -json ./pkg/...`.
func List() ([]Pkg, error) {
	cmd := exec.Command("go", "list", "-json", "./pkg/...")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}
	var pkgs []Pkg
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p Pkg
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("decode go list output: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}
