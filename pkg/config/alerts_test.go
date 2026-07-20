package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAlertConfig_MissingFile(t *testing.T) {
	cfg, err := LoadAlertConfig("/nonexistent/path/pipeline.yaml")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if cfg.DriftThreshold != 0.05 {
		t.Errorf("DriftThreshold = %.4f, want 0.05", cfg.DriftThreshold)
	}
}

func TestLoadAlertConfig_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.yaml")
	yaml := `
alerts:
  drift_threshold: 0.08
  portfolio_file: data/myall.csv
  channels: [telegram, discord]
  telegram_bot_token: tok123
  telegram_chat_id: "9876"
  discord_webhook_url: https://discord.example.com/hook
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadAlertConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DriftThreshold != 0.08 {
		t.Errorf("DriftThreshold = %.4f, want 0.08", cfg.DriftThreshold)
	}
	if cfg.PortfolioFile != "data/myall.csv" {
		t.Errorf("PortfolioFile = %q", cfg.PortfolioFile)
	}
	if len(cfg.Channels) != 2 || cfg.Channels[0] != "telegram" || cfg.Channels[1] != "discord" {
		t.Errorf("Channels = %v", cfg.Channels)
	}
	if cfg.TelegramBotToken != "tok123" {
		t.Errorf("TelegramBotToken = %q", cfg.TelegramBotToken)
	}
	if cfg.TelegramChatID != "9876" {
		t.Errorf("TelegramChatID = %q", cfg.TelegramChatID)
	}
	if cfg.DiscordWebhookURL != "https://discord.example.com/hook" {
		t.Errorf("DiscordWebhookURL = %q", cfg.DiscordWebhookURL)
	}
}

func TestLoadAlertConfig_ZeroThresholdDefaulted(t *testing.T) {
	// A zero drift_threshold in YAML should be replaced with the 0.05 default.
	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.yaml")
	yaml := `alerts:
  drift_threshold: 0
  channels: [telegram]
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := LoadAlertConfig(path)
	if cfg.DriftThreshold != 0.05 {
		t.Errorf("DriftThreshold = %.4f, want 0.05 (zero should be defaulted)", cfg.DriftThreshold)
	}
}

func TestLoadAlertConfig_NoAlertsSection(t *testing.T) {
	// A valid pipeline.yaml with no alerts: key returns defaults.
	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.yaml")
	yaml := `indices: [nifty50]
strategy: balanced
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadAlertConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DriftThreshold != 0.05 {
		t.Errorf("DriftThreshold = %.4f, want 0.05", cfg.DriftThreshold)
	}
	if len(cfg.Channels) != 0 {
		t.Errorf("Channels should be empty, got %v", cfg.Channels)
	}
}

func TestLoadAlertConfig_MalformedYAML(t *testing.T) {
	// Malformed YAML returns defaults, not an error.
	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.yaml")
	if err := os.WriteFile(path, []byte("alerts: :::INVALID:::"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadAlertConfig(path)
	if err != nil {
		t.Fatalf("expected nil error on malformed YAML, got %v", err)
	}
	if cfg.DriftThreshold != 0.05 {
		t.Errorf("DriftThreshold = %.4f, want 0.05", cfg.DriftThreshold)
	}
}
