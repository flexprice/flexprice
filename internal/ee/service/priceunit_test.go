package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/priceunit"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type PriceUnitServiceSuite struct {
	testutil.BaseServiceTestSuite
	service PriceUnitService
}

func TestPriceUnitService(t *testing.T) {
	suite.Run(t, new(PriceUnitServiceSuite))
}

func (s *PriceUnitServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewPriceUnitService(newTestServiceParams(&s.BaseServiceTestSuite))
}

// createPriceUnitFixture creates a published price unit directly through the repo.
func (s *PriceUnitServiceSuite) createPriceUnitFixture(id, code string, rate decimal.Decimal) *priceunit.PriceUnit {
	pu := &priceunit.PriceUnit{
		ID:             id,
		Name:           "Fixture Unit " + id,
		Code:           code,
		Symbol:         "FP",
		BaseCurrency:   "usd",
		ConversionRate: rate,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	created, err := s.GetStores().PriceUnitRepo.Create(s.GetContext(), pu)
	s.NoError(err)
	return created
}

func (s *PriceUnitServiceSuite) TestCreatePriceUnit() {
	testCases := []struct {
		name            string
		req             dto.CreatePriceUnitRequest
		expectedError   bool
		expectedErrCode string
	}{
		{
			name: "valid_price_unit_is_created_and_readable",
			req: dto.CreatePriceUnitRequest{
				Name:           "Flex Points",
				Code:           "flx",
				Symbol:         "FP",
				BaseCurrency:   "usd",
				ConversionRate: "0.01",
				Metadata:       types.Metadata{"team": "billing"},
			},
			expectedError: false,
		},
		{
			name: "uppercase_base_currency_is_lowercased",
			req: dto.CreatePriceUnitRequest{
				Name:           "Tokens",
				Code:           "tok",
				Symbol:         "T",
				BaseCurrency:   "USD",
				ConversionRate: "2",
			},
			expectedError: false,
		},
		{
			name: "missing_name_returns_validation_error",
			req: dto.CreatePriceUnitRequest{
				Code:           "bad",
				Symbol:         "B",
				BaseCurrency:   "usd",
				ConversionRate: "1",
			},
			expectedError:   true,
			expectedErrCode: string(ierr.ErrCodeValidation),
		},
		{
			name: "invalid_conversion_rate_format_returns_validation_error",
			req: dto.CreatePriceUnitRequest{
				Name:           "Bad Rate",
				Code:           "bdr",
				Symbol:         "B",
				BaseCurrency:   "usd",
				ConversionRate: "not-a-number",
			},
			expectedError:   true,
			expectedErrCode: string(ierr.ErrCodeValidation),
		},
		{
			name: "zero_conversion_rate_returns_validation_error",
			req: dto.CreatePriceUnitRequest{
				Name:           "Zero Rate",
				Code:           "zro",
				Symbol:         "Z",
				BaseCurrency:   "usd",
				ConversionRate: "0",
			},
			expectedError:   true,
			expectedErrCode: string(ierr.ErrCodeValidation),
		},
		{
			name: "negative_conversion_rate_returns_validation_error",
			req: dto.CreatePriceUnitRequest{
				Name:           "Negative Rate",
				Code:           "neg",
				Symbol:         "N",
				BaseCurrency:   "usd",
				ConversionRate: "-1",
			},
			expectedError:   true,
			expectedErrCode: string(ierr.ErrCodeValidation),
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.CreatePriceUnit(s.GetContext(), tc.req)
			if tc.expectedError {
				s.Error(err)
				s.True(ierr.IsValidation(err), "expected validation error, got: %v", err)
				return
			}
			s.NoError(err)
			s.NotNil(resp)
			s.NotEmpty(resp.PriceUnit.ID)

			// Read back through the repo and verify persisted state
			stored, err := s.GetStores().PriceUnitRepo.Get(s.GetContext(), resp.PriceUnit.ID)
			s.NoError(err)
			s.Equal(tc.req.Name, stored.Name)
			s.Equal(tc.req.Code, stored.Code)
			s.Equal(tc.req.Symbol, stored.Symbol)
			// Base currency is always stored lowercase
			s.Equal("usd", stored.BaseCurrency)
			s.True(stored.ConversionRate.Equal(decimal.RequireFromString(tc.req.ConversionRate)),
				"conversion rate mismatch: want %s got %s", tc.req.ConversionRate, stored.ConversionRate)
			s.Equal(types.StatusPublished, stored.Status)
		})
	}
}

func (s *PriceUnitServiceSuite) TestGetPriceUnit() {
	created := s.createPriceUnitFixture("pu_get_1", "gt1", decimal.NewFromFloat(0.5))

	testCases := []struct {
		name          string
		id            string
		expectedError bool
	}{
		{name: "existing_price_unit_is_returned", id: created.ID, expectedError: false},
		{name: "empty_id_returns_validation_error", id: "", expectedError: true},
		{name: "unknown_id_returns_not_found", id: "pu_missing", expectedError: true},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.GetPriceUnit(s.GetContext(), tc.id)
			if tc.expectedError {
				s.Error(err)
				return
			}
			s.NoError(err)
			s.Equal(created.ID, resp.PriceUnit.ID)
			s.Equal(created.Code, resp.PriceUnit.Code)
			s.True(resp.PriceUnit.ConversionRate.Equal(decimal.NewFromFloat(0.5)))
		})
	}
}

func (s *PriceUnitServiceSuite) TestGetPriceUnitByCode() {
	created := s.createPriceUnitFixture("pu_code_1", "cd1", decimal.NewFromInt(2))

	testCases := []struct {
		name          string
		code          string
		expectedError bool
	}{
		{name: "existing_code_is_returned", code: "cd1", expectedError: false},
		{name: "empty_code_returns_validation_error", code: "", expectedError: true},
		{name: "unknown_code_returns_not_found", code: "nope", expectedError: true},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.GetPriceUnitByCode(s.GetContext(), tc.code)
			if tc.expectedError {
				s.Error(err)
				return
			}
			s.NoError(err)
			s.Equal(created.ID, resp.PriceUnit.ID)
			s.Equal("cd1", resp.PriceUnit.Code)
		})
	}
}

func (s *PriceUnitServiceSuite) TestListPriceUnits() {
	s.createPriceUnitFixture("pu_list_1", "ls1", decimal.NewFromInt(1))
	s.createPriceUnitFixture("pu_list_2", "ls2", decimal.NewFromInt(2))

	s.Run("default_filter_lists_all_price_units", func() {
		resp, err := s.service.ListPriceUnits(s.GetContext(), types.NewPriceUnitFilter())
		s.NoError(err)
		s.Len(resp.Items, 2)
		ids := []string{resp.Items[0].PriceUnit.ID, resp.Items[1].PriceUnit.ID}
		s.ElementsMatch([]string{"pu_list_1", "pu_list_2"}, ids)
		s.Equal(2, resp.Pagination.Total)
	})

	s.Run("filter_by_price_unit_ids_narrows_results", func() {
		filter := types.NewPriceUnitFilter()
		filter.PriceUnitIDs = []string{"pu_list_2"}
		resp, err := s.service.ListPriceUnits(s.GetContext(), filter)
		s.NoError(err)
		s.Len(resp.Items, 1)
		s.Equal("pu_list_2", resp.Items[0].PriceUnit.ID)
	})

	s.Run("invalid_filter_limit_returns_validation_error", func() {
		filter := types.NewPriceUnitFilter()
		invalidLimit := -5
		filter.QueryFilter.Limit = &invalidLimit
		_, err := s.service.ListPriceUnits(s.GetContext(), filter)
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("empty_price_unit_id_in_filter_returns_validation_error", func() {
		filter := types.NewPriceUnitFilter()
		filter.PriceUnitIDs = []string{""}
		_, err := s.service.ListPriceUnits(s.GetContext(), filter)
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})
}

func (s *PriceUnitServiceSuite) TestUpdatePriceUnit() {
	created := s.createPriceUnitFixture("pu_upd_1", "up1", decimal.NewFromInt(1))
	newName := "Renamed Unit"

	testCases := []struct {
		name          string
		id            string
		req           dto.UpdatePriceUnitRequest
		expectedError bool
	}{
		{
			name: "update_name_and_metadata_persists",
			id:   created.ID,
			req: dto.UpdatePriceUnitRequest{
				Name:     &newName,
				Metadata: types.Metadata{"updated": "true"},
			},
			expectedError: false,
		},
		{
			name:          "empty_id_returns_validation_error",
			id:            "",
			req:           dto.UpdatePriceUnitRequest{Name: &newName},
			expectedError: true,
		},
		{
			name:          "unknown_id_returns_not_found",
			id:            "pu_missing",
			req:           dto.UpdatePriceUnitRequest{Name: &newName},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.UpdatePriceUnit(s.GetContext(), tc.id, tc.req)
			if tc.expectedError {
				s.Error(err)
				return
			}
			s.NoError(err)
			s.Equal(newName, resp.PriceUnit.Name)

			// Read back and verify only the intended fields changed
			stored, err := s.GetStores().PriceUnitRepo.Get(s.GetContext(), tc.id)
			s.NoError(err)
			s.Equal(newName, stored.Name)
			s.Equal(types.Metadata{"updated": "true"}, stored.Metadata)
			s.Equal(created.Code, stored.Code)
			s.True(stored.ConversionRate.Equal(created.ConversionRate))
		})
	}
}

func (s *PriceUnitServiceSuite) TestUpdatePriceUnitPartialUpdateKeepsOtherFields() {
	created := s.createPriceUnitFixture("pu_upd_partial", "upp", decimal.NewFromInt(3))

	// Update with no fields set — nothing should change
	resp, err := s.service.UpdatePriceUnit(s.GetContext(), created.ID, dto.UpdatePriceUnitRequest{})
	s.NoError(err)
	s.Equal(created.Name, resp.PriceUnit.Name)

	stored, err := s.GetStores().PriceUnitRepo.Get(s.GetContext(), created.ID)
	s.NoError(err)
	s.Equal(created.Name, stored.Name)
	s.Equal(created.Metadata, stored.Metadata)
}

func (s *PriceUnitServiceSuite) TestDeletePriceUnit() {
	created := s.createPriceUnitFixture("pu_del_1", "dl1", decimal.NewFromInt(1))

	testCases := []struct {
		name          string
		id            string
		expectedError bool
	}{
		{name: "empty_id_returns_validation_error", id: "", expectedError: true},
		{name: "unknown_id_returns_not_found", id: "pu_missing", expectedError: true},
		{name: "existing_price_unit_is_deleted", id: created.ID, expectedError: false},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			err := s.service.DeletePriceUnit(s.GetContext(), tc.id)
			if tc.expectedError {
				s.Error(err)
				return
			}
			s.NoError(err)

			// Read back: deleted price unit must not be retrievable anymore
			_, err = s.GetStores().PriceUnitRepo.Get(s.GetContext(), tc.id)
			s.Error(err)
			s.True(ierr.IsNotFound(err))
		})
	}
}
