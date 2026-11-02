package clock

import (
	"reflect"
	"time"
)

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

// TruncateTimes truncates, in place, every time.Time reachable through ptr to
// microseconds. It is for ingress boundaries only (HTTP binding, Temporal
// activity inputs), so instants that business code did not create with Now
// still arrive at storage precision.
func TruncateTimes(ptr any) {
	truncateTimes(reflect.ValueOf(ptr))
}

var timeType = reflect.TypeOf(time.Time{})

func truncateTimes(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			truncateTimes(v.Elem())
		}
	case reflect.Struct:
		if v.Type() == timeType {
			if v.CanSet() {
				t := v.Interface().(time.Time)
				v.Set(reflect.ValueOf(t.Truncate(time.Microsecond)))
			}
			return
		}
		for i := 0; i < v.NumField(); i++ {
			truncateTimes(v.Field(i))
		}
	case reflect.Slice, reflect.Array:
		switch v.Type().Elem().Kind() {
		case reflect.Struct, reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Array, reflect.Map:
		default:
			// Scalar elements hold no times; skip walking large byte or string payloads.
			return
		}
		for i := 0; i < v.Len(); i++ {
			truncateTimes(v.Index(i))
		}
	case reflect.Map:
		if !v.CanInterface() {
			return
		}
		for iter := v.MapRange(); iter.Next(); {
			mv := iter.Value()
			if mv.Kind() == reflect.Pointer {
				truncateTimes(mv)
			} else if mv.Type() == timeType {
				t := mv.Interface().(time.Time)
				v.SetMapIndex(iter.Key(), reflect.ValueOf(t.Truncate(time.Microsecond)))
			}
		}
	case reflect.Interface:
		// Interface elements are only addressable through a pointer.
		if !v.IsNil() && v.Elem().Kind() == reflect.Pointer {
			truncateTimes(v.Elem())
		}
	}
}
