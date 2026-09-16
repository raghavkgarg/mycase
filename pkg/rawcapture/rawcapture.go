// Package rawcapture archives raw API response bodies to disk for offline
// replay and triage, enforcing the API rule "fetch once, analyze offline"
// (see docs/roadmap.md Phase 11).
//
// It hooks the single HTTP chokepoint in each API client. On a 2xx response the
// caller hands the response body to Capture, which — when capture is enabled —
// buffers the bytes and writes them flat to a disposable archive
//
//	<data>/raw/<source>__<endpoint>__<symbol>__<YYYYMMDD-HHMMSS>.json
//
// and returns a fresh io.ReadCloser over the same bytes so the caller's decode
// path is unchanged. When capture is disabled (the default) Capture returns the
// original body untouched with zero overhead — no read, no allocation.
//
// # Layering
//
// This is a pure L0 leaf (MustBeLeaf): stdlib only, zero internal imports. It
// deliberately does NOT import pkg/config; it self-configures from the
// environment (MYCASE_CAPTURE toggle, MYCASE_DATA_DIR base) so it can be called
// from deep inside the L1/L2 API clients (yfinance, broker/schwab) without
// creating an upward import.
//
// # Secret safety
//
// The API-client chokepoints that call Capture only ever see Bearer-authenticated
// market-data / trader responses and public Yahoo responses. OAuth token-exchange
// and refresh responses flow through a separate path (schwab/auth.go, tokenURL)
// that never reaches Capture, so credentials are never archived. Callers must
// keep it that way: never route an auth/token response through Capture.
package rawcapture

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Env toggles / overrides.
const (
	// captureEnv, when truthy (1/true/yes/on, case-insensitive), enables capture.
	captureEnv = "MYCASE_CAPTURE"
	// dataDirEnv overrides the base data directory. Mirrors pkg/config's env
	// name so both resolve to the same tree; duplicated here (rather than
	// imported) to keep this package a zero-import leaf.
	dataDirEnv = "MYCASE_DATA_DIR"

	defaultDataDir = "data"
	rawSubdir      = "raw"

	// maxSymbolLen bounds the sanitized symbol segment so a pathological
	// multi-symbol query (e.g. /quotes?symbols=A,B,C,...) can't produce an
	// unusable filename.
	maxSymbolLen = 64
)

var (
	enabledOnce sync.Once
	enabled     bool
)

// Enabled reports whether capture is on (via MYCASE_CAPTURE). Cached after the
// first call, so a process decides once at startup.
func Enabled() bool {
	enabledOnce.Do(func() { enabled = truthy(os.Getenv(captureEnv)) })
	return enabled
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// baseDir resolves the raw-archive root: <MYCASE_DATA_DIR or "data">/raw.
func baseDir() string {
	d := os.Getenv(dataDirEnv)
	if d == "" {
		d = defaultDataDir
	}
	return filepath.Join(d, rawSubdir)
}

// Capture archives an API response body when capture is enabled, returning a
// ReadCloser the caller must use in place of the original body.
//
//   - Disabled (default): returns body unchanged; no read, no I/O.
//   - Enabled: buffers body fully, writes it to the archive, and returns a new
//     reader over the buffered bytes. A read or write error never fails the
//     caller — on a read error the original body is returned; a write error is
//     swallowed (archiving is best-effort diagnostics, never on the hot path of
//     correctness).
//
// source is the provider ("schwab", "yahoo"); endpoint is a short logical name
// ("quotes", "fundamentals", "pricehistory"); symbol is the primary ticker or
// query subject ("" when not applicable). All three are sanitized for use in a
// path.
func Capture(source, endpoint, symbol string, body io.ReadCloser) io.ReadCloser {
	if body == nil || !Enabled() {
		return body
	}

	data, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil {
		// Couldn't buffer — hand back what we have as a fresh reader so the
		// caller can still attempt to decode (it will likely see the same
		// error, but we don't change behavior).
		return io.NopCloser(bytes.NewReader(data))
	}

	writeArchive(source, endpoint, symbol, data) // best-effort

	return io.NopCloser(bytes.NewReader(data))
}

// writeArchive writes data flat under <base> as
// "<source>__<endpoint>__<symbol>__<YYYYMMDD-HHMMSS>.json".
// All errors are swallowed: capture is diagnostic and must never break a fetch.
func writeArchive(source, endpoint, symbol string, data []byte) {
	dir := baseDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	name := Filename(source, endpoint, symbol, time.Now())
	_ = os.WriteFile(filepath.Join(dir, name), data, 0o644)
}

// Filename builds the flat archive filename for a capture at time t:
// "<source>__<endpoint>__<symbol>__<YYYYMMDD-HHMMSS>.json". The symbol field is
// omitted when symbol is empty. Exported for tests and for a future replay
// reader that must glob by the same convention.
func Filename(source, endpoint, symbol string, t time.Time) string {
	src := sanitize(source, "unknown")
	ep := sanitize(endpoint, "endpoint")
	ts := t.Format("20060102-150405")
	sym := sanitizeSymbol(symbol)
	if sym == "" {
		return src + "__" + ep + "__" + ts + ".json"
	}
	return src + "__" + ep + "__" + sym + "__" + ts + ".json"
}

// sanitize reduces s to a filesystem-safe path segment, falling back to def
// when the result would be empty.
func sanitize(s, def string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return def
	}
	return out
}

// sanitizeSymbol is sanitize plus a length bound; returns "" for an empty symbol
// (so the filename drops the segment entirely rather than emitting a placeholder).
func sanitizeSymbol(symbol string) string {
	if strings.TrimSpace(symbol) == "" {
		return ""
	}
	s := sanitize(symbol, "")
	if len(s) > maxSymbolLen {
		s = s[:maxSymbolLen]
	}
	return s
}
