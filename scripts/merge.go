package main

import (
	"fmt"
	"os"
	"mycase/pkg/csvloader"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	mode := os.Args[1]
	switch mode {
	case "combine":
		if len(os.Args) < 4 {
			fmt.Println("Usage: go run scripts/merge.go combine <output_combined_csv> <input_csv1> <input_csv2> ...")
			os.Exit(1)
		}
		outFile := os.Args[2]
		inputFiles := os.Args[3:]
		if err := csvloader.CombineMultipleCSVs(inputFiles, outFile); err != nil {
			fmt.Printf("Error combining CSVs: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Successfully combined %d candidate files into %s.\n", len(inputFiles), outFile)

	case "golden":
		if len(os.Args) < 4 {
			fmt.Println("Usage: go run scripts/merge.go golden <source_csv> <destination_csv>")
			os.Exit(1)
		}
		src := os.Args[2]
		dst := os.Args[3]
		if err := csvloader.MergeGoldenCopy(src, dst); err != nil {
			fmt.Printf("Error merging golden copy: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Successfully merged %s into %s (exited stocks preserved at 0.0000 weight).\n", src, dst)

	default:
		// Fallback for legacy format: go run scripts/merge.go <source_csv> <destination_csv>
		if len(os.Args) == 3 {
			src := os.Args[1]
			dst := os.Args[2]
			if err := csvloader.MergeGoldenCopy(src, dst); err != nil {
				fmt.Printf("Error merging golden copy: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Successfully merged %s into %s (exited stocks preserved at 0.0000 weight).\n", src, dst)
			return
		}
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  go run scripts/merge.go combine <output_combined_csv> <input_csv1> <input_csv2> ...")
	fmt.Println("  go run scripts/merge.go golden <source_csv> <destination_csv>")
	fmt.Println("\nLegacy/Short format for golden copy updates:")
	fmt.Println("  go run scripts/merge.go <source_csv> <destination_csv>")
}
