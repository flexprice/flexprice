package clock

import "time"

// Now returns the current UTC time truncated to microseconds, the precision
// Postgres stores. Times must be born at storage precision: lib/pq rounds
// nanoseconds half-up on write, so an untruncated instant can read back later
// than the value in memory and break exact date comparisons.
//
// Business-layer code must use this instead of time.Now() (enforced by
// loglint LL010). Raw time.Now() remains fine for elapsed-time measurement
// in infra packages, where the monotonic clock reading matters.
func Now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

// Mono returns the current time with the monotonic clock reading intact, for
// elapsed-time measurement (time.Since anchors, deadlines) in business code,
// where Now's truncation would drop the reading and expose the measurement to
// wall-clock steps. Never store it or compare it with stored instants.
func Mono() time.Time {
	return time.Now()
}
