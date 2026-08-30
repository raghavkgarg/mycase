// Package logging provides structured logging for mycase built on log/slog.
//
// Design (see docs/refactor.md R14):
//   - Two channels, never conflated: user output (pkg/render / fmt → stdout) is
//     the product; diagnostic logs go here (slog → stderr text + JSON file).
//   - A fanout handler writes machine-readable JSON to a daily rotating file and
//     human-readable text to stderr, gated independently by level.
//   - Every command invocation gets a req_id (see context.go) so sub-operations
//     (API calls, DB ops, fetch batches) can be traced end-to-end within a run.
//   - Log directory / level / retention are configurable via CLI flags, env, or
//     config/defaults.json. Failures to open a log file degrade gracefully to
//     stderr-only rather than aborting the command.
package logging

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"log/slog"
)

// DefaultDir is the fallback log directory when none is configured.
const DefaultDir = "data/logs"

// DefaultRetainDays is how long daily log files are kept before CleanOldLogs removes them.
const DefaultRetainDays = 14

// Config controls how the logger is set up.
type Config struct {
	Dir        string // directory for JSON log files (default: DefaultDir)
	Level      string // "debug" | "info" | "warn" | "error" (default: "info")
	File       bool   // write JSON to a daily file (default: true via SetupFile callers)
	Quiet      bool   // suppress stderr output (file-only) — for background/scheduled runs
	RetainDays int    // days to keep log files (default: DefaultRetainDays)
}

// Logger wraps *slog.Logger and owns the underlying log file handle, if any.
type Logger struct {
	*slog.Logger
	file *os.File
}

// Close flushes and closes the underlying log file. Safe to call on a nil-file logger.
func (l *Logger) Close() {
	if l == nil || l.file == nil {
		return
	}
	_ = l.file.Sync()
	_ = l.file.Close()
}

// ParseLevel converts a level string to slog.Level, defaulting to Info on unknown input.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info", "":
		return slog.LevelInfo
	default:
		return slog.LevelInfo
	}
}

// Setup creates a stderr-only text logger. Useful for tests and simple contexts
// where file logging is not wanted.
func Setup(level string) *Logger {
	opts := handlerOpts(ParseLevel(level))
	return &Logger{Logger: slog.New(slog.NewTextHandler(os.Stderr, opts))}
}

// SetupFile creates a logger per the Config. When cfg.File is true it writes JSON
// to a daily file (mycase-YYYY-MM-DD.jsonl) in cfg.Dir and, unless cfg.Quiet, also
// writes human-readable text to stderr. If the log file cannot be opened it falls
// back to stderr-only text (never fatal).
func SetupFile(cfg Config) *Logger {
	if cfg.Dir == "" {
		cfg.Dir = DefaultDir
	}
	level := ParseLevel(cfg.Level)
	opts := handlerOpts(level)

	// No file requested: stderr text (or a discard-ish quiet logger).
	if !cfg.File {
		if cfg.Quiet {
			// Quiet + no file: emit nothing but keep a valid logger.
			return &Logger{Logger: slog.New(slog.NewTextHandler(nopWriter{}, opts))}
		}
		return &Logger{Logger: slog.New(slog.NewTextHandler(os.Stderr, opts))}
	}

	if err := os.MkdirAll(cfg.Dir, 0o750); err != nil {
		// Directory unusable — degrade to stderr-only.
		return &Logger{Logger: slog.New(slog.NewTextHandler(os.Stderr, opts))}
	}

	name := fmt.Sprintf("mycase-%s.jsonl", time.Now().Format("2006-01-02"))
	path := filepath.Join(cfg.Dir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return &Logger{Logger: slog.New(slog.NewTextHandler(os.Stderr, opts))}
	}

	jsonHandler := slog.NewJSONHandler(file, opts)
	var handler slog.Handler = jsonHandler
	if !cfg.Quiet {
		handler = &fanoutHandler{
			file:   jsonHandler,
			stderr: slog.NewTextHandler(os.Stderr, opts),
		}
	}
	return &Logger{Logger: slog.New(handler), file: file}
}

// handlerOpts returns shared slog options: the given level plus an ISO-8601
// timestamp so file and stderr agree on time formatting.
func handlerOpts(level slog.Level) *slog.HandlerOptions {
	return &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
				a.Value = slog.StringValue(a.Value.Time().Format("2006-01-02T15:04:05.000Z07:00"))
			}
			return a
		},
	}
}

// CleanOldLogs removes mycase-*.jsonl files in dir older than retainDays.
// Best-effort: I/O errors are ignored. A retainDays <= 0 is treated as DefaultRetainDays.
func CleanOldLogs(dir string, retainDays int) {
	if dir == "" {
		dir = DefaultDir
	}
	if retainDays <= 0 {
		retainDays = DefaultRetainDays
	}
	cutoff := time.Now().AddDate(0, 0, -retainDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "mycase-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// --- fanout handler: writes each record to both a file handler and stderr ---

type fanoutHandler struct {
	file   slog.Handler
	stderr slog.Handler
}

func (f *fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return f.file.Enabled(ctx, level) || f.stderr.Enabled(ctx, level)
}

func (f *fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	if f.file.Enabled(ctx, r.Level) {
		if err := f.file.Handle(ctx, r.Clone()); err != nil {
			return err
		}
	}
	if f.stderr.Enabled(ctx, r.Level) {
		return f.stderr.Handle(ctx, r.Clone())
	}
	return nil
}

func (f *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &fanoutHandler{file: f.file.WithAttrs(attrs), stderr: f.stderr.WithAttrs(attrs)}
}

func (f *fanoutHandler) WithGroup(name string) slog.Handler {
	return &fanoutHandler{file: f.file.WithGroup(name), stderr: f.stderr.WithGroup(name)}
}

// nopWriter discards all writes (used for quiet + no-file loggers).
type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
