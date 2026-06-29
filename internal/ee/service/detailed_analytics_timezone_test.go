package service

import (
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
)

// GetDetailedAnalytics must auto-derive the bucketing timezone from the primary
// customer record (never from the request), and must degrade to UTC — without
// error — when the customer can't be resolved.
func (s *MeterUsageServiceSuite) TestGetDetailedAnalytics_AutoDerivesCustomerTimezone() {
	ctx := s.GetContext()

	istCustomer := &customer.Customer{
		ID:         "cust_ist_tz",
		ExternalID: "ext_cust_ist_tz",
		Name:       "IST Customer",
		Timezone:   "Asia/Kolkata",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, istCustomer))

	newParams := func(externalID string) *events.MeterUsageDetailedAnalyticsParams {
		return &events.MeterUsageDetailedAnalyticsParams{
			TenantID:           types.GetTenantID(ctx),
			EnvironmentID:      types.GetEnvironmentID(ctx),
			ExternalCustomerID: externalID,
			StartTime:          s.periodStart,
			EndTime:            s.periodEnd,
			WindowSize:         types.WindowSizeDay,
			AggregationTypes:   []types.AggregationType{types.AggregationSum},
		}
	}

	s.Run("derives the customer's timezone into params", func() {
		params := newParams("ext_cust_ist_tz")
		_, err := s.svc.GetDetailedAnalytics(ctx, params)
		s.NoError(err)
		s.Equal("Asia/Kolkata", params.Timezone,
			"timezone must be auto-derived from the customer record")
	})

	s.Run("unknown customer leaves timezone empty (UTC) and does not error", func() {
		params := newParams("ext_does_not_exist")
		_, err := s.svc.GetDetailedAnalytics(ctx, params)
		s.NoError(err)
		s.Equal("", params.Timezone, "unresolved customer must leave tz empty -> UTC bucketing")
	})

	s.Run("customer without a timezone stays UTC", func() {
		// s.customer (ext_cust_1) is created without a timezone in setup.
		params := newParams(s.customer.ExternalID)
		_, err := s.svc.GetDetailedAnalytics(ctx, params)
		s.NoError(err)
		s.Equal("", params.Timezone)
	})
}
