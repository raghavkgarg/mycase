package logging

import (
	"context"
	"strings"
	"time"

	"log/slog"
)

// Timer returns a function that, when called, logs msg at Info level with the
// elapsed duration attached as "duration_ms". req_id from ctx is included.
//
// Usage:
//
//	defer logging.Timer(ctx, logger, "pick.score", "index", idx)()
func Timer(ctx context.Context, logger *slog.Logger, msg string, args ...any) func() {
	start := time.Now()
	return func() {
		elapsed := time.Since(start)
		all := append(withReqID(ctx, args), "duration_ms", elapsed.Milliseconds())
		logger.LogAttrs(ctx, slog.LevelInfo, msg, toAttrs(all)...)
	}
}

// LogRequest logs an outbound HTTP request at Debug level.
func LogRequest(ctx context.Context, logger *slog.Logger, method, url string) {
	logger.LogAttrs(ctx, slog.LevelDebug, "http.request",
		toAttrs(withReqID(ctx, []any{"method", method, "url", truncateURL(url)}))...)
}

// LogResponse logs an HTTP response with timing. Level scales with status:
// >=500 → Error, >=400 → Warn, else Info.
func LogResponse(ctx context.Context, logger *slog.Logger, method, url string, status int, d time.Duration) {
	level := slog.LevelInfo
	switch {
	case status >= 500:
		level = slog.LevelError
	case status >= 400:
		level = slog.LevelWarn
	}
	logger.LogAttrs(ctx, level, "http.response",
		toAttrs(withReqID(ctx, []any{
			"method", method,
			"url", truncateURL(url),
			"status", status,
			"duration_ms", d.Milliseconds(),
		}))...)
}

// LogDBOp logs a database operation with timing at Debug level.
func LogDBOp(ctx context.Context, logger *slog.Logger, op, table string, d time.Duration, rows int) {
	logger.LogAttrs(ctx, slog.LevelDebug, "db.op",
		toAttrs(withReqID(ctx, []any{
			"op", op,
			"table", table,
			"duration_ms", d.Milliseconds(),
			"rows", rows,
		}))...)
}

// withReqID appends the req_id key/value from ctx to args when present.
func withReqID(ctx context.Context, args []any) []any {
	if id := ReqID(ctx); id != "" {
		return append(args, "req_id", id)
	}
	return args
}

// toAttrs converts a flat key/value arg slice to []slog.Attr, tolerating a
// trailing dangling key by dropping it.
func toAttrs(args []any) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		key, _ := args[i].(string)
		attrs = append(attrs, slog.Any(key, args[i+1]))
	}
	return attrs
}

// truncateURL shortens long URLs for log readability, preferring to cut at the
// query string.
func truncateURL(url string) string {
	const max = 120
	if len(url) <= max {
		return url
	}
	if idx := strings.Index(url, "?"); idx > 0 && idx < 80 {
		return url[:idx] + "?..."
	}
	return url[:max-3] + "..."
}
