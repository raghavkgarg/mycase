package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/raghavkgarg/mycase/pkg/alert"
	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
)

const (
	PIDFile   = "data/daemon.pid"
	StateFile = "data/daemon_state.json"
)

// State is persisted across daemon restarts.
type State struct {
	LastCheckAt   time.Time `json:"last_check_at"`
	LastDrift     float64   `json:"last_drift"`
	AlertsSent    int       `json:"alerts_sent"`
	PortfolioFile string    `json:"portfolio_file"`
}

// LoadState reads the last persisted daemon state. Returns empty State (not an error)
// when no state file exists yet.
func LoadState() (State, error) {
	data, err := os.ReadFile(StateFile)
	if err != nil {
		return State{}, nil
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, err
	}
	return s, nil
}

func saveState(s State) error {
	_ = os.MkdirAll("data", 0755)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(StateFile, data, 0644)
}

// RunCheck loads the portfolio, computes drift, sends alerts if the threshold is exceeded,
// and persists state. Safe to call from a loop or one-shot.
func RunCheck(ctx context.Context, b broker.Broker, cfg config.AlertConfig, portfolioFile string) (DriftResult, error) {
	targetWeights, basketKeys, err := csvloader.LoadBasketCSV(portfolioFile)
	if err != nil {
		return DriftResult{}, fmt.Errorf("loading portfolio %s: %w", portfolioFile, err)
	}

	result, err := CalculateDrift(ctx, b, targetWeights, basketKeys)
	if err != nil {
		return DriftResult{}, err
	}

	state, _ := LoadState()
	state.LastCheckAt = result.CheckedAt
	state.LastDrift = result.DriftIndex
	state.PortfolioFile = portfolioFile

	if result.DriftIndex > cfg.DriftThreshold {
		msg := alert.Alert{
			Title: fmt.Sprintf("Portfolio drift %.1f%%", result.DriftIndex*100),
			Body: fmt.Sprintf("Drift index %.4f exceeds threshold %.4f\nPortfolio: %s\nTotal value: ₹%.0f",
				result.DriftIndex, cfg.DriftThreshold, portfolioFile, result.TotalValue),
			Level: "warn",
		}
		for _, a := range buildAlerters(cfg) {
			if err := a.Send(msg); err != nil {
				fmt.Fprintf(os.Stderr, "alert error: %v\n", err)
			}
		}
		state.AlertsSent++
	}

	_ = saveState(state)
	return result, nil
}

// RunLoop blocks, running RunCheck at 15:45 IST each day until ctx is cancelled.
func RunLoop(ctx context.Context, b broker.Broker, cfg config.AlertConfig, portfolioFile string) error {
	if err := writePID(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write PID file: %v\n", err)
	}
	defer os.Remove(PIDFile)

	for {
		next := nextIST1545()
		fmt.Printf("Next drift check at %s\n", next.Local().Format("2006-01-02 15:04:05 MST"))

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Until(next)):
		}

		result, err := RunCheck(ctx, b, cfg, portfolioFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "drift check error: %v\n", err)
			continue
		}
		level := "OK"
		if result.DriftIndex > cfg.DriftThreshold {
			level = "DRIFT"
		}
		fmt.Printf("[%s] %s drift=%.4f threshold=%.4f value=₹%.0f\n",
			result.CheckedAt.Format("2006-01-02 15:04:05"),
			level, result.DriftIndex, cfg.DriftThreshold, result.TotalValue)
	}
}

// nextIST1545 returns the next 15:45 IST as a UTC time.
func nextIST1545() time.Time {
	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		// Fallback: IST is UTC+5:30
		ist = time.FixedZone("IST", 5*60*60+30*60)
	}
	now := time.Now().In(ist)
	target := time.Date(now.Year(), now.Month(), now.Day(), 15, 45, 0, 0, ist)
	if !now.Before(target) {
		target = target.Add(24 * time.Hour)
	}
	return target
}

func writePID() error {
	_ = os.MkdirAll("data", 0755)
	return os.WriteFile(PIDFile, fmt.Appendf(nil, "%d\n", os.Getpid()), 0644)
}

func buildAlerters(cfg config.AlertConfig) []alert.Alerter {
	var result []alert.Alerter
	for _, ch := range cfg.Channels {
		switch ch {
		case "telegram":
			tok := os.Getenv("MYCASE_TELEGRAM_TOKEN")
			if tok == "" {
				tok = cfg.TelegramBotToken
			}
			if tok != "" && cfg.TelegramChatID != "" {
				result = append(result, &alert.TelegramAlerter{BotToken: tok, ChatID: cfg.TelegramChatID})
			}
		case "discord":
			u := os.Getenv("MYCASE_DISCORD_WEBHOOK")
			if u == "" {
				u = cfg.DiscordWebhookURL
			}
			if u != "" {
				result = append(result, &alert.DiscordAlerter{WebhookURL: u})
			}
		}
	}
	return result
}
