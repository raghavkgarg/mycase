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
// The layer map itself lives in devtools/internal/layers — the single source of
// truth shared with devtools/depsgraph (visualization).
//
// Run:  go run ./devtools/checkdeps      (wired into `make check-deps` + `make cleanup`)
package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/raghavkgarg/mycase/devtools/internal/layers"
)

func main() {
	pkgs, err := layers.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "checkdeps: %v\n", err)
		os.Exit(1)
	}

	var violations []string
	seen := map[string]bool{}

	for _, p := range pkgs {
		short := layers.Short(p.ImportPath)
		seen[short] = true

		layer, known := layers.Layers[short]
		if !known {
			violations = append(violations, fmt.Sprintf(
				"package %q is not listed in devtools/internal/layers layer map — add it at the correct layer", short))
			continue
		}

		for _, imp := range p.Imports {
			if !layers.IsInternal(imp) {
				continue // stdlib or third-party
			}
			dep := layers.Short(imp)

			if layers.MustBeLeaf[short] {
				violations = append(violations, fmt.Sprintf(
					"LEAF VIOLATION: %q must be a zero-import leaf but imports %q", short, dep))
				continue
			}

			depLayer, ok := layers.Layers[dep]
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
	for name := range layers.Layers {
		if !seen[name] {
			violations = append(violations, fmt.Sprintf(
				"package %q is in the layer map but was not found — remove it from devtools/internal/layers", name))
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
