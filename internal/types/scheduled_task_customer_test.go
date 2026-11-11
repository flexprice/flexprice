package types

import (
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
	require.Nil(t, emptyFilters.Filters)
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
