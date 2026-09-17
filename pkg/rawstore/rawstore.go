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

// stampLayout is the fixed-width timestamp format embedded in archive
// filenames. Its fixed width makes lexical order == chronological order.
const stampLayout = "20060102-150405"

// Entry is a parsed view of one archived response, as surfaced by triage
// (R-store-4). Source/Endpoint/Symbol come from the filename fields (Symbol is
// "" for symbol-less captures); When is parsed from the filename stamp; Size and
// Name come from the directory entry.
type Entry struct {
	When     time.Time
	Name     string // on-disk filename (within the raw dir)
	Source   string
	Endpoint string
	Symbol   string
	Size     int64
}

// ParseFilename decodes an archive filename into its Source/Endpoint/Symbol/When
// fields, returning false for any name that doesn't match the convention
// (<source>__<endpoint>__[<symbol>__]<stamp>.json). Size/Name are not set here
// (they come from the directory entry); callers that have the os.DirEntry fill
// them in. This is the inverse of Filename and stays schema-blind — it works for
// every source (schwab, yahoo, future edgar) without knowing their payloads.
func ParseFilename(name string) (Entry, bool) {
	if !strings.HasSuffix(name, ".json") {
		return Entry{}, false
	}
	base := strings.TrimSuffix(name, ".json")
	parts := strings.Split(base, "__")
	var e Entry
	switch len(parts) {
	case 4: // source, endpoint, symbol, stamp
		e.Source, e.Endpoint, e.Symbol = parts[0], parts[1], parts[2]
	case 3: // source, endpoint, stamp (symbol-less)
		e.Source, e.Endpoint = parts[0], parts[1]
	default:
		return Entry{}, false
	}
	stamp := parts[len(parts)-1]
	when, err := time.ParseInLocation(stampLayout, stamp, time.Local)
	if err != nil {
		return Entry{}, false
	}
	e.When = when
	e.Name = name
	return e, true
}

// ListFilter narrows a List by case-insensitive substring match on each set
// field. Empty fields match everything.
type ListFilter struct {
	Source   string
	Endpoint string
	Symbol   string
}

func (f ListFilter) matches(e Entry) bool {
	return containsFold(e.Source, f.Source) &&
		containsFold(e.Endpoint, f.Endpoint) &&
		containsFold(e.Symbol, f.Symbol)
}

// containsFold reports whether needle is a case-insensitive substring of hay. An
// empty needle always matches.
func containsFold(hay, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(hay), strings.ToLower(needle))
}

// List returns the archived entries matching filter, newest-first (ties broken
// by descending name, which is stable and deterministic). Size and When come
// from the on-disk file (When from the filename stamp, falling back to mtime if
// the stamp is unparseable — which ParseFilename already rejects, so in practice
// When is always the stamp). A nil Store or a missing raw dir yields (nil, nil):
// "nothing captured" is not an error.
func (s *Store) List(filter ListFilter) ([]Entry, error) {
	if s == nil {
		return nil, nil
	}
	dirEntries, err := os.ReadDir(s.rawDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		if de.IsDir() {
			continue
		}
		e, ok := ParseFilename(de.Name())
		if !ok {
			continue // foreign / non-conforming file
		}
		if !filter.matches(e) {
			continue
		}
		if info, err := de.Info(); err == nil {
			e.Size = info.Size()
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].When.Equal(out[j].When) {
			return out[i].When.After(out[j].When)
		}
		return out[i].Name > out[j].Name
	})
	return out, nil
}

// ResolvePath returns the absolute path to the newest archived file matching
// query, and true on a hit. query is a case-insensitive substring matched
// against the symbol first, then — if nothing matches by symbol — against the
// whole filename, so both `raw show AAPL` and `raw show schwab__quotes` work. An
// empty query resolves to the single newest capture of any kind. A nil Store or
// no match yields ("", false).
func (s *Store) ResolvePath(query string) (string, bool) {
	if s == nil {
		return "", false
	}
	all, err := s.List(ListFilter{})
	if err != nil || len(all) == 0 {
		return "", false
	}
	if query == "" {
		return filepath.Join(s.rawDir, all[0].Name), true
	}
	// Prefer a symbol match (the common case: `raw show AAPL`).
	for _, e := range all {
		if containsFold(e.Symbol, query) {
			return filepath.Join(s.rawDir, e.Name), true
		}
	}
	// Fall back to any filename-field match.
	for _, e := range all {
		if containsFold(e.Name, query) {
			return filepath.Join(s.rawDir, e.Name), true
		}
	}
	return "", false
}

// RawDir returns the archive directory (<base>/raw). A nil Store returns "".
func (s *Store) RawDir() string {
	if s == nil {
		return ""
	}
	return s.rawDir
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
