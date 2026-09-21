// Package rawcapture is the zero-import hook half of the raw-response archive
// that enforces the API rule "fetch once, analyze offline" (see docs/03-roadmap.md
// Phase 11).
//
// It hooks the single HTTP chokepoint in each API client. On a 2xx response the
// caller hands the response body to Capture, which — when capture is on and a
// Sink has been wired — buffers the bytes, hands them to the Sink to archive,
// and returns a fresh io.ReadCloser over the same bytes so the caller's decode
// path is unchanged. Capture is ON by default (see Enabled); when it is off
// (MYCASE_CAPTURE=0/off, or during replay) or no Sink is wired, Capture returns
// the original body untouched with zero overhead — no read, no allocation.
//
// # Offline replay
//
// With replay enabled (MYCASE_REPLAY), the same chokepoints call Replay first:
// on a hit it returns the newest archived body for (source, endpoint, symbol),
// and the caller short-circuits into a synthetic 200 response — no network, no
// token, no rate limiter. This lets pick/report/etc. rerun the whole pipeline
// against recorded responses with zero live calls.
//
// # Layering — hook vs. store
//
// This is a pure L0 leaf (MustBeLeaf): stdlib only, zero internal imports. It is
// called from deep inside the L1/L2 API clients (yfinance, broker/schwab), so it
// cannot import pkg/config, own directories, or know the on-disk filename
// convention without creating an upward import. It therefore holds NONE of that:
// it declares the Sink interface and delegates all persistence to an injected
// implementation.
//
// The concrete Sink (data dir, filename convention, run identity, retention)
// lives in pkg/rawstore (L4), which imports pkg/config legally. The composition
// root (main's Before hook) constructs the store and wires it via SetSink. Until
// a Sink is wired, Capture/Replay are inert no-ops — correct for tests and
// library use.
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
	"strings"
	"sync"
)

// Sink is the persistence contract for archived API responses, implemented by
// pkg/rawstore (L4) and injected via SetSink. Keeping the interface here — in
// the leaf that the API clients already import — lets the clients archive
// responses without importing the store (the consumer-defined-interface pattern
// from .kiro/steering/architecture.md).
//
// Both methods are addressed by the logical archive key (source, endpoint,
// symbol); the Sink owns the mapping from that key to on-disk files.
type Sink interface {
	// Write archives a fully-buffered response body. Implementations must be
	// best-effort: a failure to persist must never propagate to the caller
	// (archiving is diagnostics, not correctness).
	Write(source, endpoint, symbol string, body []byte)
	// Open returns the newest archived body for (source, endpoint, symbol) and
	// true on a hit; nil and false on a miss.
	Open(source, endpoint, symbol string) (io.ReadCloser, bool)
}

// Env toggles.
const (
	// captureEnv is the capture opt-OUT. Capture is ON by default (once a Sink
	// is wired); setting MYCASE_CAPTURE to a falsy value (0/false/no/off,
	// case-insensitive) disables it. Any other value (including unset or a
	// truthy value) leaves capture on. The default-on stance is deliberate: the
	// failure it guards against is evidence loss — the surprising run is the one
	// you didn't think to arm (see docs/03-roadmap.md R-store-2).
	captureEnv = "MYCASE_CAPTURE"
	// replayEnv, when truthy, enables offline replay: API-client chokepoints
	// serve archived bodies from the Sink instead of hitting the network.
	replayEnv = "MYCASE_REPLAY"
)

var (
	enabledOnce sync.Once
	enabled     bool

	replayOnce    sync.Once
	replayEnabled bool

	sinkMu sync.RWMutex
	sink   Sink
)

// SetSink wires the persistence implementation (pkg/rawstore). The composition
// root calls this once at startup. Passing nil disables archiving (the default
// state before wiring). Safe for concurrent use.
func SetSink(s Sink) {
	sinkMu.Lock()
	sink = s
	sinkMu.Unlock()
}

func currentSink() Sink {
	sinkMu.RLock()
	defer sinkMu.RUnlock()
	return sink
}

// Enabled reports whether capture is on. Capture is ON by default and is
// suppressed only when:
//
//   - MYCASE_CAPTURE is set to a falsy value (0/false/no/off) — the explicit
//     operator opt-out; or
//   - replay is enabled (MYCASE_REPLAY) — replayed bytes must not be
//     re-archived, which would compound copies of the same recorded response.
//
// Cached after the first call, so a process decides once at startup.
//
// Note this reports only the env/replay policy; Capture is still inert unless a
// Sink has been wired (SetSink). Tests and library callers that never wire a
// sink therefore archive nothing, default-on notwithstanding.
func Enabled() bool {
	enabledOnce.Do(func() {
		enabled = !falsy(os.Getenv(captureEnv)) && !ReplayEnabled()
	})
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

// falsy reports whether v is an explicit "off" value. Only these turn capture
// off; unset or any other value leaves the default (on) in place.
func falsy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "no", "off":
		return true
	default:
		return false
	}
}

// Capture archives an API response body when capture is enabled and a Sink is
// wired, returning a ReadCloser the caller must use in place of the original
// body.
//
//   - Capture off (MYCASE_CAPTURE=0/off, or replay mode) or no Sink wired:
//     returns body unchanged; no read, no I/O.
//   - On + Sink: buffers body fully, hands the bytes to the Sink, and
//     returns a new reader over the buffered bytes. A read error never fails the
//     caller — on a read error the buffered-so-far bytes are handed back as a
//     fresh reader. The Sink's Write is best-effort and never propagates errors.
//
// source is the provider ("schwab", "yahoo"); endpoint is a short logical name
// ("quotes", "fundamentals", "pricehistory"); symbol is the primary ticker or
// query subject ("" when not applicable). The Sink is responsible for any
// sanitization of these into a path.
func Capture(source, endpoint, symbol string, body io.ReadCloser) io.ReadCloser {
	if body == nil || !Enabled() {
		return body
	}
	s := currentSink()
	if s == nil {
		// No store wired — inert. Return the body untouched so the caller's
		// decode path sees exactly what it would without capture.
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

	s.Write(source, endpoint, symbol, data) // best-effort

	return io.NopCloser(bytes.NewReader(data))
}

// Replay serves a previously-archived response body for (source, endpoint,
// symbol) when replay is enabled (MYCASE_REPLAY) and a Sink is wired. It returns
// the Sink's newest matching body and true on a hit; nil and false on a miss,
// when replay is disabled, or when no Sink is wired.
//
// Callers use a hit to short-circuit the network entirely: build a synthetic
// 200 *http.Response around the returned body and skip auth, rate limiting, and
// the actual request.
func Replay(source, endpoint, symbol string) (io.ReadCloser, bool) {
	if !ReplayEnabled() {
		return nil, false
	}
	s := currentSink()
	if s == nil {
		return nil, false
	}
	return s.Open(source, endpoint, symbol)
}
