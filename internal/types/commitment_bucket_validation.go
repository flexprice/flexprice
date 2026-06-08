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
