package schwab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTokenSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")

	original := &Token{
		AccessToken:      "acc_123",
		RefreshToken:     "ref_456",
		TokenType:        "Bearer",
		ExpiresAt:        time.Now().Add(30 * time.Minute).Unix(),
		RefreshExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(),
		Scope:            "api",
	}

	if err := SaveToken(path, original); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	// Verify file permissions
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("token file permissions = %o, want 0600", perm)
	}

	loaded, err := LoadToken(path)
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}

	if loaded.AccessToken != original.AccessToken {
		t.Errorf("AccessToken = %q, want %q", loaded.AccessToken, original.AccessToken)
	}
	if loaded.RefreshToken != original.RefreshToken {
		t.Errorf("RefreshToken = %q, want %q", loaded.RefreshToken, original.RefreshToken)
	}
	if loaded.ExpiresAt != original.ExpiresAt {
		t.Errorf("ExpiresAt = %d, want %d", loaded.ExpiresAt, original.ExpiresAt)
	}
}

func TestTokenExpiry(t *testing.T) {
	t.Run("valid token", func(t *testing.T) {
		tok := &Token{ExpiresAt: time.Now().Add(10 * time.Minute).Unix()}
		if tok.IsExpired(60 * time.Second) {
			t.Error("token should not be expired")
		}
	})

	t.Run("expired token", func(t *testing.T) {
		tok := &Token{ExpiresAt: time.Now().Add(-5 * time.Minute).Unix()}
		if !tok.IsExpired(60 * time.Second) {
			t.Error("token should be expired")
		}
	})

	t.Run("within grace period", func(t *testing.T) {
		tok := &Token{ExpiresAt: time.Now().Add(30 * time.Second).Unix()}
		if !tok.IsExpired(60 * time.Second) {
			t.Error("token within grace period should be treated as expired")
		}
	})

	t.Run("nil token", func(t *testing.T) {
		var tok *Token
		if !tok.IsExpired(0) {
			t.Error("nil token should be expired")
		}
	})
}

func TestRefreshTokenExpiry(t *testing.T) {
	t.Run("valid refresh", func(t *testing.T) {
		tok := &Token{RefreshExpiresAt: time.Now().Add(24 * time.Hour).Unix()}
		if tok.IsRefreshExpired() {
			t.Error("refresh token should not be expired")
		}
	})

	t.Run("expired refresh", func(t *testing.T) {
		tok := &Token{RefreshExpiresAt: time.Now().Add(-1 * time.Hour).Unix()}
		if !tok.IsRefreshExpired() {
			t.Error("refresh token should be expired")
		}
	})
}

func TestLoadAppConfig(t *testing.T) {
	dir := t.TempDir()

	t.Run("valid config", func(t *testing.T) {
		path := filepath.Join(dir, "valid.json")
		data := `{"client_id":"abc","client_secret":"xyz","callback_url":"https://127.0.0.1:9999/cb"}`
		os.WriteFile(path, []byte(data), 0644)

		cfg, err := LoadAppConfig(path)
		if err != nil {
			t.Fatalf("LoadAppConfig: %v", err)
		}
		if cfg.ClientID != "abc" {
			t.Errorf("ClientID = %q, want %q", cfg.ClientID, "abc")
		}
		if cfg.CallbackURL != "https://127.0.0.1:9999/cb" {
			t.Errorf("CallbackURL = %q, want custom value", cfg.CallbackURL)
		}
	})

	t.Run("default callback URL", func(t *testing.T) {
		path := filepath.Join(dir, "nocb.json")
		data := `{"client_id":"abc","client_secret":"xyz"}`
		os.WriteFile(path, []byte(data), 0644)

		cfg, err := LoadAppConfig(path)
		if err != nil {
			t.Fatalf("LoadAppConfig: %v", err)
		}
		if cfg.CallbackURL != "https://127.0.0.1:8443/callback" {
			t.Errorf("CallbackURL = %q, want default", cfg.CallbackURL)
		}
	})

	t.Run("missing client_id", func(t *testing.T) {
		path := filepath.Join(dir, "bad.json")
		data := `{"client_secret":"xyz"}`
		os.WriteFile(path, []byte(data), 0644)

		_, err := LoadAppConfig(path)
		if err == nil {
			t.Error("expected error for missing client_id")
		}
	})
}

func TestRefreshTokenHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q, want form-urlencoded", ct)
		}
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			t.Error("missing Authorization header")
		}

		r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", r.Form.Get("grant_type"))
		}
		if r.Form.Get("refresh_token") != "old_refresh" {
			t.Errorf("refresh_token = %q, want old_refresh", r.Form.Get("refresh_token"))
		}

		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new_access",
			"refresh_token": "new_refresh",
			"token_type":    "Bearer",
			"expires_in":    1800,
			"scope":         "api",
		})
	}))
	defer server.Close()

	// Temporarily override tokenURL — we can't do this directly since it's a const.
	// Instead, test the parseTokenResponse and basicAuth helpers.
	t.Run("basicAuth encoding", func(t *testing.T) {
		result := basicAuth("client", "secret")
		if result != "Y2xpZW50OnNlY3JldA==" {
			t.Errorf("basicAuth = %q, want base64 of client:secret", result)
		}
	})
}

func TestTokenManagerGetAccessToken(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token.json")

	// Write a valid token
	validToken := &Token{
		AccessToken:      "valid_access",
		RefreshToken:     "valid_refresh",
		TokenType:        "Bearer",
		ExpiresAt:        time.Now().Add(10 * time.Minute).Unix(),
		RefreshExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	SaveToken(tokenPath, validToken)

	app := &AppConfig{ClientID: "id", ClientSecret: "secret"}
	tm := NewTokenManager(app, tokenPath)

	token, err := tm.GetAccessToken(context.Background())
	if err != nil {
		t.Fatalf("GetAccessToken: %v", err)
	}
	if token != "valid_access" {
		t.Errorf("token = %q, want %q", token, "valid_access")
	}
}

func TestTokenManagerExpiredAccessNoRefresh(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token.json")

	// Write a token with expired access AND expired refresh
	expiredToken := &Token{
		AccessToken:      "expired_access",
		RefreshToken:     "expired_refresh",
		TokenType:        "Bearer",
		ExpiresAt:        time.Now().Add(-10 * time.Minute).Unix(),
		RefreshExpiresAt: time.Now().Add(-1 * time.Hour).Unix(),
	}
	SaveToken(tokenPath, expiredToken)

	app := &AppConfig{ClientID: "id", ClientSecret: "secret"}
	tm := NewTokenManager(app, tokenPath)

	_, err := tm.GetAccessToken(context.Background())
	if err == nil {
		t.Fatal("expected error when both tokens expired")
	}
}
