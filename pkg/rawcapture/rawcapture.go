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
// # Offline replay
//
// With replay enabled (MYCASE_REPLAY), the same chokepoints call Replay first:
// on a hit it returns the newest archived body for (source, endpoint, symbol),
// and the caller short-circuits into a synthetic 200 response — no network, no
// token, no rate limiter. This lets pick/report/etc. rerun the whole pipeline
// against recorded responses with zero live calls.
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
	// replayEnv, when truthy, enables offline replay: API-client chokepoints
	// serve archived bodies from data/raw instead of hitting the network.
	replayEnv = "MYCASE_REPLAY"
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

	replayOnce    sync.Once
	replayEnabled bool
)

// Enabled reports whether capture is on (via MYCASE_CAPTURE). Cached after the
// first call, so a process decides once at startup.
func Enabled() bool {
	enabledOnce.Do(func() { enabled = truthy(os.Getenv(captureEnv)) })
	return enabled
}

// ReplayEnabled reports whether offline replay is on (via MYCASE_REPLAY).
// Cached after the first call.
func ReplayEnabled() bool {
	replayOnce.Do(func() { replayEnabled = truthy(os.Getenv(replayEnv)) })
	return replayEnabled
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

// Replay serves a previously-archived response body for (source, endpoint,
// symbol) when replay is enabled (MYCASE_REPLAY). It returns the newest matching
// archive file's body and true on a hit; nil and false on a miss or when replay
// is disabled.
//
// Matching is by the archive filename fields: it selects
// "<source>__<endpoint>__<symbol>__*.json" (or "<source>__<endpoint>__*.json"
// when symbol is empty), and — because the stamp field sorts lexically in
// chronological order — returns the lexically-greatest (newest) match.
//
// Callers use a hit to short-circuit the network entirely: build a synthetic
// 200 *http.Response around the returned body and skip auth, rate limiting, and
// the actual request.
func Replay(source, endpoint, symbol string) (io.ReadCloser, bool) {
	if !ReplayEnabled() {
		return nil, false
	}
	name, ok := findLatest(baseDir(), source, endpoint, symbol)
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(baseDir(), name))
	if err != nil {
		return nil, false
	}
	return io.NopCloser(bytes.NewReader(data)), true
}

// findLatest returns the newest archive filename in dir matching the given
// fields, and whether one was found. The stamp field's fixed-width
// YYYYMMDD-HHMMSS format makes lexical order == chronological order, so the
// max-by-name entry is the most recent.
func findLatest(dir, source, endpoint, symbol string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	prefix := matchPrefix(source, endpoint, symbol)
	wantFields := 4 // source, endpoint, symbol, stamp
	if sanitizeSymbol(symbol) == "" {
		wantFields = 3 // source, endpoint, stamp
	}
	var latest string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasPrefix(n, prefix) || !strings.HasSuffix(n, ".json") {
			continue
		}
		// Guard against a symbol-less query matching a symbol-carrying file
		// (e.g. "yahoo__quotes__" would prefix "yahoo__quotes__AAPL__..."):
		// require the exact field count.
		if fieldCount(n) != wantFields {
			continue
		}
		if n > latest {
			latest = n
		}
	}
	return latest, latest != ""
}

// matchPrefix builds the fixed leading portion of the archive filename for the
// given fields (everything before the stamp), using the same sanitization as
// Filename so a request and its archived response resolve to the same key.
func matchPrefix(source, endpoint, symbol string) string {
	src := sanitize(source, "unknown")
	ep := sanitize(endpoint, "endpoint")
	sym := sanitizeSymbol(symbol)
	if sym == "" {
		return src + "__" + ep + "__"
	}
	return src + "__" + ep + "__" + sym + "__"
}

// fieldCount counts the "__"-separated fields in an archive filename (with the
// .json suffix stripped).
func fieldCount(name string) int {
	return len(strings.Split(strings.TrimSuffix(name, ".json"), "__"))
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
