package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/flexprice/cli/internal/cmd"
	"github.com/flexprice/cli/internal/exitcode"
	"github.com/flexprice/cli/internal/ui"
)

// version is set by goreleaser at build time.
var version = "dev"

func main() {
	// Reaches client.Do, so an in-flight request is abandoned rather than
	// waited out.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := cmd.NewRootCommand(version)
	err := root.ExecuteContext(ctx)

	// The reason this signal handling exists: a spinner hides the cursor, and
	// dying without restoring it leaves the user's shell without one.
	cmd.RestoreTerminal()

	if err == nil {
		return
	}

	out := ui.FromEnv(false, false, true)

	// Ctrl-C, not a failure worth diagnostics.
	if errors.Is(ctx.Err(), context.Canceled) {
		out.Failure(errors.New("cancelled"))
		os.Exit(exitcode.Interrupted)
	}

	out.Failure(err)

	// Any error carrying an exit code decides its own: *client.APIError for
	// HTTP failures, *exitcode.Error for anything raised before a request.
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		os.Exit(coded.ExitCode())
	}
	os.Exit(exitcode.Generic)
}
