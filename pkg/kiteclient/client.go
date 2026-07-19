package kiteclient

import (
	"fmt"

	"github.com/raghavkgarg/mycase/pkg/config"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

// InitKiteClient initializes a Zerodha Kite Connect client.
// Returns a client pointer and a boolean indicating if we are running in mock/dry mode.
func InitKiteClient(cfg *config.Config, forceMock bool) (*kiteconnect.Client, bool) {
	isMock := forceMock || cfg.APIKey == "" || cfg.AccessToken == "" ||
		cfg.APIKey == "your_api_key" || cfg.AccessToken == "your_access_token"

	if isMock {
		return nil, true
	}

	client := kiteconnect.New(cfg.APIKey)
	client.SetAccessToken(cfg.AccessToken)
	return client, false
}

// LoadAndInitClient handles loading config/config.json with fallback and initializing the client.
func LoadAndInitClient(configPath string, liveMode bool) (*kiteconnect.Client, bool) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Printf("Note: Could not load %s, running in Mock mode. Error: %v\n", configPath, err)
		cfg = &config.Config{
			APIKey:      "your_api_key",
			AccessToken: "your_access_token",
		}
	}
	return InitKiteClient(cfg, !liveMode)
}
