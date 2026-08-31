package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"log/slog"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"DEBUG":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"":        slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"bogus":   slog.LevelInfo,
		" info ":  slog.LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v; want %v", in, got, want)
		}
	}
}

// newTestLogger builds a JSON logger writing to buf at the given level.
func newTestLogger(buf *bytes.Buffer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, handlerOpts(level)))
}

// parseLines decodes each JSON line in buf into a map.
func parseLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("invalid JSON log line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func TestLevelGating(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf, slog.LevelWarn)

	logger.Debug("debug msg")
	logger.Info("info msg")
	logger.Warn("warn msg")
	logger.Error("error msg")

	lines := parseLines(t, &buf)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines at warn level, got %d: %s", len(lines), buf.String())
	}
	if lines[0]["msg"] != "warn msg" || lines[1]["msg"] != "error msg" {
		t.Errorf("unexpected messages: %+v", lines)
	}
}

func TestJSONShapeAndTimestamp(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf, slog.LevelInfo)
	logger.Info("fetched quotes", "count", 47, "ms", 320)

	lines := parseLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	m := lines[0]
	if m["msg"] != "fetched quotes" {
		t.Errorf("msg = %v", m["msg"])
	}
	if m["level"] != "INFO" {
		t.Errorf("level = %v", m["level"])
	}
	// count comes back as float64 via JSON.
	if m["count"] != float64(47) {
		t.Errorf("count = %v (%T)", m["count"], m["count"])
	}
	// Timestamp must be ISO-8601 (RFC3339-ish), parseable.
	ts, ok := m["time"].(string)
	if !ok {
		t.Fatalf("time missing or not string: %v", m["time"])
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z07:00", ts); err != nil {
		t.Errorf("time %q not in expected format: %v", ts, err)
	}
}

func TestReqIDPropagation(t *testing.T) {
	var buf bytes.Buffer
	base := newTestLogger(&buf, slog.LevelInfo)

	ctx := WithReqID(context.Background(), "pick-153004")
	logger := With(base, ctx)
	logger.Info("stage started")

	lines := parseLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if lines[0]["req_id"] != "pick-153004" {
		t.Errorf("req_id = %v; want pick-153004", lines[0]["req_id"])
	}
}

func TestWith_NoReqID_ReturnsSameLogger(t *testing.T) {
	var buf bytes.Buffer
	base := newTestLogger(&buf, slog.LevelInfo)
	if got := With(base, context.Background()); got != base {
		t.Error("With should return the original logger when ctx has no req_id")
	}
}

func TestReqID_Empty(t *testing.T) {
	if ReqID(context.Background()) != "" {
		t.Error("ReqID on bare context should be empty")
	}
	// Guard: ReqID must tolerate a nil context. Use a variable so staticcheck
	// doesn't flag a nil-context literal (SA1012) — the nil path is the point.
	var nilCtx context.Context
	if ReqID(nilCtx) != "" {
		t.Error("ReqID(nil) should be empty")
	}
}

func TestGenerateReqID(t *testing.T) {
	id := GenerateReqID("pick")
	if !strings.HasPrefix(id, "pick-") {
		t.Errorf("id %q should start with pick-", id)
	}
	if GenerateReqID("") == "" || !strings.HasPrefix(GenerateReqID(""), "mycase-") {
		t.Error("blank command should default to mycase-")
	}
}

func TestNextTxID_Sequential(t *testing.T) {
	a := NextTxID()
	b := NextTxID()
	if a == b {
		t.Errorf("tx ids should differ: %s == %s", a, b)
	}
	if !strings.HasPrefix(a, "tx-") {
		t.Errorf("tx id %q malformed", a)
	}
}

func TestHelpers_Timer_LogResponse_LogDBOp(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf, slog.LevelDebug)
	ctx := WithReqID(context.Background(), "req-1")

	Timer(ctx, logger, "op.name", "k", "v")()
	LogRequest(ctx, logger, "GET", "https://api.example.com/x")
	LogResponse(ctx, logger, "GET", "https://api.example.com/x", 503, 12*time.Millisecond)
	LogResponse(ctx, logger, "GET", "https://api.example.com/x", 404, 5*time.Millisecond)
	LogResponse(ctx, logger, "GET", "https://api.example.com/x", 200, 8*time.Millisecond)
	LogDBOp(ctx, logger, "insert", "prices", 3*time.Millisecond, 10)

	lines := parseLines(t, &buf)
	byMsg := map[string]map[string]any{}
	var responses []map[string]any
	for _, l := range lines {
		if l["msg"] == "http.response" {
			responses = append(responses, l)
			continue
		}
		byMsg[l["msg"].(string)] = l
	}

	// Timer: has duration_ms and req_id.
	if tm := byMsg["op.name"]; tm == nil || tm["duration_ms"] == nil || tm["req_id"] != "req-1" {
		t.Errorf("Timer line malformed: %+v", byMsg["op.name"])
	}
	// LogRequest is Debug level.
	if rq := byMsg["http.request"]; rq == nil || rq["level"] != "DEBUG" {
		t.Errorf("http.request should be DEBUG: %+v", byMsg["http.request"])
	}
	// Response level mapping: 503→ERROR, 404→WARN, 200→INFO.
	wantLevels := []string{"ERROR", "WARN", "INFO"}
	if len(responses) != 3 {
		t.Fatalf("expected 3 http.response lines, got %d", len(responses))
	}
	for i, want := range wantLevels {
		if responses[i]["level"] != want {
			t.Errorf("response[%d] level = %v; want %v", i, responses[i]["level"], want)
		}
	}
	// LogDBOp rows.
	if db := byMsg["db.op"]; db == nil || db["rows"] != float64(10) || db["table"] != "prices" {
		t.Errorf("db.op line malformed: %+v", byMsg["db.op"])
	}
}

func TestToAttrs_DanglingKeyDropped(t *testing.T) {
	attrs := toAttrs([]any{"a", 1, "dangling"})
	if len(attrs) != 1 {
		t.Fatalf("expected 1 attr (dangling dropped), got %d", len(attrs))
	}
	if attrs[0].Key != "a" {
		t.Errorf("attr key = %q", attrs[0].Key)
	}
}

func TestTruncateURL(t *testing.T) {
	short := "https://x.com/a"
	if truncateURL(short) != short {
		t.Error("short URL should be unchanged")
	}
	long := "https://api.example.com/path?" + strings.Repeat("q", 200)
	got := truncateURL(long)
	if len(got) > 130 || !strings.HasSuffix(got, "?...") {
		t.Errorf("query-truncation failed: %q (len %d)", got, len(got))
	}
	longNoQuery := "https://api.example.com/" + strings.Repeat("p", 200)
	got = truncateURL(longNoQuery)
	if len(got) != 120 || !strings.HasSuffix(got, "...") {
		t.Errorf("plain truncation failed: len %d", len(got))
	}
}

func TestSetupFile_WritesJSONLAndCleanStderr(t *testing.T) {
	dir := t.TempDir()
	l := SetupFile(Config{Dir: dir, Level: "info", File: true, Quiet: true})
	defer l.Close()

	l.Info("hello", "n", 1)
	l.Close() // flush

	name := "mycase-" + time.Now().Format("2006-01-02") + ".jsonl"
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	if !strings.Contains(string(data), `"msg":"hello"`) {
		t.Errorf("log file missing entry: %s", data)
	}
}

func TestSetupFile_GracefulDegradeOnBadDir(t *testing.T) {
	// Point Dir at a path that cannot be a directory (a regular file).
	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	badDir := filepath.Join(f, "sub") // under a file → MkdirAll fails
	l := SetupFile(Config{Dir: badDir, Level: "info", File: true, Quiet: true})
	defer l.Close()
	if l == nil || l.Logger == nil {
		t.Fatal("SetupFile should never return a nil logger")
	}
	if l.file != nil {
		t.Error("expected no file handle on degraded logger")
	}
	// Must not panic.
	l.Info("still works")
}

func TestFanout_FileGetsAllStderrGetsLevel(t *testing.T) {
	var fileBuf, errBuf bytes.Buffer
	h := &fanoutHandler{
		file:   slog.NewJSONHandler(&fileBuf, handlerOpts(slog.LevelDebug)),
		stderr: slog.NewTextHandler(&errBuf, handlerOpts(slog.LevelWarn)),
	}
	logger := slog.New(h)
	logger.Debug("dbg")
	logger.Warn("wrn")

	fileLines := parseLines(t, &fileBuf)
	if len(fileLines) != 2 {
		t.Errorf("file should get both debug+warn, got %d", len(fileLines))
	}
	// stderr (text) gated at warn: should contain wrn, not dbg.
	es := errBuf.String()
	if strings.Contains(es, "dbg") {
		t.Error("stderr should not contain debug message at warn gate")
	}
	if !strings.Contains(es, "wrn") {
		t.Error("stderr should contain warn message")
	}
}

func TestCleanOldLogs(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "mycase-2000-01-01.jsonl")
	recent := filepath.Join(dir, "mycase-"+time.Now().Format("2006-01-02")+".jsonl")
	other := filepath.Join(dir, "keepme.txt")
	for _, p := range []string{old, recent, other} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Backdate the old file well beyond retention.
	past := time.Now().AddDate(0, 0, -30)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	CleanOldLogs(dir, 14)

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("old log should have been removed")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Error("recent log should be kept")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("non-mycase file should be untouched")
	}
}

func TestCleanOldLogs_MissingDir(t *testing.T) {
	// Should not panic on a nonexistent directory.
	CleanOldLogs(filepath.Join(t.TempDir(), "does-not-exist"), 14)
}

func TestSetup_StderrOnly(t *testing.T) {
	l := Setup("debug")
	if l == nil || l.Logger == nil {
		t.Fatal("Setup returned nil logger")
	}
	if l.file != nil {
		t.Error("Setup should not open a file")
	}
	l.Info("no panic")
	l.Close() // no-op on nil file
}

func TestSetupFile_QuietNoFile_Discards(t *testing.T) {
	// File=false + Quiet=true → nopWriter path, emits nothing, never panics.
	l := SetupFile(Config{File: false, Quiet: true, Level: "info"})
	defer l.Close()
	l.Info("swallowed")
}

func TestSetupFile_NoFileNotQuiet_Stderr(t *testing.T) {
	l := SetupFile(Config{File: false, Quiet: false, Level: "info"})
	defer l.Close()
	if l.file != nil {
		t.Error("no file expected")
	}
	l.Info("to stderr")
}

func TestFanout_WithAttrsAndGroup(t *testing.T) {
	var fileBuf, errBuf bytes.Buffer
	h := &fanoutHandler{
		file:   slog.NewJSONHandler(&fileBuf, handlerOpts(slog.LevelInfo)),
		stderr: slog.NewTextHandler(&errBuf, handlerOpts(slog.LevelInfo)),
	}
	// WithAttrs then WithGroup must propagate to both underlying handlers.
	logger := slog.New(h).With("svc", "mycase").WithGroup("g")
	logger.Info("hello", "n", 1)

	lines := parseLines(t, &fileBuf)
	if len(lines) != 1 {
		t.Fatalf("expected 1 file line, got %d", len(lines))
	}
	if lines[0]["svc"] != "mycase" {
		t.Errorf("WithAttrs not propagated to file: %+v", lines[0])
	}
	grp, ok := lines[0]["g"].(map[string]any)
	if !ok || grp["n"] != float64(1) {
		t.Errorf("WithGroup not propagated to file: %+v", lines[0])
	}
	if !strings.Contains(errBuf.String(), "svc=mycase") {
		t.Errorf("WithAttrs not propagated to stderr: %s", errBuf.String())
	}
}

func TestNopWriter(t *testing.T) {
	n, err := nopWriter{}.Write([]byte("abc"))
	if n != 3 || err != nil {
		t.Errorf("nopWriter.Write = (%d, %v)", n, err)
	}
}
