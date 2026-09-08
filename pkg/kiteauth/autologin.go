package kiteauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

// AutoAuthParams holds credentials and settings needed for automated login.
type AutoAuthParams struct {
	APIKey     string
	APISecret  string
	UserID     string
	Password   string
	TOTPSecret string
	ProxyURL   string
}

type apiResponse struct {
	Status  string                 `json:"status"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data"`
}

// PerformAutoLogin performs headless Zerodha login, generates TOTP dynamically,
// captures the OAuth request_token, and exchanges it for a fresh access_token.
func PerformAutoLogin(ctx context.Context, p AutoAuthParams) (string, error) {
	if p.APIKey == "" || p.APISecret == "" {
		return "", errors.New("APIKey and APISecret are required")
	}
	if p.UserID == "" || p.Password == "" || p.TOTPSecret == "" {
		return "", errors.New("UserID, Password, and TOTPSecret are required for automated login")
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", fmt.Errorf("failed to create cookie jar: %w", err)
	}

	transport := &http.Transport{}
	if p.ProxyURL != "" {
		if u, err := url.Parse(p.ProxyURL); err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
	}

	var capturedToken string
	httpClient := &http.Client{
		Jar:       jar,
		Timeout:   15 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Catch request_token in redirect callback URL (e.g. http://127.0.0.1:8000/?request_token=XXXXX)
			if token := req.URL.Query().Get("request_token"); token != "" {
				capturedToken = token
				// Return ErrUseLastResponse so we do not attempt to connect to the local redirect server
				return http.ErrUseLastResponse
			}
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			return nil
		},
	}

	// Step 1: Handshake with Kite Connect login page to initialize cookies
	loginURL := fmt.Sprintf("https://kite.trade/connect/login?v=3&api_key=%s", url.QueryEscape(p.APIKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating handshake request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("login handshake failed: %w", err)
	}
	_ = resp.Body.Close()

	// Step 2: Post user credentials to https://kite.zerodha.com/api/login
	loginForm := url.Values{
		"user_id":  {p.UserID},
		"password": {p.Password},
	}
	loginReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://kite.zerodha.com/api/login", strings.NewReader(loginForm.Encode()))
	if err != nil {
		return "", fmt.Errorf("creating login request: %w", err)
	}
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	loginResp, err := httpClient.Do(loginReq)
	if err != nil {
		return "", fmt.Errorf("credential submission failed: %w", err)
	}
	defer loginResp.Body.Close()

	var loginData apiResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&loginData); err != nil {
		return "", fmt.Errorf("parsing login response: %w", err)
	}
	if loginData.Status != "success" {
		msg := loginData.Message
		if msg == "" {
			msg = "unknown error"
		}
		return "", fmt.Errorf("login failed: %s", msg)
	}

	requestID, _ := loginData.Data["request_id"].(string)
	if requestID == "" {
		return "", errors.New("login succeeded but request_id was not returned")
	}

	// Step 3: Generate TOTP code
	totpToken, err := GenerateTOTP(p.TOTPSecret)
	if err != nil {
		return "", fmt.Errorf("generating TOTP: %w", err)
	}

	twofaType := "totp"
	if t, ok := loginData.Data["twofa_type"].(string); ok && t != "" {
		twofaType = t
	}

	// Step 4: Submit TOTP to https://kite.zerodha.com/api/twofa
	twofaForm := url.Values{
		"user_id":     {p.UserID},
		"request_id":  {requestID},
		"twofa_value": {totpToken},
		"twofa_type":  {twofaType},
	}
	twofaReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://kite.zerodha.com/api/twofa", strings.NewReader(twofaForm.Encode()))
	if err != nil {
		return "", fmt.Errorf("creating 2FA request: %w", err)
	}
	twofaReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	twofaResp, err := httpClient.Do(twofaReq)
	if err != nil {
		return "", fmt.Errorf("2FA submission failed: %w", err)
	}
	defer twofaResp.Body.Close()

	var twofaData apiResponse
	if err := json.NewDecoder(twofaResp.Body).Decode(&twofaData); err != nil {
		return "", fmt.Errorf("parsing 2FA response: %w", err)
	}
	if twofaData.Status != "success" {
		msg := twofaData.Message
		if msg == "" {
			msg = "unknown error"
		}
		return "", fmt.Errorf("2FA validation failed: %s", msg)
	}

	// Step 5: Follow redirect handshake to catch request_token
	redirectURL := fmt.Sprintf("%s&skip_session=true", loginURL)
	redirReq, err := http.NewRequestWithContext(ctx, http.MethodGet, redirectURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating redirect request: %w", err)
	}

	redirResp, err := httpClient.Do(redirReq)
	if err != nil && !errors.Is(err, http.ErrUseLastResponse) {
		return "", fmt.Errorf("following redirect handshake: %w", err)
	}
	if redirResp != nil {
		defer redirResp.Body.Close()

		// If CheckRedirect did not set capturedToken, inspect Location header or request URL
		if capturedToken == "" {
			if loc := redirResp.Header.Get("Location"); loc != "" {
				if parsedLoc, err := url.Parse(loc); err == nil {
					capturedToken = parsedLoc.Query().Get("request_token")
				}
			}
		}
		if capturedToken == "" && redirResp.Request != nil && redirResp.Request.URL != nil {
			capturedToken = redirResp.Request.URL.Query().Get("request_token")
		}
	}

	if capturedToken == "" {
		return "", errors.New("failed to retrieve request_token from authentication redirect")
	}

	// Step 6: Exchange request_token for access_token using official gokiteconnect
	kc := kiteconnect.New(p.APIKey)
	if p.ProxyURL != "" {
		if proxyURL, err := url.Parse(p.ProxyURL); err == nil {
			kc.SetHTTPClient(&http.Client{
				Timeout: 15 * time.Second,
				Transport: &http.Transport{
					Proxy: http.ProxyURL(proxyURL),
				},
			})
		}
	}

	session, err := kc.GenerateSession(capturedToken, p.APISecret)
	if err != nil {
		return "", fmt.Errorf("exchanging request_token for access_token: %w", err)
	}

	return session.AccessToken, nil
}
