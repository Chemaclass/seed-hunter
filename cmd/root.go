// Package cmd implements the seed-hunter command-line interface.
package cmd

import (
	"fmt"
	"os"
)

// Execute is the entry point for the CLI. It dispatches to the appropriate
// subcommand based on os.Args. Subcommands and flag wiring will be added in
// later phases; for now this prints a placeholder help message so the binary
// builds and runs end-to-end.
func Execute() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help", "help":
			printHelp()
			return
		}
	}
	printHelp()
}

func printHelp() {
	fmt.Println(`seed-hunter — educational BIP-39 brute-force demo

Usage:
  seed-hunter [command]

Available commands:
  run     Start the brute-force loop (not yet implemented)
  stats   Show progress summary from the database (not yet implemented)
  reset   Clear attempt history (not yet implemented)

Use "seed-hunter [command] --help" for more information about a command.`)
}
