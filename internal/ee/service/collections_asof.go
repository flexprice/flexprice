package service

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

const (
	asOfMaxAge    = 24 * time.Hour
	asOfClockSkew = time.Minute
)

// parsePageAsOf reads the as_of of a board response for a page request: empty means now, otherwise it must be RFC3339,
// within the last 24 hours and not later than now plus a little clock skew.
func parsePageAsOf(raw string, now time.Time) (time.Time, error) {
	if raw == "" {
		return now, nil
	}
	asOf, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, ierr.NewError("invalid as_of").WithHint("as_of must be the RFC3339 as_of of the board response.").Mark(ierr.ErrValidation)
	}
	if now.Sub(asOf) > asOfMaxAge {
		return time.Time{}, ierr.NewError("as_of expired").WithHint("as_of is older than 24 hours. Reload the board.").Mark(ierr.ErrValidation)
	}
	if asOf.After(now.Add(asOfClockSkew)) {
		return time.Time{}, ierr.NewError("as_of in the future").WithHint("as_of cannot be later than now.").Mark(ierr.ErrValidation)
	}
	return asOf.UTC().Truncate(time.Microsecond), nil
}
