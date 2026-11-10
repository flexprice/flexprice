package costanalytics

import (
	"testing"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
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
			Sort:     Sort{Field: SortTotal, Desc: true},
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

func TestBillingPeriodSegmentCoversWholePeriod(t *testing.T) {
	oct1 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	whole := BillingPeriodSegment{PeriodStart: oct1, PeriodEnd: nov1, Start: oct1, End: nov1}
	require.True(t, whole.CoversWholePeriod())

	partial := whole
	partial.Start = oct1.AddDate(0, 0, 7)
	require.False(t, partial.CoversWholePeriod())
}
