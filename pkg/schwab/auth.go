// Package schwab implements the Charles Schwab Trader API integration
// for US equity market data and order execution.
package schwab

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	authBaseURL  = "https://api.schwabapi.com/v1/oauth/authorize"
	tokenURL     = "https://api.schwabapi.com/v1/oauth/token"
	callbackPath = "/callback"
)

// AppConfig holds Schwab OAuth2 application credentials.
type AppConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	CallbackURL  string `json:"callback_url"`
}

// Token holds the OAuth2 token set persisted to disk.
type Token struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresAt        int64  `json:"expires_at"`         // Unix timestamp
	RefreshExpiresAt int64  `json:"refresh_expires_at"` // Unix timestamp
	Scope            string `json:"scope"`
}

// IsExpired reports whether the access token has expired or will expire
// within the given grace period.
func (t *Token) IsExpired(grace time.Duration) bool {
	if t == nil {
		return true
	}
	return time.Now().Unix() >= t.ExpiresAt-int64(grace.Seconds())
}

// IsRefreshExpired reports whether the refresh token has expired.
func (t *Token) IsRefreshExpired() bool {
	if t == nil {
		return true
	}
	return time.Now().Unix() >= t.RefreshExpiresAt
}

// TokenManager handles token loading, saving, and auto-refresh.
type TokenManager struct {
	mu        sync.Mutex
	token     *Token
	tokenPath string
	app       *AppConfig
}

// NewTokenManager creates a TokenManager that persists tokens to the given path.
func NewTokenManager(app *AppConfig, tokenPath string) *TokenManager {
	return &TokenManager{
		tokenPath: tokenPath,
		app:       app,
	}
}

// GetAccessToken returns a valid access token, refreshing if needed.
// Returns an error if the refresh token is also expired (user must re-auth).
func (tm *TokenManager) GetAccessToken(ctx context.Context) (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.token == nil {
		t, err := LoadToken(tm.tokenPath)
		if err != nil {
			return "", fmt.Errorf("no valid token found: %w (run 'mycase auth --broker schwab' to authenticate)", err)
		}
		tm.token = t
	}

	// Token still valid (with 60s grace)
	if !tm.token.IsExpired(60 * time.Second) {
		return tm.token.AccessToken, nil
	}

	// Access token expired — try refresh
	if tm.token.IsRefreshExpired() {
		return "", fmt.Errorf("refresh token expired at %s — run 'mycase auth --broker schwab' to re-authenticate",
			time.Unix(tm.token.RefreshExpiresAt, 0).Format(time.RFC3339))
	}

	refreshed, err := RefreshToken(ctx, tm.app.ClientID, tm.app.ClientSecret, tm.token.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("token refresh failed: %w", err)
	}
	tm.token = refreshed
	if err := SaveToken(tm.tokenPath, refreshed); err != nil {
		return "", fmt.Errorf("failed to persist refreshed token: %w", err)
	}
	return tm.token.AccessToken, nil
}

// LoadAppConfig reads Schwab app credentials from a JSON file.
func LoadAppConfig(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, fmt.Errorf("schwab config at %s missing client_id or client_secret", path)
	}
	if cfg.CallbackURL == "" {
		cfg.CallbackURL = "https://127.0.0.1:8443/callback"
	}
	return &cfg, nil
}

// LoadToken reads a persisted token from disk.
func LoadToken(path string) (*Token, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Token
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	if t.AccessToken == "" {
		return nil, fmt.Errorf("token file %s has empty access_token", path)
	}
	return &t, nil
}

// SaveToken persists a token to disk with restrictive permissions.
func SaveToken(path string, t *Token) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// RefreshToken exchanges a refresh token for a new token pair.
func RefreshToken(ctx context.Context, clientID, clientSecret, refreshToken string) (*Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+basicAuth(clientID, clientSecret))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody map[string]any
		json.NewDecoder(resp.Body).Decode(&errBody)
		return nil, fmt.Errorf("token refresh returned %d: %v", resp.StatusCode, errBody)
	}

	return parseTokenResponse(resp)
}

// RunAuthFlow performs the full OAuth2 authorization_code flow interactively.
// It starts a local HTTPS server, opens the browser to Schwab's auth page,
// captures the callback code, exchanges it for tokens, and saves them.
func RunAuthFlow(ctx context.Context, app *AppConfig, tokenPath string) error {
	callbackURL, err := url.Parse(app.CallbackURL)
	if err != nil {
		return fmt.Errorf("invalid callback_url %q: %w", app.CallbackURL, err)
	}

	port := callbackURL.Port()
	if port == "" {
		port = "8443"
	}

	// Channel to receive the auth code from the callback handler
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errMsg := r.URL.Query().Get("error")
			errCh <- fmt.Errorf("auth callback received error: %s — %s", errMsg, r.URL.Query().Get("error_description"))
			http.Error(w, "Authentication failed. Check terminal.", http.StatusBadRequest)
			return
		}
		codeCh <- code
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><h2>✅ Authentication successful!</h2><p>You can close this tab and return to the terminal.</p></body></html>`)
	})

	// Generate self-signed TLS cert for local callback
	tlsCert, err := generateSelfSignedCert()
	if err != nil {
		return fmt.Errorf("failed to generate TLS cert: %w", err)
	}

	server := &http.Server{
		Addr:    "127.0.0.1:" + port,
		Handler: mux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
		},
	}

	// Start server in background
	ln, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", server.Addr, err)
	}
	tlsLn := tls.NewListener(ln, server.TLSConfig)

	go func() {
		if err := server.Serve(tlsLn); err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	defer server.Shutdown(ctx)

	// Build authorization URL
	authURL := fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code",
		authBaseURL,
		url.QueryEscape(app.ClientID),
		url.QueryEscape(app.CallbackURL),
	)

	fmt.Printf("\n🔐 Opening browser for Schwab authentication...\n")
	fmt.Printf("   If the browser doesn't open, visit:\n   %s\n\n", authURL)
	openBrowser(authURL)

	// Wait for callback or timeout
	select {
	case code := <-codeCh:
		fmt.Printf("✓ Authorization code received, exchanging for tokens...\n")
		token, err := exchangeCode(ctx, app, code)
		if err != nil {
			return fmt.Errorf("code exchange failed: %w", err)
		}
		if err := SaveToken(tokenPath, token); err != nil {
			return fmt.Errorf("failed to save token: %w", err)
		}
		fmt.Printf("✓ Tokens saved to %s\n", tokenPath)
		fmt.Printf("  Access token expires: %s\n", time.Unix(token.ExpiresAt, 0).Format(time.RFC3339))
		fmt.Printf("  Refresh token expires: %s\n", time.Unix(token.RefreshExpiresAt, 0).Format(time.RFC3339))
		return nil

	case err := <-errCh:
		return err

	case <-time.After(5 * time.Minute):
		return fmt.Errorf("authentication timed out after 5 minutes — no callback received")

	case <-ctx.Done():
		return ctx.Err()
	}
}

// exchangeCode exchanges an authorization code for tokens.
func exchangeCode(ctx context.Context, app *AppConfig, code string) (*Token, error) {
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {app.CallbackURL},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+basicAuth(app.ClientID, app.ClientSecret))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody map[string]any
		json.NewDecoder(resp.Body).Decode(&errBody)
		return nil, fmt.Errorf("code exchange returned %d: %v", resp.StatusCode, errBody)
	}

	return parseTokenResponse(resp)
}

// parseTokenResponse decodes a Schwab token endpoint response.
func parseTokenResponse(resp *http.Response) (*Token, error) {
	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"` // seconds until access token expires
		Scope        string `json:"scope"`
		IDToken      string `json:"id_token"` // unused
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}
	if raw.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}

	now := time.Now().Unix()
	t := &Token{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		TokenType:    raw.TokenType,
		ExpiresAt:    now + raw.ExpiresIn,
		Scope:        raw.Scope,
	}
	// Schwab refresh tokens are valid for ~7 days
	if raw.RefreshToken != "" {
		t.RefreshExpiresAt = now + 7*24*3600
	}
	return t, nil
}

// basicAuth encodes client_id:client_secret as a Base64 Basic auth header value.
func basicAuth(clientID, clientSecret string) string {
	return base64.StdEncoding.EncodeToString([]byte(clientID + ":" + clientSecret))
}

// openBrowser opens the given URL in the default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	cmd.Start()
}
