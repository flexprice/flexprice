package service

import "time"

func rawNow() time.Time {
	return time.Now() // want `LL010: time.Now\(\) banned in business layers — use types\.Now\(\) \(born at Postgres microsecond precision\)`
}

func sinceIsFine(start time.Time) time.Duration {
	return time.Since(start)
}
