package sample

import "time"

func suppressed(at time.Time) time.Time {
	//lint:ignore CL005 this adapter writes straight to a legacy table with no ent hook
	return at.Truncate(time.Microsecond)
}

func suppressedTrailing(at time.Time) time.Time {
	return at.Truncate(time.Microsecond) //lint:ignore CL005 same legacy table as above
}

func suppressedWithoutReason(at time.Time) time.Time {
	/*lint:ignore CL005*/                // want `CL000: give the reason after the code`
	return at.Truncate(time.Microsecond) // want `CL005: business code never truncates instants`
}
