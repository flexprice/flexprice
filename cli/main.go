package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/flexprice/cli/internal/client"
	"github.com/flexprice/cli/internal/cmd"
	"github.com/flexprice/cli/internal/exitcode"
)

// version is set by goreleaser at build time.
var version = "dev"

func main() {
	root := cmd.NewRootCommand(version)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)

		var apiErr *client.APIError
		if errors.As(err, &apiErr) {
			os.Exit(apiErr.ExitCode())
		}
		os.Exit(exitcode.Generic)
	}
}
