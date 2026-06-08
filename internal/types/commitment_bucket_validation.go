package types

import (
	"sort"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// linearInterval is a half-open [start, end) interval on the minute-of-day axis [0, 1440).
type linearInterval struct {
	start    int
	end      int
	bucketIx int
}

// flattenToLinearIntervals projects each bucket into one or two linearInterval
// entries on [0, 1440). Midnight-wrapping buckets produce two intervals:
// [Start, 1440) and [0, End).
func (bs TimeOfDayBuckets) flattenToLinearIntervals() []linearInterval {
	out := make([]linearInterval, 0, len(bs)*2)
	for i, b := range bs {
		s := b.Start.MinuteOfDay()
		e := b.End.MinuteOfDay()
		if s == e {
			continue
		}
		if s < e {
			out = append(out, linearInterval{start: s, end: e, bucketIx: i})
			continue
		}
		out = append(out, linearInterval{start: s, end: 1440, bucketIx: i})
		out = append(out, linearInterval{start: 0, end: e, bucketIx: i})
	}
	return out
}

// ValidateNoOverlap returns an error when any two buckets overlap on the
// linear minute-of-day axis. Adjacency at exactly one boundary is allowed
// (half-open semantics: end-of-A == start-of-B is fine).
func (bs TimeOfDayBuckets) ValidateNoOverlap() error {
	intervals := bs.flattenToLinearIntervals()
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].start != intervals[j].start {
			return intervals[i].start < intervals[j].start
		}
		return intervals[i].end < intervals[j].end
	})
	for i := 1; i < len(intervals); i++ {
		if intervals[i].bucketIx == intervals[i-1].bucketIx {
			continue
		}
		if intervals[i].start < intervals[i-1].end {
			return ierr.NewError("buckets overlap").
				WithHint("Configured time-of-day buckets must not overlap").
				WithReportableDetails(map[string]interface{}{
					"bucket_a_index": intervals[i-1].bucketIx,
					"bucket_b_index": intervals[i].bucketIx,
				}).
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// ValidateWindowAlignment enforces the meter-window constraints required for
// per-bucket pricing:
//   - meter must be windowed (windowMin > 0)
//   - meter window size must be <= 60 minutes
//   - each bucket's (End-Start) must be an integer multiple of windowMin
//   - each bucket's Start must be aligned to the window grid
//
// Pass 0 for windowMin only when the line item has NO buckets — if buckets
// exist and windowMin == 0 we reject.
func (bs TimeOfDayBuckets) ValidateWindowAlignment(windowMin int) error {
	if len(bs) == 0 {
		return nil
	}
	if windowMin <= 0 {
		return ierr.NewError("buckets require a windowed meter").
			WithHint("Configure the meter with a window size before using time-of-day buckets").
			Mark(ierr.ErrValidation)
	}
	if windowMin > 60 {
		return ierr.NewError("meter window must be <= 60 minutes when using buckets").
			WithHint("Reduce the meter window size to 60 minutes or less").
			WithReportableDetails(map[string]interface{}{"window_minutes": windowMin}).
			Mark(ierr.ErrValidation)
	}
	for i, b := range bs {
		startMin := b.Start.MinuteOfDay()
		endMin := b.End.MinuteOfDay()
		duration := endMin - startMin
		if duration <= 0 {
			duration = (1440 - startMin) + endMin
		}
		if duration%windowMin != 0 {
			return ierr.NewError("bucket duration must be a multiple of the meter window").
				WithHint("Adjust the bucket so End-Start is an integer multiple of the meter window size").
				WithReportableDetails(map[string]interface{}{
					"bucket_index": i,
					"duration_min": duration,
					"window_min":   windowMin,
				}).
				Mark(ierr.ErrValidation)
		}
		if startMin%windowMin != 0 {
			return ierr.NewError("bucket start alignment error: start must be on the meter window grid").
				WithHint("Adjust the bucket Start so its minute-of-day is divisible by the meter window size").
				WithReportableDetails(map[string]interface{}{
					"bucket_index": i,
					"start_min":    startMin,
					"window_min":   windowMin,
				}).
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}
