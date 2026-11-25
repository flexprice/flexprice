package sample

import "time"

func names(priceMap map[string]int) { // want "CL007: `priceMap` is named after its type; name it for what it holds, such as `priceByID`"
	metersSlice := []string{} // want "CL007: `metersSlice` is named after its type; name it for what it holds, such as `meters`"
	_ = metersSlice
	pricesByID := map[string]int{}
	_ = pricesByID
	bitmap := 0
	_ = bitmap
	_ = len(priceMap)
}

func truncates(at time.Time) time.Time {
	return at.Truncate(time.Microsecond) // want `CL005: business code never truncates instants`
}

func roundsToSecond(at time.Time) time.Time {
	return at.Truncate(time.Second)
}

func roundsDuration(elapsed time.Duration) time.Duration {
	return elapsed.Truncate(time.Microsecond)
}
