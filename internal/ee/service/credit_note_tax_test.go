package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// =============================================================================
// The preview / persist split, from the credit note service
// =============================================================================

// A quote is not a credit note. Preview runs the same build and the same calculation the real
// thing does, and writing anything at all would leave a credit note nobody asked to issue.
func (s *CreditNoteServiceSuite) TestPreviewCreditNoteWritesNothing() {
	inv := s.testData.invoices.finalized

	before, err := s.GetStores().CreditNoteRepo.List(s.GetContext(), types.NewNoLimitCreditNoteFilter())
	s.Require().NoError(err)

	preview, err := s.service.PreviewCreditNote(s.GetContext(), &dto.CreateCreditNoteRequest{
		InvoiceID: inv.ID,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{InvoiceLineItemID: "line_1", Amount: decimal.NewFromInt(20)},
		},
	})
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(20).Equal(preview.Subtotal), "subtotal is the summed line items, got %s", preview.Subtotal)

	after, err := s.GetStores().CreditNoteRepo.List(s.GetContext(), types.NewNoLimitCreditNoteFilter())
	s.Require().NoError(err)
	s.Len(after, len(before), "a preview must not create a credit note")

	s.Empty(s.creditNoteTaxRows(), "a preview must not write tax rows")
}

// A reason is required to issue a credit note but has no bearing on what it is taxed at, and the
// dashboard quotes while the form is still being filled in.
func (s *CreditNoteServiceSuite) TestPreviewCreditNoteNeedsNoReason() {
	_, err := s.service.PreviewCreditNote(s.GetContext(), &dto.CreateCreditNoteRequest{
		InvoiceID: s.testData.invoices.finalized.ID,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{InvoiceLineItemID: "line_1", Amount: decimal.NewFromInt(10)},
		},
	})
	s.NoError(err, "a quote must not require a reason")
}

// What the quote depends on is still checked. An invoice or a line item missing is a bad request
// whether or not anything is being written.
func (s *CreditNoteServiceSuite) TestPreviewCreditNoteValidatesWhatItQuotes() {
	_, err := s.service.PreviewCreditNote(s.GetContext(), &dto.CreateCreditNoteRequest{
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{InvoiceLineItemID: "line_1", Amount: decimal.NewFromInt(10)},
		},
	})
	s.Error(err, "a quote with no invoice is a bad request")

	_, err = s.service.PreviewCreditNote(s.GetContext(), &dto.CreateCreditNoteRequest{
		InvoiceID: s.testData.invoices.finalized.ID,
	})
	s.Error(err, "a quote with no line items is a bad request")
}

// Under the native engine a credited amount carries no tax of its own, so the total is the
// subtotal and there is nothing to persist. This is the documented gap, pinned so a change to it
// is deliberate.
func (s *CreditNoteServiceSuite) TestNativeCreditNoteIsQuotedUntaxed() {
	preview, err := s.service.PreviewCreditNote(s.GetContext(), &dto.CreateCreditNoteRequest{
		InvoiceID: s.testData.invoices.finalized.ID,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{InvoiceLineItemID: "line_1", Amount: decimal.NewFromInt(20)},
		},
	})
	s.Require().NoError(err)

	s.True(decimal.Zero.Equal(preview.TotalTax), "native credits carry no tax, got %s", preview.TotalTax)
	s.True(decimal.NewFromInt(20).Equal(preview.TotalAmount), "the total is the subtotal, got %s", preview.TotalAmount)
	s.Empty(preview.Taxes)
}

// Create runs the same build and calculation the preview does, so the figure quoted is the
// figure written.
func (s *CreditNoteServiceSuite) TestCreateCreditNoteMatchesItsQuote() {
	req := &dto.CreateCreditNoteRequest{
		InvoiceID: s.testData.invoices.finalized.ID,
		Reason:    types.CreditNoteReasonBillingError,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{InvoiceLineItemID: "line_1", Amount: decimal.NewFromInt(20)},
		},
		ProcessCreditNote: false,
	}

	preview, err := s.service.PreviewCreditNote(s.GetContext(), req)
	s.Require().NoError(err)

	created, err := s.service.CreateCreditNote(s.GetContext(), req)
	s.Require().NoError(err)

	s.True(preview.TotalAmount.Equal(created.TotalAmount),
		"quoted %s but issued %s", preview.TotalAmount, created.TotalAmount)
}

func (s *CreditNoteServiceSuite) creditNoteTaxRows() []*taxapplied.TaxApplied {
	filter := types.NewNoLimitTaxAppliedFilter()
	filter.EntityType = types.TaxRateEntityTypeCreditNote

	rows, err := s.GetStores().TaxAppliedRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	return rows
}

// =============================================================================
// Persisting and reversing, from the tax service
// =============================================================================

// CreditNoteTaxSuite covers what the tax service does with a credit note's rows: writing them
// when it is created, and stamping them when its reversal is filed.
type CreditNoteTaxSuite struct {
	testutil.BaseServiceTestSuite
	svc TaxService
}

func TestCreditNoteTax(t *testing.T) {
	suite.Run(t, new(CreditNoteTaxSuite))
}

func (s *CreditNoteTaxSuite) SetupTest() {
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
		SubRepo:            st.SubscriptionRepo,
		SettingsRepo:       st.SettingsRepo,
		ConnectionRepo:     st.ConnectionRepo,
	})
}

func (s *CreditNoteTaxSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *CreditNoteTaxSuite) invoice() *invoice.Invoice {
	cust := &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: types.GenerateUUIDWithPrefix("ext"),
		Name:       "Credit Note Customer",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), cust))

	inv := &invoice.Invoice{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:    cust.ID,
		InvoiceStatus: types.InvoiceStatusFinalized,
		PaymentStatus: types.PaymentStatusSucceeded,
		Currency:      "USD",
		Subtotal:      decimal.NewFromInt(100),
		Total:         decimal.NewFromInt(118),
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))
	return inv
}

// filedInvoiceTax writes the row a finalized invoice carries once its tax has been recorded with
// the provider, which is what a reversal is undoing.
func (s *CreditNoteTaxSuite) filedInvoiceTax(inv *invoice.Invoice, transactionID string) *taxapplied.TaxApplied {
	row := &taxapplied.TaxApplied{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_APPLIED),
		EntityType:         types.TaxRateEntityTypeInvoice,
		EntityID:           inv.ID,
		TaxableAmount:      decimal.NewFromInt(100),
		TaxAmount:          decimal.NewFromInt(18),
		Currency:           inv.Currency,
		Provider:           types.TaxProviderStripe,
		TaxTransactionID:   lo.ToPtr(transactionID),
		TaxTransactionType: types.TaxTransactionTypeFiling,
		ExternalTaxDetails: &types.ExternalTaxDetails{
			CalculationID: "taxcalc_invoice",
			DisplayName:   "IGST",
			Reference:     inv.ID,
		},
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().TaxAppliedRepo.Create(s.GetContext(), row))
	return row
}

func (s *CreditNoteTaxSuite) rowsFor(entityType types.TaxRateEntityType, entityID string) []*taxapplied.TaxApplied {
	filter := types.NewNoLimitTaxAppliedFilter()
	filter.EntityType = entityType
	filter.EntityID = entityID

	rows, err := s.GetStores().TaxAppliedRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	return rows
}

// A calculation is held in memory until it is persisted. This is the step that makes a draft
// credit note carry the breakdown behind its total.
func (s *CreditNoteTaxSuite) TestPersistTaxResultWritesCreditNoteRows() {
	creditNoteID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_NOTE)

	result := &dto.TaxCalculationResult{
		Provider:       types.TaxProviderStripe,
		TotalTaxAmount: decimal.NewFromFloat(3.6),
		TaxAppliedRecords: []*dto.TaxAppliedResponse{
			{TaxApplied: taxapplied.TaxApplied{
				TaxableAmount:      decimal.NewFromInt(20),
				TaxAmount:          decimal.NewFromFloat(3.6),
				TaxTransactionType: types.TaxTransactionTypeReversal,
				ExternalTaxDetails: &types.ExternalTaxDetails{CalculationID: "taxcalc_cn", DisplayName: "IGST"},
			}},
		},
	}

	s.Require().NoError(s.svc.PersistTaxResult(s.GetContext(),
		types.TaxRateEntityTypeCreditNote, creditNoteID, "USD", result))

	rows := s.rowsFor(types.TaxRateEntityTypeCreditNote, creditNoteID)
	s.Require().Len(rows, 1)
	s.Equal(creditNoteID, rows[0].EntityID)
	s.Equal(types.TaxRateEntityTypeCreditNote, rows[0].EntityType)
	s.Equal("USD", rows[0].Currency)
	s.Equal(types.TaxProviderStripe, rows[0].Provider)
	s.True(rows[0].IsReversal(), "a credit note's tax is only ever filed as a reversal")
	s.Empty(lo.FromPtr(rows[0].TaxTransactionID), "nothing is filed until the credit note is finalized")

	// The calculation stays on the details the document renders from rather than being moved
	// into metadata.
	s.Equal("taxcalc_cn", rows[0].ExternalTaxDetails.GetCalculationID())
}

// Re-persisting replaces the previous generation rather than adding to it, so a recalculated
// credit note never reports two sets of tax at once.
func (s *CreditNoteTaxSuite) TestPersistTaxResultReplacesEarlierRows() {
	creditNoteID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_NOTE)

	persist := func(tax string) {
		s.Require().NoError(s.svc.PersistTaxResult(s.GetContext(),
			types.TaxRateEntityTypeCreditNote, creditNoteID, "USD", &dto.TaxCalculationResult{
				Provider: types.TaxProviderStripe,
				TaxAppliedRecords: []*dto.TaxAppliedResponse{
					{TaxApplied: taxapplied.TaxApplied{
						TaxAmount:          decimal.RequireFromString(tax),
						TaxTransactionType: types.TaxTransactionTypeReversal,
					}},
				},
			}))
	}

	persist("3.6")
	persist("1.8")

	rows := s.rowsFor(types.TaxRateEntityTypeCreditNote, creditNoteID)
	s.Require().Len(rows, 1, "the earlier generation is archived, not kept alongside")
	s.True(decimal.NewFromFloat(1.8).Equal(rows[0].TaxAmount), "got %s", rows[0].TaxAmount)
}

// A credit note already carries its reversal rows, so filing stamps them. Writing new ones would
// leave the credit note reporting its tax twice.
func (s *CreditNoteTaxSuite) TestRecordReversalStampsRowsTheEntityAlreadyHas() {
	inv := s.invoice()
	filed := s.filedInvoiceTax(inv, "tax_original")
	creditNoteID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_NOTE)

	s.Require().NoError(s.svc.(*taxService).PersistTaxResult(s.GetContext(),
		types.TaxRateEntityTypeCreditNote, creditNoteID, "USD", &dto.TaxCalculationResult{
			Provider: types.TaxProviderStripe,
			TaxAppliedRecords: []*dto.TaxAppliedResponse{
				{TaxApplied: taxapplied.TaxApplied{
					TaxAmount:          decimal.NewFromFloat(3.6),
					TaxTransactionType: types.TaxTransactionTypeReversal,
					ExternalTaxDetails: &types.ExternalTaxDetails{CalculationID: "taxcalc_cn"},
				}},
			},
		}))

	req := dto.TaxReversalRequest{
		EntityType:            types.TaxRateEntityTypeCreditNote,
		EntityID:              creditNoteID,
		InvoiceID:             inv.ID,
		Mode:                  types.TaxReversalModePartial,
		OriginalTransactionID: "tax_original",
		Reference:             "idem_key_abc",
		Amount:                decimal.NewFromFloat(23.6),
		Currency:              "USD",
	}
	s.Require().NoError(s.svc.(*taxService).recordReversal(s.GetContext(), req,
		[]*taxapplied.TaxApplied{filed}, "tax_reversal"))

	rows := s.rowsFor(types.TaxRateEntityTypeCreditNote, creditNoteID)
	s.Require().Len(rows, 1, "the existing row is stamped, not duplicated")
	s.Equal("tax_reversal", lo.FromPtr(rows[0].TaxTransactionID))
	s.True(decimal.NewFromFloat(3.6).Equal(rows[0].TaxAmount), "the amount is left as calculated, got %s", rows[0].TaxAmount)

	details := rows[0].ExternalTaxDetails
	s.Require().NotNil(details.Reversal)
	s.Equal(types.TaxReversalModePartial, details.Reversal.Mode)
	s.Equal("tax_original", details.Reversal.OriginalTransactionID)
	s.Equal("idem_key_abc", details.Reference)
	s.Equal("taxcalc_cn", details.GetCalculationID(), "the calculation stays on the details")
}

// A voided invoice carries only its filing, so the rows recording the undoing are created. They
// carry no amount: an amount says what tax a document carries, and a reversal carries none.
func (s *CreditNoteTaxSuite) TestRecordReversalWritesRowsWhenTheEntityHasNone() {
	inv := s.invoice()
	filed := s.filedInvoiceTax(inv, "tax_original")

	req := dto.TaxReversalRequest{
		EntityType:            types.TaxRateEntityTypeInvoice,
		EntityID:              inv.ID,
		Mode:                  types.TaxReversalModeFull,
		OriginalTransactionID: "tax_original",
		Reference:             types.TaxReferenceVoidPrefix + inv.ID,
		Currency:              inv.Currency,
	}
	s.Require().NoError(s.svc.(*taxService).recordReversal(s.GetContext(), req,
		[]*taxapplied.TaxApplied{filed}, "tax_reversal"))

	rows := s.rowsFor(types.TaxRateEntityTypeInvoice, inv.ID)
	s.Require().Len(rows, 2, "the filing stays and the reversal is added beside it")

	reversal, found := lo.Find(rows, func(row *taxapplied.TaxApplied) bool { return row.IsReversal() })
	s.Require().True(found)
	s.Equal("tax_reversal", lo.FromPtr(reversal.TaxTransactionID))
	s.True(reversal.TaxAmount.IsZero(), "a reversal row carries no tax, got %s", reversal.TaxAmount)
	s.True(reversal.TaxableAmount.IsZero(), "got %s", reversal.TaxableAmount)
	s.Equal(inv.Currency, reversal.Currency, "the schema requires a currency")

	original, found := lo.Find(rows, func(row *taxapplied.TaxApplied) bool { return !row.IsReversal() })
	s.Require().True(found)
	s.True(decimal.NewFromInt(18).Equal(original.TaxAmount),
		"the filed row is left exactly as finalization sealed it, got %s", original.TaxAmount)
}

// The provider does not link an original back to what reversed it, so the guard against filing a
// second correction is ours.
func (s *CreditNoteTaxSuite) TestTaxIsReversedOnlyCountsAFiledReversal() {
	inv := s.invoice()
	s.filedInvoiceTax(inv, "tax_original")

	reversed, err := s.svc.(*taxService).taxIsReversed(s.GetContext(), types.TaxRateEntityTypeInvoice, inv.ID)
	s.Require().NoError(err)
	s.False(reversed, "a filing is not a reversal")

	creditNoteID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_NOTE)
	s.Require().NoError(s.svc.PersistTaxResult(s.GetContext(),
		types.TaxRateEntityTypeCreditNote, creditNoteID, "USD", &dto.TaxCalculationResult{
			Provider: types.TaxProviderStripe,
			TaxAppliedRecords: []*dto.TaxAppliedResponse{
				{TaxApplied: taxapplied.TaxApplied{TaxTransactionType: types.TaxTransactionTypeReversal}},
			},
		}))

	reversed, err = s.svc.(*taxService).taxIsReversed(s.GetContext(), types.TaxRateEntityTypeCreditNote, creditNoteID)
	s.Require().NoError(err)
	s.False(reversed, "an unstamped reversal row was never filed")

	rows := s.rowsFor(types.TaxRateEntityTypeCreditNote, creditNoteID)
	rows[0].TaxTransactionID = lo.ToPtr("tax_reversal")
	s.Require().NoError(s.GetStores().TaxAppliedRepo.Update(s.GetContext(), rows[0]))

	reversed, err = s.svc.(*taxService).taxIsReversed(s.GetContext(), types.TaxRateEntityTypeCreditNote, creditNoteID)
	s.Require().NoError(err)
	s.True(reversed)
}

// Native tax was never filed with a provider, so there is nothing to un-file and nothing is
// written. The invoice's own rows are left alone.
func (s *CreditNoteTaxSuite) TestReverseTaxOnProviderIsANoOpForNative() {
	inv := s.invoice()
	s.filedInvoiceTax(inv, "tax_original")

	s.Require().NoError(s.svc.ReverseTaxOnProvider(s.GetContext(), dto.TaxReversalRequest{
		EntityType: types.TaxRateEntityTypeInvoice,
		EntityID:   inv.ID,
		Mode:       types.TaxReversalModeFull,
		Reference:  types.TaxReferenceVoidPrefix + inv.ID,
		Currency:   inv.Currency,
	}))

	rows := s.rowsFor(types.TaxRateEntityTypeInvoice, inv.ID)
	s.Len(rows, 1, "native reverses nothing and records nothing")
}
