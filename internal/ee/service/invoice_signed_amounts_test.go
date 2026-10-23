package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func (s *InvoiceServiceSuite) signedCycleDraft(id string, usage, positiveUsage int64) *invoice.Invoice {
	ctx := s.GetContext()
	now := time.Now().UTC()
	cutoff := now.Add(24 * time.Hour)
	draft := s.checkpointCycleDraft(id, decimal.NewFromInt(1), cutoff, false)
	s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
		Event:   events.Event{ID: types.GenerateUUID(), TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx), ExternalCustomerID: s.testData.customer.ExternalID, Timestamp: draft.PeriodStart.Add(3 * time.Hour)},
		MeterID: "meter_" + id, QtyTotal: decimal.NewFromInt(usage - 70),
	}}))
	if positiveUsage > 0 {
		positiveMeter := &meter.Meter{ID: "meter_positive_" + id, EventName: "positive_usage", ResetUsage: types.ResetUsageBillingPeriod, Aggregation: meter.Aggregation{Type: types.AggregationSum}, BaseModel: types.GetDefaultBaseModel(ctx)}
		s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, positiveMeter))
		s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
			Event:   events.Event{ID: types.GenerateUUID(), TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx), ExternalCustomerID: s.testData.customer.ExternalID, Timestamp: draft.PeriodStart.Add(time.Hour)},
			MeterID: positiveMeter.ID, QtyTotal: decimal.NewFromInt(positiveUsage),
		}}))
		p := &price.Price{ID: "positive_" + id, Amount: decimal.NewFromInt(1), Currency: "usd", MeterID: positiveMeter.ID, EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: s.testData.plan.ID, Type: types.PRICE_TYPE_USAGE,
			BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1, BillingModel: types.BILLING_MODEL_FLAT_FEE, BillingCadence: types.BILLING_CADENCE_RECURRING, InvoiceCadence: types.InvoiceCadenceArrear, BaseModel: types.GetDefaultBaseModel(ctx)}
		s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, p))
		li := &subscription.SubscriptionLineItem{ID: "li_positive_" + id, SubscriptionID: id, CustomerID: s.testData.customer.ID, EntityID: s.testData.plan.ID, EntityType: types.SubscriptionLineItemEntityTypePlan,
			PriceID: p.ID, PriceType: p.Type, MeterID: positiveMeter.ID, Quantity: decimal.NewFromInt(1), Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceArrear, StartDate: *draft.PeriodStart, BaseModel: types.GetDefaultBaseModel(ctx)}
		s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.CreateBulk(ctx, []*subscription.SubscriptionLineItem{li}))
	}
	return draft
}

func (s *InvoiceServiceSuite) checkSignedCycle(usage, positiveUsage, credits, want int64) {
	ctx := s.GetContext()
	var walletID string
	var seeded decimal.Decimal
	if credits > 0 {
		walletID, seeded = s.seedPrepaidWallet(decimal.NewFromInt(credits))
	}
	draft := s.signedCycleDraft("sub_signed", usage, positiveUsage)
	for retry := 0; retry < 2; retry++ {
		computed, skipped, err := s.service.ComputeInvoice(ctx, draft.ID, nil)
		s.Require().NoError(err)
		s.False(skipped, "offsetting or negative charges must not be skipped")
		s.True(computed.Total.Equal(decimal.NewFromInt(usage + positiveUsage)))
		s.NoError(computed.Validate())
	}
	if credits > 0 {
		before, err := s.GetStores().WalletRepo.GetWalletByID(ctx, walletID)
		s.Require().NoError(err)
		s.True(before.CreditBalance.Equal(seeded), "subscription drafts do not debit wallets")
	}
	s.Require().NoError(s.service.FinalizeInvoice(ctx, draft.ID, dto.FinalizeInvoiceRequest{}))
	finalized, err := s.invoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, finalized.InvoiceStatus)
	s.True(finalized.Total.Equal(decimal.NewFromInt(want)), "total %s", finalized.Total)
	s.True(finalized.AmountDue.Equal(decimal.Max(decimal.NewFromInt(want), decimal.Zero)))
	s.True(finalized.AmountRemaining.Equal(finalized.AmountDue))
	s.True(finalized.AmountPaid.IsZero())
	s.NoError(finalized.Validate())
	if want <= 0 {
		s.Equal(types.PaymentStatusSucceeded, finalized.PaymentStatus)
		params := s.service.(*invoiceService).ServiceParams
		sub, err := params.SubRepo.Get(ctx, *finalized.SubscriptionID)
		s.Require().NoError(err)
		result := NewSubscriptionPaymentProcessor(&params).(*subscriptionPaymentProcessor).processPayment(ctx, sub, dto.NewInvoiceResponse(finalized), types.PaymentBehaviorDefaultActive, types.InvoiceFlowRenewal)
		s.True(result.Success)
		s.True(result.AmountPaid.IsZero())
		s.Empty(result.PaymentMethods)
	}
	if credits > 0 {
		after, err := s.GetStores().WalletRepo.GetWalletByID(ctx, walletID)
		s.Require().NoError(err)
		positiveCharges := decimal.Max(decimal.NewFromInt(usage), decimal.Zero).Add(decimal.Max(decimal.NewFromInt(positiveUsage), decimal.Zero))
		expectedDebit := decimal.Min(positiveCharges, decimal.NewFromInt(credits))
		s.True(after.CreditBalance.Equal(seeded.Sub(expectedDebit)), "wallet never receives a credit from a negative total")
	}
	items, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, draft.ID)
	s.Require().NoError(err)
	foundNegative := false
	for _, li := range items {
		if li.Quantity.IsNegative() {
			foundNegative = true
			s.True(li.Amount.IsNegative())
		}
	}
	s.True(foundNegative)
	snapshot := *finalized
	snapshot.LineItems = items
	breakdown := dto.NewInvoiceResponse(&snapshot)
	s.service.(*invoiceService).recalculateInvoiceTotals(breakdown)
	s.True(breakdown.Total.Equal(finalized.Total), "usage-breakdown reads must preserve signed credit-adjusted totals")
	s.True(breakdown.AmountDue.Equal(finalized.AmountDue))
	payments, err := s.GetStores().PaymentRepo.List(ctx, types.NewNoLimitPaymentFilter())
	s.Require().NoError(err)
	s.Empty(payments, "finalization must not manufacture payments")
}

func (s *InvoiceServiceSuite) TestSignedInvoice_NegativeOnlyNoWalletCredit() {
	s.checkSignedCycle(-30, 0, 100, -30)
}
func (s *InvoiceServiceSuite) TestSignedInvoice_MixedPositiveTotal() {
	s.checkSignedCycle(-30, 100, 0, 70)
}
func (s *InvoiceServiceSuite) TestSignedInvoice_ZeroNetFinalizes() {
	s.checkSignedCycle(-100, 100, 0, 0)
}
func (s *InvoiceServiceSuite) TestSignedInvoice_PrepaidAppliesOnlyToPositiveLines() {
	s.checkSignedCycle(-30, 100, 100, -30)
}
func (s *InvoiceServiceSuite) TestSignedInvoice_PrepaidLimitedByWalletBalance() {
	s.checkSignedCycle(-30, 100, 50, 20)
}

func TestSignedInvoiceTaxAndFX(t *testing.T) {
	inv := &invoice.Invoice{Currency: "usd", Subtotal: decimal.NewFromInt(-30), Total: decimal.NewFromInt(-30), AmountPaid: decimal.Zero,
		LineItems: []*invoice.InvoiceLineItem{{Amount: decimal.NewFromInt(-30), Quantity: decimal.NewFromInt(-30), PriceType: lo.ToPtr(string(types.PRICE_TYPE_USAGE)), Currency: "usd"}}}
	applyTaxResultToInvoice(inv, &dto.TaxCalculationResult{})
	require.Equal(t, "-30", inv.Total.String())
	require.True(t, inv.AmountDue.IsZero())
	require.NoError(t, convertInvoiceAmounts(inv, &FXRateResolution{From: "usd", To: "inr", Rate: decimal.NewFromInt(80)}, time.Now().UTC()))
	require.Equal(t, "-2400", inv.Total.String())
	require.True(t, inv.AmountDue.IsZero())
	require.True(t, inv.AmountRemaining.IsZero())
	require.NoError(t, inv.Validate())
}

func (s *InvoiceServiceSuite) TestSignedInvoice_RecomputePreservesRecordedPayment() {
	ctx := s.GetContext()
	draft := s.signedCycleDraft("sub_signed_paid", 70, 0)
	computed, _, err := s.service.ComputeInvoice(ctx, draft.ID, nil)
	s.Require().NoError(err)
	computed.AmountPaid = decimal.NewFromInt(20)
	computed.AmountRemaining = computed.AmountDue.Sub(computed.AmountPaid)
	s.Require().NoError(s.invoiceRepo.Update(ctx, computed))
	s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
		Event:   events.Event{ID: types.GenerateUUID(), TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx), ExternalCustomerID: s.testData.customer.ExternalID, Timestamp: draft.PeriodStart.Add(4 * time.Hour)},
		MeterID: "meter_sub_signed_paid", QtyTotal: decimal.NewFromInt(-100),
	}}))
	computed, _, err = s.service.ComputeInvoice(ctx, draft.ID, nil)
	s.Require().NoError(err)
	s.Equal("-30", computed.Total.String())
	s.Equal("20", computed.AmountPaid.String())
	s.True(computed.AmountDue.IsZero())
	s.True(computed.AmountRemaining.IsZero())
	s.Equal(types.PaymentStatusOverpaid, computed.PaymentStatus)
	s.Require().NoError(s.service.FinalizeInvoice(ctx, draft.ID, dto.FinalizeInvoiceRequest{}))
	sealed, err := s.invoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal("20", sealed.AmountPaid.String())
	s.Equal(types.PaymentStatusOverpaid, sealed.PaymentStatus)
	s.NoError(sealed.Validate())
	s.Require().NoError(s.service.RecalculateInvoiceAmounts(ctx, sealed.ID))
	recalculated, err := s.invoiceRepo.Get(ctx, sealed.ID)
	s.Require().NoError(err)
	s.Equal("20", recalculated.AmountPaid.String())
	s.Equal(types.PaymentStatusOverpaid, recalculated.PaymentStatus)
	s.NoError(recalculated.Validate())
}

func (s *InvoiceServiceSuite) TestSignedInvoice_PreviewPreservesNegativeTotal() {
	ctx := s.GetContext()
	draft := s.signedCycleDraft("sub_signed_preview", -30, 0)
	preview, err := s.service.GetPreviewInvoice(ctx, dto.GetPreviewInvoiceRequest{SubscriptionID: "sub_signed_preview", PeriodStart: draft.PeriodStart, PeriodEnd: draft.PeriodEnd})
	s.Require().NoError(err)
	s.Equal("-30", preview.Total.String())
	s.True(preview.AmountDue.IsZero())
	s.True(preview.AmountRemaining.IsZero())
	s.NoError(preview.Invoice.Validate())
}

func (s *InvoiceServiceSuite) TestSignedInvoice_CustomCurrencyPreviewAndFinalize() {
	ctx := s.GetContext()
	s.seedCustomCurrencyConfig()
	req := dto.CreateInvoiceRequest{
		CustomerID: s.testData.customer.ID, InvoiceType: types.InvoiceTypeOneOff, Currency: "mac",
		Subtotal: decimal.NewFromInt(-30), Total: decimal.NewFromInt(-30), AmountDue: decimal.Zero,
		LineItems: []dto.CreateInvoiceLineItemRequest{{Amount: decimal.NewFromInt(-30), Quantity: decimal.NewFromInt(-30), PriceType: lo.ToPtr(string(types.PRICE_TYPE_USAGE))}},
	}
	s.Require().NoError(req.Validate())
	preview, err := s.service.CreatePreviewInvoice(ctx, req)
	s.Require().NoError(err)
	s.Equal("usd", preview.Currency)
	s.Equal("-3", preview.Total.String())
	s.True(preview.AmountDue.IsZero())
	sealed, err := s.service.CreateInvoice(ctx, req)
	s.Require().NoError(err)
	s.Equal(preview.Total.String(), sealed.Total.String())
	s.Equal(types.InvoiceStatusFinalized, sealed.InvoiceStatus)
	s.Equal(types.PaymentStatusSucceeded, sealed.PaymentStatus)
	s.True(sealed.AmountPaid.IsZero())
	s.True(sealed.AmountDue.IsZero())
	s.True(sealed.AmountRemaining.IsZero())
	s.Require().NotNil(sealed.CustomCurrency)
	s.Equal("-30", sealed.CustomCurrency.Total.String())
	s.True(sealed.CustomCurrency.AmountDue.IsZero())
	s.NoError(sealed.Invoice.Validate())
}

func TestSignedInvoiceCollectionAmounts(t *testing.T) {
	for _, tc := range []struct {
		name                                    string
		total, paid, adjustment, due, remaining int64
		status                                  types.PaymentStatus
	}{
		{"negative unpaid", -30, 0, 0, 0, 0, types.PaymentStatusPending},
		{"zero unpaid", 0, 0, 0, 0, 0, types.PaymentStatusPending},
		{"positive unpaid", 70, 0, 0, 70, 70, types.PaymentStatusPending},
		{"partial payment", 70, 20, 0, 70, 50, types.PaymentStatusPending},
		{"negative with previous payment", -30, 20, 0, 0, 0, types.PaymentStatusOverpaid},
		{"adjustment below total", 70, 20, 10, 60, 40, types.PaymentStatusPending},
		{"adjustment beyond total", 10, 0, 20, 0, 0, types.PaymentStatusPending},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inv := &invoice.Invoice{Subtotal: decimal.NewFromInt(tc.total), AmountPaid: decimal.NewFromInt(tc.paid),
				AdjustmentAmount: decimal.NewFromInt(tc.adjustment), PaymentStatus: tc.status}
			applyTaxResultToInvoice(inv, &dto.TaxCalculationResult{})
			require.Equal(t, decimal.NewFromInt(tc.total).String(), inv.Total.String())
			require.Equal(t, decimal.NewFromInt(tc.paid).String(), inv.AmountPaid.String())
			require.Equal(t, decimal.NewFromInt(tc.due).String(), inv.AmountDue.String())
			require.Equal(t, decimal.NewFromInt(tc.remaining).String(), inv.AmountRemaining.String())
			require.Equal(t, tc.status, inv.PaymentStatus)
			require.NoError(t, inv.Validate())
		})
	}
}

func (s *InvoiceServiceSuite) TestSignedInvoice_BreakdownPreservesPaymentStatus() {
	for _, status := range []types.PaymentStatus{types.PaymentStatusPending, types.PaymentStatusFailed, types.PaymentStatusSucceeded} {
		inv := &invoice.Invoice{Subtotal: decimal.NewFromInt(70), Total: decimal.NewFromInt(70),
			AmountPaid: decimal.NewFromInt(20), PaymentStatus: status, Currency: "usd",
			LineItems: []*invoice.InvoiceLineItem{{Amount: decimal.NewFromInt(70), Currency: "usd"}}}
		response := dto.NewInvoiceResponse(inv)
		s.service.(*invoiceService).recalculateInvoiceTotals(response)
		s.Equal(status, response.PaymentStatus)
		s.Equal("70", response.Total.String())
		s.Equal("50", response.AmountRemaining.String())
	}
}

func (s *InvoiceServiceSuite) TestSignedInvoice_RecomputeClearsOverpaidWhenUsageRecovers() {
	ctx := s.GetContext()
	draft := s.signedCycleDraft("sub_signed_recovered", -30, 0)
	computed, _, err := s.service.ComputeInvoice(ctx, draft.ID, nil)
	s.Require().NoError(err)
	computed.AmountPaid = decimal.NewFromInt(20)
	computed.PaymentStatus = types.PaymentStatusOverpaid
	s.Require().NoError(s.invoiceRepo.Update(ctx, computed))
	s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
		Event: events.Event{ID: types.GenerateUUID(), TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
			ExternalCustomerID: s.testData.customer.ExternalID, Timestamp: draft.PeriodStart.Add(4 * time.Hour)},
		MeterID: "meter_sub_signed_recovered", QtyTotal: decimal.NewFromInt(100),
	}}))
	computed, _, err = s.service.ComputeInvoice(ctx, draft.ID, nil)
	s.Require().NoError(err)
	s.Equal("70", computed.Total.String())
	s.Equal("20", computed.AmountPaid.String())
	s.Equal("50", computed.AmountRemaining.String())
	s.Equal(types.PaymentStatusPending, computed.PaymentStatus)
	s.NoError(computed.Validate())
}
