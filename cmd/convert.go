package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/raghavkgarg/mycase/pkg/excel"
	"github.com/urfave/cli/v3"
)

var ConvertCommand = &cli.Command{
	Name:  "convert",
	Usage: "Convert Excel (.xlsx) portfolio/ETF holdings file to clean CSV",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Usage: "Path to input Excel (.xlsx) file"},
		&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "Path to output CSV file"},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		input := c.String("file")
		if input == "" && c.Args().Len() > 0 {
			input = c.Args().First()
		}
		if input == "" {
			return fmt.Errorf("missing input file. Usage: mycase convert --file path/to/file.xlsx [output.csv]")
		}

		output := c.String("output")
		if output == "" && c.Args().Len() > 1 {
			output = c.Args().Get(1)
		}

		count, err := excel.ConvertXLSXToCSV(input, output)
		if err != nil {
			return fmt.Errorf("converting Excel to CSV: %w", err)
		}

		if output == "" {
			ext := filepath.Ext(input)
			output = strings.TrimSuffix(input, ext) + ".csv"
		}

		fmt.Printf("Successfully converted %s -> %s (%d constituents extracted)\n", input, output, count)
		return nil
	},
}
