package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// TelegramAlerter sends alerts via Telegram Bot API.
type TelegramAlerter struct {
	BotToken string
	ChatID   string
}

var _ Alerter = (*TelegramAlerter)(nil)

func (t *TelegramAlerter) Send(a Alert) error {
	text := fmt.Sprintf("[%s] *%s*\n%s", a.Level, a.Title, a.Body)
	payload, _ := json.Marshal(map[string]string{
		"chat_id":    t.ChatID,
		"text":       text,
		"parse_mode": "Markdown",
	})
	url := "https://api.telegram.org/bot" + t.BotToken + "/sendMessage"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram: HTTP %d", resp.StatusCode)
	}
	return nil
}
