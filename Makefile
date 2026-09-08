.PHONY: build build-linux-arm64 build-linux-amd64 build-darwin-arm64 build-darwin-amd64
.PHONY: install run test test-verbose test-race test-integration test-coverage cleanup analyze clean fetch-echarts check-deps deps-graph arch-graph help

# Pinned advisory-analysis tool versions (run via `go run` — no global install needed).
# Bump deliberately; keep reproducible per the project's determinism convention.
DEADCODE_VER    ?= v0.49.0
BETTERALIGN_VER ?= v0.15.0
UNPARAM_VER     ?= v0.0.0-20260823230713-2fa3d841b0c8

UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
  ARM64_CC  ?= aarch64-unknown-linux-gnu-gcc
  ARM64_CXX ?= aarch64-unknown-linux-gnu-g++
else
  ARM64_CC  ?= aarch64-linux-gnu-gcc
  ARM64_CXX ?= aarch64-linux-gnu-g++
endif

VERSION    ?= $(shell git describe --tags 2>/dev/null || echo "0.0.0-dev")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X github.com/raghavkgarg/mycase/cmd.Version=$(VERSION) \
              -X github.com/raghavkgarg/mycase/cmd.GitCommit=$(GIT_COMMIT) \
              -X github.com/raghavkgarg/mycase/cmd.BuildDate=$(BUILD_DATE)

build:
	@echo "Building mycase..."
	@mkdir -p dist
	@go build -ldflags "$(LDFLAGS)" -o dist/mycase .
	@echo "Build complete: dist/mycase"

install:
	@go install -ldflags "$(LDFLAGS)" .

build-linux-arm64:
	@echo "Building for Linux ARM64..."
	@mkdir -p dist
	@CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=$(ARM64_CC) CXX=$(ARM64_CXX) \
		go build -ldflags "$(LDFLAGS)" -o dist/mycase-arm64 .
	@echo "Build complete: dist/mycase-arm64"

build-linux-amd64:
	@echo "Building for Linux AMD64..."
	@mkdir -p dist
	@CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
		go build -ldflags "$(LDFLAGS)" -o dist/mycase-amd64 .
	@echo "Build complete: dist/mycase-amd64"

build-darwin-arm64:
	@echo "Building for macOS ARM64..."
	@mkdir -p dist
	@CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
		go build -ldflags "$(LDFLAGS)" -o dist/mycase-darwin-arm64 .
	@echo "Build complete: dist/mycase-darwin-arm64"

build-darwin-amd64:
	@echo "Building for macOS AMD64..."
	@mkdir -p dist
	@CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 \
		go build -ldflags "$(LDFLAGS)" -o dist/mycase-darwin-amd64 .
	@echo "Build complete: dist/mycase-darwin-amd64"

run:
	@go run . $(ARGS)

test:
	@echo "Running tests..."
	@go test -count=1 -timeout 30s ./...

test-verbose:
	@go test -count=1 -v -timeout 30s ./...

test-race:
	@echo "Running tests with race detector..."
	@go test -count=1 -race -timeout 60s ./...

test-integration:
	@echo "Running integration tests (requires network)..."
	@go test -tags=integration -timeout 120s ./...

test-coverage:
	@echo "Running tests with coverage..."
	@go test -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | grep -E "^(total|github)" | tail -1
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

cleanup:
	@echo "=== Format ==="
	@gofmt -w .
	@echo "=== Modernize ==="
	@go fix ./...
	@echo "=== Vet ==="
	@go vet ./...
	@echo "=== Staticcheck ==="
	@staticcheck ./...
	@echo "=== Vulnerabilities ==="
	@govulncheck ./...
	@echo "=== Dependency layering ==="
	@go run ./devtools/checkdeps
	@echo "=== Advisory analysis (non-blocking) ==="
	@$(MAKE) --no-print-directory analyze || true
	@echo "=== All clean ==="

# analyze runs advisory static-analysis tools that surface refactor opportunities
# but are NOT hard gates: each has known false positives (reflection/interface
# reachability for deadcode, exported-but-unused for unparam, intentional layout
# for betteralign). Run it directly to review findings; `cleanup` invokes it
# non-blocking so a finding never fails the build. All three run via `go run` at
# pinned versions — no global install required, and always built against the
# current toolchain (a stale global `unparam` binary errors on newer go/types).
analyze:
	@echo "--- deadcode (unreachable funcs from main; verify before deleting) ---"
	@go run golang.org/x/tools/cmd/deadcode@$(DEADCODE_VER) ./... || true
	@echo "--- unparam (unused params / always-same args) ---"
	@go run mvdan.cc/unparam@$(UNPARAM_VER) ./... || true
	@echo "--- betteralign (struct field ordering; run 'betteralign -apply ./...' to fix) ---"
	@go run github.com/dkorunic/betteralign/cmd/betteralign@$(BETTERALIGN_VER) ./... || true

check-deps:
	@go run ./devtools/checkdeps

deps-graph:
	@mkdir -p dist
	@go run ./devtools/depsgraph > dist/deps.dot
	@if command -v dot >/dev/null 2>&1; then \
		dot -Tsvg dist/deps.dot -o dist/deps.svg; \
		echo "Dependency graph: dist/deps.svg (source: dist/deps.dot)"; \
	else \
		echo "Dependency graph: dist/deps.dot"; \
		echo "Install Graphviz (brew install graphviz) to render SVG: dot -Tsvg dist/deps.dot -o dist/deps.svg"; \
	fi

# arch-graph renders the same layer-map graph as an architecture-style diagram
# via D2's TALA engine (orthogonal, clustered by layer). Transitive reduction is
# on by default (REDUCE=1) — it drops edges already implied by a longer path, so
# a composition root doesn't draw an edge to every leaf it transitively reaches;
# far fewer crossings, identical reachability. Set REDUCE=0 for the full graph.
# Falls back to leaving the .d2 source if d2 is not installed.
D2_LAYOUT ?= tala
REDUCE    ?= 1
arch-graph:
	@mkdir -p dist
	@go run ./devtools/depsgraph -format=d2 $(if $(filter-out 0,$(REDUCE)),-reduce) > dist/deps.d2
	@if command -v d2 >/dev/null 2>&1; then \
		d2 --layout=$(D2_LAYOUT) dist/deps.d2 dist/arch.svg >/dev/null; \
		echo "Architecture graph: dist/arch.svg (layout=$(D2_LAYOUT), reduce=$(REDUCE), source: dist/deps.d2)"; \
	else \
		echo "Architecture graph source: dist/deps.d2"; \
		echo "Install D2 (https://d2lang.com) to render SVG: d2 --layout=tala dist/deps.d2 dist/arch.svg"; \
	fi

clean:
	@rm -f dist/mycase dist/mycase-arm64 dist/mycase-amd64 dist/mycase-darwin-arm64 dist/mycase-darwin-amd64 dist/deps.dot dist/deps.svg dist/deps.d2 dist/arch.svg
	@echo "Cleaned"

fetch-echarts:
	@echo "Downloading ECharts 5.6.0…"
	@curl -fsSL "https://cdn.jsdelivr.net/npm/echarts@5.6.0/dist/echarts.min.js" \
		-o pkg/server/static/vendor/echarts.min.js
	@echo "ECharts downloaded to pkg/server/static/vendor/echarts.min.js"

help:
	@echo "Available targets:"
	@echo "  build              - Build dist/mycase binary"
	@echo "  install            - Install to GOPATH/bin"
	@echo "  build-linux-arm64  - Cross-compile for Linux ARM64"
	@echo "  build-linux-amd64  - Cross-compile for Linux AMD64"
	@echo "  build-darwin-arm64 - Build for macOS ARM64"
	@echo "  build-darwin-amd64 - Build for macOS AMD64"
	@echo "  run ARGS=...       - Run with go run (dev mode)"
	@echo "  test               - Run all tests"
	@echo "  test-verbose       - Run all tests verbosely"
	@echo "  test-race          - Run tests with race detector"
	@echo "  test-integration   - Run integration tests (requires network)"
	@echo "  test-coverage      - Run tests and generate coverage.html"
	@echo "  cleanup            - gofmt + go fix + go vet + staticcheck + govulncheck + check-deps + analyze (advisory)"
	@echo "  analyze            - Advisory static analysis: deadcode + unparam + betteralign (non-blocking)"
	@echo "  check-deps         - Enforce R16 package layering (leaves + downward imports)"
	@echo "  deps-graph         - Render pkg/ dependency graph to dist/deps.svg (Graphviz, layer-colored)"
	@echo "  arch-graph         - Render pkg/ architecture diagram to dist/arch.svg (D2/TALA, transitive-reduced; REDUCE=0 for full)"
	@echo "  clean              - Remove build artifacts"
	@echo "  fetch-echarts      - Download ECharts 5.6.0 into pkg/server/static/vendor/"
