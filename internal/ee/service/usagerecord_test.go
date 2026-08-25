package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/usagerecord"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type UsageRecordServiceSuite struct {
	testutil.BaseServiceTestSuite
	svc UsageRecordService
}

func TestUsageRecordService(t *testing.T) {
	suite.Run(t, new(UsageRecordServiceSuite))
}

func (s *UsageRecordServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()

	s.svc = NewUsageRecordService(ServiceParams{
		Logger:          s.GetLogger(),
		Config:          s.GetConfig(),
		DB:              s.GetDB(),
		UsageRecordRepo: s.GetStores().UsageRecordRepo,
	})
}

// seedRecord creates a published usage record for subscriptionID, with its window ending
// offsetFromNow before the suite's clock (records must have distinct windows: the store enforces
// a unique key on subscription+period).
func (s *UsageRecordServiceSuite) seedRecord(id, subscriptionID string, offsetFromNow time.Duration) {
	ctx := s.GetContext()
	end := s.GetNow().Add(-offsetFromNow)
	rec := &usagerecord.UsageRecord{
		ID:             id,
		CustomerID:     "cust_1",
		SubscriptionID: subscriptionID,
		PlanID:         "plan_1",
		Amount:         decimal.NewFromInt(10),
		Currency:       "usd",
		PeriodStart:    end.Add(-time.Hour),
		PeriodEnd:      end,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	require.NoError(s.T(), s.GetStores().UsageRecordRepo.Create(ctx, rec))
}

func (s *UsageRecordServiceSuite) TestListUsageRecords_TotalIsIndependentOfPageLimit() {
	s.seedRecord("ur_1", "sub_1", 3*time.Hour)
	s.seedRecord("ur_2", "sub_1", 2*time.Hour)
	s.seedRecord("ur_3", "sub_2", 1*time.Hour)

	resp, err := s.svc.ListUsageRecords(s.GetContext(), &types.UsageRecordFilter{
		QueryFilter: &types.QueryFilter{Limit: lo.ToPtr(1), Offset: lo.ToPtr(0)},
	})

	require.NoError(s.T(), err)
	require.Len(s.T(), resp.Items, 1, "the returned page respects the requested limit")
	require.Equal(s.T(), 3, resp.Pagination.Total, "the total must reflect every matching row, not just this page")
	require.Equal(s.T(), 1, resp.Pagination.Limit)
	// types.NewPaginationResponse reports the next page's offset (offset + limit), as every other
	// list endpoint does.
	require.Equal(s.T(), 1, resp.Pagination.Offset)
}

func (s *UsageRecordServiceSuite) TestListUsageRecords_AppliesNamedSubscriptionFilter() {
	s.seedRecord("ur_1", "sub_1", 2*time.Hour)
	s.seedRecord("ur_2", "sub_2", 1*time.Hour)

	resp, err := s.svc.ListUsageRecords(s.GetContext(), &types.UsageRecordFilter{SubscriptionID: "sub_1"})

	require.NoError(s.T(), err)
	require.Len(s.T(), resp.Items, 1)
	require.Equal(s.T(), "ur_1", resp.Items[0].ID)
	require.Equal(s.T(), 1, resp.Pagination.Total)
}

func (s *UsageRecordServiceSuite) TestListUsageRecords_DefaultsLimitWhenUnset() {
	s.seedRecord("ur_1", "sub_1", time.Hour)

	resp, err := s.svc.ListUsageRecords(s.GetContext(), &types.UsageRecordFilter{})

	require.NoError(s.T(), err)
	require.Equal(s.T(), types.NewDefaultQueryFilter().GetLimit(), resp.Pagination.Limit,
		"an unset limit falls back to the default query filter, matching every other list endpoint")
	require.Len(s.T(), resp.Items, 1)
}

func (s *UsageRecordServiceSuite) TestListUsageRecords_NilFilterGetsDefaultPagination() {
	s.seedRecord("ur_1", "sub_1", time.Hour)

	resp, err := s.svc.ListUsageRecords(s.GetContext(), nil)

	require.NoError(s.T(), err)
	require.Equal(s.T(), types.NewDefaultQueryFilter().GetLimit(), resp.Pagination.Limit)
	require.Len(s.T(), resp.Items, 1)
}

func (s *UsageRecordServiceSuite) TestListUsageRecords_RejectsAnInvalidFilterCondition() {
	_, err := s.svc.ListUsageRecords(s.GetContext(), &types.UsageRecordFilter{
		Filters: []*types.FilterCondition{
			{Field: lo.ToPtr("currency")}, // missing operator/data_type/value
		},
	})

	require.Error(s.T(), err)
	require.True(s.T(), ierr.IsValidation(err), "an incomplete DSL filter must fail validation before hitting the repo, got: %v", err)
	require.Contains(s.T(), err.Error(), "operator is required")
}

func (s *UsageRecordServiceSuite) TestListUsageRecords_UnlimitedFilterIsNotClobbered() {
	s.seedRecord("ur_1", "sub_1", time.Hour)

	filter := types.NewNoLimitUsageRecordFilter()
	resp, err := s.svc.ListUsageRecords(s.GetContext(), filter)

	require.NoError(s.T(), err)
	require.True(s.T(), filter.IsUnlimited(), "GetLimit()==0 must not replace a no-limit filter with the default page size")
	require.Equal(s.T(), 0, resp.Pagination.Limit)
	require.Len(s.T(), resp.Items, 1)
	require.Equal(s.T(), 1, resp.Pagination.Total)
}
