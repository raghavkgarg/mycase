package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/server"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// ServeCommand starts the web dashboard HTTP server.
var ServeCommand = &cli.Command{
	Name:  "serve",
	Usage: "Start the web dashboard server",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "port", Value: "8080", Usage: "HTTP port"},
		&cli.BoolFlag{Name: "live", Usage: "Use live broker (default: mock)"},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		b, err := newBroker(c.Bool("live"))
		if err != nil {
			return fmt.Errorf("creating broker: %w", err)
		}

		var dc *cache.Cache
		if cc, err := cache.Open("data/cache.db"); err == nil {
			yfinance.SetCache(cc)
			dc = cc
			defer dc.Close()
		}

		alertCfg, _ := config.LoadAlertConfig("config/pipeline.yaml")

		addr := ":" + c.String("port")
		fmt.Printf("Dashboard running at http://localhost%s\n", addr)

		srv := server.New(b, dc, alertCfg, server.WithFetcher(newDataRouter()))
		return srv.ListenAndServe(ctx, addr)
	},
}
