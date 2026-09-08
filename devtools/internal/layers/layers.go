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
	// L0 — leaves: zero internal imports.
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
	"render":           0,
	"selectiontracker": 0,
	"universe":         0,

	// L1 — stores / low-level impls over leaves.
	"broker":     1, // broker/types, config, costs
	"kiteclient": 1, // config (Zerodha/Kite low-level client, India legacy)
	"portfolio":  1, // broker/types (India legacy) — dormant; string utils + Holding alias, consumed by optimizer/zerodha/themereturn
	"tax":        1, // broker/types
	"yfinance":   1, // cache, marketdata

	// L2 — domains + data routing.
	"backtest":       2, // yfinance
	"broker/schwab":  2, // broker, marketdata, tax
	"broker/zerodha": 2, // broker, config
	"monitoring":     2, // yfinance
	"optimizer":      2, // broker/types, costs, market, marketdata

	// L3 — higher-level domains.
	"attribution": 3, // backtest, cache, marketdata
	"datafetcher": 3, // broker, broker/schwab, yfinance
	"printer":     3, // broker/types, market, optimizer, render
	"stockpicker": 3, // config, csvloader, excel, optimizer, selectiontracker, yfinance
	"themereturn": 3, // config, csvloader, portfolio (India legacy) — dormant; wired only via cmd/returns

	// L4 — orchestration / IO.
	"daemon":     4, // alert, broker, config, csvloader
	"executor":   4, // broker, config, market, printer, render, yfinance
	"pithistory": 4, // stockpicker

	// L5 — top composition (below cmd/main, which live outside pkg/).
	"autopilot": 5,

	// L6 — server embeds autopilot + most domains.
	"server": 6,
}

// MustBeLeaf lists the L0 packages that must never acquire an internal import.
// Called out explicitly because these are the ones R16 deliberately made/kept
// leaves.
var MustBeLeaf = map[string]bool{
	"marketdata":   true,
	"broker/types": true,
	"cache":        true,
	"config":       true,
	"costs":        true,
	"render":       true,
	"market":       true,
	"logging":      true,
	"alert":        true,
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
