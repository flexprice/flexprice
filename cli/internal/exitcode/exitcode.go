// Package exitcode defines the CLI's stable exit codes. These are a public
// contract: scripts depend on them, so values never change.
package exitcode

const (
	OK          = 0
	Generic     = 1
	Usage       = 2
	Auth        = 3
	NotFound    = 4
	RateLimited = 5

	// Interrupted follows the shell convention of 128 + SIGINT(2). It is
	// additive: no existing value changes.
	Interrupted = 130
)
