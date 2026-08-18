package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/flexprice/cli/internal/client"
	"github.com/flexprice/cli/internal/cmd"
	"github.com/flexprice/cli/internal/exitcode"
	"github.com/flexprice/cli/internal/ui"
)

// version is set by goreleaser at build time.
var version = "dev"

func main() {
	// NotifyContext cancels ctx on Ctrl-C. The context reaches client.Do,
	// which already accepts one, so an in-flight request is abandoned rather
	// than waited out.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := cmd.NewRootCommand(version)
	err := root.ExecuteContext(ctx)

	// Restoring the cursor is the reason this handling exists at all: a
	// spinner hides it, and a process that dies without restoring leaves the
	// user's shell with no visible cursor. This runs on every exit path,
	// including those where no spinner ever started, where it is a harmless
	// no-op sequence.
	cmd.RestoreTerminal()

	if err == nil {
		return
	}

	out := ui.FromEnv(false, false, true)

	// A cancelled context means Ctrl-C, not a failure worth a stack of
	// diagnostics. Report it plainly and use the conventional code.
	if errors.Is(ctx.Err(), context.Canceled) {
		out.Failure(errors.New("cancelled"))
		os.Exit(exitcode.Interrupted)
	}

	out.Failure(err)

	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		os.Exit(apiErr.ExitCode())
	}
	os.Exit(exitcode.Generic)
}
