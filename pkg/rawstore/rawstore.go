// Package rawstore is the L4 persistence half of the raw-response archive: it
// implements rawcapture.Sink, owning everything the zero-import hook cannot —
// the data directory, the on-disk filename convention, and (for future
// retention/triage) run identity. See docs/roadmap.md Phase 11 (R-store-1).
//
// The archive is a flat directory of disposable JSON files under <data>/raw:
//
//	<data>/raw/<source>__<endpoint>__<symbol>__<YYYYMMDD-HHMMSS>.json
//
// The double-underscore field separator and the fixed-width stamp make the
// filename both greppable and lexically-sortable in chronological order, so
// "newest matching archive" is just the max-by-name entry.
//
// # Layering
//
// This package sits at L4 (orchestration/IO). It legally imports pkg/config for
// the resolved data dir; nothing below L4 references it. The composition root
// (main's Before hook) constructs a Store and injects it via
// rawcapture.SetSink, so the L1/L2 API clients archive through the
// rawcapture.Sink interface without ever importing this package (the
// consumer-defined-interface pattern from .kiro/steering/architecture.md).
package rawstore

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/config"
)

const (
	rawSubdir = "raw"

	// maxSymbolLen bounds the sanitized symbol segment so a pathological
	// multi-symbol query (e.g. /quotes?symbols=A,B,C,...) can't produce an
	// unusable filename.
	maxSymbolLen = 64
)

// Store is the filesystem-backed rawcapture.Sink. It archives to and replays
// from a raw dir (<base>/raw). A nil *Store is a safe no-op sink, so callers can
// treat "no store" as "archiving disabled".
type Store struct {
	rawDir string
	// reqID identifies the current process/run. Captures are already
	// attributable via the default slog logger, which main's Before hook tags
	// with the same req_id; this field retains it for run-grouping/retention
	// (later R-store tasks) and is not yet encoded into filenames, so the
	// on-disk convention is unchanged.
	reqID string
}

// New constructs a Store rooted at base (typically config.DataDir()); the
// archive lives under <base>/raw. reqID is the run identity (e.g.
// logging.ReqID(ctx)) attached to capture log lines and reserved for future
// retention/run-grouping. base must be non-empty.
func New(base, reqID string) *Store {
	if base == "" {
		return nil
	}
	return &Store{rawDir: filepath.Join(base, rawSubdir), reqID: reqID}
}

// NewDefault constructs a Store rooted at the resolved config.DataDir().
func NewDefault(reqID string) *Store {
	return New(config.DataDir(), reqID)
}

// ReqID returns the run identity this store was constructed with. A nil Store
// returns "". Reserved for run-grouping/retention (later R-store tasks).
func (s *Store) ReqID() string {
	if s == nil {
		return ""
	}
	return s.reqID
}

// Write archives data flat under the raw dir as
// "<source>__<endpoint>__<symbol>__<YYYYMMDD-HHMMSS>.json". All errors are
// swallowed: archiving is diagnostic and must never break a fetch. A nil Store
// is a no-op.
//
// On a successful write it emits a Debug "rawstore.captured" line (tagged with
// the run's req_id by the default logger) recording the archive key/size —
// never the body — so a capture is attributable to the run that produced it.
func (s *Store) Write(source, endpoint, symbol string, data []byte) {
	if s == nil {
		return
	}
	if err := os.MkdirAll(s.rawDir, 0o755); err != nil {
		return
	}
	name := Filename(source, endpoint, symbol, time.Now())
	if err := os.WriteFile(filepath.Join(s.rawDir, name), data, 0o644); err != nil {
		return
	}
	slog.Debug("rawstore.captured",
		"source", source,
		"endpoint", endpoint,
		"symbol", symbol,
		"file", name,
		"bytes", len(data),
	)
}

// Open returns the newest archived body for (source, endpoint, symbol) and true
// on a hit; nil and false on a miss or read error. A nil Store is always a miss.
func (s *Store) Open(source, endpoint, symbol string) (io.ReadCloser, bool) {
	if s == nil {
		return nil, false
	}
	name, ok := findLatest(s.rawDir, source, endpoint, symbol)
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(s.rawDir, name))
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
// omitted when symbol is empty. Exported so the writer and the (future) triage
// reader share one convention.
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
