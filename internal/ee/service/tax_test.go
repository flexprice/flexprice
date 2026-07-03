package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type TaxServiceSuite struct {
	testutil.BaseServiceTestSuite
	service TaxService
	now     time.Time
}

func TestTaxService(t *testing.T) {
	suite.Run(t, new(TaxServiceSuite))
}

func (s *TaxServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewTaxService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.now = time.Now().UTC()
}

// ---------------------------------------------------------------------------
// fixtures
// ---------------------------------------------------------------------------

func (s *TaxServiceSuite) createPercentageTaxRate(name, code, percentage string) *dto.TaxRateResponse {
	resp, err := s.service.CreateTaxRate(s.GetContext(), dto.CreateTaxRateRequest{
		Name:            name,
		Code:            code,
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.RequireFromString(percentage)),
	})
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	return resp
}

func (s *TaxServiceSuite) createFixedTaxRate(name, code, fixed string) *dto.TaxRateResponse {
	resp, err := s.service.CreateTaxRate(s.GetContext(), dto.CreateTaxRateRequest{
		Name:        name,
		Code:        code,
		TaxRateType: types.TaxRateTypeFixed,
		FixedValue:  lo.ToPtr(decimal.RequireFromString(fixed)),
	})
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	return resp
}

func (s *TaxServiceSuite) createCustomerWithExternalID(externalID string) *customer.Customer {
	ctx := s.GetContext()
	cust := &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: externalID,
		Name:       "Tax Test Customer",
		Email:      externalID + "@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	return cust
}

// ---------------------------------------------------------------------------
// CreateTaxRate
// ---------------------------------------------------------------------------

func (s *TaxServiceSuite) TestCreateTaxRate() {
	testCases := []struct {
		name          string
		req           dto.CreateTaxRateRequest
		expectedError bool
	}{
		{
			name: "valid_percentage_tax_rate_is_created_active",
			req: dto.CreateTaxRateRequest{
				Name:            "VAT",
				Code:            "vat-20",
				Description:     "value added tax",
				TaxRateType:     types.TaxRateTypePercentage,
				PercentageValue: lo.ToPtr(decimal.RequireFromString("20")),
				Metadata:        map[string]string{"region": "eu"},
			},
			expectedError: false,
		},
		{
			name: "valid_fixed_tax_rate_is_created_active",
			req: dto.CreateTaxRateRequest{
				Name:        "Flat Levy",
				Code:        "flat-levy",
				TaxRateType: types.TaxRateTypeFixed,
				FixedValue:  lo.ToPtr(decimal.RequireFromString("2.50")),
			},
			expectedError: false,
		},
		{
			name: "missing_name_returns_validation_error",
			req: dto.CreateTaxRateRequest{
				Code:            "no-name",
				TaxRateType:     types.TaxRateTypePercentage,
				PercentageValue: lo.ToPtr(decimal.RequireFromString("10")),
			},
			expectedError: true,
		},
		{
			name: "missing_code_returns_validation_error",
			req: dto.CreateTaxRateRequest{
				Name:            "No Code",
				TaxRateType:     types.TaxRateTypePercentage,
				PercentageValue: lo.ToPtr(decimal.RequireFromString("10")),
			},
			expectedError: true,
		},
		{
			name: "percentage_type_without_percentage_value_returns_error",
			req: dto.CreateTaxRateRequest{
				Name:        "Broken Percentage",
				Code:        "broken-pct",
				TaxRateType: types.TaxRateTypePercentage,
			},
			expectedError: true,
		},
		{
			name: "percentage_above_100_returns_error",
			req: dto.CreateTaxRateRequest{
				Name:            "Too Big",
				Code:            "too-big",
				TaxRateType:     types.TaxRateTypePercentage,
				PercentageValue: lo.ToPtr(decimal.RequireFromString("100.01")),
			},
			expectedError: true,
		},
		{
			name: "negative_fixed_value_returns_error",
			req: dto.CreateTaxRateRequest{
				Name:        "Negative Fixed",
				Code:        "neg-fixed",
				TaxRateType: types.TaxRateTypeFixed,
				FixedValue:  lo.ToPtr(decimal.RequireFromString("-1")),
			},
			expectedError: true,
		},
		{
			name: "both_percentage_and_fixed_values_returns_error",
			req: dto.CreateTaxRateRequest{
				Name:            "Both Values",
				Code:            "both-values",
				TaxRateType:     types.TaxRateTypePercentage,
				PercentageValue: lo.ToPtr(decimal.RequireFromString("10")),
				FixedValue:      lo.ToPtr(decimal.RequireFromString("5")),
			},
			expectedError: true,
		},
		{
			name: "invalid_tax_rate_type_returns_error",
			req: dto.CreateTaxRateRequest{
				Name:        "Bogus Type",
				Code:        "bogus-type",
				TaxRateType: types.TaxRateType("bogus"),
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.CreateTaxRate(s.GetContext(), tc.req)
			if tc.expectedError {
				s.Error(err)
				return
			}
			s.NoError(err)
			s.Require().NotNil(resp)
			s.Equal(types.TaxRateStatusActive, resp.TaxRateStatus)

			// Read back through the repository and verify persisted state
			stored, err := s.GetStores().TaxRateRepo.Get(s.GetContext(), resp.ID)
			s.Require().NoError(err)
			s.Equal(tc.req.Name, stored.Name)
			s.Equal(tc.req.Code, stored.Code)
			s.Equal(tc.req.TaxRateType, stored.TaxRateType)
			if tc.req.PercentageValue != nil {
				s.Require().NotNil(stored.PercentageValue)
				s.True(stored.PercentageValue.Equal(*tc.req.PercentageValue))
			}
			if tc.req.FixedValue != nil {
				s.Require().NotNil(stored.FixedValue)
				s.True(stored.FixedValue.Equal(*tc.req.FixedValue))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GetTaxRate / GetTaxRateByCode
// ---------------------------------------------------------------------------

func (s *TaxServiceSuite) TestGetTaxRate() {
	created := s.createPercentageTaxRate("GST", "gst-18", "18")

	s.Run("empty_id_returns_validation_error", func() {
		_, err := s.service.GetTaxRate(s.GetContext(), "")
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("not_found_id_returns_error", func() {
		_, err := s.service.GetTaxRate(s.GetContext(), "taxrate_does_not_exist")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("existing_id_returns_tax_rate", func() {
		resp, err := s.service.GetTaxRate(s.GetContext(), created.ID)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Equal(created.ID, resp.ID)
		s.Equal("gst-18", resp.Code)
		s.Require().NotNil(resp.PercentageValue)
		s.True(resp.PercentageValue.Equal(decimal.RequireFromString("18")))
	})
}

func (s *TaxServiceSuite) TestGetTaxRateByCode() {
	created := s.createFixedTaxRate("City Fee", "city-fee", "1.25")

	s.Run("empty_code_returns_validation_error", func() {
		_, err := s.service.GetTaxRateByCode(s.GetContext(), "")
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("unknown_code_returns_not_found", func() {
		_, err := s.service.GetTaxRateByCode(s.GetContext(), "unknown-code")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("existing_code_returns_tax_rate", func() {
		resp, err := s.service.GetTaxRateByCode(s.GetContext(), "city-fee")
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Equal(created.ID, resp.ID)
		s.Require().NotNil(resp.FixedValue)
		s.True(resp.FixedValue.Equal(decimal.RequireFromString("1.25")))
	})
}

// ---------------------------------------------------------------------------
// ListTaxRates
// ---------------------------------------------------------------------------

func (s *TaxServiceSuite) TestListTaxRates() {
	rateA := s.createPercentageTaxRate("Rate A", "rate-a", "5")
	rateB := s.createPercentageTaxRate("Rate B", "rate-b", "10")
	s.createFixedTaxRate("Rate C", "rate-c", "3")

	s.Run("nil_filter_returns_all_tax_rates", func() {
		resp, err := s.service.ListTaxRates(s.GetContext(), nil)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Len(resp.Items, 3)
		s.Equal(3, resp.Pagination.Total)
	})

	s.Run("filter_by_codes_returns_matching_rates_only", func() {
		filter := types.NewNoLimitTaxRateFilter()
		filter.TaxRateCodes = []string{"rate-a", "rate-b"}
		resp, err := s.service.ListTaxRates(s.GetContext(), filter)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Len(resp.Items, 2)
		codes := []string{resp.Items[0].Code, resp.Items[1].Code}
		s.ElementsMatch([]string{"rate-a", "rate-b"}, codes)
	})

	s.Run("filter_by_ids_returns_matching_rate_only", func() {
		filter := types.NewNoLimitTaxRateFilter()
		filter.TaxRateIDs = []string{rateA.ID}
		resp, err := s.service.ListTaxRates(s.GetContext(), filter)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Require().Len(resp.Items, 1)
		s.Equal(rateA.ID, resp.Items[0].ID)
		s.NotEqual(rateB.ID, resp.Items[0].ID)
	})
}

// ---------------------------------------------------------------------------
// UpdateTaxRate
// ---------------------------------------------------------------------------

func (s *TaxServiceSuite) TestUpdateTaxRate() {
	s.Run("empty_id_returns_validation_error", func() {
		_, err := s.service.UpdateTaxRate(s.GetContext(), "", dto.UpdateTaxRateRequest{Name: "x"})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("invalid_tax_rate_status_returns_validation_error", func() {
		created := s.createPercentageTaxRate("Status Rate", "status-rate", "9")
		_, err := s.service.UpdateTaxRate(s.GetContext(), created.ID, dto.UpdateTaxRateRequest{
			TaxRateStatus: lo.ToPtr(types.TaxRateStatus("bogus")),
		})
		s.Error(err)
	})

	s.Run("not_found_id_returns_error", func() {
		_, err := s.service.UpdateTaxRate(s.GetContext(), "taxrate_missing", dto.UpdateTaxRateRequest{Name: "x"})
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("updates_all_provided_fields_and_persists", func() {
		created := s.createPercentageTaxRate("Old Name", "old-code", "7")
		resp, err := s.service.UpdateTaxRate(s.GetContext(), created.ID, dto.UpdateTaxRateRequest{
			Name:          "New Name",
			Code:          "new-code",
			Description:   "updated description",
			Metadata:      map[string]string{"k": "v"},
			TaxRateStatus: lo.ToPtr(types.TaxRateStatusInactive),
		})
		s.NoError(err)
		s.Require().NotNil(resp)

		stored, err := s.GetStores().TaxRateRepo.Get(s.GetContext(), created.ID)
		s.Require().NoError(err)
		s.Equal("New Name", stored.Name)
		s.Equal("new-code", stored.Code)
		s.Equal("updated description", stored.Description)
		s.Equal(map[string]string{"k": "v"}, stored.Metadata)
		s.Equal(types.TaxRateStatusInactive, stored.TaxRateStatus)
	})

	s.Run("empty_fields_keep_existing_values", func() {
		created := s.createPercentageTaxRate("Keep Name", "keep-code", "3")
		_, err := s.service.UpdateTaxRate(s.GetContext(), created.ID, dto.UpdateTaxRateRequest{})
		s.NoError(err)

		stored, err := s.GetStores().TaxRateRepo.Get(s.GetContext(), created.ID)
		s.Require().NoError(err)
		s.Equal("Keep Name", stored.Name)
		s.Equal("keep-code", stored.Code)
	})

	s.Run("tax_rate_used_in_association_cannot_be_updated", func() {
		created := s.createPercentageTaxRate("Assoc Rate", "assoc-rate", "5")
		_, err := s.service.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
			TaxRateCode: "assoc-rate",
			EntityType:  types.TaxRateEntityTypeCustomer,
			EntityID:    "cust_assoc_update",
			Currency:    "usd",
		})
		s.Require().NoError(err)

		_, err = s.service.UpdateTaxRate(s.GetContext(), created.ID, dto.UpdateTaxRateRequest{Name: "Should Fail"})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("tax_rate_used_in_tax_applied_cannot_be_updated", func() {
		created := s.createPercentageTaxRate("Applied Rate", "applied-rate", "5")
		_, err := s.service.CreateTaxApplied(s.GetContext(), dto.CreateTaxAppliedRequest{
			TaxRateID:     created.ID,
			EntityType:    types.TaxRateEntityTypeInvoice,
			EntityID:      "inv_applied_update",
			TaxableAmount: decimal.RequireFromString("100"),
			TaxAmount:     decimal.RequireFromString("5"),
			Currency:      "usd",
		})
		s.Require().NoError(err)

		_, err = s.service.UpdateTaxRate(s.GetContext(), created.ID, dto.UpdateTaxRateRequest{Name: "Should Fail"})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})
}

// ---------------------------------------------------------------------------
// DeleteTaxRate
// ---------------------------------------------------------------------------

func (s *TaxServiceSuite) TestDeleteTaxRate() {
	s.Run("empty_id_returns_validation_error", func() {
		err := s.service.DeleteTaxRate(s.GetContext(), "")
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("not_found_id_returns_error", func() {
		err := s.service.DeleteTaxRate(s.GetContext(), "taxrate_missing")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("tax_rate_with_active_association_cannot_be_deleted", func() {
		created := s.createPercentageTaxRate("Delete Blocked", "del-blocked", "5")
		_, err := s.service.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
			TaxRateCode: "del-blocked",
			EntityType:  types.TaxRateEntityTypeCustomer,
			EntityID:    "cust_del_blocked",
			Currency:    "usd",
		})
		s.Require().NoError(err)

		err = s.service.DeleteTaxRate(s.GetContext(), created.ID)
		s.Error(err)
		s.True(ierr.IsValidation(err))

		// Still present in the repo
		_, err = s.GetStores().TaxRateRepo.Get(s.GetContext(), created.ID)
		s.NoError(err)
	})

	s.Run("unused_tax_rate_is_deleted", func() {
		created := s.createPercentageTaxRate("Delete Me", "del-me", "5")
		err := s.service.DeleteTaxRate(s.GetContext(), created.ID)
		s.NoError(err)

		_, err = s.GetStores().TaxRateRepo.Get(s.GetContext(), created.ID)
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})
}

// ---------------------------------------------------------------------------
// Tax applied CRUD
// ---------------------------------------------------------------------------

func (s *TaxServiceSuite) TestCreateTaxApplied() {
	rate := s.createPercentageTaxRate("Applied CRUD", "applied-crud", "10")

	testCases := []struct {
		name          string
		req           dto.CreateTaxAppliedRequest
		expectedError bool
	}{
		{
			name: "valid_tax_applied_record_is_created",
			req: dto.CreateTaxAppliedRequest{
				TaxRateID:     rate.ID,
				EntityType:    types.TaxRateEntityTypeInvoice,
				EntityID:      "inv_applied_crud",
				TaxableAmount: decimal.RequireFromString("100"),
				TaxAmount:     decimal.RequireFromString("10"),
				Currency:      "usd",
			},
			expectedError: false,
		},
		{
			name: "missing_tax_rate_id_returns_validation_error",
			req: dto.CreateTaxAppliedRequest{
				EntityType:    types.TaxRateEntityTypeInvoice,
				EntityID:      "inv_applied_crud",
				TaxableAmount: decimal.RequireFromString("100"),
				TaxAmount:     decimal.RequireFromString("10"),
				Currency:      "usd",
			},
			expectedError: true,
		},
		{
			name: "missing_currency_returns_validation_error",
			req: dto.CreateTaxAppliedRequest{
				TaxRateID:     rate.ID,
				EntityType:    types.TaxRateEntityTypeInvoice,
				EntityID:      "inv_applied_crud",
				TaxableAmount: decimal.RequireFromString("100"),
				TaxAmount:     decimal.RequireFromString("10"),
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.CreateTaxApplied(s.GetContext(), tc.req)
			if tc.expectedError {
				s.Error(err)
				return
			}
			s.NoError(err)
			s.Require().NotNil(resp)

			stored, err := s.GetStores().TaxAppliedRepo.Get(s.GetContext(), resp.ID)
			s.Require().NoError(err)
			s.Equal(tc.req.TaxRateID, stored.TaxRateID)
			s.Equal(tc.req.EntityID, stored.EntityID)
			s.True(stored.TaxableAmount.Equal(tc.req.TaxableAmount))
			s.True(stored.TaxAmount.Equal(tc.req.TaxAmount))
			s.Equal(tc.req.Currency, stored.Currency)
		})
	}
}

func (s *TaxServiceSuite) TestGetTaxApplied() {
	rate := s.createPercentageTaxRate("Applied Get", "applied-get", "10")
	created, err := s.service.CreateTaxApplied(s.GetContext(), dto.CreateTaxAppliedRequest{
		TaxRateID:     rate.ID,
		EntityType:    types.TaxRateEntityTypeInvoice,
		EntityID:      "inv_applied_get",
		TaxableAmount: decimal.RequireFromString("50"),
		TaxAmount:     decimal.RequireFromString("5"),
		Currency:      "usd",
	})
	s.Require().NoError(err)

	s.Run("empty_id_returns_validation_error", func() {
		_, err := s.service.GetTaxApplied(s.GetContext(), "")
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("not_found_id_returns_error", func() {
		_, err := s.service.GetTaxApplied(s.GetContext(), "taxapplied_missing")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("existing_id_returns_record", func() {
		resp, err := s.service.GetTaxApplied(s.GetContext(), created.ID)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Equal(created.ID, resp.ID)
		s.True(resp.TaxAmount.Equal(decimal.RequireFromString("5")))
	})
}

func (s *TaxServiceSuite) TestListTaxApplied() {
	rate := s.createPercentageTaxRate("Applied List", "applied-list", "10")
	for _, entityID := range []string{"inv_list_1", "inv_list_2"} {
		_, err := s.service.CreateTaxApplied(s.GetContext(), dto.CreateTaxAppliedRequest{
			TaxRateID:     rate.ID,
			EntityType:    types.TaxRateEntityTypeInvoice,
			EntityID:      entityID,
			TaxableAmount: decimal.RequireFromString("100"),
			TaxAmount:     decimal.RequireFromString("10"),
			Currency:      "usd",
		})
		s.Require().NoError(err)
	}

	s.Run("nil_filter_returns_all_records", func() {
		resp, err := s.service.ListTaxApplied(s.GetContext(), nil)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Len(resp.Items, 2)
		s.Equal(2, resp.Pagination.Total)
	})

	s.Run("filter_by_entity_id_returns_matching_record", func() {
		filter := types.NewNoLimitTaxAppliedFilter()
		filter.EntityID = "inv_list_1"
		resp, err := s.service.ListTaxApplied(s.GetContext(), filter)
		s.NoError(err)
		s.Require().Len(resp.Items, 1)
		s.Equal("inv_list_1", resp.Items[0].EntityID)
	})

	s.Run("expand_tax_rate_populates_tax_rate_on_items", func() {
		filter := types.NewNoLimitTaxAppliedFilter()
		filter.QueryFilter.Expand = lo.ToPtr(string(types.ExpandTaxRate))
		resp, err := s.service.ListTaxApplied(s.GetContext(), filter)
		s.NoError(err)
		s.Require().Len(resp.Items, 2)
		for _, item := range resp.Items {
			s.Require().NotNil(item.TaxRate)
			s.Equal(rate.ID, item.TaxRate.ID)
			s.Equal("applied-list", item.TaxRate.Code)
		}
	})
}

func (s *TaxServiceSuite) TestDeleteTaxApplied() {
	rate := s.createPercentageTaxRate("Applied Delete", "applied-del", "10")
	created, err := s.service.CreateTaxApplied(s.GetContext(), dto.CreateTaxAppliedRequest{
		TaxRateID:     rate.ID,
		EntityType:    types.TaxRateEntityTypeInvoice,
		EntityID:      "inv_applied_del",
		TaxableAmount: decimal.RequireFromString("100"),
		TaxAmount:     decimal.RequireFromString("10"),
		Currency:      "usd",
	})
	s.Require().NoError(err)

	s.Run("empty_id_returns_validation_error", func() {
		err := s.service.DeleteTaxApplied(s.GetContext(), "")
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("not_found_id_returns_error", func() {
		err := s.service.DeleteTaxApplied(s.GetContext(), "taxapplied_missing")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("existing_record_is_deleted", func() {
		err := s.service.DeleteTaxApplied(s.GetContext(), created.ID)
		s.NoError(err)

		_, err = s.GetStores().TaxAppliedRepo.Get(s.GetContext(), created.ID)
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})
}

// ---------------------------------------------------------------------------
// Tax association CRUD
// ---------------------------------------------------------------------------

func (s *TaxServiceSuite) TestCreateTaxAssociation() {
	s.createPercentageTaxRate("Assoc Create", "assoc-create", "10")

	s.Run("missing_tax_rate_code_returns_validation_error", func() {
		_, err := s.service.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
			EntityType: types.TaxRateEntityTypeCustomer,
			EntityID:   "cust_1",
		})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("negative_priority_returns_validation_error", func() {
		_, err := s.service.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
			TaxRateCode: "assoc-create",
			EntityType:  types.TaxRateEntityTypeCustomer,
			EntityID:    "cust_1",
			Priority:    -1,
		})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("unknown_tax_rate_code_returns_not_found", func() {
		_, err := s.service.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
			TaxRateCode: "unknown-assoc-code",
			EntityType:  types.TaxRateEntityTypeCustomer,
			EntityID:    "cust_1",
		})
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("inactive_tax_rate_returns_validation_error", func() {
		ctx := s.GetContext()
		inactive := &taxrate.TaxRate{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
			Name:            "Inactive Rate",
			Code:            "inactive-rate",
			TaxRateType:     types.TaxRateTypePercentage,
			PercentageValue: lo.ToPtr(decimal.RequireFromString("5")),
			TaxRateStatus:   types.TaxRateStatusInactive,
			EnvironmentID:   types.GetEnvironmentID(ctx),
			BaseModel:       types.GetDefaultBaseModel(ctx),
		}
		s.Require().NoError(s.GetStores().TaxRateRepo.Create(ctx, inactive))

		_, err := s.service.CreateTaxAssociation(ctx, &dto.CreateTaxAssociationRequest{
			TaxRateCode: "inactive-rate",
			EntityType:  types.TaxRateEntityTypeCustomer,
			EntityID:    "cust_1",
		})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("valid_association_is_created_and_persisted", func() {
		resp, err := s.service.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
			TaxRateCode: "assoc-create",
			EntityType:  types.TaxRateEntityTypeCustomer,
			EntityID:    "cust_assoc_create",
			Priority:    2,
			Currency:    "usd",
			AutoApply:   true,
		})
		s.NoError(err)
		s.Require().NotNil(resp)

		stored, err := s.GetStores().TaxAssociationRepo.Get(s.GetContext(), resp.ID)
		s.Require().NoError(err)
		s.Equal(types.TaxRateEntityTypeCustomer, stored.EntityType)
		s.Equal("cust_assoc_create", stored.EntityID)
		s.Equal(2, stored.Priority)
		s.True(stored.AutoApply)
		s.Equal("usd", stored.Currency)
	})

	s.Run("external_customer_id_is_resolved_to_customer_entity", func() {
		cust := s.createCustomerWithExternalID("ext_tax_assoc_ok")
		resp, err := s.service.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
			TaxRateCode:        "assoc-create",
			ExternalCustomerID: "ext_tax_assoc_ok",
			Currency:           "usd",
		})
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Equal(types.TaxRateEntityTypeCustomer, resp.EntityType)
		s.Equal(cust.ID, resp.EntityID)
	})

	s.Run("unknown_external_customer_id_returns_not_found", func() {
		_, err := s.service.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
			TaxRateCode:        "assoc-create",
			ExternalCustomerID: "ext_tax_missing",
		})
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("mismatched_entity_id_and_external_customer_id_returns_error", func() {
		s.createCustomerWithExternalID("ext_tax_assoc_mismatch")
		_, err := s.service.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
			TaxRateCode:        "assoc-create",
			EntityID:           "cust_some_other",
			ExternalCustomerID: "ext_tax_assoc_mismatch",
		})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})
}

func (s *TaxServiceSuite) TestGetTaxAssociation() {
	rate := s.createPercentageTaxRate("Assoc Get", "assoc-get", "10")
	created, err := s.service.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
		TaxRateCode: "assoc-get",
		EntityType:  types.TaxRateEntityTypeCustomer,
		EntityID:    "cust_assoc_get",
		Currency:    "usd",
	})
	s.Require().NoError(err)

	s.Run("empty_id_returns_validation_error", func() {
		_, err := s.service.GetTaxAssociation(s.GetContext(), "")
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("not_found_id_returns_error", func() {
		_, err := s.service.GetTaxAssociation(s.GetContext(), "taxassoc_missing")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("existing_id_returns_association_with_tax_rate", func() {
		resp, err := s.service.GetTaxAssociation(s.GetContext(), created.ID)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Equal(created.ID, resp.ID)
		s.Require().NotNil(resp.TaxRate)
		s.Equal(rate.ID, resp.TaxRate.ID)
		s.Equal("assoc-get", resp.TaxRate.Code)
	})
}

func (s *TaxServiceSuite) TestUpdateTaxAssociation() {
	s.createPercentageTaxRate("Assoc Update", "assoc-update", "10")
	created, err := s.service.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
		TaxRateCode: "assoc-update",
		EntityType:  types.TaxRateEntityTypeCustomer,
		EntityID:    "cust_assoc_update_crud",
		Priority:    1,
		Currency:    "usd",
	})
	s.Require().NoError(err)

	s.Run("negative_priority_returns_validation_error", func() {
		_, err := s.service.UpdateTaxAssociation(s.GetContext(), created.ID, &dto.TaxAssociationUpdateRequest{
			Priority: lo.ToPtr(-5),
		})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("empty_id_returns_validation_error", func() {
		_, err := s.service.UpdateTaxAssociation(s.GetContext(), "", &dto.TaxAssociationUpdateRequest{})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("not_found_id_returns_error", func() {
		_, err := s.service.UpdateTaxAssociation(s.GetContext(), "taxassoc_missing", &dto.TaxAssociationUpdateRequest{})
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("updates_priority_auto_apply_and_metadata", func() {
		resp, err := s.service.UpdateTaxAssociation(s.GetContext(), created.ID, &dto.TaxAssociationUpdateRequest{
			Priority:  lo.ToPtr(9),
			AutoApply: lo.ToPtr(true),
			Metadata:  lo.ToPtr(map[string]string{"source": "test"}),
		})
		s.NoError(err)
		s.Require().NotNil(resp)

		stored, err := s.GetStores().TaxAssociationRepo.Get(s.GetContext(), created.ID)
		s.Require().NoError(err)
		s.Equal(9, stored.Priority)
		s.True(stored.AutoApply)
		s.Equal(map[string]string{"source": "test"}, stored.Metadata)
	})
}

func (s *TaxServiceSuite) TestDeleteTaxAssociation() {
	s.createPercentageTaxRate("Assoc Delete", "assoc-delete", "10")
	created, err := s.service.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
		TaxRateCode: "assoc-delete",
		EntityType:  types.TaxRateEntityTypeCustomer,
		EntityID:    "cust_assoc_delete",
		Currency:    "usd",
	})
	s.Require().NoError(err)

	s.Run("empty_id_returns_validation_error", func() {
		err := s.service.DeleteTaxAssociation(s.GetContext(), "")
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("not_found_id_returns_error", func() {
		err := s.service.DeleteTaxAssociation(s.GetContext(), "taxassoc_missing")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("existing_association_is_deleted", func() {
		err := s.service.DeleteTaxAssociation(s.GetContext(), created.ID)
		s.NoError(err)

		_, err = s.GetStores().TaxAssociationRepo.Get(s.GetContext(), created.ID)
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})
}

func (s *TaxServiceSuite) TestListTaxAssociations() {
	rate := s.createPercentageTaxRate("Assoc List", "assoc-list", "10")
	_, err := s.service.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
		TaxRateCode: "assoc-list",
		EntityType:  types.TaxRateEntityTypeCustomer,
		EntityID:    "cust_assoc_list",
		Currency:    "usd",
	})
	s.Require().NoError(err)

	s.Run("nil_filter_returns_all_associations", func() {
		resp, err := s.service.ListTaxAssociations(s.GetContext(), nil)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Len(resp.Items, 1)
		s.Equal(1, resp.Pagination.Total)
	})

	s.Run("filter_by_entity_returns_matching_associations", func() {
		filter := types.NewNoLimitTaxAssociationFilter()
		filter.EntityType = types.TaxRateEntityTypeCustomer
		filter.EntityID = "cust_assoc_list"
		resp, err := s.service.ListTaxAssociations(s.GetContext(), filter)
		s.NoError(err)
		s.Require().Len(resp.Items, 1)
		s.Equal("cust_assoc_list", resp.Items[0].EntityID)
	})

	s.Run("filter_by_unknown_entity_returns_empty", func() {
		filter := types.NewNoLimitTaxAssociationFilter()
		filter.EntityType = types.TaxRateEntityTypeCustomer
		filter.EntityID = "cust_unknown"
		resp, err := s.service.ListTaxAssociations(s.GetContext(), filter)
		s.NoError(err)
		s.Empty(resp.Items)
	})

	s.Run("expand_tax_rate_populates_tax_rate_on_items", func() {
		filter := types.NewNoLimitTaxAssociationFilter()
		filter.QueryFilter.Expand = lo.ToPtr(string(types.ExpandTaxRate))
		resp, err := s.service.ListTaxAssociations(s.GetContext(), filter)
		s.NoError(err)
		s.Require().Len(resp.Items, 1)
		s.Require().NotNil(resp.Items[0].TaxRate)
		s.Equal(rate.ID, resp.Items[0].TaxRate.ID)
	})

	s.Run("external_customer_id_is_resolved_before_listing", func() {
		cust := s.createCustomerWithExternalID("ext_tax_list_ok")
		_, err := s.service.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
			TaxRateCode:        "assoc-list",
			ExternalCustomerID: "ext_tax_list_ok",
			Currency:           "usd",
		})
		s.Require().NoError(err)

		filter := types.NewNoLimitTaxAssociationFilter()
		filter.ExternalCustomerID = "ext_tax_list_ok"
		resp, err := s.service.ListTaxAssociations(s.GetContext(), filter)
		s.NoError(err)
		s.Require().Len(resp.Items, 1)
		s.Equal(cust.ID, resp.Items[0].EntityID)
		s.Equal(types.TaxRateEntityTypeCustomer, resp.Items[0].EntityType)
	})

	s.Run("unknown_external_customer_id_returns_not_found", func() {
		filter := types.NewNoLimitTaxAssociationFilter()
		filter.ExternalCustomerID = "ext_tax_list_missing"
		_, err := s.service.ListTaxAssociations(s.GetContext(), filter)
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("mismatched_entity_id_and_external_customer_id_returns_error", func() {
		s.createCustomerWithExternalID("ext_tax_list_mismatch")
		filter := types.NewNoLimitTaxAssociationFilter()
		filter.ExternalCustomerID = "ext_tax_list_mismatch"
		filter.EntityID = "cust_other_entity"
		_, err := s.service.ListTaxAssociations(s.GetContext(), filter)
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})
}

// ---------------------------------------------------------------------------
// LinkTaxRatesToEntity
// ---------------------------------------------------------------------------

func (s *TaxServiceSuite) TestLinkTaxRatesToEntity() {
	rate := s.createPercentageTaxRate("Link Rate", "link-rate", "10")

	s.Run("no_overrides_or_existing_associations_is_a_no_op", func() {
		err := s.service.LinkTaxRatesToEntity(s.GetContext(), dto.LinkTaxRateToEntityRequest{
			EntityType: types.TaxRateEntityTypeCustomer,
			EntityID:   "cust_link_noop",
		})
		s.NoError(err)

		filter := types.NewNoLimitTaxAssociationFilter()
		filter.EntityID = "cust_link_noop"
		assocs, listErr := s.GetStores().TaxAssociationRepo.List(s.GetContext(), filter)
		s.NoError(listErr)
		s.Empty(assocs)
	})

	s.Run("overrides_create_associations_for_entity", func() {
		err := s.service.LinkTaxRatesToEntity(s.GetContext(), dto.LinkTaxRateToEntityRequest{
			EntityType: types.TaxRateEntityTypeCustomer,
			EntityID:   "cust_link_overrides",
			TaxRateOverrides: []*dto.TaxRateOverride{
				{
					TaxRateCode: "link-rate",
					Currency:    "usd",
					Priority:    3,
					AutoApply:   true,
				},
			},
		})
		s.NoError(err)

		filter := types.NewNoLimitTaxAssociationFilter()
		filter.EntityID = "cust_link_overrides"
		assocs, listErr := s.GetStores().TaxAssociationRepo.List(s.GetContext(), filter)
		s.Require().NoError(listErr)
		s.Require().Len(assocs, 1)
		s.Equal(rate.ID, assocs[0].TaxRateID)
		s.Equal(3, assocs[0].Priority)
		s.True(assocs[0].AutoApply)
	})

	s.Run("invalid_override_priority_fails_without_creating_association", func() {
		err := s.service.LinkTaxRatesToEntity(s.GetContext(), dto.LinkTaxRateToEntityRequest{
			EntityType: types.TaxRateEntityTypeCustomer,
			EntityID:   "cust_link_invalid",
			TaxRateOverrides: []*dto.TaxRateOverride{
				{
					TaxRateCode: "link-rate",
					Currency:    "usd",
					Priority:    -1,
				},
			},
		})
		s.Error(err)

		filter := types.NewNoLimitTaxAssociationFilter()
		filter.EntityID = "cust_link_invalid"
		assocs, listErr := s.GetStores().TaxAssociationRepo.List(s.GetContext(), filter)
		s.NoError(listErr)
		s.Empty(assocs)
	})

	s.Run("existing_associations_are_copied_to_new_entity", func() {
		source, err := s.service.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
			TaxRateCode: "link-rate",
			EntityType:  types.TaxRateEntityTypeCustomer,
			EntityID:    "cust_link_source",
			Priority:    7,
			Currency:    "usd",
			AutoApply:   true,
		})
		s.Require().NoError(err)

		err = s.service.LinkTaxRatesToEntity(s.GetContext(), dto.LinkTaxRateToEntityRequest{
			EntityType:              types.TaxRateEntityTypeSubscription,
			EntityID:                "sub_link_target",
			ExistingTaxAssociations: []*dto.TaxAssociationResponse{source},
		})
		s.NoError(err)

		filter := types.NewNoLimitTaxAssociationFilter()
		filter.EntityID = "sub_link_target"
		assocs, listErr := s.GetStores().TaxAssociationRepo.List(s.GetContext(), filter)
		s.Require().NoError(listErr)
		s.Require().Len(assocs, 1)
		s.Equal(rate.ID, assocs[0].TaxRateID)
		s.Equal(types.TaxRateEntityTypeSubscription, assocs[0].EntityType)
		s.Equal(7, assocs[0].Priority)
	})

	s.Run("existing_association_with_unknown_tax_rate_returns_error", func() {
		bogus := &dto.TaxAssociationResponse{}
		bogus.TaxRateID = "taxrate_missing"
		err := s.service.LinkTaxRatesToEntity(s.GetContext(), dto.LinkTaxRateToEntityRequest{
			EntityType:              types.TaxRateEntityTypeCustomer,
			EntityID:                "cust_link_bogus",
			ExistingTaxAssociations: []*dto.TaxAssociationResponse{bogus},
		})
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})
}

// ---------------------------------------------------------------------------
// PrepareTaxRatesForInvoice
// ---------------------------------------------------------------------------

func (s *TaxServiceSuite) TestPrepareTaxRatesForInvoice() {
	rateA := s.createPercentageTaxRate("Prep A", "prep-a", "10")
	rateB := s.createFixedTaxRate("Prep B", "prep-b", "2")

	s.Run("overrides_resolve_tax_rates_by_code", func() {
		rates, err := s.service.PrepareTaxRatesForInvoice(s.GetContext(), dto.CreateInvoiceRequest{
			TaxRateOverrides: []*dto.TaxRateOverride{
				{TaxRateCode: "prep-a", Currency: "usd"},
				{TaxRateCode: "prep-b", Currency: "usd"},
			},
		})
		s.NoError(err)
		s.Require().Len(rates, 2)
		codes := []string{rates[0].Code, rates[1].Code}
		s.ElementsMatch([]string{"prep-a", "prep-b"}, codes)
	})

	s.Run("subscription_with_auto_apply_association_returns_its_rates", func() {
		subID := "sub_prep_taxes"
		_, err := s.service.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
			TaxRateCode: "prep-a",
			EntityType:  types.TaxRateEntityTypeSubscription,
			EntityID:    subID,
			AutoApply:   true,
			Currency:    "usd",
		})
		s.Require().NoError(err)

		rates, err := s.service.PrepareTaxRatesForInvoice(s.GetContext(), dto.CreateInvoiceRequest{
			SubscriptionID: lo.ToPtr(subID),
			PeriodStart:    lo.ToPtr(s.now.Add(-24 * time.Hour)),
			PeriodEnd:      lo.ToPtr(s.now.Add(24 * time.Hour)),
		})
		s.NoError(err)
		s.Require().Len(rates, 1)
		s.Equal(rateA.ID, rates[0].ID)
		s.NotEqual(rateB.ID, rates[0].ID)
	})

	s.Run("subscription_without_associations_returns_empty", func() {
		rates, err := s.service.PrepareTaxRatesForInvoice(s.GetContext(), dto.CreateInvoiceRequest{
			SubscriptionID: lo.ToPtr("sub_without_taxes"),
		})
		s.NoError(err)
		s.Empty(rates)
	})

	s.Run("no_overrides_and_no_subscription_returns_empty", func() {
		rates, err := s.service.PrepareTaxRatesForInvoice(s.GetContext(), dto.CreateInvoiceRequest{})
		s.NoError(err)
		s.Empty(rates)
	})
}

// ---------------------------------------------------------------------------
// ApplyTaxesOnInvoice — billing-critical tax math
// ---------------------------------------------------------------------------

func (s *TaxServiceSuite) newInvoice(subtotal, discount string) *invoice.Invoice {
	return &invoice.Invoice{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		Currency:      "usd",
		Subtotal:      decimal.RequireFromString(subtotal),
		TotalDiscount: decimal.RequireFromString(discount),
	}
}

// newTaxRateResponse builds an unpersisted tax rate response — ApplyTaxesOnInvoice
// only reads rate fields and persists tax-applied records.
func (s *TaxServiceSuite) newTaxRateResponse(rateType types.TaxRateType, percentage, fixed *string) *dto.TaxRateResponse {
	tr := &taxrate.TaxRate{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
		Name:          "inline rate",
		Code:          "inline-" + types.GenerateUUID(),
		TaxRateType:   rateType,
		TaxRateStatus: types.TaxRateStatusActive,
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	if percentage != nil {
		tr.PercentageValue = lo.ToPtr(decimal.RequireFromString(*percentage))
	}
	if fixed != nil {
		tr.FixedValue = lo.ToPtr(decimal.RequireFromString(*fixed))
	}
	return &dto.TaxRateResponse{TaxRate: tr}
}

func (s *TaxServiceSuite) TestApplyTaxesOnInvoice() {
	testCases := []struct {
		name            string
		subtotal        string
		discount        string
		rates           []*dto.TaxRateResponse
		expectedTax     string
		expectedRecords int
	}{
		{
			name:            "no_tax_rates_returns_zero_tax",
			subtotal:        "100",
			discount:        "0",
			rates:           []*dto.TaxRateResponse{},
			expectedTax:     "0",
			expectedRecords: 0,
		},
		{
			name:     "percentage_tax_on_full_subtotal",
			subtotal: "100",
			discount: "0",
			rates: []*dto.TaxRateResponse{
				s.newTaxRateResponse(types.TaxRateTypePercentage, lo.ToPtr("10"), nil),
			},
			expectedTax:     "10",
			expectedRecords: 1,
		},
		{
			name:     "percentage_tax_rounds_to_currency_precision",
			subtotal: "100",
			discount: "0",
			rates: []*dto.TaxRateResponse{
				s.newTaxRateResponse(types.TaxRateTypePercentage, lo.ToPtr("8.875"), nil),
			},
			expectedTax:     "8.88",
			expectedRecords: 1,
		},
		{
			name:     "fixed_tax_is_applied_directly",
			subtotal: "100",
			discount: "0",
			rates: []*dto.TaxRateResponse{
				s.newTaxRateResponse(types.TaxRateTypeFixed, nil, lo.ToPtr("5.50")),
			},
			expectedTax:     "5.50",
			expectedRecords: 1,
		},
		{
			name:     "multiple_tax_rates_are_summed",
			subtotal: "100",
			discount: "0",
			rates: []*dto.TaxRateResponse{
				s.newTaxRateResponse(types.TaxRateTypePercentage, lo.ToPtr("10"), nil),
				s.newTaxRateResponse(types.TaxRateTypeFixed, nil, lo.ToPtr("2.50")),
			},
			expectedTax:     "12.50",
			expectedRecords: 2,
		},
		{
			name:     "discount_reduces_taxable_amount",
			subtotal: "100",
			discount: "40",
			rates: []*dto.TaxRateResponse{
				s.newTaxRateResponse(types.TaxRateTypePercentage, lo.ToPtr("10"), nil),
			},
			expectedTax:     "6",
			expectedRecords: 1,
		},
		{
			name:     "discount_exceeding_subtotal_clamps_taxable_to_zero",
			subtotal: "100",
			discount: "150",
			rates: []*dto.TaxRateResponse{
				s.newTaxRateResponse(types.TaxRateTypePercentage, lo.ToPtr("10"), nil),
			},
			expectedTax:     "0",
			expectedRecords: 1,
		},
		{
			name:     "zero_percent_tax_yields_zero_tax",
			subtotal: "100",
			discount: "0",
			rates: []*dto.TaxRateResponse{
				s.newTaxRateResponse(types.TaxRateTypePercentage, lo.ToPtr("0"), nil),
			},
			expectedTax:     "0",
			expectedRecords: 1,
		},
		{
			name:     "percentage_rate_missing_value_is_skipped",
			subtotal: "100",
			discount: "0",
			rates: []*dto.TaxRateResponse{
				s.newTaxRateResponse(types.TaxRateTypePercentage, nil, nil),
			},
			expectedTax:     "0",
			expectedRecords: 0,
		},
		{
			name:     "fixed_rate_missing_value_is_skipped",
			subtotal: "100",
			discount: "0",
			rates: []*dto.TaxRateResponse{
				s.newTaxRateResponse(types.TaxRateTypeFixed, nil, nil),
			},
			expectedTax:     "0",
			expectedRecords: 0,
		},
		{
			name:     "unknown_rate_type_is_skipped",
			subtotal: "100",
			discount: "0",
			rates: []*dto.TaxRateResponse{
				s.newTaxRateResponse(types.TaxRateType("bogus"), nil, nil),
			},
			expectedTax:     "0",
			expectedRecords: 0,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			inv := s.newInvoice(tc.subtotal, tc.discount)
			result, err := s.service.ApplyTaxesOnInvoice(s.GetContext(), inv, tc.rates)
			s.Require().NoError(err)
			s.Require().NotNil(result)

			s.True(result.TotalTaxAmount.Equal(decimal.RequireFromString(tc.expectedTax)),
				"expected total tax %s, got %s", tc.expectedTax, result.TotalTaxAmount.String())
			s.Len(result.TaxAppliedRecords, tc.expectedRecords)
			s.Equal(tc.rates, result.TaxRates)

			// Read back persisted tax applied records for this invoice
			filter := types.NewNoLimitTaxAppliedFilter()
			filter.EntityID = inv.ID
			filter.EntityType = types.TaxRateEntityTypeInvoice
			stored, err := s.GetStores().TaxAppliedRepo.List(s.GetContext(), filter)
			s.Require().NoError(err)
			s.Len(stored, tc.expectedRecords)
		})
	}
}

func (s *TaxServiceSuite) TestApplyTaxesOnInvoice_TaxAppliedRecordFields() {
	inv := s.newInvoice("200", "50")
	rate := s.newTaxRateResponse(types.TaxRateTypePercentage, lo.ToPtr("10"), nil)

	result, err := s.service.ApplyTaxesOnInvoice(s.GetContext(), inv, []*dto.TaxRateResponse{rate})
	s.Require().NoError(err)
	s.Require().Len(result.TaxAppliedRecords, 1)

	record := result.TaxAppliedRecords[0]
	s.Equal(rate.ID, record.TaxRateID)
	s.Equal(types.TaxRateEntityTypeInvoice, record.EntityType)
	s.Equal(inv.ID, record.EntityID)
	s.Equal("usd", record.Currency)
	s.True(record.TaxableAmount.Equal(decimal.RequireFromString("150")))
	s.True(record.TaxAmount.Equal(decimal.RequireFromString("15")))
	s.Require().NotNil(record.IdempotencyKey)

	stored, err := s.GetStores().TaxAppliedRepo.Get(s.GetContext(), record.ID)
	s.Require().NoError(err)
	s.True(stored.TaxAmount.Equal(decimal.RequireFromString("15")))
}

func (s *TaxServiceSuite) TestApplyTaxesOnInvoice_IsIdempotent() {
	inv := s.newInvoice("100", "0")
	rate := s.newTaxRateResponse(types.TaxRateTypePercentage, lo.ToPtr("10"), nil)

	// First application creates a record
	first, err := s.service.ApplyTaxesOnInvoice(s.GetContext(), inv, []*dto.TaxRateResponse{rate})
	s.Require().NoError(err)
	s.Require().Len(first.TaxAppliedRecords, 1)
	s.True(first.TotalTaxAmount.Equal(decimal.RequireFromString("10")))
	firstRecordID := first.TaxAppliedRecords[0].ID

	// Recompute after the invoice subtotal changed — must update, not duplicate
	inv.Subtotal = decimal.RequireFromString("200")
	second, err := s.service.ApplyTaxesOnInvoice(s.GetContext(), inv, []*dto.TaxRateResponse{rate})
	s.Require().NoError(err)
	s.Require().Len(second.TaxAppliedRecords, 1)
	s.True(second.TotalTaxAmount.Equal(decimal.RequireFromString("20")))
	s.Equal(firstRecordID, second.TaxAppliedRecords[0].ID, "second application must reuse the same record")

	// Exactly one persisted record for the invoice — no double-billing
	filter := types.NewNoLimitTaxAppliedFilter()
	filter.EntityID = inv.ID
	stored, err := s.GetStores().TaxAppliedRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	s.Require().Len(stored, 1)
	s.True(stored[0].TaxableAmount.Equal(decimal.RequireFromString("200")))
	s.True(stored[0].TaxAmount.Equal(decimal.RequireFromString("20")))
}
