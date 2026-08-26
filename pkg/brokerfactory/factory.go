// Package brokerfactory creates broker instances based on user configuration.
// It exists as a separate package to avoid circular dependencies between
// pkg/broker (interface), pkg/broker/zerodha, and pkg/schwab.
package brokerfactory

import (
	"context"
	"fmt"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/broker/zerodha"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/broker/schwab"
)

const defaultsPath = "config/defaults.json"

// NewFromDefaults creates the appropriate broker based on config/defaults.json.
// If live is false or credentials are missing/invalid, returns MockBroker.
func NewFromDefaults(live bool) (broker.Broker, error) {
	defaults := config.LoadUserDefaults(defaultsPath)
	return NewByName(defaults.Broker, live)
}

// NewByName creates a broker by explicit name.
// If live is false, returns MockBroker regardless of broker name.
func NewByName(name string, live bool) (broker.Broker, error) {
	if !live {
		return &broker.MockBroker{}, nil
	}

	switch name {
	case "schwab":
		return newSchwabBroker()
	case "zerodha":
		return zerodha.New(true, "config/config.json"), nil
	case "", "mock":
		return &broker.MockBroker{}, nil
	default:
		return nil, fmt.Errorf("unsupported broker: %q (supported: schwab, zerodha, mock)", name)
	}
}

// newSchwabBroker constructs a live SchwabBroker from config files.
func newSchwabBroker() (broker.Broker, error) {
	defaults := config.LoadUserDefaults(defaultsPath)

	schwabConfigPath := "config/schwab.json"
	schwabTokenPath := "config/schwab_token.json"

	// Check if pipeline config specifies custom paths
	if defaults.PipelineConfig != "" {
		if pipeCfg, err := config.LoadPipelineConfig(defaults.PipelineConfig); err == nil {
			if pipeCfg.SchwabConfig != "" {
				schwabConfigPath = pipeCfg.SchwabConfig
			}
			if pipeCfg.SchwabToken != "" {
				schwabTokenPath = pipeCfg.SchwabToken
			}
		}
	}

	app, err := schwab.LoadAppConfig(schwabConfigPath)
	if err != nil {
		return nil, fmt.Errorf("schwab broker: %w\n  Run 'mycase auth --broker schwab' to set up credentials", err)
	}

	tokenMgr := schwab.NewTokenManager(app, schwabTokenPath)
	client := schwab.NewClient(tokenMgr)

	ctx := context.Background()
	hash, err := client.FetchAccountHash(ctx)
	if err != nil {
		return nil, fmt.Errorf("schwab broker: could not fetch account hash: %w\n  Run 'mycase auth --broker schwab' to refresh tokens", err)
	}

	return schwab.NewBroker(client, hash), nil
}
