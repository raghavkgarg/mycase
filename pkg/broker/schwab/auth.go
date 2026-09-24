// Package schwab implements the Charles Schwab Trader API integration
// for US equity market data and order execution.
package schwab

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
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

// tokenHTTPClient bounds the OAuth token exchange/refresh calls. These previously
// used http.DefaultClient (Timeout: 0 = infinite); if a caller passed a
// context.Background() (the auto-refresh-on-401 path can), a hung token endpoint
// would stall forever — and in the scheduler that means sitting on the DuckDB
// writer lock. An explicit client timeout bounds the call regardless of the
// context the caller supplies.
var tokenHTTPClient = &http.Client{Timeout: 15 * time.Second}

// ErrCodeExpired signals that Schwab rejected the authorization code as
// invalid, already used, or expired (400 invalid_grant / unsupported_token_type).
// This is recoverable by re-running the browser flow to obtain a fresh code —
// the most common cause is a slow click-through of the local certificate
// warning, since codes expire in ~30 seconds. RunAuthFlow retries once on this.
var ErrCodeExpired = errors.New("schwab authorization code invalid, used, or expired")

// ErrReauthRequired signals that the refresh token itself is no longer valid
// (expired after 7 days, or revoked): the token endpoint rejected the refresh
// with 400/401. Unlike a transient network error this NEVER self-heals — it
// requires the operator to re-run `mycase auth`. Callers (the scheduler) match
// this with errors.Is to alert immediately rather than retrying silently.
var ErrReauthRequired = errors.New("schwab refresh token expired or revoked; re-run `mycase auth`")

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
	Scope            string `json:"scope"`
	ExpiresAt        int64  `json:"expires_at"`         // Unix timestamp
	RefreshExpiresAt int64  `json:"refresh_expires_at"` // Unix timestamp
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
	token     *Token
	app       *AppConfig
	tokenPath string
	mu        sync.Mutex
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

	resp, err := tokenHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody map[string]any
		json.NewDecoder(resp.Body).Decode(&errBody)
		// 400/401 on a refresh means the refresh token is dead (expired/revoked):
		// wrap the sentinel so the scheduler can alert for manual re-auth instead
		// of retrying a token that will never work again. Other codes (5xx, 429)
		// are transient and left unwrapped so they follow the normal retry path.
		if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("%w (token endpoint %d: %v)", ErrReauthRequired, resp.StatusCode, errBody)
		}
		return nil, fmt.Errorf("token refresh returned %d: %v", resp.StatusCode, errBody)
	}

	return parseTokenResponse(resp)
}

// RunAuthFlow performs the full OAuth2 authorization_code flow interactively.
// It starts a local HTTPS server, opens the browser to Schwab's auth page,
// captures the callback code, exchanges it for tokens, and saves them.
//
// Because Schwab authorization codes expire in ~30 seconds and the local
// callback's self-signed cert forces a browser warning click-through, a slow
// user can miss the window (400 invalid_grant). The flow therefore retries the
// browser round-trip ONCE on that specific recoverable error — obtaining a fresh
// code rather than uselessly re-submitting the expired one.
func RunAuthFlow(ctx context.Context, app *AppConfig, tokenPath string) error {
	callbackURL, err := url.Parse(app.CallbackURL)
	if err != nil {
		return fmt.Errorf("invalid callback_url %q: %w", app.CallbackURL, err)
	}

	port := callbackURL.Port()
	if port == "" {
		port = "8443"
	}

	// Generate self-signed TLS cert for local callback (reused across attempts).
	tlsCert, err := generateSelfSignedCert()
	if err != nil {
		return fmt.Errorf("failed to generate TLS cert: %w", err)
	}

	// Build authorization URL (constant across attempts).
	authURL := fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code",
		authBaseURL,
		url.QueryEscape(app.ClientID),
		url.QueryEscape(app.CallbackURL),
	)

	const maxAttempts = 2
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			fmt.Printf("\n⏳ The authorization code expired before it could be exchanged "+
				"(this happens when the browser warning takes too long to clear).\n"+
				"   Retrying (%d/%d) — please move through the browser prompts quickly this time.\n", attempt, maxAttempts)
		}

		token, err := runAuthAttempt(ctx, app, authURL, port, tlsCert)
		if err == nil {
			if serr := SaveToken(tokenPath, token); serr != nil {
				return fmt.Errorf("failed to save token: %w", serr)
			}
			fmt.Printf("✓ Tokens saved to %s\n", tokenPath)
			fmt.Printf("  Access token expires: %s\n", time.Unix(token.ExpiresAt, 0).Format(time.RFC3339))
			fmt.Printf("  Refresh token expires: %s\n", time.Unix(token.RefreshExpiresAt, 0).Format(time.RFC3339))
			return nil
		}

		// Only an expired/used code is worth retrying — everything else
		// (bad credentials, timeout, server error) is terminal.
		if errors.Is(err, ErrCodeExpired) && attempt < maxAttempts {
			continue
		}
		if errors.Is(err, ErrCodeExpired) {
			fmt.Printf("\n❌ The authorization code kept expiring before it could be exchanged.\n" +
				"   Re-run 'mycase auth --broker schwab' and clear the browser certificate warning as fast\n" +
				"   as possible (Safari: Show Details → visit this website; Chrome: Advanced → Proceed),\n" +
				"   without reloading the success page.\n\n")
			return fmt.Errorf("schwab auth failed after %d attempts: %w", maxAttempts, err)
		}
		return err
	}
	return fmt.Errorf("authentication failed after %d attempts", maxAttempts)
}

// runAuthAttempt performs a single browser round-trip: start the local HTTPS
// callback server, open the browser, wait for the authorization code, and
// exchange it for a token. The caller handles persistence and retry policy.
func runAuthAttempt(ctx context.Context, app *AppConfig, authURL, port string, tlsCert tls.Certificate) (*Token, error) {
	// Channel to receive the auth code from the callback handler
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	// The browser can hit the callback more than once (a self-signed cert
	// triggers retries, and browsers also probe /favicon.ico etc.). Schwab
	// authorization codes are single-use and short-lived, so a duplicate hit
	// that captured a second (or empty) code would race the real one and yield
	// a 400 invalid_grant at exchange. Latch on the FIRST request that carries
	// a real code and ignore every later invocation.
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			// A callback with no code is either an explicit auth error or a
			// browser probe. Only surface it as an error if Schwab actually
			// reported one; otherwise ignore (do not poison errCh on a favicon
			// or reload).
			if errMsg := r.URL.Query().Get("error"); errMsg != "" {
				once.Do(func() {
					errCh <- fmt.Errorf("auth callback received error: %s — %s", errMsg, r.URL.Query().Get("error_description"))
				})
				http.Error(w, "Authentication failed. Check terminal.", http.StatusBadRequest)
			}
			return
		}
		once.Do(func() {
			codeCh <- code
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body><h2>✅ Authentication successful!</h2><p>You can close this tab and return to the terminal.</p></body></html>`)
		})
	})

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
		return nil, fmt.Errorf("failed to listen on %s: %w", server.Addr, err)
	}
	tlsLn := tls.NewListener(ln, server.TLSConfig)

	go func() {
		if err := server.Serve(tlsLn); err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	// Ensure the listener is fully released before this attempt returns, so a
	// retry can re-bind the same port without "address already in use".
	defer func() {
		server.Close()
		tlsLn.Close()
	}()

	fmt.Printf("\n🔐 Opening browser for Schwab authentication...\n")
	fmt.Printf("   If the browser doesn't open, visit:\n   %s\n\n", authURL)
	fmt.Printf("⚠️  Your browser will warn that the connection to 127.0.0.1 is \"not private\"\n")
	fmt.Printf("   (the local callback uses a self-signed certificate — this is expected and safe).\n")
	fmt.Printf("   Proceed through it QUICKLY — Schwab authorization codes expire in ~30 seconds:\n")
	fmt.Printf("     • Safari:  Show Details → \"visit this website\"\n")
	fmt.Printf("     • Chrome:  Advanced → \"Proceed to 127.0.0.1 (unsafe)\"\n")
	fmt.Printf("   Do NOT reload the success page after it appears.\n\n")
	openBrowser(authURL)

	// Wait for callback or timeout
	select {
	case code := <-codeCh:
		fmt.Printf("✓ Authorization code received, exchanging for tokens...\n")
		token, err := exchangeCode(ctx, app, code)
		if err != nil {
			return nil, err
		}
		return token, nil

	case err := <-errCh:
		return nil, err

	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("authentication timed out after 5 minutes — no callback received")

	case <-ctx.Done():
		return nil, ctx.Err()
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

	resp, err := tokenHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody map[string]any
		json.NewDecoder(resp.Body).Decode(&errBody)
		errCode, _ := errBody["error"].(string)
		switch errCode {
		case "invalid_client":
			return nil, fmt.Errorf("code exchange returned %d (invalid_client): Schwab rejected the app credentials. "+
				"Check client_id/client_secret in config/schwab.json for typos and confirm the app is 'Ready For Use' in the developer portal. Raw: %v",
				resp.StatusCode, errBody)
		case "invalid_grant", "unsupported_token_type":
			return nil, fmt.Errorf("%w: code exchange returned %d (%s). Raw: %v",
				ErrCodeExpired, resp.StatusCode, errCode, errBody)
		default:
			return nil, fmt.Errorf("code exchange returned %d: %v", resp.StatusCode, errBody)
		}
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
