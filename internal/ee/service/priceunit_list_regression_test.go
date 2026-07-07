package service

import (
	"fmt"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/priceunit"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// PriceUnitListRegressionSuite pins ListPriceUnits filter-guard behavior.
// Regressions covered:
//  1. a nil *types.PriceUnitFilter panicked inside filter.Validate()
//     (field access on a nil receiver) instead of listing with defaults;
//  2. a no-limit filter (GetLimit() == 0) was silently replaced with the
//     default limit-50 QueryFilter, making unlimited listing impossible and
//     clobbering any other QueryFilter fields the caller had set.
type PriceUnitListRegressionSuite struct {
	testutil.BaseServiceTestSuite
	service PriceUnitService
}

func TestPriceUnitListRegression(t *testing.T) {
	suite.Run(t, new(PriceUnitListRegressionSuite))
}

func (s *PriceUnitListRegressionSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewPriceUnitService(newTestServiceParams(&s.BaseServiceTestSuite))
}

func (s *PriceUnitListRegressionSuite) seedPriceUnits(count int) {
	for i := 0; i < count; i++ {
		pu := &priceunit.PriceUnit{
			ID:             fmt.Sprintf("pu_regr_%03d", i),
			Name:           fmt.Sprintf("Regression Unit %03d", i),
			Code:           fmt.Sprintf("r%02d", i),
			Symbol:         "RU",
			BaseCurrency:   "usd",
			ConversionRate: decimal.NewFromInt(1),
			BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
		}
		_, err := s.GetStores().PriceUnitRepo.Create(s.GetContext(), pu)
		s.NoError(err)
	}
}

func (s *PriceUnitListRegressionSuite) TestListPriceUnitsFilterGuards() {
	// 55 units: one more than fits in the default limit-50 page, so the
	// unlimited and default paths return observably different counts.
	const seeded = 55
	s.seedPriceUnits(seeded)

	s.Run("nil_filter_lists_with_defaults_not_panic", func() {
		resp, err := s.service.ListPriceUnits(s.GetContext(), nil)
		s.NoError(err)
		s.NotNil(resp)
		s.Len(resp.Items, 50)
		s.Equal(50, resp.Pagination.Limit)
	})

	s.Run("nil_query_filter_is_defaulted_not_panic", func() {
		resp, err := s.service.ListPriceUnits(s.GetContext(), &types.PriceUnitFilter{})
		s.NoError(err)
		s.NotNil(resp)
		s.Len(resp.Items, 50)
		s.Equal(50, resp.Pagination.Limit)
	})

	s.Run("no_limit_filter_returns_all_items", func() {
		filter := types.NewNoLimitPriceUnitFilter()
		resp, err := s.service.ListPriceUnits(s.GetContext(), filter)
		s.NoError(err)
		s.Len(resp.Items, seeded)
		// The caller's filter must not be clobbered into a limited one.
		s.True(filter.IsUnlimited())
	})

	s.Run("default_filter_caps_at_default_page_size", func() {
		resp, err := s.service.ListPriceUnits(s.GetContext(), types.NewPriceUnitFilter())
		s.NoError(err)
		s.Len(resp.Items, 50)
	})
}
