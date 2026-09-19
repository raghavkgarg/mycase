package edgar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/raghavkgarg/mycase/pkg/cache"
)

const testUA = "mycase-test/1.0 test@example.com"

func TestValidateUserAgent(t *testing.T) {
	tests := []struct {
		name    string
		ua      string
		wantErr bool
	}{
		{"empty", "", true},
		{"placeholder", "mycase/1.0 (set-your-contact@example.com)", true},
		{"no contact", "mycase/1.0", true},
		{"with email", "mycase/1.0 me@example.com", false},
		{"with url", "mycase/1.0 https://example.com/bot", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUserAgent(tt.ua)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateUserAgent(%q) err=%v, wantErr=%v", tt.ua, err, tt.wantErr)
			}
		})
	}
}

func TestNewClient_RejectsBadUA(t *testing.T) {
	if _, err := NewClient("", nil); err == nil {
		t.Error("want error for empty UA, got nil")
	}
}

func TestStripUSPrefix(t *testing.T) {
	cases := map[string]string{
		"US:AAPL":     "AAPL",
		"NYSE:IBM":    "IBM",
		"NASDAQ:MSFT": "MSFT",
		"AAPL":        "AAPL",
		"TCS.NS":      "TCS.NS",
	}
	for in, want := range cases {
		if got := stripUSPrefix(in); got != want {
			t.Errorf("stripUSPrefix(%q)=%q, want %q", in, got, want)
		}
	}
}

// newTestServer serves the company_tickers.json and companyfacts fixtures, and
// asserts that every request carries the mandatory User-Agent.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	facts, err := os.ReadFile(filepath.Join("testdata", "companyfacts_TESTCO.json"))
	if err != nil {
		t.Fatalf("read facts fixture: %v", err)
	}
	const tickersJSON = `{
		"0": {"cik_str": 111111, "ticker": "TESTCO", "title": "TESTCO INC"},
		"1": {"cik_str": 320193, "ticker": "AAPL", "title": "Apple Inc."}
	}`

	mux := http.NewServeMux()
	mux.HandleFunc("/files/company_tickers.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != testUA {
			t.Errorf("missing/wrong User-Agent on %s: %q", r.URL.Path, r.Header.Get("User-Agent"))
		}
		w.Write([]byte(tickersJSON))
	})
	// CIK 0000111111 → facts; anything else → 404 (unknown/no-facts).
	mux.HandleFunc("/api/xbrl/companyfacts/CIK0000111111.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != testUA {
			t.Errorf("missing/wrong User-Agent on %s", r.URL.Path)
		}
		w.Write(facts)
	})
	mux.HandleFunc("/api/xbrl/companyfacts/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	return httptest.NewServer(mux)
}

func newTestClient(t *testing.T, srv *httptest.Server, c *cache.Cache) *Client {
	t.Helper()
	cl, err := NewClient(testUA, c)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	cl.SetBaseURLs(srv.URL, srv.URL)
	return cl
}

func TestCIKLookup(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	cl := newTestClient(t, srv, nil) // nil cache → network path

	cik, ok, err := cl.CIK(context.Background(), "US:TESTCO")
	if err != nil {
		t.Fatalf("CIK: %v", err)
	}
	if !ok || cik != "0000111111" {
		t.Errorf("CIK(US:TESTCO)=%q ok=%v, want 0000111111 true", cik, ok)
	}

	// Unknown ticker → (·, false, nil), not an error.
	_, ok, err = cl.CIK(context.Background(), "US:NOPE")
	if err != nil {
		t.Fatalf("CIK unknown: unexpected err %v", err)
	}
	if ok {
		t.Error("CIK(US:NOPE): want ok=false for unknown ticker")
	}
}

func TestCIKOverride(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	cl := newTestClient(t, srv, nil) // nil cache → network path

	// XOM is absent from the test ticker file, so without the override it would
	// resolve to (·, false). The cikOverrides entry must win and short-circuit to
	// the real filer CIK 34088 → "0000034088", never touching the map/network.
	cik, ok, err := cl.CIK(context.Background(), "US:XOM")
	if err != nil {
		t.Fatalf("CIK(XOM): unexpected err %v", err)
	}
	if !ok || cik != "0000034088" {
		t.Errorf("CIK(US:XOM)=%q ok=%v, want 0000034088 true (override)", cik, ok)
	}
}

func TestFetchFundamentals_EndToEnd(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	cl := newTestClient(t, srv, nil)

	// TESTCO resolves + has facts; NOPE resolves to no CIK → skipped gracefully.
	res, err := cl.FetchFundamentals(context.Background(), []string{"US:TESTCO", "US:NOPE"})
	if err != nil {
		t.Fatalf("FetchFundamentals: %v", err)
	}
	if _, ok := res["US:NOPE"]; ok {
		t.Error("unknown ticker should be absent from result")
	}
	f, ok := res["US:TESTCO"]
	if !ok {
		t.Fatal("US:TESTCO missing from result")
	}
	if f.FreeCashflow != 700 {
		t.Errorf("merged TESTCO FreeCashflow: want 700, got %v", f.FreeCashflow)
	}
	if len(f.AnnualRevenue) != 2 {
		t.Errorf("merged TESTCO AnnualRevenue: want 2, got %d", len(f.AnnualRevenue))
	}
}

func TestFetchFacts_CachesBlob(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	// Use a real DuckDB cache in a temp file to exercise the edgar_facts +
	// edgar_cik_map tables.
	dbPath := filepath.Join(t.TempDir(), "edgar_test.db")
	c, err := cache.Open(dbPath)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	defer c.Close()

	cl := newTestClient(t, srv, c)

	// First lookup populates the CIK map + facts cache.
	if _, _, err := cl.CIK(context.Background(), "TESTCO"); err != nil {
		t.Fatalf("CIK: %v", err)
	}
	facts, ok, err := cl.fetchFacts(context.Background(), "0000111111")
	if err != nil || !ok || facts == nil {
		t.Fatalf("fetchFacts: ok=%v err=%v", ok, err)
	}

	// Second call should be served from cache (raw blob present).
	raw, cached := cl.cachedFacts(context.Background(), "0000111111")
	if !cached || len(raw) == 0 {
		t.Error("expected facts to be cached after first fetch")
	}
}
