package types

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestSanitizeExportCustomerFilter(t *testing.T) {
	require.Nil(t, SanitizeExportCustomerFilter(nil))

	archived := StatusArchived
	in := &CustomerFilter{
		QueryFilter:     &QueryFilter{Limit: lo.ToPtr(5), Offset: lo.ToPtr(2), Status: &archived, Sort: lo.ToPtr("name"), Order: lo.ToPtr("asc"), Expand: lo.ToPtr("x")},
		TimeRangeFilter: &TimeRangeFilter{},
		MetadataFilter:  &MetadataFilter{Metadata: map[string]string{"k": "v"}},
		Filters:         []*FilterCondition{{Field: lo.ToPtr("name")}},
		Sort:            []*SortCondition{{Field: "name"}},
		CustomerIDs:     []string{"c1"},
		ExternalIDs:     []string{"e1"},
		ExternalID:      "e1",
		Email:           "a@b.co",
		BillingEntityID: "be1",
	}
	out := SanitizeExportCustomerFilter(in)

	require.Equal(t, &QueryFilter{Status: &archived}, out.QueryFilter)
	require.Nil(t, out.TimeRangeFilter)
	require.Nil(t, out.Sort)
	require.Empty(t, out.CustomerIDs)
	require.Empty(t, out.ExternalIDs)
	require.Empty(t, out.ExternalID)
	require.Equal(t, in.Filters, out.Filters)
	require.Equal(t, in.MetadataFilter, out.MetadataFilter)
	require.Equal(t, "a@b.co", out.Email)
	require.Equal(t, "be1", out.BillingEntityID)
	require.NotNil(t, in.QueryFilter.Limit, "input untouched")

	empty := SanitizeExportCustomerFilter(&CustomerFilter{})
	require.NotNil(t, empty)
	require.Nil(t, empty.QueryFilter)

	emptyFilters := SanitizeExportCustomerFilter(&CustomerFilter{Filters: []*FilterCondition{}})
	require.NotNil(t, emptyFilters.Filters)
	require.Empty(t, emptyFilters.Filters)
}

func TestCustomerFilter_EmptyFiltersSurviveJSON(t *testing.T) {
	stored := func(in *CustomerFilter) *CustomerFilter {
		raw, err := json.Marshal(SanitizeExportCustomerFilter(in))
		require.NoError(t, err)
		var out CustomerFilter
		require.NoError(t, json.Unmarshal(raw, &out))
		return &out
	}

	explicit := stored(&CustomerFilter{Filters: []*FilterCondition{}})
	require.NotNil(t, explicit.Filters)
	require.Equal(t, "", explicit.GetStatus())

	absent := stored(&CustomerFilter{})
	require.Nil(t, absent.Filters)
	require.Equal(t, string(StatusPublished), absent.GetStatus())

	var fromNull CustomerFilter
	require.NoError(t, json.Unmarshal([]byte(`{"filters":null}`), &fromNull))
	require.Nil(t, fromNull.Filters)
}

func TestValidateExportCustomerFilter(t *testing.T) {
	require.NoError(t, ValidateExportCustomerFilter(nil))
	require.NoError(t, ValidateExportCustomerFilter(&CustomerFilter{Email: "a@b.co"}))
	for _, f := range []*CustomerFilter{
		{CustomerIDs: []string{"c1"}},
		{ExternalIDs: []string{"e1"}},
		{ExternalID: "e1"},
	} {
		require.Error(t, ValidateExportCustomerFilter(f))
	}
}

func TestExportSourceConfig_ValidateCustomerFilter(t *testing.T) {
	cf := &CustomerFilter{}
	require.NoError(t, (&ExportSourceConfig{CustomerFilter: cf}).Validate(ScheduledTaskEntityTypeCustomers))
	require.Error(t, (*ExportSourceConfig)(nil).Validate(ScheduledTaskEntityTypeCustomers))
	require.Error(t, (&ExportSourceConfig{}).Validate(ScheduledTaskEntityTypeCustomers))
	require.NoError(t, (&ExportSourceConfig{CustomerFilter: cf}).Validate(ScheduledTaskEntityTypeInvoice))
	require.Error(t, (&ExportSourceConfig{CustomerFilter: cf}).Validate(ScheduledTaskEntityTypeEvents))
	require.Error(t, (&ExportSourceConfig{ViewID: "v", CustomerFilter: cf}).Validate(ScheduledTaskEntityTypeAnalyticsView))
	require.NoError(t, ScheduledTaskEntityTypeCustomers.Validate())
	require.NoError(t, ExportOriginConsole.Validate())
	require.NoError(t, ExportOriginCustomersPage.Validate())
	require.Error(t, ExportOrigin("x").Validate())
}
