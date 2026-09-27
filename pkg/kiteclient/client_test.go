package kiteclient

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/raghavkgarg/mycase/pkg/config"
)

func TestInitKiteClient_ForceMock(t *testing.T) {
	cfg := &config.Config{
		APIKey:      "real_key",
		AccessToken: "real_token",
	}
	client, isMock := InitKiteClient(cfg, true)
	if !isMock || client != nil {
		t.Errorf("expected isMock=true, client=nil; got isMock=%v, client=%v", isMock, client)
	}
}

func TestInitKiteClient_EmptyCredentials(t *testing.T) {
	cfg := &config.Config{
		APIKey:      "",
		AccessToken: "",
	}
	client, isMock := InitKiteClient(cfg, false)
	if !isMock || client != nil {
		t.Errorf("expected isMock=true for empty credentials")
	}
}

func TestInitKiteClient_PlaceholderCredentials(t *testing.T) {
	cfg := &config.Config{
		APIKey:      "your_api_key",
		AccessToken: "your_access_token",
	}
	client, isMock := InitKiteClient(cfg, false)
	if !isMock || client != nil {
		t.Errorf("expected isMock=true for placeholder credentials")
	}
}

func TestInitKiteClient_ValidCredentials(t *testing.T) {
	cfg := &config.Config{
		APIKey:      "valid_api_key_123",
		AccessToken: "valid_access_token_456",
		HTTPProxy:   "http://127.0.0.1:8080",
	}
	client, isMock := InitKiteClient(cfg, false)
	if isMock || client == nil {
		t.Errorf("expected isMock=false, client!=nil for valid credentials")
	}
}

func TestLoadAndInitClient_MissingFile(t *testing.T) {
	client, isMock := LoadAndInitClient("/nonexistent/config.json", true)
	if !isMock || client != nil {
		t.Errorf("expected isMock=true when config file is missing")
	}
}

func TestLoadAndInitClient_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")
	cfgContent := `{"api_key": "my_api_key", "access_token": "my_access_token"}`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	client, isMock := LoadAndInitClient(cfgPath, true)
	if isMock || client == nil {
		t.Errorf("expected isMock=false, client!=nil with valid config file")
	}
}
