package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMFSConfig_MissingFile(t *testing.T) {
	cfg, err := LoadMFSConfig("/nonexistent/path/mfs.json", "balanced")
	if err != nil {
		t.Fatalf("expected no error for missing file (graceful fallback), got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected default config, got nil")
	}
	// Default should have non-zero Sharpe and Sortino weights
	if cfg.Sharpe == 0 || cfg.Sortino == 0 {
		t.Errorf("default config has zero Sharpe/Sortino: %+v", cfg)
	}
}

func TestLoadMFSConfig_UnknownStrategy(t *testing.T) {
	// Write a valid mfs.json with only "balanced" strategy.
	dir := t.TempDir()
	path := filepath.Join(dir, "mfs.json")
	data := MFSStrategies{
		Strategies: map[string]MFSConfig{
			"balanced": {Sharpe: 0.20, Sortino: 0.20},
		},
	}
	b, _ := json.Marshal(data)
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadMFSConfig(path, "nonexistent_strategy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fall back to defaults, not nil
	if cfg == nil {
		t.Fatal("expected default config, got nil")
	}
}

func TestLoadMFSConfig_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mfs.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadMFSConfig(path, "balanced")
	if err == nil {
		t.Error("expected error for malformed JSON, got nil")
	}
}

func TestLoadMFSConfig_ValidStrategy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mfs.json")
	data := MFSStrategies{
		Strategies: map[string]MFSConfig{
			"aggressive": {Sharpe: 0.30, Sortino: 0.25, Return: 0.20},
		},
	}
	b, _ := json.Marshal(data)
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadMFSConfig(path, "aggressive")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Sharpe != 0.30 {
		t.Errorf("expected Sharpe=0.30, got %.2f", cfg.Sharpe)
	}
}

func TestLoadThemes_MissingFile(t *testing.T) {
	// Missing file should return hardcoded default themes, not an error.
	themes, err := LoadThemes("/nonexistent/themes.json")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(themes) == 0 {
		t.Error("expected at least one default theme, got none")
	}
}

func TestLoadThemes_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "themes.json")
	if err := os.WriteFile(path, []byte("[{bad json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadThemes(path)
	if err == nil {
		t.Error("expected error for malformed JSON, got nil")
	}
}

func TestLoadThemes_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "themes.json")
	themes := []ThemeConfig{
		{Name: "Tech", Prefix: "T", CSVPath: "data/tech.csv"},
		{Name: "Finance", Prefix: "F", CSVPath: "data/finance.csv"},
	}
	b, _ := json.Marshal(themes)
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadThemes(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 themes, got %d", len(got))
	}
	if got[0].Name != "Tech" {
		t.Errorf("expected first theme Name=Tech, got %s", got[0].Name)
	}
}

func TestSaveAndLoadConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := &Config{APIKey: "key123", APISecret: "secret456", AccessToken: "token789"}

	if err := SaveConfig(path, original); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.APIKey != original.APIKey || loaded.AccessToken != original.AccessToken {
		t.Errorf("round-trip mismatch: got %+v, want %+v", loaded, original)
	}
}

func TestLoadConfig_HTTPProxy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := &Config{APIKey: "key123", APISecret: "secret456", AccessToken: "token789", HTTPProxy: "http://proxy.example.com:8080"}

	if err := SaveConfig(path, original); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.HTTPProxy != "http://proxy.example.com:8080" {
		t.Errorf("expected HTTPProxy=http://proxy.example.com:8080, got %s", loaded.HTTPProxy)
	}
	if os.Getenv("HTTP_PROXY") != "http://proxy.example.com:8080" {
		t.Errorf("expected HTTP_PROXY env var to be set, got %s", os.Getenv("HTTP_PROXY"))
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := LoadConfig("/nonexistent/config.json")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
