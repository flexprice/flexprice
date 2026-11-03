package dto

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestGetUsageAnalyticsV2Request_ApplyDefaults(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	r := &GetUsageAnalyticsV2Request{ExternalCustomerID: "c", GroupBy: []string{"properties.app"}}
	r.ApplyDefaults(now)

	require.Equal(t, now, r.EndTime)
	require.Equal(t, now.Add(-7*24*time.Hour), r.StartTime)
	require.Equal(t, AnalyticsV2DefaultLimit, r.Limit)
	require.Equal(t, AnalyticsV2SortByTotal, r.Sort.By)
	require.Equal(t, types.SortDirectionDesc, r.Sort.Order)
	require.NoError(t, r.Validate())
}

func TestGetUsageAnalyticsV2Request_Validate(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	base := func() *GetUsageAnalyticsV2Request {
		return &GetUsageAnalyticsV2Request{
			ExternalCustomerID: "cust",
			StartTime:          time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			EndTime:            time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			GroupBy:            []string{"properties.app"},
		}
	}

	tests := []struct {
		name    string
		mutate  func(r *GetUsageAnalyticsV2Request)
		wantErr string
	}{
		{"valid minimal", func(r *GetUsageAnalyticsV2Request) {}, ""},
		{"valid zomato table", func(r *GetUsageAnalyticsV2Request) {
			r.Columns = "properties.type"
			r.Sort = &AnalyticsV2Sort{By: AnalyticsV2SortByColumn, Column: "run"}
			r.Filters = []AnalyticsV2Filter{{Key: "properties.app", Op: AnalyticsV2FilterOpNotIn, Values: []string{"archive"}}}
			r.Metrics = []string{"distinct:properties.owner"}
			r.WindowSize = types.WindowSizeDay
		}, ""},
		{"valid month columns", func(r *GetUsageAnalyticsV2Request) { r.Columns = AnalyticsV2KeyMonth }, ""},
		{"valid expand on meter rows", func(r *GetUsageAnalyticsV2Request) {
			r.GroupBy = []string{"meter_id", "properties.app"}
			r.Expand = []string{"meter", "price"}
		}, ""},
		{"valid usage sort with one meter", func(r *GetUsageAnalyticsV2Request) {
			r.MeterIDs = []string{"m1"}
			r.Sort = &AnalyticsV2Sort{By: AnalyticsV2SortByUsage}
		}, ""},
		{"missing customer", func(r *GetUsageAnalyticsV2Request) { r.ExternalCustomerID = "" }, "ExternalCustomerID"},
		{"end before start", func(r *GetUsageAnalyticsV2Request) { r.EndTime = r.StartTime }, "end_time must be after start_time"},
		{"no group_by", func(r *GetUsageAnalyticsV2Request) { r.GroupBy = nil }, "GroupBy"},
		{"four group_by keys", func(r *GetUsageAnalyticsV2Request) {
			r.GroupBy = []string{"source", "meter_id", "properties.a", "properties.b"}
		}, "group_by needs 1 to 3 keys"},
		{"duplicate group_by", func(r *GetUsageAnalyticsV2Request) {
			r.GroupBy = []string{"properties.app", "properties.app"}
		}, "duplicate"},
		{"unknown group_by", func(r *GetUsageAnalyticsV2Request) { r.GroupBy = []string{"customer"} }, "not supported"},
		{"bad property name", func(r *GetUsageAnalyticsV2Request) { r.GroupBy = []string{"properties.a'b"} }, "not supported"},
		{"columns also a row key", func(r *GetUsageAnalyticsV2Request) { r.Columns = "properties.app" }, "cannot also be a group_by key"},
		{"bad filter op", func(r *GetUsageAnalyticsV2Request) {
			r.Filters = []AnalyticsV2Filter{{Key: "properties.app", Op: "eq", Values: []string{"x"}}}
		}, "not supported"},
		{"filter without values", func(r *GetUsageAnalyticsV2Request) {
			r.Filters = []AnalyticsV2Filter{{Key: "properties.app", Op: AnalyticsV2FilterOpIn}}
		}, "Values"},
		{"limit too high", func(r *GetUsageAnalyticsV2Request) { r.Limit = 101 }, "limit must be between"},
		{"negative offset", func(r *GetUsageAnalyticsV2Request) { r.Offset = -1 }, "offset cannot be negative"},
		{"column sort without columns", func(r *GetUsageAnalyticsV2Request) {
			r.Sort = &AnalyticsV2Sort{By: AnalyticsV2SortByColumn, Column: "run"}
		}, "sort by column needs"},
		{"usage sort across meters", func(r *GetUsageAnalyticsV2Request) {
			r.Sort = &AnalyticsV2Sort{By: AnalyticsV2SortByUsage}
		}, "single meter"},
		{"bad sort order", func(r *GetUsageAnalyticsV2Request) {
			r.Sort = &AnalyticsV2Sort{By: AnalyticsV2SortByTotal, Order: "up"}
		}, "sort order"},
		{"too many daily windows", func(r *GetUsageAnalyticsV2Request) {
			r.StartTime = r.EndTime.AddDate(-2, 0, 0)
			r.WindowSize = types.WindowSizeDay
		}, "use a larger window_size"},
		{"valid expand of price, plan, addon, subscription on any rows", func(r *GetUsageAnalyticsV2Request) {
			r.Expand = []string{"price", "plan", "addon", "subscription"}
		}, ""},
		{"expand meter without meter rows", func(r *GetUsageAnalyticsV2Request) { r.Expand = []string{"meter"} }, "needs meter_id"},
		{"unknown expand", func(r *GetUsageAnalyticsV2Request) { r.Expand = []string{"customer"} }, "not supported"},
		{"metric on row key", func(r *GetUsageAnalyticsV2Request) { r.Metrics = []string{"distinct:properties.app"} }, "always 1"},
		{"unknown metric", func(r *GetUsageAnalyticsV2Request) { r.Metrics = []string{"avg:properties.x"} }, "not supported"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := base()
			tc.mutate(r)
			r.ApplyDefaults(now)
			err := r.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
