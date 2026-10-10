package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// TaxAppliedLifecycleSuite covers what happens to an invoice's tax_applied records across
// recomputes, and what reads see once an external engine has written some.
type TaxAppliedLifecycleSuite struct {
	testutil.BaseServiceTestSuite
	taxSvc TaxService
}

func TestTaxAppliedLifecycle(t *testing.T) {
	suite.Run(t, new(TaxAppliedLifecycleSuite))
}

func (s *TaxAppliedLifecycleSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	st := s.GetStores()
	s.taxSvc = NewTaxService(ServiceParams{
		Logger:             s.GetLogger(),
		Config:             s.GetConfig(),
		DB:                 s.GetDB(),
		TaxRateRepo:        st.TaxRateRepo,
		TaxAppliedRepo:     st.TaxAppliedRepo,
		TaxAssociationRepo: st.TaxAssociationRepo,
		InvoiceRepo:        st.InvoiceRepo,
		CustomerRepo:       st.CustomerRepo,
		SubRepo:            st.SubscriptionRepo,
		SettingsRepo:       st.SettingsRepo,
		ConnectionRepo:     st.ConnectionRepo,
	})
}

func (s *TaxAppliedLifecycleSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *TaxAppliedLifecycleSuite) customer() *customer.Customer {
	cust := &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: types.GenerateUUIDWithPrefix("ext"),
		Name:       "Lifecycle Customer",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), cust))
	return cust
}

func (s *TaxAppliedLifecycleSuite) rate(name string, percentage int64) *taxrate.TaxRate {
	tr := &taxrate.TaxRate{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
		Name:            name,
		Code:            name + "_" + types.GenerateUUIDWithPrefix("u"),
		TaxRateStatus:   types.TaxRateStatusActive,
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromInt(percentage)),
		EnvironmentID:   types.GetEnvironmentID(s.GetContext()),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().TaxRateRepo.Create(s.GetContext(), tr))
	return tr
}

func (s *TaxAppliedLifecycleSuite) draftInvoice(cust *customer.Customer, subtotal string) *invoice.Invoice {
	amount := decimal.RequireFromString(subtotal)
	inv := &invoice.Invoice{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:    cust.ID,
		InvoiceType:   types.InvoiceTypeOneOff,
		InvoiceStatus: types.InvoiceStatusDraft,
		PaymentStatus: types.PaymentStatusPending,
		Currency:      "usd",
		Subtotal:      amount,
		Total:         amount,
		AmountDue:     amount,
		TotalTax:      decimal.Zero,
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))
	return inv
}

func (s *TaxAppliedLifecycleSuite) appliedFor(invoiceID string) []*taxapplied.TaxApplied {
	filter := types.NewNoLimitTaxAppliedFilter()
	filter.EntityType = types.TaxRateEntityTypeInvoice
	filter.EntityID = invoiceID
	records, err := s.GetStores().TaxAppliedRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	return records
}

func (s *TaxAppliedLifecycleSuite) rates(rs ...*taxrate.TaxRate) *dto.InvoiceTaxRates {
	out := make([]*dto.TaxRateWithBehavior, 0, len(rs))
	for _, r := range rs {
		out = append(out, &dto.TaxRateWithBehavior{
			TaxRateResponse: &dto.TaxRateResponse{TaxRate: r},
			TaxBehavior:     types.TaxBehaviorExclusive,
		})
	}
	return &dto.InvoiceTaxRates{Rates: out}
}

// --- recompute ------------------------------------------------------------

func (s *TaxAppliedLifecycleSuite) TestRecomputeDoesNotAccumulateRecords() {
	cust := s.customer()
	inv := s.draftInvoice(cust, "100")
	rate := s.rate("gst", 10)

	for i := 0; i < 3; i++ {
		_, err := s.taxSvc.ApplyTaxesOnInvoice(s.GetContext(), inv, s.rates(rate))
		s.Require().NoError(err)
	}

	records := s.appliedFor(inv.ID)
	s.Len(records, 1, "each recompute replaces what is published, it does not append")
	s.True(decimal.NewFromInt(10).Equal(records[0].TaxAmount))
}

func (s *TaxAppliedLifecycleSuite) TestRecomputeReflectsTheLatestAmount() {
	cust := s.customer()
	inv := s.draftInvoice(cust, "100")
	rate := s.rate("gst", 10)

	_, err := s.taxSvc.ApplyTaxesOnInvoice(s.GetContext(), inv, s.rates(rate))
	s.Require().NoError(err)

	// A discount lands, so the taxable base drops.
	inv.TotalDiscount = decimal.NewFromInt(50)
	_, err = s.taxSvc.ApplyTaxesOnInvoice(s.GetContext(), inv, s.rates(rate))
	s.Require().NoError(err)

	records := s.appliedFor(inv.ID)
	s.Require().Len(records, 1)
	s.True(decimal.NewFromInt(5).Equal(records[0].TaxAmount),
		"the record must carry the latest calculation, got %s", records[0].TaxAmount)
	s.True(decimal.NewFromInt(50).Equal(records[0].TaxableAmount))
}

func (s *TaxAppliedLifecycleSuite) TestRecordSetShrinksWhenARateIsRemoved() {
	cust := s.customer()
	inv := s.draftInvoice(cust, "100")
	kept, removed := s.rate("kept", 10), s.rate("removed", 5)

	_, err := s.taxSvc.ApplyTaxesOnInvoice(s.GetContext(), inv, s.rates(kept, removed))
	s.Require().NoError(err)
	s.Len(s.appliedFor(inv.ID), 2)

	_, err = s.taxSvc.ApplyTaxesOnInvoice(s.GetContext(), inv, s.rates(kept))
	s.Require().NoError(err)

	records := s.appliedFor(inv.ID)
	s.Require().Len(records, 1, "a rate that no longer applies must stop being charged")
	s.Equal(kept.ID, lo.FromPtr(records[0].TaxRateID))
}

func (s *TaxAppliedLifecycleSuite) TestRecordSetGrowsWhenARateIsAdded() {
	cust := s.customer()
	inv := s.draftInvoice(cust, "100")
	first, second := s.rate("first", 10), s.rate("second", 5)

	_, err := s.taxSvc.ApplyTaxesOnInvoice(s.GetContext(), inv, s.rates(first))
	s.Require().NoError(err)

	_, err = s.taxSvc.ApplyTaxesOnInvoice(s.GetContext(), inv, s.rates(first, second))
	s.Require().NoError(err)

	s.Len(s.appliedFor(inv.ID), 2, "the record set matches the latest calculation")
}

func (s *TaxAppliedLifecycleSuite) TestEveryRateRemovedClearsTheTax() {
	cust := s.customer()
	inv := s.draftInvoice(cust, "100")
	rate := s.rate("gst", 10)

	_, err := s.taxSvc.ApplyTaxesOnInvoice(s.GetContext(), inv, s.rates(rate))
	s.Require().NoError(err)
	s.Len(s.appliedFor(inv.ID), 1)

	result, err := s.taxSvc.ApplyTaxesOnInvoice(s.GetContext(), inv, &dto.InvoiceTaxRates{})
	s.Require().NoError(err)

	s.Empty(s.appliedFor(inv.ID),
		"an empty selection resolved, so the records go, rather than being left in force")
	s.True(result.TotalTaxAmount.IsZero())
}

func (s *TaxAppliedLifecycleSuite) TestRecordsAreScopedToTheirOwnInvoice() {
	cust := s.customer()
	first := s.draftInvoice(cust, "100")
	second := s.draftInvoice(cust, "200")
	rate := s.rate("gst", 10)

	_, err := s.taxSvc.ApplyTaxesOnInvoice(s.GetContext(), first, s.rates(rate))
	s.Require().NoError(err)
	_, err = s.taxSvc.ApplyTaxesOnInvoice(s.GetContext(), second, s.rates(rate))
	s.Require().NoError(err)

	// Recomputing one must not archive the other's records.
	_, err = s.taxSvc.ApplyTaxesOnInvoice(s.GetContext(), first, s.rates(rate))
	s.Require().NoError(err)

	s.Len(s.appliedFor(first.ID), 1)
	s.Len(s.appliedFor(second.ID), 1, "archiving is scoped to the invoice being recomputed")
}

// --- external records -----------------------------------------------------

func (s *TaxAppliedLifecycleSuite) persistExternal(inv *invoice.Invoice, amounts ...string) *dto.TaxCalculationResult {
	records := make([]*dto.TaxAppliedResponse, 0, len(amounts))
	total := decimal.Zero
	for _, a := range amounts {
		amount := decimal.RequireFromString(a)
		total = total.Add(amount)
		records = append(records, &dto.TaxAppliedResponse{
			TaxApplied: taxapplied.TaxApplied{
				EntityType:         types.TaxRateEntityTypeInvoice,
				EntityID:           inv.ID,
				TaxableAmount:      inv.Subtotal,
				TaxAmount:          amount,
				TaxBehavior:        types.TaxBehaviorExclusive,
				Currency:           inv.Currency,
				Provider:           types.TaxProviderStripe,
				ExternalTaxDetails: &types.ExternalTaxDetails{CalculationID: "taxcalc_lifecycle"},
			},
		})
	}

	result := &dto.TaxCalculationResult{
		ExclusiveTax:      total,
		TotalTaxAmount:    total,
		Provider:          types.TaxProviderStripe,
		TaxAppliedRecords: records,
	}
	s.Require().NoError(s.taxSvc.PersistTaxResult(s.GetContext(), types.TaxRateEntityTypeInvoice, inv.ID, inv.Currency, result))
	return result
}

func (s *TaxAppliedLifecycleSuite) TestExternalRecordsCarryNoRateIDAndNameTheirProvider() {
	cust := s.customer()
	inv := s.draftInvoice(cust, "100")

	s.persistExternal(inv, "8.75")

	records := s.appliedFor(inv.ID)
	s.Require().Len(records, 1)
	s.Nil(records[0].TaxRateID, "an engine holds the rate, so there is nothing to point at")
	s.Equal(types.TaxProviderStripe, records[0].Provider)
	s.True(records[0].IsExternal())
}

func (s *TaxAppliedLifecycleSuite) TestSeveralExternalRecordsSurviveARecompute() {
	cust := s.customer()
	inv := s.draftInvoice(cust, "100")

	s.persistExternal(inv, "1.00", "0.54")
	s.Len(s.appliedFor(inv.ID), 2)

	// Two rates sharing a behaviour used to collapse into one record on recompute.
	s.persistExternal(inv, "1.00", "0.54")

	records := s.appliedFor(inv.ID)
	s.Require().Len(records, 2, "two groups stay two records, they do not overwrite each other")

	total := decimal.Zero
	for _, r := range records {
		total = total.Add(r.TaxAmount)
	}
	s.True(decimal.RequireFromString("1.54").Equal(total),
		"records must sum to the invoice tax, got %s", total)
}

func (s *TaxAppliedLifecycleSuite) TestExternalRecordsReplaceNativeOnesOnRecompute() {
	cust := s.customer()
	inv := s.draftInvoice(cust, "100")
	rate := s.rate("gst", 10)

	_, err := s.taxSvc.ApplyTaxesOnInvoice(s.GetContext(), inv, s.rates(rate))
	s.Require().NoError(err)

	s.persistExternal(inv, "8.75")

	records := s.appliedFor(inv.ID)
	s.Require().Len(records, 1, "everything published is archived, whichever engine wrote it")
	s.Equal(types.TaxProviderStripe, records[0].Provider)
}

func (s *TaxAppliedLifecycleSuite) TestListWithRateExpandToleratesExternalRecords() {
	cust := s.customer()
	inv := s.draftInvoice(cust, "100")
	s.persistExternal(inv, "8.75")

	filter := types.NewNoLimitTaxAppliedFilter()
	filter.EntityType = types.TaxRateEntityTypeInvoice
	filter.EntityID = inv.ID
	filter.QueryFilter.Expand = lo.ToPtr(types.NewExpand("tax_rate").String())

	response, err := s.taxSvc.ListTaxApplied(s.GetContext(), filter)

	s.Require().NoError(err, "a null rate id must not fail the read")
	s.Require().Len(response.Items, 1)
	s.Nil(response.Items[0].TaxRate, "there is no Flexprice rate to expand")
}

func (s *TaxAppliedLifecycleSuite) TestListWithRateExpandStillResolvesNativeRates() {
	cust := s.customer()
	inv := s.draftInvoice(cust, "100")
	rate := s.rate("gst", 10)
	_, err := s.taxSvc.ApplyTaxesOnInvoice(s.GetContext(), inv, s.rates(rate))
	s.Require().NoError(err)

	filter := types.NewNoLimitTaxAppliedFilter()
	filter.EntityType = types.TaxRateEntityTypeInvoice
	filter.EntityID = inv.ID
	filter.QueryFilter.Expand = lo.ToPtr(types.NewExpand("tax_rate").String())

	response, err := s.taxSvc.ListTaxApplied(s.GetContext(), filter)

	s.Require().NoError(err)
	s.Require().Len(response.Items, 1)
	s.Require().NotNil(response.Items[0].TaxRate, "a native record still expands its rate")
	s.Equal(rate.ID, response.Items[0].TaxRate.ID)
}

// --- rebuilding a selection from the records ------------------------------

func (s *TaxAppliedLifecycleSuite) TestTaxRatesFromAppliedTaxesRebuildsTheSelection() {
	cust := s.customer()
	inv := s.draftInvoice(cust, "100")
	rate := s.rate("gst", 10)
	_, err := s.taxSvc.ApplyTaxesOnInvoice(s.GetContext(), inv, s.rates(rate))
	s.Require().NoError(err)

	rebuilt, err := s.taxSvc.TaxRatesFromAppliedTaxes(s.GetContext(), inv)

	s.Require().NoError(err)
	s.Require().Len(rebuilt.GetRates(), 1, "the selection survives as records even though the request is gone")
	s.Equal(rate.ID, rebuilt.GetRates()[0].ID)
	s.Equal(types.TaxBehaviorExclusive, rebuilt.GetRates()[0].TaxBehavior,
		"the behaviour frozen with the record comes back with it")
}

func (s *TaxAppliedLifecycleSuite) TestTaxRatesFromAppliedTaxesSkipsExternalRecords() {
	cust := s.customer()
	inv := s.draftInvoice(cust, "100")
	s.persistExternal(inv, "8.75")

	rebuilt, err := s.taxSvc.TaxRatesFromAppliedTaxes(s.GetContext(), inv)

	s.Require().NoError(err, "an external record carries no rate to rebuild from")
	s.Empty(rebuilt.GetRates())
}

func (s *TaxAppliedLifecycleSuite) TestTaxRatesFromAppliedTaxesOnAnUntaxedInvoiceResolvesNothing() {
	cust := s.customer()
	inv := s.draftInvoice(cust, "100")

	rebuilt, err := s.taxSvc.TaxRatesFromAppliedTaxes(s.GetContext(), inv)

	s.Require().NoError(err)
	s.Empty(rebuilt.GetRates(), "nothing was ever charged, so nothing is rebuilt")
}

// --- commit ---------------------------------------------------------------

func (s *TaxAppliedLifecycleSuite) TestCommitIsANoOpForTheNativeEngine() {
	cust := s.customer()
	inv := s.draftInvoice(cust, "100")
	rate := s.rate("gst", 10)
	_, err := s.taxSvc.ApplyTaxesOnInvoice(s.GetContext(), inv, s.rates(rate))
	s.Require().NoError(err)

	s.taxSvc.CommitTaxToProvider(s.GetContext(), inv)

	// Native tax is filed by the tenant, so there is nothing to record and nothing to stamp.
	for _, r := range s.appliedFor(inv.ID) {
		s.Nil(r.TaxTransactionID)
	}
}

func (s *TaxAppliedLifecycleSuite) TestCommitStampsOnlyRecordsThatAreNotYetFiled() {
	cust := s.customer()
	inv := s.draftInvoice(cust, "100")
	s.persistExternal(inv, "8.75")

	// Simulate a stamp that already landed.
	records := s.appliedFor(inv.ID)
	s.Require().Len(records, 1)
	records[0].TaxTransactionID = lo.ToPtr("tax_already_filed")
	s.Require().NoError(s.GetStores().TaxAppliedRepo.Update(s.GetContext(), records[0]))

	// Native engine resolves, so commit returns before reaching a provider. What matters is
	// that an already-filed record is never offered again.
	s.taxSvc.CommitTaxToProvider(s.GetContext(), inv)

	after := s.appliedFor(inv.ID)
	s.Require().Len(after, 1)
	s.Equal("tax_already_filed", lo.FromPtr(after[0].TaxTransactionID),
		"a repeated stamp must not change what is already recorded")
}

// --- freezing -------------------------------------------------------------

func (s *TaxAppliedLifecycleSuite) TestRecalculationIsRefusedOnASealedInvoice() {
	cust := s.customer()
	inv := s.draftInvoice(cust, "100")

	for _, status := range []types.InvoiceStatus{
		types.InvoiceStatusFinalized,
		types.InvoiceStatusVoided,
	} {
		inv.InvoiceStatus = status

		invSvc := NewInvoiceService(ServiceParams{
			Logger:         s.GetLogger(),
			Config:         s.GetConfig(),
			DB:             s.GetDB(),
			InvoiceRepo:    s.GetStores().InvoiceRepo,
			TaxAppliedRepo: s.GetStores().TaxAppliedRepo,
			SettingsRepo:   s.GetStores().SettingsRepo,
			ConnectionRepo: s.GetStores().ConnectionRepo,
		})

		_, err := invSvc.RecalculateTaxesOnInvoice(s.GetContext(), inv)

		s.Require().Error(err, "rewriting a sealed invoice's tax makes our record disagree with the provider's")
		s.True(ierr.IsValidation(err))
	}
}
