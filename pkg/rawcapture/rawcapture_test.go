package rawcapture

import (
	"io"
	"strings"
	"testing"
)

// fakeSink records Write calls and serves a canned body on Open, so the leaf's
// delegation can be tested without any filesystem/store dependency.
type fakeSink struct {
	writes  []capturedWrite
	body    string // served by Open when openHit is true
	openHit bool
	lastKey [3]string // source, endpoint, symbol of the last Open call
}

type capturedWrite struct {
	source, endpoint, symbol string
	body                     string
}

func (f *fakeSink) Write(source, endpoint, symbol string, body []byte) {
	f.writes = append(f.writes, capturedWrite{source, endpoint, symbol, string(body)})
}

func (f *fakeSink) Open(source, endpoint, symbol string) (io.ReadCloser, bool) {
	f.lastKey = [3]string{source, endpoint, symbol}
	if !f.openHit {
		return nil, false
	}
	return io.NopCloser(strings.NewReader(f.body)), true
}

func TestTruthy(t *testing.T) {
	on := []string{"1", "true", "TRUE", "Yes", " on "}
	off := []string{"", "0", "false", "no", "off", "maybe"}
	for _, v := range on {
		if !truthy(v) {
			t.Errorf("truthy(%q) = false, want true", v)
		}
	}
	for _, v := range off {
		if truthy(v) {
			t.Errorf("truthy(%q) = true, want false", v)
		}
	}
}

func TestCapture_Disabled_PassThrough(t *testing.T) {
	t.Setenv(captureEnv, "")
	resetState()
	fs := &fakeSink{}
	SetSink(fs)
	t.Cleanup(func() { SetSink(nil) })

	orig := io.NopCloser(strings.NewReader("hello"))
	got := Capture("yahoo", "quotes", "AAPL", orig)

	body, err := io.ReadAll(got)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "hello" {
		t.Errorf("body = %q, want %q", body, "hello")
	}
	if len(fs.writes) != 0 {
		t.Errorf("expected no Write when capture disabled, got %d", len(fs.writes))
	}
}

func TestCapture_NoSink_PassThrough(t *testing.T) {
	t.Setenv(captureEnv, "1")
	resetState()
	SetSink(nil)

	orig := io.NopCloser(strings.NewReader("hello"))
	got := Capture("yahoo", "quotes", "AAPL", orig)

	// With no sink wired the leaf is inert: the exact same body is returned.
	if got != orig {
		t.Error("expected the original body to be returned unchanged when no sink is wired")
	}
}

func TestCapture_Enabled_WritesAndBodyReadable(t *testing.T) {
	t.Setenv(captureEnv, "1")
	resetState()
	fs := &fakeSink{}
	SetSink(fs)
	t.Cleanup(func() { SetSink(nil) })

	const payload = `{"ok":true}`
	got := Capture("schwab", "instruments", "AAPL", io.NopCloser(strings.NewReader(payload)))

	// Caller can still fully read the body.
	body, err := io.ReadAll(got)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != payload {
		t.Errorf("body = %q, want %q", body, payload)
	}

	// The bytes and key were handed to the sink.
	if len(fs.writes) != 1 {
		t.Fatalf("expected 1 Write, got %d", len(fs.writes))
	}
	w := fs.writes[0]
	if w.source != "schwab" || w.endpoint != "instruments" || w.symbol != "AAPL" || w.body != payload {
		t.Errorf("unexpected Write: %+v", w)
	}
}

func TestCapture_NilBody(t *testing.T) {
	t.Setenv(captureEnv, "1")
	resetState()
	SetSink(&fakeSink{})
	t.Cleanup(func() { SetSink(nil) })
	if got := Capture("yahoo", "quotes", "AAPL", nil); got != nil {
		t.Errorf("Capture(nil) = %v, want nil", got)
	}
}

func TestReplay_Disabled_Miss(t *testing.T) {
	t.Setenv(replayEnv, "")
	resetState()
	SetSink(&fakeSink{body: `{"x":1}`, openHit: true})
	t.Cleanup(func() { SetSink(nil) })

	if _, ok := Replay("schwab", "quotes", "AAPL"); ok {
		t.Error("Replay returned a hit when disabled")
	}
}

func TestReplay_NoSink_Miss(t *testing.T) {
	t.Setenv(replayEnv, "1")
	resetState()
	SetSink(nil)

	if _, ok := Replay("schwab", "quotes", "AAPL"); ok {
		t.Error("Replay returned a hit with no sink wired")
	}
}

func TestReplay_Hit_DelegatesToSink(t *testing.T) {
	t.Setenv(replayEnv, "1")
	resetState()
	const want = `{"symbol":"AAPL"}`
	fs := &fakeSink{body: want, openHit: true}
	SetSink(fs)
	t.Cleanup(func() { SetSink(nil) })

	rc, ok := Replay("schwab", "quotes", "AAPL")
	if !ok {
		t.Fatal("expected replay hit")
	}
	got, _ := io.ReadAll(rc)
	if string(got) != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if fs.lastKey != [3]string{"schwab", "quotes", "AAPL"} {
		t.Errorf("Open called with %v, want [schwab quotes AAPL]", fs.lastKey)
	}
}

func TestReplay_Miss_DelegatesToSink(t *testing.T) {
	t.Setenv(replayEnv, "1")
	resetState()
	fs := &fakeSink{openHit: false}
	SetSink(fs)
	t.Cleanup(func() { SetSink(nil) })

	if _, ok := Replay("schwab", "quotes", "MSFT"); ok {
		t.Error("expected miss when sink reports no hit")
	}
}
