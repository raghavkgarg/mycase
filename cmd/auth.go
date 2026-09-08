package cmd

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/kiteauth"
)

var AuthCommand = &cli.Command{
	Name:  "auth",
	Usage: "Set up Zerodha Kite Connect authentication",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "manual",
			Usage: "Force interactive browser login even if auto-login credentials exist",
		},
		&cli.BoolFlag{
			Name:  "force",
			Usage: "Force re-authentication even if current session/token is still valid",
		},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		return runAuthCmdWithFlags(ctx, c.Bool("manual"), c.Bool("force"))
	},
}

func runAuthCmd(ctx context.Context) error {
	return runAuthCmdWithFlags(ctx, false, false)
}

func runAuthCmdWithFlags(ctx context.Context, forceManual, forceRefresh bool) error {
	fmt.Println("====================================================================")
	fmt.Println("             Zerodha Kite Connect Auth Setup Utility               ")
	fmt.Println("====================================================================")
	if publicIP := config.FetchPublicIP(); publicIP != "" {
		fmt.Printf("Current Public IP: %s\n", publicIP)
		fmt.Println("  (Make sure this IP is whitelisted under App Settings on https://developers.kite.trade/profile)")
	}

	configFile := "config/config.json"
	var apiKey, apiSecret string

	cfg, err := config.LoadConfig(configFile)
	if err == nil && cfg != nil {
		apiKey = cfg.APIKey
		apiSecret = cfg.APISecret
	} else if cfg == nil {
		cfg = &config.Config{}
	}

	reader := bufio.NewReader(os.Stdin)

	if apiKey == "" {
		fmt.Print("Enter your Zerodha Kite API Key: ")
		apiKey, _ = reader.ReadString('\n')
		apiKey = strings.TrimSpace(apiKey)
	} else {
		fmt.Printf("Using existing API Key: %s\n", apiKey)
	}

	if apiSecret == "" {
		fmt.Print("Enter your Zerodha Kite API Secret: ")
		apiSecret, _ = reader.ReadString('\n')
		apiSecret = strings.TrimSpace(apiSecret)
	} else {
		fmt.Println("Using existing API Secret from config file.")
	}

	if apiKey == "" || apiSecret == "" {
		return fmt.Errorf("API Key and API Secret are required")
	}

	// 1. First check if current access token is already valid and active
	if !forceManual && !forceRefresh && cfg.AccessToken != "" && cfg.AccessToken != "your_access_token" {
		testClient := kiteconnect.New(apiKey)
		testClient.SetAccessToken(cfg.AccessToken)
		if cfg.HTTPProxy != "" {
			if proxyURL, err := url.Parse(cfg.HTTPProxy); err == nil {
				testClient.SetHTTPClient(&http.Client{
					Timeout: 5 * time.Second,
					Transport: &http.Transport{
						Proxy: http.ProxyURL(proxyURL),
					},
				})
			}
		}
		profile, pErr := testClient.GetUserProfile()
		if pErr == nil && profile.UserID != "" {
			fmt.Println("\n✅ Zerodha Kite Connect session is already active and authenticated!")
			fmt.Printf("   User: %s (%s) | Broker: %s\n", profile.UserName, profile.UserID, profile.Broker)
			fmt.Println("   Access token is valid. Webpage login not required.")
			return nil
		}
	}

	// 2. Check if automated headless login credentials are configured
	if !forceManual && (cfg.UserID == "" || cfg.Password == "" || cfg.TOTPSecret == "") {
		fmt.Println("\n🔐 Auto-login credentials ('user_id', 'password', 'totp_secret') are not yet set.")
		fmt.Print("Would you like to configure them now to enable zero-browser login? (y/n): ")
		ans, _ := reader.ReadString('\n')
		ans = strings.ToLower(strings.TrimSpace(ans))
		if ans == "y" || ans == "yes" {
			if cfg.UserID == "" {
				fmt.Print("Enter Zerodha User ID (e.g. CBR420): ")
				uid, _ := reader.ReadString('\n')
				cfg.UserID = strings.TrimSpace(uid)
			}
			if cfg.Password == "" {
				fmt.Print("Enter Zerodha Password: ")
				pwd, _ := reader.ReadString('\n')
				cfg.Password = strings.TrimSpace(pwd)
			}
			if cfg.TOTPSecret == "" {
				fmt.Print("Enter Zerodha TOTP Secret (from Kite Web -> Profile -> Password & Security -> External TOTP): ")
				tSecret, _ := reader.ReadString('\n')
				cfg.TOTPSecret = strings.ReplaceAll(strings.TrimSpace(tSecret), " ", "")
			}
			_ = config.SaveConfig(configFile, cfg)
		}
	}

	if !forceManual && cfg.UserID != "" && cfg.Password != "" && cfg.TOTPSecret != "" {
		fmt.Println("\n🔐 Auto-login credentials detected (user_id, password, totp_secret).")
		fmt.Println("Attempting headless authentication & dynamic TOTP generation...")

		autoParams := kiteauth.AutoAuthParams{
			APIKey:     apiKey,
			APISecret:  apiSecret,
			UserID:     cfg.UserID,
			Password:   cfg.Password,
			TOTPSecret: cfg.TOTPSecret,
			ProxyURL:   cfg.HTTPProxy,
		}

		accessToken, autoErr := kiteauth.PerformAutoLogin(ctx, autoParams)
		if autoErr == nil {
			fmt.Println("🎉 Success! Headless authentication generated daily access token.")
			cfg.AccessToken = accessToken
			if err := config.SaveConfig(configFile, cfg); err != nil {
				return fmt.Errorf("saving configuration: %w", err)
			}
			fmt.Println("\nSuccessfully refreshed and synchronized 'config/config.json'!")
			fmt.Println("You can now run `mycase basket --live` or `mycase holdings --live`.")
			return nil
		}

		fmt.Printf("⚠️  Headless login failed: %v\n", autoErr)
		fmt.Println("Falling back to interactive browser authorization...")
	} else if !forceManual {
		fmt.Println("\n💡 Tip: Add 'user_id', 'password', and 'totp_secret' to config/config.json for automatic zero-browser login.")
	}

	client := kiteconnect.New(apiKey)
	if cfg != nil && cfg.HTTPProxy != "" {
		if proxyURL, err := url.Parse(cfg.HTTPProxy); err == nil {
			client.SetHTTPClient(&http.Client{
				Timeout: 10 * time.Second,
				Transport: &http.Transport{
					Proxy: http.ProxyURL(proxyURL),
				},
			})
		}
	}
	loginURL := client.GetLoginURL()
	fmt.Println("\nInitializing authorization flow...")

	tokenChan := make(chan string, 1)
	errChan := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("request_token")
		if token == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, "Error: request_token parameter is missing.")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Authentication Successful</title>
<style>
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;background-color:#0d1117;color:#c9d1d9;display:flex;justify-content:center;align-items:center;height:100vh;margin:0}
.container{text-align:center;background:#161b22;padding:40px;border-radius:12px;box-shadow:0 4px 12px rgba(0,0,0,.3);border:1px solid #30363d;max-width:450px}
h1{color:#2ea44f;margin-bottom:20px;font-size:24px}
p{font-size:16px;line-height:1.5;margin-bottom:30px}
.icon{font-size:48px;color:#2ea44f;margin-bottom:20px}
</style></head>
<body><div class="container"><div class="icon">✓</div>
<h1>Authentication Successful</h1>
<p>Zerodha Kite has successfully authenticated. This browser tab will close automatically...</p>
</div>
<script>
setTimeout(function() {
    window.open('', '_self', '');
    window.close();
}, 1000);
</script>
</body></html>`))
		tokenChan <- token
	})

	srv := &http.Server{Addr: ":8000", Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	fmt.Println("Listening for authorization redirect on http://127.0.0.1:8000 ...")
	_ = exec.Command("open", loginURL).Start()
	fmt.Println("Opened the login page in your browser automatically.")
	fmt.Println("Please log in to Zerodha. If the browser didn't open, visit this URL:")
	fmt.Println(loginURL)

	var requestToken string
	select {
	case requestToken = <-tokenChan:
		fmt.Println("Successfully captured request token automatically!")
	case err := <-errChan:
		return fmt.Errorf("local server error: %w", err)
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("timeout: authorization took too long (exceeded 5 minutes)")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)

	fmt.Println("\nExchanging request token for an access token...")
	session, err := client.GenerateSession(requestToken, apiSecret)
	if err != nil {
		return fmt.Errorf("generating session: %w", err)
	}
	fmt.Println("Success! Generated access token.")

	cfg.APIKey = apiKey
	cfg.AccessToken = session.AccessToken
	if apiSecret != "" {
		cfg.APISecret = apiSecret
	}

	if err := config.SaveConfig(configFile, cfg); err != nil {
		return fmt.Errorf("saving credentials: %w", err)
	}

	fmt.Println("\nSuccessfully updated 'config/config.json' with your credentials!")
	fmt.Println("You can now run `mycase basket --live` to use live data.")
	return nil
}
