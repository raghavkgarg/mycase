package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// AlertConfig holds drift monitoring and notification configuration.
type AlertConfig struct {
	DriftThreshold    float64  `yaml:"drift_threshold"`
	PortfolioFile     string   `yaml:"portfolio_file"`
	Channels          []string `yaml:"channels"`
	TelegramBotToken  string   `yaml:"telegram_bot_token"`
	TelegramChatID    string   `yaml:"telegram_chat_id"`
	DiscordWebhookURL string   `yaml:"discord_webhook_url"`
}

// LoadAlertConfig reads the alerts: section from a pipeline YAML file.
// Returns safe defaults if the file is missing or the section is absent.
func LoadAlertConfig(pipelinePath string) (AlertConfig, error) {
	defaults := AlertConfig{DriftThreshold: 0.05}
	data, err := os.ReadFile(pipelinePath)
	if err != nil {
		return defaults, nil
	}
	var raw struct {
		Alerts AlertConfig `yaml:"alerts"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return defaults, nil
	}
	cfg := raw.Alerts
	if cfg.DriftThreshold == 0 {
		cfg.DriftThreshold = defaults.DriftThreshold
	}
	return cfg, nil
}
