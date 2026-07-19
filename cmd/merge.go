package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/csvloader"
)

var MergeCommand = &cli.Command{
	Name:  "merge",
	Usage: "CSV merge utilities (combine candidates or update golden copy)",
	Commands: []*cli.Command{
		{
			Name:      "combine",
			Usage:     "Combine multiple candidate CSV files into one",
			ArgsUsage: "<output_csv> <input_csv1> <input_csv2> ...",
			Action: func(ctx context.Context, c *cli.Command) error {
				args := c.Args().Slice()
				if len(args) < 2 {
					return fmt.Errorf("usage: mycase merge combine <output_csv> <input_csv1> <input_csv2> ...")
				}
				outFile := args[0]
				inputFiles := args[1:]
				if err := csvloader.CombineMultipleCSVs(inputFiles, outFile); err != nil {
					return fmt.Errorf("combining CSVs: %w", err)
				}
				fmt.Printf("Successfully combined %d candidate files into %s.\n", len(inputFiles), outFile)
				return nil
			},
		},
		{
			Name:      "golden",
			Usage:     "Merge source CSV into golden copy (preserves exited tickers at 0.0000 weight)",
			ArgsUsage: "<source_csv> <destination_csv>",
			Action: func(ctx context.Context, c *cli.Command) error {
				args := c.Args().Slice()
				if len(args) < 2 {
					return fmt.Errorf("usage: mycase merge golden <source_csv> <destination_csv>")
				}
				src, dst := args[0], args[1]
				if err := csvloader.MergeGoldenCopy(src, dst); err != nil {
					return fmt.Errorf("merging golden copy: %w", err)
				}
				fmt.Printf("Successfully merged %s into %s (exited stocks preserved at 0.0000 weight).\n", src, dst)
				return nil
			},
		},
	},
}
