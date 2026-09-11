package autopilot

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"log/slog"

	"github.com/raghavkgarg/mycase/pkg/alert"
	"github.com/raghavkgarg/mycase/pkg/attribution"
	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
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

// AssessPortfolioAlpha builds a trailing (default: 1-year) NAV series for the
// portfolio's current basket versus the passive benchmark and evaluates whether
// its risk-adjusted performance warrants a strategy-review nudge. It is
// best-effort: any data/fetch failure yields a no-nudge assessment plus the
// error, so callers can log and continue (the roadmap's "fail gracefully" rule).
//
// basketPath is the golden-copy CSV (Proposal.Portfolio). fetcher is typically
// the same *datafetcher.Router the pipeline uses.
func AssessPortfolioAlpha(ctx context.Context, fetcher attribution.PriceFetcher, basketPath string) (attribution.NudgeAssessment, string, error) {
	weights, tickers, err := csvloader.LoadBasketCSV(basketPath)
	if err != nil {
		return attribution.NudgeAssessment{}, "", fmt.Errorf("loading basket %s: %w", basketPath, err)
	}
	var holdings []attribution.Holding
	for _, tk := range tickers {
		if w := weights[tk]; w > 0 {
			holdings = append(holdings, attribution.Holding{Ticker: tk, Weight: w})
		}
	}
	if len(holdings) == 0 {
		return attribution.NudgeAssessment{}, "", fmt.Errorf("no holdings with positive weight in %s", basketPath)
	}

	nyLoc, err := time.LoadLocation("America/New_York")
	if err != nil {
		nyLoc = time.UTC
	}
	to := time.Now().In(nyLoc)
	cfg := attribution.Config{
		From:     to.AddDate(-1, 0, 0),
		To:       to,
		Location: nyLoc,
	}

	tracker := attribution.NewTracker(fetcher, slog.Default())
	points, err := tracker.BuildNAVSeries(ctx, holdings, cfg)
	if err != nil {
		return attribution.NudgeAssessment{}, cfg.Benchmark, fmt.Errorf("building NAV series: %w", err)
	}
	res := attribution.Attribution(points, cfg.RiskFree)
	benchmark := attribution.DefaultBenchmark
	return attribution.AssessNudge(res, 0), benchmark, nil
}

// SendProposalAlerts sends the proposal alert to all configured channels.
func SendProposalAlerts(p *Proposal, cfg config.ScheduleConfig, alertCfg config.AlertConfig) error {
	a := FormatProposalAlert(p)
	return sendToChannels(a, cfg.Notify, alertCfg)
}

// FormatAlphaNudgeAlert builds a "review your strategy" alert from a trailing
// performance assessment. Only meaningful when assessment.Nudge is true.
func FormatAlphaNudgeAlert(portfolio, benchmark string, assessment attribution.NudgeAssessment) alert.Alert {
	var body strings.Builder
	body.WriteString(fmt.Sprintf("*Portfolio*: %s\n", portfolio))
	body.WriteString(fmt.Sprintf("*Benchmark*: %s\n", benchmark))
	body.WriteString(fmt.Sprintf("*Trailing alpha*: %+.2f%% (annualized, over %d trading days)\n",
		assessment.Alpha*100, assessment.TradingDays))
	body.WriteString(fmt.Sprintf("*Threshold*: %+.2f%%\n\n", assessment.Threshold*100))
	body.WriteString("The active strategy has trailed the passive benchmark on a risk-adjusted basis. ")
	body.WriteString("Consider whether the factor tilt is still working, or whether simplifying to a low-cost index fund is the better call.\n\n")
	body.WriteString("👉 Review performance: http://localhost:8080/#/performance\n")

	return alert.Alert{
		Title: "🧭 Strategy Review Suggested",
		Body:  body.String(),
		Level: "warn",
	}
}

// SendAlphaNudgeAlerts dispatches the strategy-review nudge to the configured
// channels. It is a no-op (returns nil) when the assessment does not warrant a
// nudge, so callers can invoke it unconditionally.
func SendAlphaNudgeAlerts(portfolio, benchmark string, assessment attribution.NudgeAssessment, cfg config.ScheduleConfig, alertCfg config.AlertConfig) error {
	if !assessment.Nudge {
		return nil
	}
	a := FormatAlphaNudgeAlert(portfolio, benchmark, assessment)
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
