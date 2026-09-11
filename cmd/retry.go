package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/executor"
)

// RetryCommand retries unfulfilled orders from an error payload JSON file.
var RetryCommand = &cli.Command{
	Name:      "retry",
	Usage:     "Retry failed orders from a previous error log",
	ArgsUsage: "[path/to/Order_*.json]",
	Flags: []cli.Flag{
		&cli.BoolFlag{Name: "live", Usage: "Use live broker (default: mock)"},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		jsonPath := c.Args().First()
		liveMode := c.Bool("live")

		b, err := newBroker(liveMode)
		if err != nil {
			return fmt.Errorf("creating broker: %w", err)
		}
		reader := bufio.NewReader(os.Stdin)

		executor.ExecuteRetryPayload(jsonPath, b, reader)
		return nil
	},
}
