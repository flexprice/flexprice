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
)
