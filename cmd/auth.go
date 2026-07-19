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

	"github.com/gkgarg24/mycase/pkg/config"
)

var AuthCommand = &cli.Command{
	Name:  "auth",
	Usage: "Set up Zerodha Kite Connect authentication",
	Action: func(ctx context.Context, c *cli.Command) error {
		return runAuthCmd(ctx)
	},
}

func runAuthCmd(ctx context.Context) error {
	fmt.Println("====================================================================")
	fmt.Println("             Zerodha Kite Connect Auth Setup Utility               ")
	fmt.Println("====================================================================")

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
