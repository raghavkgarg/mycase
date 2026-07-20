package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// DiscordAlerter sends alerts via a Discord Incoming Webhook.
type DiscordAlerter struct {
	WebhookURL string
}

var _ Alerter = (*DiscordAlerter)(nil)

func (d *DiscordAlerter) Send(a Alert) error {
	content := fmt.Sprintf("**[%s] %s**\n%s", a.Level, a.Title, a.Body)
	payload, _ := json.Marshal(map[string]string{"content": content})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("discord: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("discord: %w", err)
	}
	defer resp.Body.Close()
	// Discord returns 204 No Content on success
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("discord: HTTP %d", resp.StatusCode)
	}
	return nil
}
