package rawstore_test

// Integration coverage for the composition-root path: the rawcapture leaf
// (default-on policy) wired to a real rawstore.Store, proving that a normal run
// archives to <data>/raw and that the MYCASE_CAPTURE=0 opt-out archives nothing.
//
// rawcapture caches its Enabled()/ReplayEnabled() decision in a sync.Once, so a
// process decides the capture policy exactly once. These tests therefore set the
// env before the first Capture call and run as independent cases; the Makefile
// runs the whole package in one `go test` process, so we assert the policy that
// the shared cached decision produces (default-on) and cover the opt-out via the
// unit tests in package rawcapture. Here we focus on the on-disk effect.

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/raghavkgarg/mycase/pkg/rawcapture"
	"github.com/raghavkgarg/mycase/pkg/rawstore"
)

// With MYCASE_CAPTURE unset (default) and a real Store wired, a 2xx body handed
// to rawcapture.Capture lands as a file under <base>/raw — the exact behavior a
// normal `mycase` run gets from main's Before hook.
func TestIntegration_DefaultOn_ArchivesToDisk(t *testing.T) {
	base := t.TempDir()
	t.Setenv("MYCASE_CAPTURE", "")
	t.Setenv("MYCASE_REPLAY", "")

	store := rawstore.New(base, "req-int-1")
	rawcapture.SetSink(store)
	t.Cleanup(func() { rawcapture.SetSink(nil) })

	if !rawcapture.Enabled() {
		t.Fatal("capture should be ON by default")
	}

	const payload = `{"marketCap":123}`
	got := rawcapture.Capture("schwab", "instruments", "AAPL", io.NopCloser(strings.NewReader(payload)))

	// Caller's body is intact.
	body, err := io.ReadAll(got)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != payload {
		t.Errorf("body = %q, want %q", body, payload)
	}

	// A file landed under <base>/raw with the expected content.
	rawDir := base + string(os.PathSeparator) + "raw"
	entries, err := os.ReadDir(rawDir)
	if err != nil {
		t.Fatalf("read raw dir (expected default-on to create it): %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 archived file, got %d", len(entries))
	}
	name := entries[0].Name()
	if !strings.HasPrefix(name, "schwab__instruments__AAPL__") || !strings.HasSuffix(name, ".json") {
		t.Errorf("unexpected archive filename: %q", name)
	}
	archived, err := os.ReadFile(rawDir + string(os.PathSeparator) + name)
	if err != nil {
		t.Fatalf("read archived file: %v", err)
	}
	if string(archived) != payload {
		t.Errorf("archived = %q, want %q", archived, payload)
	}
}
