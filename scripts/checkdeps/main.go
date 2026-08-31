// Command checkdeps enforces the package layering established by R16
// (dependency untangling). It runs `go list` to read each pkg/ package's direct
// internal imports and fails if:
//
//   - a package imports another package at the same or a higher layer
//     (an upward or sideways edge — the shape that invites cycles), or
//   - a designated leaf package acquires any internal import, or
//   - a package listed in the layer map is missing / an unlisted pkg/ package
//     appears (so new packages must be placed deliberately).
//
// Go already rejects import cycles at compile time; this guard is about
// preserving the *direction* and *leaf-ness* the refactor established, which the
// compiler does not enforce. See docs/refactor.md R16 and
// .kiro/steering/architecture.md.
//
// Run:  go run ./scripts/checkdeps      (wired into `make check-deps` + `make cleanup`)
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

const modulePrefix = "github.com/raghavkgarg/mycase/"

// layers maps each pkg/ package (short path, e.g. "broker/types") to its layer.
// A package may only import packages at a STRICTLY LOWER layer. Layer 0 packages
// are leaves and must have zero internal imports.
//
// When adding a new package, place it here at the correct layer. The check fails
// on any pkg/ package missing from this map, so the placement is a deliberate,
// reviewed decision.
var layers = map[string]int{
	// L0 — leaves: zero internal imports.
	"alert":            0,
	"broker/types":     0,
	"cache":            0,
	"config":           0,
	"costs":            0,
	"csvloader":        0,
	"excel":            0,
	"logging":          0,
	"market":           0,
	"marketdata":       0,
	"render":           0,
	"selectiontracker": 0,

	// L1 — stores / low-level impls over leaves.
	"broker":   1, // broker/types, config, costs
	"tax":      1, // broker/types
	"yfinance": 1, // cache, marketdata

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

	// L4 — orchestration / IO.
	"daemon":   4, // alert, broker, config, csvloader
	"executor": 4, // broker, config, market, printer, render, yfinance

	// L5 — top composition (below cmd/main, which live outside pkg/).
	"autopilot": 5,

	// L6 — server embeds autopilot + most domains.
	"server": 6,
}

// leaves that must never acquire an internal import (subset of L0, called out
// explicitly because these are the ones R16 deliberately made/kept leaves).
var mustBeLeaf = map[string]bool{
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

// goListPkg is the subset of `go list -json` output we consume.
type goListPkg struct {
	ImportPath string
	Imports    []string
}

func main() {
	pkgs, err := listPackages()
	if err != nil {
		fmt.Fprintf(os.Stderr, "checkdeps: %v\n", err)
		os.Exit(1)
	}

	var violations []string
	seen := map[string]bool{}

	for _, p := range pkgs {
		short := strings.TrimPrefix(p.ImportPath, modulePrefix)
		short = strings.TrimPrefix(short, "pkg/")
		seen[short] = true

		layer, known := layers[short]
		if !known {
			violations = append(violations, fmt.Sprintf(
				"package %q is not listed in scripts/checkdeps layer map — add it at the correct layer", short))
			continue
		}

		for _, imp := range p.Imports {
			if !strings.HasPrefix(imp, modulePrefix) {
				continue // stdlib or third-party
			}
			dep := strings.TrimPrefix(imp, modulePrefix)
			dep = strings.TrimPrefix(dep, "pkg/")

			if mustBeLeaf[short] {
				violations = append(violations, fmt.Sprintf(
					"LEAF VIOLATION: %q must be a zero-import leaf but imports %q", short, dep))
				continue
			}

			depLayer, ok := layers[dep]
			if !ok {
				// Dep is outside pkg/ (shouldn't happen for internal imports) — skip.
				continue
			}
			if depLayer >= layer {
				violations = append(violations, fmt.Sprintf(
					"LAYER VIOLATION: %q (L%d) imports %q (L%d) — imports must go strictly downward",
					short, layer, dep, depLayer))
			}
		}
	}

	// Flag any package in the map that no longer exists (keeps the map honest).
	for name := range layers {
		if !seen[name] {
			violations = append(violations, fmt.Sprintf(
				"package %q is in the layer map but was not found — remove it from scripts/checkdeps", name))
		}
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		fmt.Fprintln(os.Stderr, "checkdeps: layering violations found:")
		for _, v := range violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
		}
		os.Exit(1)
	}

	fmt.Println("checkdeps: OK — package layering intact")
}

// listPackages returns the module's pkg/ packages with their direct imports.
func listPackages() ([]goListPkg, error) {
	cmd := exec.Command("go", "list", "-json", "./pkg/...")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}
	var pkgs []goListPkg
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p goListPkg
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("decode go list output: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}
