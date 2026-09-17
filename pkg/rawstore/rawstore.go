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
	"sort"
	"strconv"
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

// Env overrides for retention (env layer of flag > env > config > default).
const (
	retainDaysEnv = "MYCASE_RAW_RETAIN_DAYS"
	maxSizeMBEnv  = "MYCASE_RAW_MAX_SIZE_MB"
)

// ResolveRetention builds a Retention from config/defaults.json plus env
// overrides, applying flag > env > config > default precedence for the
// config/env/default layers (a caller with an explicit flag value should pass
// it via the overrides). retainDaysOverride / maxSizeMBOverride are applied when
// >= 0 (use -1 for "not set"), sitting above env and config.
//
// Precedence per field: override (>=0) > env > config (>0) > built-in default.
func ResolveRetention(cfg config.RawConfig, retainDaysOverride, maxSizeMBOverride int) Retention {
	days := config.DefaultRawRetainDays
	if cfg.RetainDays > 0 {
		days = cfg.RetainDays
	}
	if v, ok := envInt(retainDaysEnv); ok {
		days = v
	}
	if retainDaysOverride >= 0 {
		days = retainDaysOverride
	}

	sizeMB := config.DefaultRawMaxSizeMB
	if cfg.MaxSizeMB > 0 {
		sizeMB = cfg.MaxSizeMB
	}
	if v, ok := envInt(maxSizeMBEnv); ok {
		sizeMB = v
	}
	if maxSizeMBOverride >= 0 {
		sizeMB = maxSizeMBOverride
	}

	return Retention{
		MaxAge:   time.Duration(days) * 24 * time.Hour,
		MaxBytes: int64(sizeMB) * 1024 * 1024,
	}
}

// envInt reads a non-negative integer env var. Returns (0,false) when unset or
// unparseable so the caller keeps the lower-precedence value.
func envInt(name string) (int, bool) {
	s := strings.TrimSpace(os.Getenv(name))
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
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

// Retention bounds the growth of the archive. A file is pruned if it violates
// *either* ceiling; both are independent and either can be disabled with a
// non-positive value:
//
//   - MaxAge: delete archives whose modification time is older than now-MaxAge
//     (<= 0 → age pruning disabled).
//   - MaxBytes: cap the archive's total size; when the total exceeds MaxBytes,
//     delete oldest-first (by modification time) until at or under the cap
//     (<= 0 → size pruning disabled).
//
// This is deliberately the "keep everything recent, prune only the boring old
// middle" policy from docs/roadmap.md R-store-3: no run/verdict concept, just
// age and total size. False-keep is cheap (disk); false-delete is catastrophic
// (an unreproducible bug), so the ceilings are the only triggers.
type Retention struct {
	MaxAge   time.Duration
	MaxBytes int64
}

// PruneResult reports what a Prune call removed.
type PruneResult struct {
	RemovedByAge  int   // files deleted for exceeding MaxAge
	RemovedBySize int   // files deleted to bring the total under MaxBytes
	FreedBytes    int64 // total bytes reclaimed
	RemainingSize int64 // archive size after pruning
	Remaining     int   // file count after pruning
}

// Removed returns the total number of files deleted.
func (r PruneResult) Removed() int { return r.RemovedByAge + r.RemovedBySize }

// Prune enforces the Retention policy over the archive, oldest-first. It is
// best-effort: unreadable entries and individual delete failures are skipped
// rather than propagated (a nil error means "the pass ran", not "every delete
// succeeded"). A nil Store or a missing raw dir is a no-op returning a zero
// result. Only files matching the archive naming convention
// (<...>__<stamp>.json) are considered; foreign files are ignored.
//
// It emits a single Info "rawstore.pruned" line summarizing the pass (never any
// file bodies), and a Debug line per deleted file.
func (s *Store) Prune(ret Retention) (PruneResult, error) {
	var res PruneResult
	if s == nil {
		return res, nil
	}

	entries, err := os.ReadDir(s.rawDir)
	if err != nil {
		// Missing dir (nothing captured yet) is not an error worth surfacing.
		if os.IsNotExist(err) {
			return res, nil
		}
		return res, err
	}

	type archived struct {
		name    string
		size    int64
		modTime time.Time
	}
	files := make([]archived, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		// Only touch our own archive files; never delete foreign content.
		if !strings.HasSuffix(n, ".json") || !strings.Contains(n, "__") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, archived{name: n, size: info.Size(), modTime: info.ModTime()})
	}

	// Oldest-first so both the age pass and the size pass evict in the same
	// order (a size overflow removes the least-recently-useful captures first).
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })

	remove := func(f archived) {
		if err := os.Remove(filepath.Join(s.rawDir, f.name)); err != nil {
			return
		}
		res.FreedBytes += f.size
		slog.Debug("rawstore.pruned_file", "file", f.name, "bytes", f.size)
	}

	// Age pass.
	kept := files[:0:0]
	var total int64
	if ret.MaxAge > 0 {
		cutoff := time.Now().Add(-ret.MaxAge)
		for _, f := range files {
			if f.modTime.Before(cutoff) {
				remove(f)
				res.RemovedByAge++
				continue
			}
			kept = append(kept, f)
			total += f.size
		}
	} else {
		for _, f := range files {
			kept = append(kept, f)
			total += f.size
		}
	}

	// Size pass: evict oldest survivors until under the cap.
	if ret.MaxBytes > 0 {
		i := 0
		for total > ret.MaxBytes && i < len(kept) {
			remove(kept[i])
			total -= kept[i].size
			res.RemovedBySize++
			i++
		}
		kept = kept[i:]
	}

	res.RemainingSize = total
	res.Remaining = len(kept)

	if res.Removed() > 0 {
		slog.Info("rawstore.pruned",
			"removed_by_age", res.RemovedByAge,
			"removed_by_size", res.RemovedBySize,
			"freed_bytes", res.FreedBytes,
			"remaining", res.Remaining,
			"remaining_bytes", res.RemainingSize,
		)
	}
	return res, nil
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
