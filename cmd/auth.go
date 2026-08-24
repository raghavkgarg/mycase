package cmd

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/schwab"
)

var AuthCommand = &cli.Command{
	Name:  "auth",
	Usage: "Set up broker authentication (Zerodha or Schwab)",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "broker",
			Aliases: []string{"b"},
			Value:   "zerodha",
			Usage:   "Broker to authenticate with: zerodha, schwab",
		},
		&cli.StringFlag{
			Name:  "config",
			Value: "",
			Usage: "Path to broker config file (default: config/config.json for zerodha, config/schwab.json for schwab)",
		},
		&cli.StringFlag{
			Name:  "token-path",
			Value: "config/schwab_token.json",
			Usage: "Path to save Schwab OAuth tokens",
		},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		broker := strings.ToLower(c.String("broker"))
		switch broker {
		case "schwab":
			configPath := c.String("config")
			if configPath == "" {
				configPath = "config/schwab.json"
			}
			tokenPath := c.String("token-path")
			return runSchwabAuth(ctx, configPath, tokenPath)
		case "zerodha", "":
			return runAuthCmd(ctx)
		default:
			return fmt.Errorf("unsupported broker %q — supported: zerodha, schwab", broker)
		}
	},
}

func runAuthCmd(ctx context.Context) error {
	fmt.Println("====================================================================")
	fmt.Println("             Zerodha Kite Connect Auth Setup Utility               ")
	fmt.Println("====================================================================")
	if publicIP := config.FetchPublicIP(); publicIP != "" {
		fmt.Printf("Current Public IP: %s\n", publicIP)
		fmt.Println("  (Make sure this IP is whitelisted under App Settings on https://developers.kite.trade/profile)")
	}

	configFile := "config/config.json"
	var apiKey, apiSecret string
	var saveCreds bool

	cfg, err := config.LoadConfig(configFile)
	if err == nil {
		apiKey = cfg.APIKey
		apiSecret = cfg.APISecret
	}

	reader := bufio.NewReader(os.Stdin)

	if apiKey == "" {
		fmt.Print("Enter your Zerodha Kite API Key: ")
		apiKey, _ = reader.ReadString('\n')
		apiKey = strings.TrimSpace(apiKey)
		saveCreds = true
	} else {
		fmt.Printf("Using existing API Key: %s\n", apiKey)
	}

	if apiSecret == "" {
		fmt.Print("Enter your Zerodha Kite API Secret: ")
		apiSecret, _ = reader.ReadString('\n')
		apiSecret = strings.TrimSpace(apiSecret)
		saveCreds = true
	} else {
		fmt.Println("Using existing API Secret from config file.")
	}

	if apiKey == "" || apiSecret == "" {
		return fmt.Errorf("API Key and API Secret are required")
	}

	client := kiteconnect.New(apiKey)
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
<p>Zerodha Kite has successfully authenticated. You can safely close this browser window and return to the terminal.</p>
</div></body></html>`))
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

	newCfg := &config.Config{
		APIKey:      apiKey,
		AccessToken: session.AccessToken,
	}

	if saveCreds {
		fmt.Print("Would you like to save the API Key and API Secret to config/config.json for future runs? (y/n): ")
		ans, _ := reader.ReadString('\n')
		ans = strings.ToLower(strings.TrimSpace(ans))
		if ans == "y" || ans == "yes" {
			newCfg.APISecret = apiSecret
		}
	} else if apiSecret != "" {
		newCfg.APISecret = apiSecret
	}

	if err := config.SaveConfig(configFile, newCfg); err != nil {
		return fmt.Errorf("saving credentials: %w", err)
	}

	fmt.Println("\nSuccessfully updated 'config/config.json' with your credentials!")
	fmt.Println("You can now run `mycase basket --live` to use live data.")
	return nil
}


// runSchwabAuth performs the Schwab OAuth2 authorization_code flow.
func runSchwabAuth(ctx context.Context, configPath, tokenPath string) error {
	fmt.Println("====================================================================")
	fmt.Println("           Charles Schwab OAuth2 Auth Setup Utility                 ")
	fmt.Println("====================================================================")

	app, err := schwab.LoadAppConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load Schwab config from %s: %w\n\n"+
			"Create %s with your Schwab app credentials:\n"+
			"  {\n"+
			"    \"client_id\": \"your_app_key\",\n"+
			"    \"client_secret\": \"your_app_secret\",\n"+
			"    \"callback_url\": \"https://127.0.0.1:8443/callback\"\n"+
			"  }\n\n"+
			"Register your app at https://developer.schwab.com", configPath, err, configPath)
	}

	fmt.Printf("App Key: %s...%s\n", app.ClientID[:4], app.ClientID[len(app.ClientID)-4:])
	fmt.Printf("Callback URL: %s\n", app.CallbackURL)
	fmt.Printf("Token will be saved to: %s\n\n", tokenPath)

	if err := schwab.RunAuthFlow(ctx, app, tokenPath); err != nil {
		return err
	}

	fmt.Println("\n✅ Schwab authentication complete!")
	fmt.Println("You can now use US market features:")
	fmt.Println("  mycase pick --index sp500 --method us_quality_momentum")
	fmt.Println("  mycase basket --broker schwab --live")
	return nil
}
