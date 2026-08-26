package autopilot

import (
	"fmt"
	"os"
	"strings"

	"github.com/raghavkgarg/mycase/pkg/alert"
	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/config"
)

// FormatProposalAlert builds a rich alert message summarizing the autopilot proposal.
func FormatProposalAlert(p *Proposal) alert.Alert {
	var body strings.Builder

	body.WriteString(fmt.Sprintf("*Portfolio*: %s\n", p.Portfolio))
	body.WriteString(fmt.Sprintf("*Strategy*: %s\n", p.Strategy))
	body.WriteString(fmt.Sprintf("*Date*: %s\n\n", p.CreatedAt.Format("2006-01-02")))

	// Entries
	if len(p.Entries) > 0 {
		body.WriteString("*New entries:*\n")
		for _, e := range p.Entries {
			body.WriteString(fmt.Sprintf("  + %s (wt %.1f%%)", e.Ticker, e.Weight*100))
			if e.Reason != "" {
				body.WriteString(fmt.Sprintf(" — %s", e.Reason))
			}
			body.WriteString("\n")
		}
		body.WriteString("\n")
	}

	// Exits
	if len(p.Exits) > 0 {
		body.WriteString("*Exits:*\n")
		for _, e := range p.Exits {
			body.WriteString(fmt.Sprintf("  − %s", e.Ticker))
			if e.Reason != "" {
				body.WriteString(fmt.Sprintf(" — %s", e.Reason))
			}
			body.WriteString("\n")
		}
		body.WriteString("\n")
	}

	// Order summary
	buyCount, sellCount := 0, 0
	for _, o := range p.Orders {
		if o.Action == "BUY" {
			buyCount++
		} else {
			sellCount++
		}
	}
	currency := broker.LoadMarketConfig().Currency
	body.WriteString(fmt.Sprintf("*Orders*: %d buys, %d sells\n", buyCount, sellCount))
	body.WriteString(fmt.Sprintf("*Buy value*: %s%.0f\n", currency, p.TotalBuyValue))
	body.WriteString(fmt.Sprintf("*Sell value*: %s%.0f\n", currency, p.TotalSellValue))
	body.WriteString(fmt.Sprintf("*Estimated cost*: %s%.0f\n", currency, p.EstimatedCost))

	if len(p.FilteredOut) > 0 {
		body.WriteString(fmt.Sprintf("*Filtered (micro-tx)*: %d orders skipped\n", len(p.FilteredOut)))
	}

	// Tax warnings
	if len(p.TaxWarnings) > 0 {
		body.WriteString("\n*Tax impact:*\n")
		for _, tw := range p.TaxWarnings {
			body.WriteString(fmt.Sprintf("  ⚠ %s\n", tw))
		}
	}

	body.WriteString("\n")
	body.WriteString("👉 Review & confirm: http://localhost:8080/#/rebalance\n")
	body.WriteString(fmt.Sprintf("⏳ Proposal expires: %s\n", p.ExpiresAt.Format("2006-01-02")))

	title := fmt.Sprintf("📊 %s Rebalance Ready", capitalizeFirst(p.Frequency))

	return alert.Alert{
		Title: title,
		Body:  body.String(),
		Level: "info",
	}
}

// FormatConfirmationAlert builds an alert sent after successful order execution.
func FormatConfirmationAlert(p *Proposal) alert.Alert {
	successCount := 0
	failCount := 0
	for _, r := range p.ExecutionLog {
		if r.Success {
			successCount++
		} else {
			failCount++
		}
	}

	var body strings.Builder
	body.WriteString(fmt.Sprintf("*Portfolio*: %s\n", p.Portfolio))
	body.WriteString(fmt.Sprintf("*Orders placed*: %d/%d successful\n", successCount, successCount+failCount))

	if failCount > 0 {
		body.WriteString(fmt.Sprintf("*Failed*: %d orders\n", failCount))
		for _, r := range p.ExecutionLog {
			if !r.Success {
				body.WriteString(fmt.Sprintf("  ✗ %s %s: %s\n", r.Action, r.Ticker, r.Error))
			}
		}
	}

	level := "info"
	title := "✅ Rebalance Executed"
	if failCount > 0 {
		level = "warn"
		title = "⚠️ Rebalance Partial"
	}

	return alert.Alert{
		Title: title,
		Body:  body.String(),
		Level: level,
	}
}

// SendProposalAlerts sends the proposal alert to all configured channels.
func SendProposalAlerts(p *Proposal, cfg config.ScheduleConfig, alertCfg config.AlertConfig) error {
	a := FormatProposalAlert(p)
	return sendToChannels(a, cfg.Notify, alertCfg)
}

// SendConfirmationAlerts sends the confirmation alert to all configured channels.
func SendConfirmationAlerts(p *Proposal, cfg config.ScheduleConfig, alertCfg config.AlertConfig) error {
	a := FormatConfirmationAlert(p)
	return sendToChannels(a, cfg.Notify, alertCfg)
}

func sendToChannels(a alert.Alert, channels []string, alertCfg config.AlertConfig) error {
	var errs []string
	for _, ch := range channels {
		var alerter alert.Alerter
		switch strings.ToLower(ch) {
		case "telegram":
			token := alertCfg.TelegramBotToken
			if token == "" {
				token = os.Getenv("MYCASE_TELEGRAM_TOKEN")
			}
			chatID := alertCfg.TelegramChatID
			if token == "" || chatID == "" {
				errs = append(errs, "telegram: missing bot token or chat ID")
				continue
			}
			alerter = &alert.TelegramAlerter{BotToken: token, ChatID: chatID}
		case "discord":
			webhookURL := alertCfg.DiscordWebhookURL
			if webhookURL == "" {
				webhookURL = os.Getenv("MYCASE_DISCORD_WEBHOOK")
			}
			if webhookURL == "" {
				errs = append(errs, "discord: missing webhook URL")
				continue
			}
			alerter = &alert.DiscordAlerter{WebhookURL: webhookURL}
		default:
			errs = append(errs, fmt.Sprintf("unknown channel: %s", ch))
			continue
		}
		if err := alerter.Send(a); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", ch, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("alert errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// capitalizeFirst capitalizes the first letter of a string.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
