package zerodha

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnrichIPError_Nil(t *testing.T) {
	if err := enrichIPError(nil); err != nil {
		t.Errorf("expected nil for nil input, got %v", err)
	}
}

func TestEnrichIPError_WithIP(t *testing.T) {
	rawErr := errors.New("API error: IP (203.0.113.195) is not allowed to access this resource")
	enriched := enrichIPError(rawErr)
	if enriched == nil {
		t.Fatal("expected enriched error, got nil")
	}
	if !strings.Contains(enriched.Error(), "203.0.113.195") {
		t.Errorf("expected IP in error message, got %v", enriched)
	}
	if !strings.Contains(enriched.Error(), "[ACTION REQUIRED]") {
		t.Errorf("expected [ACTION REQUIRED] in message, got %v", enriched)
	}
}

func TestEnrichIPError_UnrelatedError(t *testing.T) {
	rawErr := errors.New("timeout connecting to server")
	enriched := enrichIPError(rawErr)
	if enriched == nil {
		t.Fatal("expected non-nil error")
	}
	if enriched.Error() != rawErr.Error() {
		t.Errorf("expected identical error string, got %v", enriched)
	}
}

func TestNew_MockMode(t *testing.T) {
	b := New(false, "config/config.json")
	if !b.IsMock() {
		t.Errorf("expected mock broker when liveMode is false")
	}
}

func TestNew_MissingConfigFile(t *testing.T) {
	b := New(true, "/nonexistent/path/config.json")
	if !b.IsMock() {
		t.Errorf("expected fallback to mock broker when config is missing")
	}
}

func TestNew_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")
	cfgContent := `{"api_key": "test_api_key", "access_token": "test_access_token"}`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	b := New(true, cfgPath)
	if b.IsMock() {
		t.Errorf("expected live ZerodhaBroker, got mock")
	}
}
