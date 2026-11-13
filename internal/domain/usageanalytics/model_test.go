package usageanalytics

import (
	"testing"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestParseKey(t *testing.T) {
	tests := []struct {
		in      string
		want    Key
		wantErr bool
	}{
		{in: "properties.user_id", want: Key{Kind: KeyProperty, Property: "user_id"}},
		{in: "properties.model name", want: Key{Kind: KeyProperty, Property: "model name"}},
		{in: "properties.http.status", want: Key{Kind: KeyProperty, Property: "http.status"}},
		{in: "source", want: Key{Kind: KeySource}},
		{in: "meter_id", want: Key{Kind: KeyMeterID}},
		{in: "feature_id", want: Key{Kind: KeyFeatureID}},
		{in: "sub_line_item_id", want: Key{Kind: KeySubLineItemID}},
		{in: "month", want: Key{Kind: KeyMonth}},
		{in: "properties.", wantErr: true},
		{in: "properties.a'b", wantErr: true},
		{in: `properties.a\b`, wantErr: true},
		{in: "customer_id", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseKey(tt.in)
			if tt.wantErr {
				require.True(t, ierr.IsValidation(err), "want validation error, got %v", err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.in, got.String())
		})
	}
}

func TestQueryValidate(t *testing.T) {
	user := Key{Kind: KeyProperty, Property: "user"}
	month := Key{Kind: KeyMonth}
	valid := func() *Query {
		return &Query{
			Start:    time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			End:      time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
			Timezone: time.UTC,
			GroupBy:  []Key{user},
			Sort:     Sort{Field: SortAmount, Desc: true},
			Limit:    DefaultLimit,
		}
	}
	tests := []struct {
		name   string
		mutate func(q *Query)
		ok     bool
	}{
		{name: "valid", mutate: func(q *Query) {}, ok: true},
		{name: "month as column", mutate: func(q *Query) { q.Columns = &month }, ok: true},
		{name: "end before start", mutate: func(q *Query) { q.End = q.Start }},
		{name: "no group by", mutate: func(q *Query) { q.GroupBy = nil }},
		{name: "four group by keys", mutate: func(q *Query) {
			q.GroupBy = []Key{user, {Kind: KeySource}, {Kind: KeyMeterID}, {Kind: KeyFeatureID}}
		}},
		{name: "repeated group by key", mutate: func(q *Query) { q.GroupBy = []Key{user, user} }},
		{name: "month in group by", mutate: func(q *Query) { q.GroupBy = []Key{month} }},
		{name: "filter without values", mutate: func(q *Query) { q.Filters = []Filter{{Key: user, Op: FilterIn}} }},
		{name: "unknown filter op", mutate: func(q *Query) { q.Filters = []Filter{{Key: user, Op: "eq", Values: []string{"a"}}} }},
		{name: "unknown metric", mutate: func(q *Query) { q.Metrics = []Metric{{Type: "sum", Key: user}} }},
		{name: "column sort without columns", mutate: func(q *Query) { q.Sort = Sort{Field: SortColumn, Column: "run"} }},
		{name: "limit too high", mutate: func(q *Query) { q.Limit = MaxLimit + 1 }},
		{name: "zero limit", mutate: func(q *Query) { q.Limit = 0 }},
		{name: "negative offset", mutate: func(q *Query) { q.Offset = -1 }},
		{name: "no timezone", mutate: func(q *Query) { q.Timezone = nil }},
		{name: "too many filters", mutate: func(q *Query) {
			for i := 0; i <= MaxFilters; i++ {
				q.Filters = append(q.Filters, Filter{Key: user, Op: FilterIn, Values: []string{"a"}})
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := valid()
			tt.mutate(q)
			err := q.Validate()
			if tt.ok {
				require.NoError(t, err)
				return
			}
			require.True(t, ierr.IsValidation(err), "want validation error, got %v", err)
		})
	}
}

func TestValidateRejectsKeysParseKeyCannotProduce(t *testing.T) {
	base := func() *Query {
		return &Query{
			Start: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
			Timezone: time.UTC, GroupBy: []Key{{Kind: KeySource}}, Sort: Sort{Field: SortAmount}, Limit: 10,
		}
	}
	unknown := Key{Kind: "unknown"}
	unsafe := Key{Kind: KeyProperty, Property: "x') OR 1=1 --"}
	for name, mutate := range map[string]func(q *Query){
		"unknown kind in group_by":  func(q *Query) { q.GroupBy = []Key{unknown} },
		"unknown kind in columns":   func(q *Query) { q.Columns = &unknown },
		"unsafe property in filter": func(q *Query) { q.Filters = []Filter{{Key: unsafe, Op: FilterIn, Values: []string{"a"}}} },
		"unsafe property in metric": func(q *Query) { q.Metrics = []Metric{{Type: MetricDistinct, Key: unsafe}} },
	} {
		t.Run(name, func(t *testing.T) {
			q := base()
			mutate(q)
			require.True(t, ierr.IsValidation(q.Validate()))
		})
	}
}

func TestWindows(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	la, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)
	scl, err := time.LoadLocation("America/Santiago")
	require.NoError(t, err)
	// Santiago skips midnight on 2026-09-06: the day starts at 01:00 -03.
	sep6 := time.Date(2026, 9, 6, 1, 0, 0, 0, scl)
	day := func(tz *time.Location, y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, tz)
	}

	tests := []struct {
		name  string
		start time.Time
		end   time.Time
		size  types.WindowSize
		tz    *time.Location
		want  []time.Time
	}{
		{
			// 2026-10-01 is a Thursday; its week starts on Sunday Sep 27.
			name: "weeks start on Sunday", start: day(time.UTC, 2026, 10, 1), end: day(time.UTC, 2026, 10, 12),
			size: types.WindowSizeWeek, tz: time.UTC,
			want: []time.Time{day(time.UTC, 2026, 9, 27), day(time.UTC, 2026, 10, 4), day(time.UTC, 2026, 10, 11)},
		},
		{
			// 2026-10-31 20:00 UTC is already Nov 1 in India.
			name: "months follow the query timezone", start: time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC), end: day(ist, 2026, 12, 1),
			size: types.WindowSizeMonth, tz: ist,
			want: []time.Time{day(ist, 2026, 11, 1)},
		},
		{
			// Los Angeles leaves daylight saving on Nov 1: windows stay on local midnight.
			name: "days across a DST change", start: day(la, 2026, 10, 31), end: day(la, 2026, 11, 3),
			size: types.WindowSizeDay, tz: la,
			want: []time.Time{day(la, 2026, 10, 31), day(la, 2026, 11, 1), day(la, 2026, 11, 2)},
		},
		{
			name: "days across a skipped midnight", start: day(scl, 2026, 9, 5), end: day(scl, 2026, 9, 8),
			size: types.WindowSizeDay, tz: scl,
			want: []time.Time{day(scl, 2026, 9, 5), sep6, day(scl, 2026, 9, 7)},
		},
		{
			// Sunday 2026-09-06 starts at the skipped midnight; the next week is back on 00:00.
			name: "weeks across a skipped midnight", start: day(scl, 2026, 9, 2), end: day(scl, 2026, 9, 14),
			size: types.WindowSizeWeek, tz: scl,
			want: []time.Time{day(scl, 2026, 8, 30), sep6, day(scl, 2026, 9, 13)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &Query{Start: tt.start, End: tt.end, WindowSize: tt.size, Timezone: tt.tz}
			got := q.Windows()
			require.Len(t, got, len(tt.want))
			for i := range tt.want {
				require.True(t, tt.want[i].Equal(got[i]), "window %d: want %s got %s", i, tt.want[i], got[i])
			}
		})
	}
}

func TestValidateWindowSize(t *testing.T) {
	q := func(size types.WindowSize, days int) *Query {
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		return &Query{
			Start: start, End: start.AddDate(0, 0, days), Timezone: time.UTC, WindowSize: size,
			GroupBy: []Key{{Kind: KeySource}}, Sort: Sort{Field: SortAmount}, Limit: 10,
		}
	}
	require.NoError(t, q(types.WindowSizeDay, MaxWindows).Validate())
	require.True(t, ierr.IsValidation(q(types.WindowSizeDay, MaxWindows+1).Validate()), "more than MaxWindows days")
	require.NoError(t, q(types.WindowSizeWeek, MaxWindows+1).Validate())
	require.True(t, ierr.IsValidation(q(types.WindowSizeHour, 1).Validate()), "only DAY, WEEK and MONTH")
}
