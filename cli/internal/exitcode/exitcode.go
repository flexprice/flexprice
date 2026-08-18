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

// Error carries an explicit exit code out of a package that cannot depend on
// internal/client. Without it, errors raised before any HTTP call — a missing
// profile, an unparseable --output — all exited 1, so a script could not tell
// "needs login" from any other failure.
type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }
func (e *Error) ExitCode() int { return e.Code }

func Wrap(code int, err error) *Error { return &Error{Code: code, Err: err} }
