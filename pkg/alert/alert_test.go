package alert

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscordAlerter_Success(t *testing.T) {
	receivedPayload := make(map[string]string)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected application/json, got %s", ct)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	alerter := &DiscordAlerter{WebhookURL: ts.URL}
	a := Alert{
		Title: "Test Title",
		Body:  "Test Body",
		Level: "warn",
	}

	if err := alerter.Send(a); err != nil {
		t.Fatalf("unexpected error from Send: %v", err)
	}

	content := receivedPayload["content"]
	if !strings.Contains(content, "[warn] Test Title") {
		t.Errorf("expected content to contain '[warn] Test Title', got %q", content)
	}
	if !strings.Contains(content, "Test Body") {
		t.Errorf("expected content to contain 'Test Body', got %q", content)
	}
}

func TestDiscordAlerter_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	alerter := &DiscordAlerter{WebhookURL: ts.URL}
	err := alerter.Send(Alert{Title: "Error Test", Level: "critical"})
	if err == nil {
		t.Fatal("expected error on HTTP 400, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("expected error to mention 'HTTP 400', got %v", err)
	}
}

func TestDiscordAlerter_InvalidURL(t *testing.T) {
	alerter := &DiscordAlerter{WebhookURL: "http://127.0.0.1:0/invalid"}
	err := alerter.Send(Alert{Title: "Invalid URL", Level: "info"})
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
}

func TestTelegramAlerter_ConnectionError(t *testing.T) {
	// Telegram hardcodes https://api.telegram.org; testing with invalid credentials should return an error
	alerter := &TelegramAlerter{
		BotToken: "invalid_dummy_token_12345",
		ChatID:   "dummy_chat_id",
	}
	err := alerter.Send(Alert{
		Title: "Telegram Test",
		Body:  "Body",
		Level: "info",
	})
	if err == nil {
		t.Fatal("expected error with dummy telegram token, got nil")
	}
}
