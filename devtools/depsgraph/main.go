// Command depsgraph emits a dependency graph of the module's internal pkg/
// packages, colored and grouped by the R16 layer map.
//
// It is the visual companion to devtools/checkdeps: checkdeps gives a pass/fail
// verdict on the layering, depsgraph lets you *see* the graph — every internal
// edge, grouped into layer bands (L0 leaves at the bottom, L6 server at the
// top). An unexpected cross-package edge that technically passes checkdeps but
// smells wrong is obvious in the picture.
//
// Two output formats, same layer-map source of truth (shared with checkdeps via
// devtools/internal/layers):
//
//	-format=dot  (default) Graphviz DOT — render with `dot -Tsvg`.
//	-format=d2             D2 with one container per layer — render with
//	                       `d2 --layout=tala` for an architecture-style diagram.
//
// Only internal edges (github.com/raghavkgarg/mycase/pkg/*) are drawn; stdlib
// and third-party imports are omitted to keep the graph about our own structure.
//
// Run:  go run ./devtools/depsgraph > deps.dot
//
//	go run ./devtools/depsgraph | dot -Tsvg -o deps.svg
//	go run ./devtools/depsgraph -format=d2 | d2 --layout=tala - deps.svg
//
// (wired into `make deps-graph` and `make arch-graph`).
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/raghavkgarg/mycase/devtools/internal/layers"
)

// layerColor gives each layer band a fill color (light → dark, low → high).
var layerColor = map[int]string{
	0: "#e3f2fd",
	1: "#c8e6c9",
	2: "#fff9c4",
	3: "#ffe0b2",
	4: "#ffccbc",
	5: "#f8bbd0",
	6: "#e1bee7",
}

// layerRole is a short human label for each layer band, mirrored from the
// architecture steering doc, used as a container sub-title in the D2 output.
var layerRole = map[int]string{
	0: "leaves — zero internal imports",
	1: "stores / low-level impls",
	2: "domains + data routing",
	3: "higher-level domains",
	4: "orchestration / IO",
	5: "top composition",
	6: "server",
}

type edge struct{ from, to string }

// graph is the resolved internal dependency graph, grouped by layer.
type graph struct {
	byLayer map[int][]string
	edges   []edge
}

func main() {
	format := flag.String("format", "dot", "output format: dot | d2")
	reduce := flag.Bool("reduce", false, "transitive reduction: drop edges already implied by a longer path (much cleaner picture, same reachability)")
	flag.Parse()

	g, err := build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "depsgraph: %v\n", err)
		os.Exit(1)
	}
	if *reduce {
		g.edges = transitiveReduction(g.edges)
	}

	switch strings.ToLower(*format) {
	case "dot":
		fmt.Print(emitDOT(g))
	case "d2":
		fmt.Print(emitD2(g))
	default:
		fmt.Fprintf(os.Stderr, "depsgraph: unknown -format %q (want dot|d2)\n", *format)
		os.Exit(2)
	}
}

// transitiveReduction removes edges (a→b) that are already implied by a longer
// path a→…→b, i.e. b is reachable from a without using the direct edge. The
// result has identical reachability but far fewer edges, which is what makes a
// layered dependency graph legible (a composition root like autopilot need not
// draw an edge to every leaf it can already reach through its direct deps).
//
// Safe here because the graph is a DAG (checkdeps enforces strictly-downward
// imports — no cycles), so "reachable via another path" is unambiguous.
func transitiveReduction(edges []edge) []edge {
	adj := map[string]map[string]bool{}
	for _, e := range edges {
		if adj[e.from] == nil {
			adj[e.from] = map[string]bool{}
		}
		adj[e.from][e.to] = true
	}

	// reachableSkipping reports whether dst is reachable from src without taking
	// the direct src→dst edge (DFS over the DAG).
	reachableSkipping := func(src, dst string) bool {
		seen := map[string]bool{}
		var stack []string
		for n := range adj[src] {
			if n == dst {
				continue // skip the direct edge under test
			}
			stack = append(stack, n)
		}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if n == dst {
				return true
			}
			if seen[n] {
				continue
			}
			seen[n] = true
			for m := range adj[n] {
				stack = append(stack, m)
			}
		}
		return false
	}

	var out []edge
	seen := map[string]bool{}
	for _, e := range edges {
		key := e.from + "\x00" + e.to
		if seen[key] {
			continue
		}
		seen[key] = true
		if reachableSkipping(e.from, e.to) {
			continue // implied by a longer path — drop it
		}
		out = append(out, e)
	}
	return out
}

// build resolves the pkg/ graph into layer-grouped nodes and deduplicated edges.
func build() (*graph, error) {
	pkgs, err := layers.List()
	if err != nil {
		return nil, err
	}

	byLayer := map[int][]string{}
	seen := map[string]bool{}
	var edges []edge

	for _, p := range pkgs {
		short := layers.Short(p.ImportPath)
		if _, known := layers.Layers[short]; !known {
			// Unknown package — checkdeps will flag it; show it in an "unplaced"
			// band (layer -1) rather than dropping it silently.
			if !seen[short] {
				byLayer[-1] = append(byLayer[-1], short)
				seen[short] = true
			}
		} else if !seen[short] {
			byLayer[layers.Layers[short]] = append(byLayer[layers.Layers[short]], short)
			seen[short] = true
		}

		for _, imp := range p.Imports {
			if !layers.IsInternal(imp) {
				continue
			}
			edges = append(edges, edge{from: short, to: layers.Short(imp)})
		}
	}

	sort.Slice(edges, func(i, j int) bool {
		if edges[i].from != edges[j].from {
			return edges[i].from < edges[j].from
		}
		return edges[i].to < edges[j].to
	})

	return &graph{byLayer: byLayer, edges: edges}, nil
}

// emitDOT renders the graph as Graphviz DOT (L0 leaves at bottom → L6 top).
func emitDOT(g *graph) string {
	var b strings.Builder
	b.WriteString("// Generated by devtools/depsgraph — internal pkg/ dependency graph.\n")
	b.WriteString("// Layers: L0 (leaves) at bottom → L6 (server) at top. Edges point to the imported package.\n")
	b.WriteString("digraph mycase_deps {\n")
	b.WriteString("  rankdir=BT;\n")
	b.WriteString("  node [shape=box, style=\"rounded,filled\", fontname=\"Helvetica\", fontsize=10];\n")
	b.WriteString("  edge [color=\"#90a4ae\", arrowsize=0.7];\n")
	b.WriteString("  graph [fontname=\"Helvetica\", labelloc=t, label=\"mycase pkg/ dependency graph (L0 leaves → L6 server)\"];\n\n")

	for _, l := range layerBands(g.byLayer) {
		names := g.byLayer[l]
		sort.Strings(names)
		label, color := "unplaced (not in layer map)", "#ffcdd2"
		if l >= 0 {
			label = fmt.Sprintf("L%d", l)
			color = layerColor[l]
		}
		fmt.Fprintf(&b, "  subgraph cluster_L%d {\n", l+1) // +1 so -1 → cluster_L0-safe id
		fmt.Fprintf(&b, "    label=%q; style=filled; color=\"#eceff1\"; fontsize=12;\n", label)
		b.WriteString("    { rank=same;\n")
		for _, n := range names {
			fmt.Fprintf(&b, "      %q [fillcolor=%q];\n", n, color)
		}
		b.WriteString("    }\n")
		b.WriteString("  }\n\n")
	}

	edgeSeen := map[string]bool{}
	for _, e := range g.edges {
		key := e.from + "\x00" + e.to
		if edgeSeen[key] {
			continue
		}
		edgeSeen[key] = true
		fmt.Fprintf(&b, "  %q -> %q;\n", e.from, e.to)
	}

	b.WriteString("}\n")
	return b.String()
}

// emitD2 renders the graph as D2: one container per layer band (which TALA lays
// out as clusters), each package a node, edges pointing to the imported package.
// Designated leaves (layers.MustBeLeaf) are marked so the "define, don't import"
// packages stand out. Render with `d2 --layout=tala`.
func emitD2(g *graph) string {
	var b strings.Builder
	b.WriteString("# Generated by devtools/depsgraph -format=d2 — internal pkg/ dependency graph.\n")
	b.WriteString("# Source of truth: devtools/internal/layers/layers.go (same map as checkdeps).\n")
	b.WriteString("# Render:  d2 --layout=tala deps.d2 deps.svg\n\n")

	b.WriteString("direction: up\n\n")
	b.WriteString("title: |md\n  # mycase `pkg/` architecture\n  L0 leaves → L6 server · edges point to the imported package\n| {\n  near: top-center\n  style.font-size: 24\n}\n\n")

	// nodeID maps a short package name to a layer-qualified D2 id so names with
	// slashes (broker/types) don't collide with D2's container path syntax.
	nodeID := func(short string, layer int) string {
		safe := strings.ReplaceAll(short, "/", "_")
		if layer < 0 {
			return "unplaced." + safe
		}
		return fmt.Sprintf("L%d.%s", layer, safe)
	}
	layerOf := map[string]int{}
	for l, names := range g.byLayer {
		for _, n := range names {
			layerOf[n] = l
		}
	}

	// One container per layer band, low → high.
	for _, l := range layerBands(g.byLayer) {
		names := g.byLayer[l]
		sort.Strings(names)

		var cid, clabel, color string
		if l < 0 {
			cid, clabel, color = "unplaced", "unplaced (not in layer map)", "#ffcdd2"
		} else {
			cid = fmt.Sprintf("L%d", l)
			clabel = fmt.Sprintf("L%d — %s", l, layerRole[l])
			color = layerColor[l]
		}
		fmt.Fprintf(&b, "%s: %q {\n", cid, clabel)
		b.WriteString("  style.fill: \"#eceff1\"\n")
		b.WriteString("  style.stroke: \"#b0bec5\"\n")
		for _, n := range names {
			safe := strings.ReplaceAll(n, "/", "_")
			label := n
			shape := "rectangle"
			if layers.MustBeLeaf[n] {
				// Designated leaves: must never acquire an internal import.
				label = n + "  ◆"
				shape = "hexagon"
			}
			fmt.Fprintf(&b, "  %s: %q {\n", safe, label)
			fmt.Fprintf(&b, "    shape: %s\n", shape)
			fmt.Fprintf(&b, "    style.fill: %q\n", color)
			b.WriteString("  }\n")
		}
		b.WriteString("}\n\n")
	}

	// Edges (deduplicated).
	edgeSeen := map[string]bool{}
	for _, e := range g.edges {
		key := e.from + "\x00" + e.to
		if edgeSeen[key] {
			continue
		}
		edgeSeen[key] = true
		fl, ok1 := layerOf[e.from]
		tl, ok2 := layerOf[e.to]
		if !ok1 {
			fl = -1
		}
		if !ok2 {
			tl = -1
		}
		fmt.Fprintf(&b, "%s -> %s\n", nodeID(e.from, fl), nodeID(e.to, tl))
	}

	return b.String()
}

// layerBands returns the layer keys present, sorted ascending (-1 unplaced first).
func layerBands(byLayer map[int][]string) []int {
	var ls []int
	for l := range byLayer {
		ls = append(ls, l)
	}
	sort.Ints(ls)
	return ls
}
