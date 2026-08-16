package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type TaxCalculationSuite struct {
	testutil.BaseServiceTestSuite
	svc TaxService
}

func TestTaxCalculation(t *testing.T) {
	suite.Run(t, new(TaxCalculationSuite))
}

func (s *TaxCalculationSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	st := s.GetStores()
	s.svc = NewTaxService(ServiceParams{
		Logger:             s.GetLogger(),
		Config:             s.GetConfig(),
		DB:                 s.GetDB(),
		TaxRateRepo:        st.TaxRateRepo,
		TaxAppliedRepo:     st.TaxAppliedRepo,
		TaxAssociationRepo: st.TaxAssociationRepo,
		InvoiceRepo:        st.InvoiceRepo,
		CustomerRepo:       st.CustomerRepo,
	})
}

func (s *TaxCalculationSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *TaxCalculationSuite) previewInvoice() *invoice.Invoice {
	return &invoice.Invoice{
		ID:       types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		Currency: "usd",
		Subtotal: decimal.NewFromInt(100),
	}
}

func (s *TaxCalculationSuite) tenPercent() []*dto.TaxRateResponse {
	return []*dto.TaxRateResponse{{
		TaxRate: &taxrate.TaxRate{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
			Name:            "VAT",
			Code:            "vat_10",
			TaxRateType:     types.TaxRateTypePercentage,
			PercentageValue: lo.ToPtr(decimal.NewFromInt(10)),
		},
	}}
}

func (s *TaxCalculationSuite) countTaxApplied() int {
	ctx := s.GetContext()
	filter := types.NewNoLimitTaxAppliedFilter()
	records, err := s.GetStores().TaxAppliedRepo.List(ctx, filter)
	s.Require().NoError(err)
	return len(records)
}

// A quote is not a charge. Applying taxes writes one tax_applied row per rate, and a
// previewed invoice is never created — those rows would point at nothing forever.
func (s *TaxCalculationSuite) TestCalculateWritesNothingWhileApplyPersists() {
	inv := s.previewInvoice()
	rates := s.tenPercent()

	quoted := s.svc.CalculateTaxesOnInvoice(s.GetContext(), inv, rates)
	s.Require().NotNil(quoted)
	s.True(quoted.TotalTaxAmount.Equal(decimal.NewFromInt(10)),
		"10%% of 100 is 10, got %s", quoted.TotalTaxAmount)
	s.Empty(quoted.TaxAppliedRecords, "a quote records nothing")
	s.Equal(0, s.countTaxApplied(), "a quote must not touch the tax_applied table")

	charged, err := s.svc.ApplyTaxesOnInvoice(s.GetContext(), inv, rates)
	s.Require().NoError(err)
	s.True(charged.TotalTaxAmount.Equal(quoted.TotalTaxAmount),
		"quoting and charging must agree on the amount: quoted %s, charged %s",
		quoted.TotalTaxAmount, charged.TotalTaxAmount)
	s.Equal(1, s.countTaxApplied(), "charging is what records the tax")
}
