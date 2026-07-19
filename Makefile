.PHONY: build build-linux-arm64 build-linux-amd64 build-darwin-arm64 build-darwin-amd64
.PHONY: install run test test-verbose test-race test-integration test-coverage cleanup clean help

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
	@go test -timeout 30s ./...

test-verbose:
	@go test -v -timeout 30s ./...

test-race:
	@echo "Running tests with race detector..."
	@go test -race -timeout 60s ./...

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
	@echo "=== All clean ==="

clean:
	@rm -f dist/mycase dist/mycase-arm64 dist/mycase-amd64 dist/mycase-darwin-arm64 dist/mycase-darwin-amd64
	@echo "Cleaned"

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
	@echo "  cleanup            - gofmt + go fix + go vet + staticcheck + govulncheck"
	@echo "  clean              - Remove build artifacts"
