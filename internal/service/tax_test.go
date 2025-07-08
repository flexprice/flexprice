package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	"github.com/flexprice/flexprice/internal/domain/taxassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type TaxServiceSuite struct {
	testutil.BaseServiceTestSuite
	service         TaxService
	customerService CustomerService
	invoiceService  InvoiceService
	subsService     SubscriptionService
	testData        struct {
		taxRates struct {
			vatPercentage *taxrate.TaxRate
			gstPercentage *taxrate.TaxRate
			fixedTax      *taxrate.TaxRate
			inactiveTax   *taxrate.TaxRate
		}
		customers struct {
			customer1 *customer.Customer
			customer2 *customer.Customer
		}
		plans struct {
			basicPlan *plan.Plan
		}
		subscriptions struct {
			subscription1 *subscription.Subscription
		}
		invoices struct {
			invoice1 *invoice.Invoice
		}
		taxAssociations struct {
			customerVat     *taxassociation.TaxAssociation
			subscriptionGst *taxassociation.TaxAssociation
		}
		taxApplied struct {
			invoiceVat *taxapplied.TaxApplied
		}
		now time.Time
	}
}

func TestTaxService(t *testing.T) {
	suite.Run(t, new(TaxServiceSuite))
}

func (s *TaxServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *TaxServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *TaxServiceSuite) setupService() {
	serviceParams := ServiceParams{
		Logger:             s.GetLogger(),
		Config:             s.GetConfig(),
		DB:                 s.GetDB(),
		TaxRateRepo:        s.GetStores().TaxRateRepo,
		TaxAppliedRepo:     s.GetStores().TaxAppliedRepo,
		TaxAssociationRepo: s.GetStores().TaxAssociationRepo,
		CustomerRepo:       s.GetStores().CustomerRepo,
		InvoiceRepo:        s.GetStores().InvoiceRepo,
		SubRepo:            s.GetStores().SubscriptionRepo,
		PlanRepo:           s.GetStores().PlanRepo,
		EventPublisher:     s.GetPublisher(),
		WebhookPublisher:   s.GetWebhookPublisher(),
	}

	s.service = NewTaxService(serviceParams)
	s.customerService = NewCustomerService(serviceParams)
	s.invoiceService = NewInvoiceService(serviceParams)
	s.subsService = NewSubscriptionService(serviceParams)
}

func (s *TaxServiceSuite) setupTestData() {
	s.testData.now = time.Now().UTC()
	s.setupTaxRates()
	s.setupCustomers()
	s.setupPlans()
}

func (s *TaxServiceSuite) setupTaxRates() {
	ctx := s.GetContext()

	// VAT Percentage Tax (20%)
	s.testData.taxRates.vatPercentage = &taxrate.TaxRate{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
		Name:            "VAT 20%",
		Code:            "VAT20",
		Description:     "Value Added Tax 20%",
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromFloat(20.0)),
		Currency:        "usd",
		Scope:           types.TaxRateScopeExternal,
		TaxRateStatus:   types.TaxRateStatusActive,
		ValidFrom:       lo.ToPtr(s.testData.now.AddDate(0, 0, -30)),
		ValidTo:         lo.ToPtr(s.testData.now.AddDate(0, 0, 30)),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}

	// GST Percentage Tax (10%)
	s.testData.taxRates.gstPercentage = &taxrate.TaxRate{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
		Name:            "GST 10%",
		Code:            "GST10",
		Description:     "Goods and Services Tax 10%",
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromFloat(10.0)),
		Currency:        "usd",
		Scope:           types.TaxRateScopeExternal,
		TaxRateStatus:   types.TaxRateStatusActive,
		ValidFrom:       lo.ToPtr(s.testData.now.AddDate(0, 0, -30)),
		ValidTo:         lo.ToPtr(s.testData.now.AddDate(0, 0, 30)),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}

	// Fixed Tax ($5)
	s.testData.taxRates.fixedTax = &taxrate.TaxRate{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
		Name:          "Fixed Tax",
		Code:          "FIXED5",
		Description:   "Fixed Tax of $5",
		TaxRateType:   types.TaxRateTypeFixed,
		FixedValue:    lo.ToPtr(decimal.NewFromFloat(5.0)),
		Currency:      "usd",
		Scope:         types.TaxRateScopeExternal,
		TaxRateStatus: types.TaxRateStatusActive,
		ValidFrom:     lo.ToPtr(s.testData.now.AddDate(0, 0, -30)),
		ValidTo:       lo.ToPtr(s.testData.now.AddDate(0, 0, 30)),
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	// Inactive Tax
	s.testData.taxRates.inactiveTax = &taxrate.TaxRate{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
		Name:            "Inactive Tax",
		Code:            "INACTIVE",
		Description:     "Inactive Tax Rate",
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromFloat(15.0)),
		Currency:        "usd",
		Scope:           types.TaxRateScopeExternal,
		TaxRateStatus:   types.TaxRateStatusInactive,
		ValidFrom:       lo.ToPtr(s.testData.now.AddDate(0, 0, -60)),
		ValidTo:         lo.ToPtr(s.testData.now.AddDate(0, 0, -30)),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}

	// Create tax rates in repository
	err := s.GetStores().TaxRateRepo.Create(ctx, s.testData.taxRates.vatPercentage)
	s.Require().NoError(err)

	err = s.GetStores().TaxRateRepo.Create(ctx, s.testData.taxRates.gstPercentage)
	s.Require().NoError(err)

	err = s.GetStores().TaxRateRepo.Create(ctx, s.testData.taxRates.fixedTax)
	s.Require().NoError(err)

	err = s.GetStores().TaxRateRepo.Create(ctx, s.testData.taxRates.inactiveTax)
	s.Require().NoError(err)
}

func (s *TaxServiceSuite) setupCustomers() {
	ctx := s.GetContext()

	s.testData.customers.customer1 = &customer.Customer{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID:    "cust-001",
		Name:          "Test Customer 1",
		Email:         "customer1@example.com",
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	s.testData.customers.customer2 = &customer.Customer{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID:    "cust-002",
		Name:          "Test Customer 2",
		Email:         "customer2@example.com",
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	err := s.GetStores().CustomerRepo.Create(ctx, s.testData.customers.customer1)
	s.Require().NoError(err)

	err = s.GetStores().CustomerRepo.Create(ctx, s.testData.customers.customer2)
	s.Require().NoError(err)
}

func (s *TaxServiceSuite) setupPlans() {
	ctx := s.GetContext()

	s.testData.plans.basicPlan = &plan.Plan{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:          "Basic Plan",
		LookupKey:     "basic-plan-001",
		Description:   "Basic subscription plan",
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	err := s.GetStores().PlanRepo.Create(ctx, s.testData.plans.basicPlan)
	s.Require().NoError(err)
}

// =============================================================================
// Tax Rate CRUD Tests
// =============================================================================

func (s *TaxServiceSuite) TestCreateTaxRate_Percentage() {
	req := dto.CreateTaxRateRequest{
		Name:            "Sales Tax",
		Code:            "SALES8",
		Description:     "Sales Tax 8%",
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromFloat(8.0)),
		Currency:        "usd",
		Scope:           types.TaxRateScopeExternal,
		ValidFrom:       lo.ToPtr(s.testData.now.AddDate(0, 0, -1)),
		ValidTo:         lo.ToPtr(s.testData.now.AddDate(1, 0, 0)),
		Metadata:        map[string]string{"region": "US"},
	}

	resp, err := s.service.CreateTaxRate(s.GetContext(), req)

	s.NoError(err)
	s.NotNil(resp)
	s.Equal(req.Name, resp.Name)
	s.Equal(req.Code, resp.Code)
	s.Equal(req.Description, resp.Description)
	s.Equal(req.TaxRateType, resp.TaxRateType)
	s.Equal(req.PercentageValue, resp.PercentageValue)
	s.Equal(req.Currency, resp.Currency)
	s.Equal(req.Scope, resp.Scope)
	s.Equal(types.TaxRateStatusActive, resp.TaxRateStatus)
	s.Equal(req.Metadata, resp.Metadata)
	s.NotEmpty(resp.ID)
	s.NotNil(resp.ValidFrom)
	s.NotNil(resp.ValidTo)
}

func (s *TaxServiceSuite) TestCreateTaxRate_Fixed() {
	req := dto.CreateTaxRateRequest{
		Name:        "Fixed Fee",
		Code:        "FIXED10",
		Description: "Fixed Fee of $10",
		TaxRateType: types.TaxRateTypeFixed,
		FixedValue:  lo.ToPtr(decimal.NewFromFloat(10.0)),
		Currency:    "usd",
		Scope:       types.TaxRateScopeExternal,
		ValidFrom:   lo.ToPtr(s.testData.now.AddDate(0, 0, -1)),
		ValidTo:     lo.ToPtr(s.testData.now.AddDate(1, 0, 0)),
	}

	resp, err := s.service.CreateTaxRate(s.GetContext(), req)

	s.NoError(err)
	s.NotNil(resp)
	s.Equal(req.Name, resp.Name)
	s.Equal(req.Code, resp.Code)
	s.Equal(req.TaxRateType, resp.TaxRateType)
	s.Equal(req.FixedValue, resp.FixedValue)
	s.Equal(types.TaxRateStatusActive, resp.TaxRateStatus)
}

func (s *TaxServiceSuite) TestCreateTaxRate_ValidationErrors() {
	testCases := []struct {
		name        string
		req         dto.CreateTaxRateRequest
		expectedErr string
	}{
		{
			name: "missing_name",
			req: dto.CreateTaxRateRequest{
				Code:        "TEST",
				TaxRateType: types.TaxRateTypePercentage,
				Currency:    "usd",
				Scope:       types.TaxRateScopeExternal,
			},
			expectedErr: "name is required",
		},
		{
			name: "missing_code",
			req: dto.CreateTaxRateRequest{
				Name:        "Test Tax",
				TaxRateType: types.TaxRateTypePercentage,
				Currency:    "usd",
				Scope:       types.TaxRateScopeExternal,
			},
			expectedErr: "code is required",
		},
		{
			name: "percentage_without_value",
			req: dto.CreateTaxRateRequest{
				Name:        "Test Tax",
				Code:        "TEST",
				TaxRateType: types.TaxRateTypePercentage,
				Currency:    "usd",
				Scope:       types.TaxRateScopeExternal,
			},
			expectedErr: "percentage_value is required",
		},
		{
			name: "fixed_without_value",
			req: dto.CreateTaxRateRequest{
				Name:        "Test Tax",
				Code:        "TEST",
				TaxRateType: types.TaxRateTypeFixed,
				Currency:    "usd",
				Scope:       types.TaxRateScopeExternal,
			},
			expectedErr: "fixed_value is required",
		},
		{
			name: "negative_percentage",
			req: dto.CreateTaxRateRequest{
				Name:            "Test Tax",
				Code:            "TEST",
				TaxRateType:     types.TaxRateTypePercentage,
				PercentageValue: lo.ToPtr(decimal.NewFromFloat(-5.0)),
				Currency:        "usd",
				Scope:           types.TaxRateScopeExternal,
			},
			expectedErr: "percentage_value cannot be negative",
		},
		{
			name: "percentage_over_100",
			req: dto.CreateTaxRateRequest{
				Name:            "Test Tax",
				Code:            "TEST",
				TaxRateType:     types.TaxRateTypePercentage,
				PercentageValue: lo.ToPtr(decimal.NewFromFloat(150.0)),
				Currency:        "usd",
				Scope:           types.TaxRateScopeExternal,
			},
			expectedErr: "percentage_value cannot be negative",
		},
		{
			name: "negative_fixed_value",
			req: dto.CreateTaxRateRequest{
				Name:        "Test Tax",
				Code:        "TEST",
				TaxRateType: types.TaxRateTypeFixed,
				FixedValue:  lo.ToPtr(decimal.NewFromFloat(-10.0)),
				Currency:    "usd",
				Scope:       types.TaxRateScopeExternal,
			},
			expectedErr: "fixed_value cannot be negative",
		},
		{
			name: "both_percentage_and_fixed",
			req: dto.CreateTaxRateRequest{
				Name:            "Test Tax",
				Code:            "TEST",
				TaxRateType:     types.TaxRateTypePercentage,
				PercentageValue: lo.ToPtr(decimal.NewFromFloat(10.0)),
				FixedValue:      lo.ToPtr(decimal.NewFromFloat(5.0)),
				Currency:        "usd",
				Scope:           types.TaxRateScopeExternal,
			},
			expectedErr: "percentage_value and fixed_value cannot be provided together",
		},
		{
			name: "invalid_date_range",
			req: dto.CreateTaxRateRequest{
				Name:            "Test Tax",
				Code:            "TEST",
				TaxRateType:     types.TaxRateTypePercentage,
				PercentageValue: lo.ToPtr(decimal.NewFromFloat(10.0)),
				Currency:        "usd",
				Scope:           types.TaxRateScopeExternal,
				ValidFrom:       lo.ToPtr(s.testData.now.AddDate(0, 0, 1)),
				ValidTo:         lo.ToPtr(s.testData.now.AddDate(0, 0, -1)),
			},
			expectedErr: "valid_from cannot be after valid_to",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.CreateTaxRate(s.GetContext(), tc.req)

			s.Error(err)
			s.Nil(resp)
			s.Contains(err.Error(), tc.expectedErr)
		})
	}
}

func (s *TaxServiceSuite) TestCreateTaxRate_DuplicateCode() {
	// First creation should succeed
	req1 := dto.CreateTaxRateRequest{
		Name:            "First Tax",
		Code:            "DUPLICATE",
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromFloat(10.0)),
		Currency:        "usd",
		Scope:           types.TaxRateScopeExternal,
	}

	resp1, err := s.service.CreateTaxRate(s.GetContext(), req1)
	s.NoError(err)
	s.NotNil(resp1)

	// Second creation with same code should fail
	req2 := dto.CreateTaxRateRequest{
		Name:            "Second Tax",
		Code:            "DUPLICATE",
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromFloat(15.0)),
		Currency:        "usd",
		Scope:           types.TaxRateScopeExternal,
	}

	resp2, err := s.service.CreateTaxRate(s.GetContext(), req2)
	s.Error(err)
	s.Nil(resp2)
	s.Contains(err.Error(), "tax rate with this code already exists")
}

func (s *TaxServiceSuite) TestGetTaxRate() {
	// Test successful retrieval
	resp, err := s.service.GetTaxRate(s.GetContext(), s.testData.taxRates.vatPercentage.ID)

	s.NoError(err)
	s.NotNil(resp)
	s.Equal(s.testData.taxRates.vatPercentage.ID, resp.ID)
	s.Equal(s.testData.taxRates.vatPercentage.Name, resp.Name)
	s.Equal(s.testData.taxRates.vatPercentage.Code, resp.Code)
}

func (s *TaxServiceSuite) TestGetTaxRate_NotFound() {
	resp, err := s.service.GetTaxRate(s.GetContext(), "non-existent-id")

	s.Error(err)
	s.Nil(resp)
	s.True(ierr.IsNotFound(err))
}

func (s *TaxServiceSuite) TestGetTaxRate_EmptyID() {
	resp, err := s.service.GetTaxRate(s.GetContext(), "")

	s.Error(err)
	s.Nil(resp)
	s.Contains(err.Error(), "tax_rate_id is required")
}

func (s *TaxServiceSuite) TestGetTaxRateByCode() {
	// Test successful retrieval
	resp, err := s.service.GetTaxRateByCode(s.GetContext(), s.testData.taxRates.vatPercentage.Code)

	s.NoError(err)
	s.NotNil(resp)
	s.Equal(s.testData.taxRates.vatPercentage.ID, resp.ID)
	s.Equal(s.testData.taxRates.vatPercentage.Code, resp.Code)
}

func (s *TaxServiceSuite) TestGetTaxRateByCode_NotFound() {
	resp, err := s.service.GetTaxRateByCode(s.GetContext(), "NON-EXISTENT")

	s.Error(err)
	s.Nil(resp)
	s.True(ierr.IsNotFound(err))
}

func (s *TaxServiceSuite) TestGetTaxRateByCode_EmptyCode() {
	resp, err := s.service.GetTaxRateByCode(s.GetContext(), "")

	s.Error(err)
	s.Nil(resp)
	s.Contains(err.Error(), "tax_rate_code is required")
}

func (s *TaxServiceSuite) TestListTaxRates() {
	// Test listing all tax rates
	filter := types.NewDefaultTaxRateFilter()
	resp, err := s.service.ListTaxRates(s.GetContext(), filter)

	s.NoError(err)
	s.NotNil(resp)
	s.Len(resp.Items, 4) // We created 4 tax rates in setup
	s.NotNil(resp.Pagination)
	s.Equal(4, resp.Pagination.Total)
}

func (s *TaxServiceSuite) TestListTaxRates_WithFilters() {
	testCases := []struct {
		name          string
		filter        *types.TaxRateFilter
		expectedCount int
		expectedCodes []string
	}{
		{
			name: "filter_by_code",
			filter: &types.TaxRateFilter{
				QueryFilter: types.NewDefaultQueryFilter(),
				Code:        "VAT20",
			},
			expectedCount: 1,
			expectedCodes: []string{"VAT20"},
		},
		{
			name: "filter_by_scope",
			filter: &types.TaxRateFilter{
				QueryFilter: types.NewDefaultQueryFilter(),
				Scope:       types.TaxRateScopeExternal,
			},
			expectedCount: 4,
			expectedCodes: []string{"VAT20", "GST10", "FIXED5", "INACTIVE"},
		},
		{
			name: "filter_by_tax_rate_ids",
			filter: &types.TaxRateFilter{
				QueryFilter: types.NewDefaultQueryFilter(),
				TaxRateIDs:  []string{s.testData.taxRates.vatPercentage.ID, s.testData.taxRates.fixedTax.ID},
			},
			expectedCount: 2,
			expectedCodes: []string{"VAT20", "FIXED5"},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.ListTaxRates(s.GetContext(), tc.filter)

			s.NoError(err)
			s.NotNil(resp)
			s.Len(resp.Items, tc.expectedCount)

			codes := make([]string, len(resp.Items))
			for i, item := range resp.Items {
				codes[i] = item.Code
			}

			for _, expectedCode := range tc.expectedCodes {
				s.Contains(codes, expectedCode)
			}
		})
	}
}

func (s *TaxServiceSuite) TestUpdateTaxRate() {
	// Create a new tax rate for updating
	createReq := dto.CreateTaxRateRequest{
		Name:            "Original Tax",
		Code:            "ORIGINAL",
		Description:     "Original Description",
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromFloat(10.0)),
		Currency:        "usd",
		Scope:           types.TaxRateScopeExternal,
	}

	createResp, err := s.service.CreateTaxRate(s.GetContext(), createReq)
	s.Require().NoError(err)
	s.Require().NotNil(createResp)

	// Test successful update
	updateReq := dto.UpdateTaxRateRequest{
		Name:        "Updated Tax",
		Description: "Updated Description",
		Metadata:    map[string]string{"updated": "true"},
	}

	updateResp, err := s.service.UpdateTaxRate(s.GetContext(), createResp.ID, updateReq)

	s.NoError(err)
	s.NotNil(updateResp)
	s.Equal(updateReq.Name, updateResp.Name)
	s.Equal(updateReq.Description, updateResp.Description)
	s.Equal(updateReq.Metadata, updateResp.Metadata)
	s.Equal(createReq.Code, updateResp.Code) // Code shouldn't change
}

func (s *TaxServiceSuite) TestUpdateTaxRate_ValidationChecks() {
	// First, create a tax association to test validation
	taxAssociation := &taxassociation.TaxAssociation{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_ASSOCIATION),
		TaxRateID:     s.testData.taxRates.vatPercentage.ID,
		EntityType:    types.TaxrateEntityTypeCustomer,
		EntityID:      s.testData.customers.customer1.ID,
		Priority:      1,
		AutoApply:     true,
		Currency:      "usd",
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}

	err := s.GetStores().TaxAssociationRepo.Create(s.GetContext(), taxAssociation)
	s.Require().NoError(err)

	// Now try to update the tax rate that has associations
	updateReq := dto.UpdateTaxRateRequest{
		Name: "Updated VAT",
	}

	updateResp, err := s.service.UpdateTaxRate(s.GetContext(), s.testData.taxRates.vatPercentage.ID, updateReq)

	s.Error(err)
	s.Nil(updateResp)
	s.Contains(err.Error(), "tax rate is being used in tax assignments, cannot update")
}

func (s *TaxServiceSuite) TestUpdateTaxRate_WithTaxAppliedValidation() {
	// Create a tax applied record to test the enhanced validation
	// First create an invoice
	invoice := &invoice.Invoice{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:      s.testData.customers.customer1.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(100.0),
		Total:           decimal.NewFromFloat(120.0),
		Subtotal:        decimal.NewFromFloat(100.0),
		TotalTax:        decimal.NewFromFloat(20.0),
		AmountRemaining: decimal.NewFromFloat(120.0),
		EnvironmentID:   types.GetEnvironmentID(s.GetContext()),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}

	err := s.GetStores().InvoiceRepo.Create(s.GetContext(), invoice)
	s.Require().NoError(err)

	// Create a tax applied record
	taxApplied := &taxapplied.TaxApplied{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_APPLIED),
		TaxRateID:     s.testData.taxRates.gstPercentage.ID,
		EntityType:    types.TaxrateEntityTypeInvoice,
		EntityID:      invoice.ID,
		TaxableAmount: decimal.NewFromFloat(100.0),
		TaxAmount:     decimal.NewFromFloat(10.0),
		Currency:      "usd",
		AppliedAt:     s.testData.now,
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}

	err = s.GetStores().TaxAppliedRepo.Create(s.GetContext(), taxApplied)
	s.Require().NoError(err)

	// Now try to update the tax rate that has been applied
	updateReq := dto.UpdateTaxRateRequest{
		Name: "Updated GST",
	}

	updateResp, err := s.service.UpdateTaxRate(s.GetContext(), s.testData.taxRates.gstPercentage.ID, updateReq)

	// This should still work in the current implementation, but this test demonstrates
	// where we could add the additional validation mentioned in the requirements
	s.NoError(err)
	s.NotNil(updateResp)

	// TODO: Add enhanced validation to check for tax applied records
	// The test shows that we may want to prevent updates when tax has been applied
}

func (s *TaxServiceSuite) TestUpdateTaxRate_ValidationErrors() {
	testCases := []struct {
		name        string
		taxRateID   string
		req         dto.UpdateTaxRateRequest
		expectedErr string
	}{
		{
			name:        "empty_tax_rate_id",
			taxRateID:   "",
			req:         dto.UpdateTaxRateRequest{Name: "Test"},
			expectedErr: "tax_rate_id is required",
		},
		{
			name:        "non_existent_tax_rate",
			taxRateID:   "non-existent-id",
			req:         dto.UpdateTaxRateRequest{Name: "Test"},
			expectedErr: "not found",
		},
		{
			name:        "no_fields_to_update",
			taxRateID:   s.testData.taxRates.fixedTax.ID,
			req:         dto.UpdateTaxRateRequest{},
			expectedErr: "at least one field must be provided for update",
		},
		{
			name:      "invalid_date_range",
			taxRateID: s.testData.taxRates.fixedTax.ID,
			req: dto.UpdateTaxRateRequest{
				ValidFrom: lo.ToPtr(s.testData.now.AddDate(0, 0, 1)),
				ValidTo:   lo.ToPtr(s.testData.now.AddDate(0, 0, -1)),
			},
			expectedErr: "valid_from cannot be after valid_to",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.UpdateTaxRate(s.GetContext(), tc.taxRateID, tc.req)

			s.Error(err)
			s.Nil(resp)
			s.Contains(err.Error(), tc.expectedErr)
		})
	}
}

func (s *TaxServiceSuite) TestDeleteTaxRate() {
	// Create a new tax rate for deletion
	createReq := dto.CreateTaxRateRequest{
		Name:            "To Delete",
		Code:            "DELETE_ME",
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromFloat(15.0)),
		Currency:        "usd",
		Scope:           types.TaxRateScopeExternal,
	}

	createResp, err := s.service.CreateTaxRate(s.GetContext(), createReq)
	s.Require().NoError(err)
	s.Require().NotNil(createResp)

	// Test successful deletion
	err = s.service.DeleteTaxRate(s.GetContext(), createResp.ID)
	s.NoError(err)

	// Verify tax rate is no longer accessible
	getResp, err := s.service.GetTaxRate(s.GetContext(), createResp.ID)
	s.Error(err)
	s.Nil(getResp)
	s.True(ierr.IsNotFound(err))
}

// =============================================================================
// Complex Workflow Tests
// =============================================================================

func (s *TaxServiceSuite) TestCustomerCreationWithTaxRateOverrides_NewTaxRates() {
	// Test scenario 5: Create a customer with tax_rate_overrides (new tax rates)
	req := dto.CreateCustomerRequest{
		ExternalID: "test-customer-with-new-taxes",
		Name:       "Customer with New Tax Rates",
		Email:      "new-taxes@example.com",
		TaxRateOverrides: []*dto.TaxRateOverride{
			{
				CreateTaxRateRequest: dto.CreateTaxRateRequest{
					Name:            "New Customer Tax",
					Code:            "CUST_NEW_15",
					Description:     "New 15% tax for customer",
					TaxRateType:     types.TaxRateTypePercentage,
					PercentageValue: lo.ToPtr(decimal.NewFromFloat(15.0)),
					Currency:        "usd",
					Scope:           types.TaxRateScopeExternal,
				},
				Priority:  1,
				AutoApply: true,
			},
			{
				CreateTaxRateRequest: dto.CreateTaxRateRequest{
					Name:        "Fixed Service Fee",
					Code:        "CUST_FEE_2",
					Description: "Fixed $2 service fee",
					TaxRateType: types.TaxRateTypeFixed,
					FixedValue:  lo.ToPtr(decimal.NewFromFloat(2.0)),
					Currency:    "usd",
					Scope:       types.TaxRateScopeExternal,
				},
				Priority:  2,
				AutoApply: false,
			},
		},
	}

	resp, err := s.customerService.CreateCustomer(s.GetContext(), req)

	s.NoError(err)
	s.NotNil(resp)
	s.Equal(req.ExternalID, resp.ExternalID)
	s.Equal(req.Name, resp.Name)

	// Verify tax associations were created
	filter := types.NewTaxAssociationFilter()
	filter.EntityType = types.TaxrateEntityTypeCustomer
	filter.EntityID = resp.ID

	associations, err := s.service.ListTaxAssociations(s.GetContext(), filter)
	s.NoError(err)
	s.NotNil(associations)
	s.Len(associations.Items, 2) // Should have 2 tax associations

	// Verify the tax rates were created
	for _, assoc := range associations.Items {
		taxRate, err := s.service.GetTaxRate(s.GetContext(), assoc.TaxRateID)
		s.NoError(err)
		s.NotNil(taxRate)
		s.Contains([]string{"CUST_NEW_15", "CUST_FEE_2"}, taxRate.Code)
	}
}

func (s *TaxServiceSuite) TestCustomerCreationWithTaxRateOverrides_MixedTaxRates() {
	// Test scenario 6: Create a customer with mixed new and existing tax rates
	req := dto.CreateCustomerRequest{
		ExternalID: "test-customer-mixed-taxes",
		Name:       "Customer with Mixed Tax Rates",
		Email:      "mixed-taxes@example.com",
		TaxRateOverrides: []*dto.TaxRateOverride{
			{
				// Use existing tax rate
				TaxRateID: lo.ToPtr(s.testData.taxRates.vatPercentage.ID),
				Priority:  1,
				AutoApply: true,
			},
			{
				// Create new tax rate
				CreateTaxRateRequest: dto.CreateTaxRateRequest{
					Name:            "Custom State Tax",
					Code:            "STATE_7",
					Description:     "State tax 7%",
					TaxRateType:     types.TaxRateTypePercentage,
					PercentageValue: lo.ToPtr(decimal.NewFromFloat(7.0)),
					Currency:        "usd",
					Scope:           types.TaxRateScopeExternal,
				},
				Priority:  2,
				AutoApply: false,
			},
			{
				// Use another existing tax rate
				TaxRateID: lo.ToPtr(s.testData.taxRates.fixedTax.ID),
				Priority:  3,
				AutoApply: true,
			},
		},
	}

	resp, err := s.customerService.CreateCustomer(s.GetContext(), req)

	s.NoError(err)
	s.NotNil(resp)

	// Verify tax associations were created
	filter := types.NewTaxAssociationFilter()
	filter.EntityType = types.TaxrateEntityTypeCustomer
	filter.EntityID = resp.ID

	associations, err := s.service.ListTaxAssociations(s.GetContext(), filter)
	s.NoError(err)
	s.NotNil(associations)
	s.Len(associations.Items, 3) // Should have 3 tax associations

	// Verify priorities and auto apply settings
	for _, assoc := range associations.Items {
		taxRate, err := s.service.GetTaxRate(s.GetContext(), assoc.TaxRateID)
		s.NoError(err)
		s.NotNil(taxRate)

		switch taxRate.Code {
		case "VAT20":
			s.Equal(1, assoc.Priority)
			s.True(assoc.AutoApply)
		case "STATE_7":
			s.Equal(2, assoc.Priority)
			s.False(assoc.AutoApply)
		case "FIXED5":
			s.Equal(3, assoc.Priority)
			s.True(assoc.AutoApply)
		default:
			s.Fail("Unexpected tax rate code: " + taxRate.Code)
		}
	}
}

func (s *TaxServiceSuite) TestSubscriptionCreationWithTaxRateOverrides() {
	// Test scenario 7: Create a subscription with tax_rate_overrides

	// First check if we have a price to use
	// Note: This is a simplified test since we don't have full price setup
	// In a real scenario, we'd need to create proper prices with the plan

	subReq := dto.CreateSubscriptionRequest{
		CustomerID:   s.testData.customers.customer1.ID,
		PlanID:       s.testData.plans.basicPlan.ID,
		StartDate:    s.testData.now,
		BillingCycle: types.BillingCycleAnniversary,
		TaxRateOverrides: []*dto.TaxRateOverride{
			{
				CreateTaxRateRequest: dto.CreateTaxRateRequest{
					Name:            "Subscription Tax",
					Code:            "SUBS_TAX_12",
					Description:     "12% subscription tax",
					TaxRateType:     types.TaxRateTypePercentage,
					PercentageValue: lo.ToPtr(decimal.NewFromFloat(12.0)),
					Currency:        "usd",
					Scope:           types.TaxRateScopeExternal,
				},
				Priority:  1,
				AutoApply: true,
			},
			{
				TaxRateID: lo.ToPtr(s.testData.taxRates.gstPercentage.ID),
				Priority:  2,
				AutoApply: false,
			},
		},
	}

	// Note: This test might need to be adjusted based on subscription service implementation
	// For now, we'll test the tax service directly with the LinkTaxRatesToEntity method

	// Simulate subscription creation by directly testing tax linking
	mockSubscriptionID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION)

	// Convert tax rate overrides to entity tax associations
	entityTaxLinks := make([]*dto.CreateEntityTaxAssociation, len(subReq.TaxRateOverrides))
	for i, override := range subReq.TaxRateOverrides {
		entityTaxLinks[i] = override.ToTaxEntityAssociation(s.GetContext(), mockSubscriptionID, types.TaxrateEntityTypeSubscription)
	}

	linkResp, err := s.service.LinkTaxRatesToEntity(s.GetContext(), types.TaxrateEntityTypeSubscription, mockSubscriptionID, entityTaxLinks)

	s.NoError(err)
	s.NotNil(linkResp)
	s.Equal(mockSubscriptionID, linkResp.EntityID)
	s.Equal(types.TaxrateEntityTypeSubscription, linkResp.EntityType)
	s.Len(linkResp.LinkedTaxRates, 2)

	// Verify that one was created and one was existing
	createdCount := 0
	existingCount := 0
	for _, linkedTax := range linkResp.LinkedTaxRates {
		if linkedTax.WasCreated {
			createdCount++
		} else {
			existingCount++
		}
	}
	s.Equal(1, createdCount)  // One new tax rate
	s.Equal(1, existingCount) // One existing tax rate
}

func (s *TaxServiceSuite) TestOneOffInvoiceWithTaxRateOverrides() {
	// Test scenario 10: Create a one-off invoice with tax_rate_overrides
	invoiceReq := dto.CreateInvoiceRequest{
		CustomerID:  s.testData.customers.customer1.ID,
		InvoiceType: types.InvoiceTypeOneOff,
		Currency:    "usd",
		AmountDue:   decimal.NewFromFloat(500.0),
		Total:       decimal.NewFromFloat(575.0),
		Subtotal:    decimal.NewFromFloat(500.0),
		Description: "One-off invoice with custom taxes",
		TaxRateOverrides: []*dto.TaxRateOverride{
			{
				CreateTaxRateRequest: dto.CreateTaxRateRequest{
					Name:            "One-off Invoice Tax",
					Code:            "ONEOFF_TAX_15",
					Description:     "15% tax for one-off invoice",
					TaxRateType:     types.TaxRateTypePercentage,
					PercentageValue: lo.ToPtr(decimal.NewFromFloat(15.0)),
					Currency:        "usd",
					Scope:           types.TaxRateScopeOneTime,
				},
				Priority:  1,
				AutoApply: true,
			},
		},
	}

	// Test tax preparation for invoice
	taxRates, err := s.service.PrepareTaxRatesForInvoice(s.GetContext(), invoiceReq)

	s.NoError(err)
	s.NotNil(taxRates)
	s.Len(taxRates, 1)
	s.Equal("ONEOFF_TAX_15", taxRates[0].Code)
	s.Equal(types.TaxRateScopeOneTime, taxRates[0].Scope)

	// Create the invoice
	invoice, err := invoiceReq.ToInvoice(s.GetContext())
	s.NoError(err)
	s.NotNil(invoice)

	err = s.GetStores().InvoiceRepo.Create(s.GetContext(), invoice)
	s.NoError(err)

	// Apply taxes to the invoice
	taxResult, err := s.service.ApplyTaxesOnInvoice(s.GetContext(), invoice, taxRates)

	s.NoError(err)
	s.NotNil(taxResult)
	s.Equal(decimal.NewFromFloat(75.0), taxResult.TotalTaxAmount) // 15% of 500 = 75
	s.Len(taxResult.TaxAppliedRecords, 1)
	s.Len(taxResult.TaxRates, 1)

	// Verify tax applied record
	taxApplied := taxResult.TaxAppliedRecords[0]
	s.Equal(taxRates[0].ID, taxApplied.TaxRateID)
	s.Equal(types.TaxrateEntityTypeInvoice, taxApplied.EntityType)
	s.Equal(invoice.ID, taxApplied.EntityID)
	s.Equal(decimal.NewFromFloat(500.0), taxApplied.TaxableAmount)
	s.Equal(decimal.NewFromFloat(75.0), taxApplied.TaxAmount)
}

func (s *TaxServiceSuite) TestRecalculateInvoiceTaxes() {
	// Create a subscription with tax associations
	mockSubscription := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         s.testData.customers.customer1.ID,
		PlanID:             s.testData.plans.basicPlan.ID,
		StartDate:          s.testData.now,
		SubscriptionStatus: types.SubscriptionStatusActive,
		EnvironmentID:      types.GetEnvironmentID(s.GetContext()),
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}

	err := s.GetStores().SubscriptionRepo.Create(s.GetContext(), mockSubscription)
	s.Require().NoError(err)

	// Create tax associations for the subscription
	taxAssociations := []*dto.CreateTaxAssociationRequest{
		{
			TaxRateID:  s.testData.taxRates.vatPercentage.ID,
			EntityType: types.TaxrateEntityTypeSubscription,
			EntityID:   mockSubscription.ID,
			Priority:   1,
			AutoApply:  true,
		},
		{
			TaxRateID:  s.testData.taxRates.gstPercentage.ID,
			EntityType: types.TaxrateEntityTypeSubscription,
			EntityID:   mockSubscription.ID,
			Priority:   2,
			AutoApply:  true,
		},
	}

	for _, assocReq := range taxAssociations {
		_, err := s.service.CreateTaxAssociation(s.GetContext(), assocReq)
		s.Require().NoError(err)
	}

	// Create an invoice linked to the subscription
	invoice := &invoice.Invoice{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:      s.testData.customers.customer1.ID,
		SubscriptionID:  lo.ToPtr(mockSubscription.ID),
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(1000.0),
		Total:           decimal.NewFromFloat(1000.0),
		Subtotal:        decimal.NewFromFloat(1000.0),
		TotalTax:        decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(1000.0),
		EnvironmentID:   types.GetEnvironmentID(s.GetContext()),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}

	err = s.GetStores().InvoiceRepo.Create(s.GetContext(), invoice)
	s.Require().NoError(err)

	// Recalculate taxes for the invoice
	err = s.service.RecalculateInvoiceTaxes(s.GetContext(), invoice.ID)

	s.NoError(err)

	// Verify the invoice was updated with taxes
	updatedInvoice, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), invoice.ID)
	s.NoError(err)
	s.NotNil(updatedInvoice)

	// Expected tax: 20% (VAT) + 10% (GST) = 30% of 1000 = 300
	expectedTax := decimal.NewFromFloat(300.0)
	expectedTotal := decimal.NewFromFloat(1300.0)

	s.Equal(expectedTax, updatedInvoice.TotalTax)
	s.Equal(expectedTotal, updatedInvoice.Total)

	// Verify tax applied records were created
	filter := types.NewDefaultTaxAppliedFilter()
	filter.EntityType = types.TaxrateEntityTypeInvoice
	filter.EntityID = invoice.ID

	taxAppliedList, err := s.service.ListTaxApplied(s.GetContext(), filter)
	s.NoError(err)
	s.NotNil(taxAppliedList)
	s.Len(taxAppliedList.Items, 2) // VAT and GST

	// Verify individual tax amounts
	vatFound := false
	gstFound := false
	for _, taxApplied := range taxAppliedList.Items {
		if taxApplied.TaxRateID == s.testData.taxRates.vatPercentage.ID {
			vatFound = true
			s.Equal(decimal.NewFromFloat(200.0), taxApplied.TaxAmount) // 20% of 1000
		} else if taxApplied.TaxRateID == s.testData.taxRates.gstPercentage.ID {
			gstFound = true
			s.Equal(decimal.NewFromFloat(100.0), taxApplied.TaxAmount) // 10% of 1000
		}
	}
	s.True(vatFound, "VAT tax should be applied")
	s.True(gstFound, "GST tax should be applied")
}

func (s *TaxServiceSuite) TestDeleteTaxRate_ValidationErrors() {
	testCases := []struct {
		name        string
		taxRateID   string
		expectedErr string
	}{
		{
			name:        "empty_tax_rate_id",
			taxRateID:   "",
			expectedErr: "tax_rate_id is required",
		},
		{
			name:        "non_existent_tax_rate",
			taxRateID:   "non-existent-id",
			expectedErr: "not found",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			err := s.service.DeleteTaxRate(s.GetContext(), tc.taxRateID)

			s.Error(err)
			s.Contains(err.Error(), tc.expectedErr)
		})
	}
}

// =============================================================================
// Tax Association CRUD Tests
// =============================================================================

func (s *TaxServiceSuite) TestCreateTaxAssociation() {
	req := &dto.CreateTaxAssociationRequest{
		TaxRateID:  s.testData.taxRates.vatPercentage.ID,
		EntityType: types.TaxrateEntityTypeCustomer,
		EntityID:   s.testData.customers.customer1.ID,
		Priority:   1,
		AutoApply:  true,
	}

	resp, err := s.service.CreateTaxAssociation(s.GetContext(), req)

	s.NoError(err)
	s.NotNil(resp)
	s.Equal(req.TaxRateID, resp.TaxRateID)
	s.Equal(req.EntityType, resp.EntityType)
	s.Equal(req.EntityID, resp.EntityID)
	s.Equal(req.Priority, resp.Priority)
	s.Equal(req.AutoApply, resp.AutoApply)
	s.NotEmpty(resp.ID)
}

func (s *TaxServiceSuite) TestCreateTaxAssociation_ValidationErrors() {
	testCases := []struct {
		name        string
		req         *dto.CreateTaxAssociationRequest
		expectedErr string
	}{
		{
			name: "missing_tax_rate_id",
			req: &dto.CreateTaxAssociationRequest{
				EntityType: types.TaxrateEntityTypeCustomer,
				EntityID:   s.testData.customers.customer1.ID,
			},
			expectedErr: "tax_rate_id is required",
		},
		{
			name: "missing_entity_id",
			req: &dto.CreateTaxAssociationRequest{
				TaxRateID:  s.testData.taxRates.vatPercentage.ID,
				EntityType: types.TaxrateEntityTypeCustomer,
			},
			expectedErr: "entity_id is required",
		},
		{
			name: "invalid_entity_type",
			req: &dto.CreateTaxAssociationRequest{
				TaxRateID:  s.testData.taxRates.vatPercentage.ID,
				EntityType: "invalid",
				EntityID:   s.testData.customers.customer1.ID,
			},
			expectedErr: "invalid tax rate entity type",
		},
		{
			name: "negative_priority",
			req: &dto.CreateTaxAssociationRequest{
				TaxRateID:  s.testData.taxRates.vatPercentage.ID,
				EntityType: types.TaxrateEntityTypeCustomer,
				EntityID:   s.testData.customers.customer1.ID,
				Priority:   -1,
			},
			expectedErr: "priority cannot be less than 0",
		},
		{
			name: "non_existent_tax_rate",
			req: &dto.CreateTaxAssociationRequest{
				TaxRateID:  "non-existent-id",
				EntityType: types.TaxrateEntityTypeCustomer,
				EntityID:   s.testData.customers.customer1.ID,
			},
			expectedErr: "not found",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.CreateTaxAssociation(s.GetContext(), tc.req)

			s.Error(err)
			s.Nil(resp)
			s.Contains(err.Error(), tc.expectedErr)
		})
	}
}

func (s *TaxServiceSuite) TestGetTaxAssociation() {
	// Create a tax association first
	createReq := &dto.CreateTaxAssociationRequest{
		TaxRateID:  s.testData.taxRates.gstPercentage.ID,
		EntityType: types.TaxrateEntityTypeCustomer,
		EntityID:   s.testData.customers.customer2.ID,
		Priority:   2,
		AutoApply:  false,
	}

	createResp, err := s.service.CreateTaxAssociation(s.GetContext(), createReq)
	s.Require().NoError(err)
	s.Require().NotNil(createResp)

	// Test retrieval
	getResp, err := s.service.GetTaxAssociation(s.GetContext(), createResp.ID)

	s.NoError(err)
	s.NotNil(getResp)
	s.Equal(createResp.ID, getResp.ID)
	s.Equal(createResp.TaxRateID, getResp.TaxRateID)
	s.Equal(createResp.EntityType, getResp.EntityType)
	s.Equal(createResp.EntityID, getResp.EntityID)
}

func (s *TaxServiceSuite) TestGetTaxAssociation_NotFound() {
	resp, err := s.service.GetTaxAssociation(s.GetContext(), "non-existent-id")

	s.Error(err)
	s.Nil(resp)
	s.True(ierr.IsNotFound(err))
}

func (s *TaxServiceSuite) TestUpdateTaxAssociation() {
	// Create a tax association first
	createReq := &dto.CreateTaxAssociationRequest{
		TaxRateID:  s.testData.taxRates.fixedTax.ID,
		EntityType: types.TaxrateEntityTypeCustomer,
		EntityID:   s.testData.customers.customer1.ID,
		Priority:   1,
		AutoApply:  false,
	}

	createResp, err := s.service.CreateTaxAssociation(s.GetContext(), createReq)
	s.Require().NoError(err)
	s.Require().NotNil(createResp)

	// Test update
	updateReq := &dto.TaxAssociationUpdateRequest{
		Priority:  5,
		AutoApply: true,
		Metadata:  map[string]string{"updated": "true"},
	}

	updateResp, err := s.service.UpdateTaxAssociation(s.GetContext(), createResp.ID, updateReq)

	s.NoError(err)
	s.NotNil(updateResp)
	s.Equal(updateReq.Priority, updateResp.Priority)
	s.Equal(updateReq.AutoApply, updateResp.AutoApply)
	s.Equal(updateReq.Metadata, updateResp.Metadata)
}

func (s *TaxServiceSuite) TestDeleteTaxAssociation() {
	// Create a tax association first
	createReq := &dto.CreateTaxAssociationRequest{
		TaxRateID:  s.testData.taxRates.inactiveTax.ID,
		EntityType: types.TaxrateEntityTypeCustomer,
		EntityID:   s.testData.customers.customer2.ID,
		Priority:   1,
		AutoApply:  true,
	}

	createResp, err := s.service.CreateTaxAssociation(s.GetContext(), createReq)
	s.Require().NoError(err)
	s.Require().NotNil(createResp)

	// Test deletion
	err = s.service.DeleteTaxAssociation(s.GetContext(), createResp.ID)
	s.NoError(err)

	// Verify it's no longer accessible
	getResp, err := s.service.GetTaxAssociation(s.GetContext(), createResp.ID)
	s.Error(err)
	s.Nil(getResp)
}

func (s *TaxServiceSuite) TestListTaxAssociations() {
	// Create multiple tax associations
	associations := []*dto.CreateTaxAssociationRequest{
		{
			TaxRateID:  s.testData.taxRates.vatPercentage.ID,
			EntityType: types.TaxrateEntityTypeCustomer,
			EntityID:   s.testData.customers.customer1.ID,
			Priority:   1,
			AutoApply:  true,
		},
		{
			TaxRateID:  s.testData.taxRates.gstPercentage.ID,
			EntityType: types.TaxrateEntityTypeCustomer,
			EntityID:   s.testData.customers.customer1.ID,
			Priority:   2,
			AutoApply:  false,
		},
		{
			TaxRateID:  s.testData.taxRates.fixedTax.ID,
			EntityType: types.TaxrateEntityTypeCustomer,
			EntityID:   s.testData.customers.customer2.ID,
			Priority:   1,
			AutoApply:  true,
		},
	}

	createdIDs := make([]string, len(associations))
	for i, assoc := range associations {
		createResp, err := s.service.CreateTaxAssociation(s.GetContext(), assoc)
		s.Require().NoError(err)
		createdIDs[i] = createResp.ID
	}

	// Test listing all
	filter := types.NewTaxAssociationFilter()
	listResp, err := s.service.ListTaxAssociations(s.GetContext(), filter)

	s.NoError(err)
	s.NotNil(listResp)
	s.GreaterOrEqual(len(listResp.Items), 3) // At least our 3 created associations

	// Test filtering by entity
	filterByEntity := types.NewTaxAssociationFilter()
	filterByEntity.EntityType = types.TaxrateEntityTypeCustomer
	filterByEntity.EntityID = s.testData.customers.customer1.ID

	entityResp, err := s.service.ListTaxAssociations(s.GetContext(), filterByEntity)

	s.NoError(err)
	s.NotNil(entityResp)
	s.Equal(2, len(entityResp.Items)) // Customer1 has 2 associations

	// Test filtering by auto apply
	filterByAutoApply := types.NewTaxAssociationFilter()
	filterByAutoApply.AutoApply = lo.ToPtr(true)

	autoApplyResp, err := s.service.ListTaxAssociations(s.GetContext(), filterByAutoApply)

	s.NoError(err)
	s.NotNil(autoApplyResp)
	s.GreaterOrEqual(len(autoApplyResp.Items), 2) // At least 2 with AutoApply=true
}

// =============================================================================
// Tax Applied CRUD Tests
// =============================================================================

func (s *TaxServiceSuite) TestCreateTaxApplied() {
	// Create an invoice first
	invoice := &invoice.Invoice{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:      s.testData.customers.customer1.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(100.0),
		Total:           decimal.NewFromFloat(120.0),
		Subtotal:        decimal.NewFromFloat(100.0),
		TotalTax:        decimal.NewFromFloat(20.0),
		AmountRemaining: decimal.NewFromFloat(120.0),
		EnvironmentID:   types.GetEnvironmentID(s.GetContext()),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}

	err := s.GetStores().InvoiceRepo.Create(s.GetContext(), invoice)
	s.Require().NoError(err)

	// Create tax applied record
	req := dto.CreateTaxAppliedRequest{
		TaxRateID:     s.testData.taxRates.vatPercentage.ID,
		EntityType:    types.TaxrateEntityTypeInvoice,
		EntityID:      invoice.ID,
		TaxableAmount: decimal.NewFromFloat(100.0),
		TaxAmount:     decimal.NewFromFloat(20.0),
		Currency:      "usd",
		Metadata:      map[string]string{"applied_by": "test"},
	}

	resp, err := s.service.CreateTaxApplied(s.GetContext(), req)

	s.NoError(err)
	s.NotNil(resp)
	s.Equal(req.TaxRateID, resp.TaxRateID)
	s.Equal(req.EntityType, resp.EntityType)
	s.Equal(req.EntityID, resp.EntityID)
	s.Equal(req.TaxableAmount, resp.TaxableAmount)
	s.Equal(req.TaxAmount, resp.TaxAmount)
	s.Equal(req.Currency, resp.Currency)
	s.Equal(req.Metadata, resp.Metadata)
	s.NotEmpty(resp.ID)
	s.NotZero(resp.AppliedAt)
}

func (s *TaxServiceSuite) TestGetTaxApplied() {
	// Create a tax applied record first
	invoice := &invoice.Invoice{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:      s.testData.customers.customer1.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(50.0),
		Total:           decimal.NewFromFloat(55.0),
		Subtotal:        decimal.NewFromFloat(50.0),
		TotalTax:        decimal.NewFromFloat(5.0),
		AmountRemaining: decimal.NewFromFloat(55.0),
		EnvironmentID:   types.GetEnvironmentID(s.GetContext()),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}

	err := s.GetStores().InvoiceRepo.Create(s.GetContext(), invoice)
	s.Require().NoError(err)

	createReq := dto.CreateTaxAppliedRequest{
		TaxRateID:     s.testData.taxRates.gstPercentage.ID,
		EntityType:    types.TaxrateEntityTypeInvoice,
		EntityID:      invoice.ID,
		TaxableAmount: decimal.NewFromFloat(50.0),
		TaxAmount:     decimal.NewFromFloat(5.0),
		Currency:      "usd",
	}

	createResp, err := s.service.CreateTaxApplied(s.GetContext(), createReq)
	s.Require().NoError(err)
	s.Require().NotNil(createResp)

	// Test retrieval
	getResp, err := s.service.GetTaxApplied(s.GetContext(), createResp.ID)

	s.NoError(err)
	s.NotNil(getResp)
	s.Equal(createResp.ID, getResp.ID)
	s.Equal(createResp.TaxRateID, getResp.TaxRateID)
	s.Equal(createResp.EntityID, getResp.EntityID)
	s.Equal(createResp.TaxAmount, getResp.TaxAmount)
}

func (s *TaxServiceSuite) TestListTaxApplied() {
	// Create multiple invoices and tax applied records
	invoices := make([]*invoice.Invoice, 2)
	for i := 0; i < 2; i++ {
		inv := &invoice.Invoice{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
			CustomerID:      s.testData.customers.customer1.ID,
			InvoiceType:     types.InvoiceTypeOneOff,
			InvoiceStatus:   types.InvoiceStatusDraft,
			PaymentStatus:   types.PaymentStatusPending,
			Currency:        "usd",
			AmountDue:       decimal.NewFromFloat(100.0),
			Total:           decimal.NewFromFloat(110.0),
			Subtotal:        decimal.NewFromFloat(100.0),
			TotalTax:        decimal.NewFromFloat(10.0),
			AmountRemaining: decimal.NewFromFloat(110.0),
			EnvironmentID:   types.GetEnvironmentID(s.GetContext()),
			BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		}
		err := s.GetStores().InvoiceRepo.Create(s.GetContext(), inv)
		s.Require().NoError(err)
		invoices[i] = inv
	}

	// Create tax applied records
	taxAppliedRecords := []dto.CreateTaxAppliedRequest{
		{
			TaxRateID:     s.testData.taxRates.vatPercentage.ID,
			EntityType:    types.TaxrateEntityTypeInvoice,
			EntityID:      invoices[0].ID,
			TaxableAmount: decimal.NewFromFloat(100.0),
			TaxAmount:     decimal.NewFromFloat(20.0),
			Currency:      "usd",
		},
		{
			TaxRateID:     s.testData.taxRates.gstPercentage.ID,
			EntityType:    types.TaxrateEntityTypeInvoice,
			EntityID:      invoices[1].ID,
			TaxableAmount: decimal.NewFromFloat(100.0),
			TaxAmount:     decimal.NewFromFloat(10.0),
			Currency:      "usd",
		},
	}

	for _, req := range taxAppliedRecords {
		_, err := s.service.CreateTaxApplied(s.GetContext(), req)
		s.Require().NoError(err)
	}

	// Test listing all
	filter := types.NewDefaultTaxAppliedFilter()
	listResp, err := s.service.ListTaxApplied(s.GetContext(), filter)

	s.NoError(err)
	s.NotNil(listResp)
	s.GreaterOrEqual(len(listResp.Items), 2) // At least our 2 created records

	// Test filtering by tax rate
	filterByTaxRate := types.NewDefaultTaxAppliedFilter()
	filterByTaxRate.TaxRateIDs = []string{s.testData.taxRates.vatPercentage.ID}

	taxRateResp, err := s.service.ListTaxApplied(s.GetContext(), filterByTaxRate)

	s.NoError(err)
	s.NotNil(taxRateResp)
	s.GreaterOrEqual(len(taxRateResp.Items), 1) // At least 1 with VAT tax rate

	// Test filtering by entity
	filterByEntity := types.NewDefaultTaxAppliedFilter()
	filterByEntity.EntityType = types.TaxrateEntityTypeInvoice
	filterByEntity.EntityID = invoices[0].ID

	entityResp, err := s.service.ListTaxApplied(s.GetContext(), filterByEntity)

	s.NoError(err)
	s.NotNil(entityResp)
	s.Equal(1, len(entityResp.Items)) // Only one record for this invoice
}

func (s *TaxServiceSuite) TestDeleteTaxApplied() {
	// Create a tax applied record first
	invoice := &invoice.Invoice{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:      s.testData.customers.customer1.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(30.0),
		Total:           decimal.NewFromFloat(35.0),
		Subtotal:        decimal.NewFromFloat(30.0),
		TotalTax:        decimal.NewFromFloat(5.0),
		AmountRemaining: decimal.NewFromFloat(35.0),
		EnvironmentID:   types.GetEnvironmentID(s.GetContext()),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}

	err := s.GetStores().InvoiceRepo.Create(s.GetContext(), invoice)
	s.Require().NoError(err)

	createReq := dto.CreateTaxAppliedRequest{
		TaxRateID:     s.testData.taxRates.fixedTax.ID,
		EntityType:    types.TaxrateEntityTypeInvoice,
		EntityID:      invoice.ID,
		TaxableAmount: decimal.NewFromFloat(30.0),
		TaxAmount:     decimal.NewFromFloat(5.0),
		Currency:      "usd",
	}

	createResp, err := s.service.CreateTaxApplied(s.GetContext(), createReq)
	s.Require().NoError(err)
	s.Require().NotNil(createResp)

	// Test deletion
	err = s.service.DeleteTaxApplied(s.GetContext(), createResp.ID)
	s.NoError(err)

	// Verify it's no longer accessible
	getResp, err := s.service.GetTaxApplied(s.GetContext(), createResp.ID)
	s.Error(err)
	s.Nil(getResp)
	s.True(ierr.IsNotFound(err))
}

// =============================================================================
// Edge Case and Integration Tests
// =============================================================================

func (s *TaxServiceSuite) TestTaxRateCalculationEdgeCases() {
	// Test zero percentage tax rate
	req := dto.CreateTaxRateRequest{
		Name:            "Zero Tax",
		Code:            "ZERO_TAX",
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.Zero),
		Currency:        "usd",
		Scope:           types.TaxRateScopeExternal,
	}

	resp, err := s.service.CreateTaxRate(s.GetContext(), req)
	s.NoError(err)
	s.NotNil(resp)
	s.True(resp.PercentageValue.Equal(decimal.Zero))

	// Test 100% tax rate
	req100 := dto.CreateTaxRateRequest{
		Name:            "Full Tax",
		Code:            "FULL_TAX",
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromInt(100)),
		Currency:        "usd",
		Scope:           types.TaxRateScopeExternal,
	}

	resp100, err := s.service.CreateTaxRate(s.GetContext(), req100)
	s.NoError(err)
	s.NotNil(resp100)
	s.True(resp100.PercentageValue.Equal(decimal.NewFromInt(100)))

	// Test very small fixed tax rate
	reqSmall := dto.CreateTaxRateRequest{
		Name:        "Penny Tax",
		Code:        "PENNY_TAX",
		TaxRateType: types.TaxRateTypeFixed,
		FixedValue:  lo.ToPtr(decimal.NewFromFloat(0.01)),
		Currency:    "usd",
		Scope:       types.TaxRateScopeExternal,
	}

	respSmall, err := s.service.CreateTaxRate(s.GetContext(), reqSmall)
	s.NoError(err)
	s.NotNil(respSmall)
	s.True(respSmall.FixedValue.Equal(decimal.NewFromFloat(0.01)))
}

func (s *TaxServiceSuite) TestTaxAssociationPriorityOrdering() {
	// Create multiple tax associations with different priorities
	taxAssociations := []*dto.CreateTaxAssociationRequest{
		{
			TaxRateID:  s.testData.taxRates.vatPercentage.ID,
			EntityType: types.TaxrateEntityTypeCustomer,
			EntityID:   s.testData.customers.customer1.ID,
			Priority:   3,
			AutoApply:  true,
		},
		{
			TaxRateID:  s.testData.taxRates.gstPercentage.ID,
			EntityType: types.TaxrateEntityTypeCustomer,
			EntityID:   s.testData.customers.customer1.ID,
			Priority:   1,
			AutoApply:  true,
		},
		{
			TaxRateID:  s.testData.taxRates.fixedTax.ID,
			EntityType: types.TaxrateEntityTypeCustomer,
			EntityID:   s.testData.customers.customer1.ID,
			Priority:   2,
			AutoApply:  true,
		},
	}

	// Create all associations
	for _, assoc := range taxAssociations {
		_, err := s.service.CreateTaxAssociation(s.GetContext(), assoc)
		s.NoError(err)
	}

	// List and verify priority ordering
	filter := types.NewTaxAssociationFilter()
	filter.EntityType = types.TaxrateEntityTypeCustomer
	filter.EntityID = s.testData.customers.customer1.ID

	resp, err := s.service.ListTaxAssociations(s.GetContext(), filter)
	s.NoError(err)
	s.NotNil(resp)
	s.Len(resp.Items, 3)

	// Verify priorities are set correctly
	priorities := make([]int, len(resp.Items))
	for i, item := range resp.Items {
		priorities[i] = item.Priority
	}
	s.Contains(priorities, 1)
	s.Contains(priorities, 2)
	s.Contains(priorities, 3)
}

func (s *TaxServiceSuite) TestTaxCalculationWithMultipleTaxRates() {
	// Create an invoice
	invoice := &invoice.Invoice{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:      s.testData.customers.customer1.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(1000.0),
		Total:           decimal.NewFromFloat(1000.0),
		Subtotal:        decimal.NewFromFloat(1000.0),
		TotalTax:        decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(1000.0),
		EnvironmentID:   types.GetEnvironmentID(s.GetContext()),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}

	err := s.GetStores().InvoiceRepo.Create(s.GetContext(), invoice)
	s.Require().NoError(err)

	// Apply multiple tax rates
	taxRates := []*dto.TaxRateResponse{
		{TaxRate: s.testData.taxRates.vatPercentage}, // 20%
		{TaxRate: s.testData.taxRates.gstPercentage}, // 10%
		{TaxRate: s.testData.taxRates.fixedTax},      // $5
	}

	result, err := s.service.ApplyTaxesOnInvoice(s.GetContext(), invoice, taxRates)

	s.NoError(err)
	s.NotNil(result)
	s.Len(result.TaxAppliedRecords, 3)

	// Verify tax calculations
	// 20% of 1000 = 200
	// 10% of 1000 = 100
	// $5 fixed = 5
	// Total = 305
	expectedTotal := decimal.NewFromFloat(305.0)
	s.True(result.TotalTaxAmount.Equal(expectedTotal))

	// Verify individual tax amounts
	for _, taxApplied := range result.TaxAppliedRecords {
		if taxApplied.TaxRateID == s.testData.taxRates.vatPercentage.ID {
			s.True(taxApplied.TaxAmount.Equal(decimal.NewFromFloat(200.0)))
		} else if taxApplied.TaxRateID == s.testData.taxRates.gstPercentage.ID {
			s.True(taxApplied.TaxAmount.Equal(decimal.NewFromFloat(100.0)))
		} else if taxApplied.TaxRateID == s.testData.taxRates.fixedTax.ID {
			s.True(taxApplied.TaxAmount.Equal(decimal.NewFromFloat(5.0)))
		}
	}
}

func (s *TaxServiceSuite) TestTaxAppliedIdempotency() {
	// Create an invoice
	invoice := &invoice.Invoice{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:      s.testData.customers.customer1.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(100.0),
		Total:           decimal.NewFromFloat(100.0),
		Subtotal:        decimal.NewFromFloat(100.0),
		TotalTax:        decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(100.0),
		EnvironmentID:   types.GetEnvironmentID(s.GetContext()),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}

	err := s.GetStores().InvoiceRepo.Create(s.GetContext(), invoice)
	s.Require().NoError(err)

	taxRates := []*dto.TaxRateResponse{
		{TaxRate: s.testData.taxRates.vatPercentage}, // 20%
	}

	// Apply taxes first time
	result1, err := s.service.ApplyTaxesOnInvoice(s.GetContext(), invoice, taxRates)
	s.NoError(err)
	s.NotNil(result1)

	// Apply same taxes again (should be idempotent)
	result2, err := s.service.ApplyTaxesOnInvoice(s.GetContext(), invoice, taxRates)
	s.NoError(err)
	s.NotNil(result2)

	// Results should be identical
	s.True(result1.TotalTaxAmount.Equal(result2.TotalTaxAmount))
	s.Len(result1.TaxAppliedRecords, 1)
	s.Len(result2.TaxAppliedRecords, 1)
}

func (s *TaxServiceSuite) TestTaxRateValidityPeriods() {
	now := time.Now().UTC()
	past := now.AddDate(0, 0, -10)
	future := now.AddDate(0, 0, 10)

	// Test future valid from date
	futureReq := dto.CreateTaxRateRequest{
		Name:            "Future Tax",
		Code:            "FUTURE_TAX",
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromFloat(15.0)),
		Currency:        "usd",
		Scope:           types.TaxRateScopeExternal,
		ValidFrom:       &future,
	}

	futureResp, err := s.service.CreateTaxRate(s.GetContext(), futureReq)
	s.NoError(err)
	s.NotNil(futureResp)
	s.Equal(types.TaxRateStatusInactive, futureResp.TaxRateStatus)

	// Test past valid to date
	pastReq := dto.CreateTaxRateRequest{
		Name:            "Past Tax",
		Code:            "PAST_TAX",
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromFloat(25.0)),
		Currency:        "usd",
		Scope:           types.TaxRateScopeExternal,
		ValidTo:         &past,
	}

	pastResp, err := s.service.CreateTaxRate(s.GetContext(), pastReq)
	s.NoError(err)
	s.NotNil(pastResp)
	s.Equal(types.TaxRateStatusInactive, pastResp.TaxRateStatus)

	// Test active validity period
	activeReq := dto.CreateTaxRateRequest{
		Name:            "Active Tax",
		Code:            "ACTIVE_TAX",
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromFloat(5.0)),
		Currency:        "usd",
		Scope:           types.TaxRateScopeExternal,
		ValidFrom:       &past,
		ValidTo:         &future,
	}

	activeResp, err := s.service.CreateTaxRate(s.GetContext(), activeReq)
	s.NoError(err)
	s.NotNil(activeResp)
	s.Equal(types.TaxRateStatusActive, activeResp.TaxRateStatus)
}

func (s *TaxServiceSuite) TestTaxRateFiltering() {
	// Test filtering by scope
	filter := types.NewDefaultTaxRateFilter()
	filter.Scope = types.TaxRateScopeExternal

	resp, err := s.service.ListTaxRates(s.GetContext(), filter)
	s.NoError(err)
	s.NotNil(resp)

	// Verify all returned tax rates have external scope
	for _, taxRate := range resp.Items {
		s.Equal(types.TaxRateScopeExternal, taxRate.Scope)
	}

	// Test filtering by code
	codeFilter := types.NewDefaultTaxRateFilter()
	codeFilter.Code = s.testData.taxRates.vatPercentage.Code

	codeResp, err := s.service.ListTaxRates(s.GetContext(), codeFilter)
	s.NoError(err)
	s.NotNil(codeResp)

	// Verify we get the specific tax rate with that code
	s.Len(codeResp.Items, 1)
	s.Equal(s.testData.taxRates.vatPercentage.Code, codeResp.Items[0].Code)
}

func (s *TaxServiceSuite) TestEntityTaxAssociationIntegration() {
	// Test complete flow of linking tax rates to an entity
	entityID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER)

	// Create tax rate links with mixed new and existing rates
	taxRateLinks := []*dto.CreateEntityTaxAssociation{
		{
			// Use existing tax rate
			TaxRateID: &s.testData.taxRates.vatPercentage.ID,
			Priority:  1,
			AutoApply: true,
		},
		{
			// Create new tax rate
			CreateTaxRateRequest: dto.CreateTaxRateRequest{
				Name:            "Entity Specific Tax",
				Code:            "ENTITY_TAX_8",
				TaxRateType:     types.TaxRateTypePercentage,
				PercentageValue: lo.ToPtr(decimal.NewFromFloat(8.0)),
				Currency:        "usd",
				Scope:           types.TaxRateScopeExternal,
			},
			Priority:  2,
			AutoApply: false,
		},
	}

	// Link tax rates to entity
	linkResp, err := s.service.LinkTaxRatesToEntity(s.GetContext(), types.TaxrateEntityTypeCustomer, entityID, taxRateLinks)

	s.NoError(err)
	s.NotNil(linkResp)
	s.Equal(entityID, linkResp.EntityID)
	s.Equal(types.TaxrateEntityTypeCustomer, linkResp.EntityType)
	s.Len(linkResp.LinkedTaxRates, 2)

	// Verify one was existing and one was created
	createdCount := 0
	existingCount := 0
	for _, linked := range linkResp.LinkedTaxRates {
		if linked.WasCreated {
			createdCount++
		} else {
			existingCount++
		}
	}
	s.Equal(1, createdCount)
	s.Equal(1, existingCount)

	// Verify tax associations were created
	filter := types.NewTaxAssociationFilter()
	filter.EntityType = types.TaxrateEntityTypeCustomer
	filter.EntityID = entityID

	associations, err := s.service.ListTaxAssociations(s.GetContext(), filter)
	s.NoError(err)
	s.NotNil(associations)
	s.Len(associations.Items, 2)
}

func (s *TaxServiceSuite) TestTaxRateCodeUniquenessAcrossScopes() {
	// Test that tax rate codes must be unique within the same environment
	// but can be reused across different scopes

	// Create first tax rate with external scope
	req1 := dto.CreateTaxRateRequest{
		Name:            "External Tax",
		Code:            "UNIQUE_CODE",
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromFloat(10.0)),
		Currency:        "usd",
		Scope:           types.TaxRateScopeExternal,
	}

	resp1, err := s.service.CreateTaxRate(s.GetContext(), req1)
	s.NoError(err)
	s.NotNil(resp1)

	// Try to create another tax rate with same code and scope - should fail
	req2 := dto.CreateTaxRateRequest{
		Name:            "Another External Tax",
		Code:            "UNIQUE_CODE",
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromFloat(15.0)),
		Currency:        "usd",
		Scope:           types.TaxRateScopeExternal,
	}

	resp2, err := s.service.CreateTaxRate(s.GetContext(), req2)
	s.Error(err)
	s.Nil(resp2)
	s.Contains(err.Error(), "tax rate with this code already exists")
}

func (s *TaxServiceSuite) TestComplexTaxCalculationScenarios() {
	// Test scenario with very large amounts
	largeAmount := decimal.NewFromFloat(999999.99)

	largeInvoice := &invoice.Invoice{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:      s.testData.customers.customer1.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       largeAmount,
		Total:           largeAmount,
		Subtotal:        largeAmount,
		TotalTax:        decimal.Zero,
		AmountRemaining: largeAmount,
		EnvironmentID:   types.GetEnvironmentID(s.GetContext()),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}

	err := s.GetStores().InvoiceRepo.Create(s.GetContext(), largeInvoice)
	s.Require().NoError(err)

	// Apply percentage tax
	taxRates := []*dto.TaxRateResponse{
		{TaxRate: s.testData.taxRates.vatPercentage}, // 20%
	}

	result, err := s.service.ApplyTaxesOnInvoice(s.GetContext(), largeInvoice, taxRates)
	s.NoError(err)
	s.NotNil(result)

	// Verify large amount calculation
	expectedTax := largeAmount.Mul(decimal.NewFromFloat(0.20))
	s.True(result.TotalTaxAmount.Equal(expectedTax))

	// Test scenario with very small amounts
	smallAmount := decimal.NewFromFloat(0.01)

	smallInvoice := &invoice.Invoice{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:      s.testData.customers.customer1.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       smallAmount,
		Total:           smallAmount,
		Subtotal:        smallAmount,
		TotalTax:        decimal.Zero,
		AmountRemaining: smallAmount,
		EnvironmentID:   types.GetEnvironmentID(s.GetContext()),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}

	err = s.GetStores().InvoiceRepo.Create(s.GetContext(), smallInvoice)
	s.Require().NoError(err)

	smallResult, err := s.service.ApplyTaxesOnInvoice(s.GetContext(), smallInvoice, taxRates)
	s.NoError(err)
	s.NotNil(smallResult)

	// Verify small amount calculation
	expectedSmallTax := smallAmount.Mul(decimal.NewFromFloat(0.20))
	s.True(smallResult.TotalTaxAmount.Equal(expectedSmallTax))
}

func (s *TaxServiceSuite) TestTaxRateMetadata() {
	// Test creation with metadata
	metadata := map[string]string{
		"region":     "US",
		"state":      "CA",
		"county":     "Los Angeles",
		"created_by": "test_user",
	}

	req := dto.CreateTaxRateRequest{
		Name:            "CA State Tax",
		Code:            "CA_STATE_TAX",
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromFloat(8.25)),
		Currency:        "usd",
		Scope:           types.TaxRateScopeExternal,
		Metadata:        metadata,
	}

	resp, err := s.service.CreateTaxRate(s.GetContext(), req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(metadata, resp.Metadata)

	// Test updating metadata
	updateReq := dto.UpdateTaxRateRequest{
		Metadata: map[string]string{
			"region":     "US",
			"state":      "CA",
			"county":     "Los Angeles",
			"created_by": "test_user",
			"updated_by": "admin_user",
		},
	}

	updateResp, err := s.service.UpdateTaxRate(s.GetContext(), resp.ID, updateReq)
	s.NoError(err)
	s.NotNil(updateResp)
	s.Equal(updateReq.Metadata, updateResp.Metadata)
}
