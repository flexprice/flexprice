package service

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

const (
	asOfMaxAge    = 24 * time.Hour
	asOfClockSkew = time.Minute
)

// parsePageAsOf reads the as_of of a board response for a page request (already microsecond-truncated by the request
// binding): none means now, otherwise it must be within the last 24 hours and not later than now plus a little clock skew.
func parsePageAsOf(raw *time.Time, now time.Time) (time.Time, error) {
	if raw == nil {
		return now, nil
	}
	asOf := *raw
	if now.Sub(asOf) > asOfMaxAge {
		return time.Time{}, ierr.NewError("as_of expired").WithHint("as_of is older than 24 hours. Reload the board.").Mark(ierr.ErrValidation)
	}
	if asOf.After(now.Add(asOfClockSkew)) {
		return time.Time{}, ierr.NewError("as_of in the future").WithHint("as_of cannot be later than now.").Mark(ierr.ErrValidation)
	}
	return asOf.UTC(), nil
}
