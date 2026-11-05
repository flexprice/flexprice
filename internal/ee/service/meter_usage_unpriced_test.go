package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// A cost meter shares the billed event but is not on the subscription: revenue
// analytics drops it, unpriced analytics returns only this customer's usage on it.
func (s *MeterUsageServiceSuite) TestGetUnpricedMeterAnalytics_IncludesMetersOffSubscription() {
	ctx := s.GetContext()

	s.createLineItemForMeter(ctx, "li_api", s.meterAPI.ID, s.priceAPI.ID)
	vendor := s.createMeterWithAggregation(ctx, "meter_vendor", "api_call", types.AggregationSum)

	other := &customer.Customer{
		ID:         "cust_other",
		ExternalID: "ext_cust_other",
		Name:       "Other Customer",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, other))

	ts := s.periodStart.Add(48 * time.Hour)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, ts, 10)
	s.insertMeterUsage(ctx, vendor.ID, s.customer.ExternalID, ts, 10)
	s.insertMeterUsage(ctx, vendor.ID, other.ExternalID, ts, 99)

	newParams := func(meterIDs ...string) *events.MeterUsageDetailedAnalyticsParams {
		return &events.MeterUsageDetailedAnalyticsParams{
			TenantID:           types.GetTenantID(ctx),
			EnvironmentID:      types.GetEnvironmentID(ctx),
			ExternalCustomerID: s.customer.ExternalID,
			MeterIDs:           meterIDs,
			StartTime:          s.periodStart,
			EndTime:            s.periodEnd,
			GroupBy:            []string{"meter_id"},
		}
	}

	s.Run("revenue analytics drops the off-subscription meter", func() {
		resp, err := s.svc.GetDetailedAnalytics(ctx, newParams(vendor.ID))
		s.Require().NoError(err)
		s.Empty(resp.Items)
	})

	s.Run("unpriced analytics returns the customer's usage on it", func() {
		resp, err := s.svc.GetUnpricedMeterAnalytics(ctx, newParams(vendor.ID))
		s.Require().NoError(err)
		s.Require().Len(resp.Items, 1)
		s.Equal(vendor.ID, resp.Items[0].MeterID)
		s.True(resp.Items[0].TotalUsage.Equal(decimal.NewFromInt(10)), "got %s", resp.Items[0].TotalUsage)
		s.True(resp.TotalCost.IsZero())
	})

	s.Run("requires an explicit meter set", func() {
		_, err := s.svc.GetUnpricedMeterAnalytics(ctx, newParams())
		s.Require().Error(err)
		s.True(ierr.IsValidation(err))
	})
}
