package sample

import "time"

// time.Now() is allowed outside the business-layer packages (LL010).
func infraNow() time.Time {
	return time.Now()
}
