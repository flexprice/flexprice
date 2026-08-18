package main

import (
	"fmt"
	"os"

	"github.com/flexprice/cli/internal/cmd"
)

// version is set by goreleaser at build time.
var version = "dev"

func main() {
	root := cmd.NewRootCommand(version)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
