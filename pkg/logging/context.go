package logging

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"log/slog"
)

// contextKey is an unexported type to avoid context key collisions.
type contextKey struct{}

var reqIDKey = contextKey{}

// txCounter provides monotonically increasing sub-operation IDs within a process.
var txCounter atomic.Int64

// WithReqID returns a copy of ctx carrying the given request ID.
func WithReqID(ctx context.Context, reqID string) context.Context {
	return context.WithValue(ctx, reqIDKey, reqID)
}

// ReqID returns the request ID stored in ctx, or "" if none.
func ReqID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(reqIDKey).(string); ok {
		return v
	}
	return ""
}

// GenerateReqID builds a request ID from a command name and the current time,
// e.g. "pick-153004". A blank command becomes "mycase".
func GenerateReqID(command string) string {
	if command == "" {
		command = "mycase"
	}
	return fmt.Sprintf("%s-%s", command, time.Now().Format("150405"))
}

// NextTxID returns a short sequential sub-operation ID, e.g. "tx-0001".
func NextTxID() string {
	return fmt.Sprintf("tx-%04d", txCounter.Add(1))
}

// With returns a child logger pre-populated with the req_id from ctx.
// If ctx carries no req_id, the original logger is returned unchanged.
func With(logger *slog.Logger, ctx context.Context) *slog.Logger {
	reqID := ReqID(ctx)
	if reqID == "" {
		return logger
	}
	return logger.With("req_id", reqID)
}
