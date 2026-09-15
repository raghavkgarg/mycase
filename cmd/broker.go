package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/broker/schwab"
	"github.com/raghavkgarg/mycase/pkg/broker/zerodha"
	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/datafetcher"
	"github.com/raghavkgarg/mycase/pkg/edgar"
)

const defaultsFileName = "defaults.json"

// defaultsPath returns the resolved path to config/defaults.json.
func defaultsPath() string { return config.Path(defaultsFileName) }

// newBroker creates the appropriate broker based on config/defaults.json.
// If live is false or credentials are missing/invalid, returns MockBroker.
func newBroker(live bool) (broker.Broker, error) {
	defaults := config.LoadUserDefaults(defaultsPath())
	return newBrokerByName(defaults.Broker, live)
}

// newBrokerByName creates a broker by explicit name.
// If live is false, returns MockBroker regardless of broker name.
func newBrokerByName(name string, live bool) (broker.Broker, error) {
	if !live {
		return &broker.MockBroker{}, nil
	}

	switch name {
	case "schwab":
		return newSchwabBroker()
	case "zerodha":
		return zerodha.New(true, config.Path("config.json")), nil
	case "", "mock":
		return &broker.MockBroker{}, nil
	default:
		return nil, fmt.Errorf("unsupported broker: %q (supported: schwab, zerodha, mock)", name)
	}
}

// newSchwabBroker constructs a live SchwabBroker from config files.
func newSchwabBroker() (broker.Broker, error) {
	defaults := config.LoadUserDefaults(defaultsPath())

	schwabConfigPath := config.Path("schwab.json")
	schwabTokenPath := config.Path("schwab_token.json")

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

// newSchwabClient creates just a *schwab.Client (no broker/account needed) for market data.
// Returns nil if Schwab credentials are not configured or tokens are invalid (not an error —
// the Router will fall back to Yahoo Finance).
func newSchwabClient() *schwab.Client {
	defaults := config.LoadUserDefaults(defaultsPath())

	schwabConfigPath := config.Path("schwab.json")
	schwabTokenPath := config.Path("schwab_token.json")

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
		return nil
	}

	tokenMgr := schwab.NewTokenManager(app, schwabTokenPath)
	return schwab.NewClient(tokenMgr)
}

// newDataRouter creates a datafetcher.Router, wiring Schwab if credentials are
// available and (opt-in) EDGAR as a US-fundamentals overlay when enabled in
// config/defaults.json.
func newDataRouter() *datafetcher.Router {
	router := datafetcher.NewRouter(newSchwabClient())
	if e := newEDGARClient(); e != nil {
		router = router.WithEDGAR(e)
	}
	return router
}

// newEDGARClient constructs a *edgar.Client from config/defaults.json when the
// EDGAR fundamentals source is enabled. Returns nil (not an error) when disabled
// or misconfigured — EDGAR is a strict enrichment, so any setup problem simply
// leaves the Schwab-only fundamentals path in place. The User-Agent honors the
// MYCASE_EDGAR_USER_AGENT env override (env > config), per the config precedence
// convention.
func newEDGARClient() *edgar.Client {
	defaults := config.LoadUserDefaults(defaultsPath())
	ec := defaults.EDGAR
	if !ec.Enabled {
		return nil
	}

	ua := ec.UserAgent
	if env := os.Getenv("MYCASE_EDGAR_USER_AGENT"); env != "" {
		ua = env
	}

	var opts []edgar.Option
	if ec.FactsTTLDays > 0 {
		opts = append(opts, edgar.WithFactsTTL(time.Duration(ec.FactsTTLDays)*24*time.Hour))
	}
	if ec.CIKTTLDays > 0 {
		opts = append(opts, edgar.WithCIKTTL(time.Duration(ec.CIKTTLDays)*24*time.Hour))
	}

	client, err := edgar.NewClient(ua, cache.GetDB(), opts...)
	if err != nil {
		// Misconfigured UA is the common case: log to stderr and disable EDGAR
		// rather than failing the command. Do not abort the pipeline.
		fmt.Fprintf(os.Stderr, "[edgar] disabled: %v\n", err)
		return nil
	}
	return client
}
