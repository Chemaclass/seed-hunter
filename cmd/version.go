package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Version is the seed-hunter binary version. The default "dev" indicates
// an untagged local build (`go build .`); release builds inject the real
// version via the linker:
//
//	go build -ldflags "-X github.com/Chemaclass/seed-hunter/cmd.Version=0.1.0" .
//
// The Makefile release target does this automatically — see `make release`.
var Version = "dev"

// Commit is the git commit SHA the binary was built from. Like Version,
// it is injected at build time via -ldflags. Defaults to "unknown" so a
// plain `go build .` still produces a usable binary.
var Commit = "unknown"

// BuildDate is the build timestamp (UTC, RFC 3339). Injected at build
// time via -ldflags.
var BuildDate = "unknown"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the seed-hunter version, commit, and build date",
	Run: func(cmd *cobra.Command, _ []string) {
		fmt.Fprintf(cmd.OutOrStdout(),
			"seed-hunter %s\n  commit:     %s\n  build date: %s\n  go:         %s\n  platform:   %s/%s\n",
			Version, Commit, BuildDate, runtime.Version(), runtime.GOOS, runtime.GOARCH,
		)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate("seed-hunter {{.Version}}\n")
}
