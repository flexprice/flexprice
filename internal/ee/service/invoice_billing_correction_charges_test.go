package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestInvoiceCorrectionCharges(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	line := &invoice.InvoiceLineItem{ID: "ili_usage", SubscriptionLineItemID: lo.ToPtr("sli_usage"), PriceID: lo.ToPtr("price_usage"),
		PriceType: lo.ToPtr(string(types.PRICE_TYPE_USAGE)), Quantity: decimal.NewFromInt(10), Amount: decimal.NewFromInt(10), PeriodStart: &start, PeriodEnd: &end}
	original := &invoice.Invoice{ID: "inv_original", LineItems: []*invoice.InvoiceLineItem{line}}
	for _, amount := range []int64{4, 0, 20} {
		t.Run("usage corrected to "+decimal.NewFromInt(amount).String(), func(t *testing.T) {
			replacement := billingScheduleLineRequest(line)
			replacement.Amount = decimal.NewFromInt(amount)
			replacement.Quantity = decimal.NewFromInt(amount)
			require.NoError(t, validateCorrectionCharges(original, []dto.CreateInvoiceLineItemRequest{replacement}))
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*dto.CreateInvoiceLineItemRequest)
	}{
		{"removed source", func(line *dto.CreateInvoiceLineItemRequest) { line.SubscriptionLineItemID = lo.ToPtr("sli_different") }},
		{"different window", func(line *dto.CreateInvoiceLineItemRequest) { line.PeriodStart = lo.ToPtr(start.AddDate(0, 1, 0)) }},
		{"scheduled line cannot substitute ordinary charge", func(line *dto.CreateInvoiceLineItemRequest) { line.BillingScheduleItemID = lo.ToPtr("bsi_replacement") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replacement := billingScheduleLineRequest(line)
			tc.change(&replacement)
			err := validateCorrectionCharges(original, []dto.CreateInvoiceLineItemRequest{replacement})
			require.True(t, ierr.IsVersionConflict(err), "%v", err)
			require.ErrorContains(t, err, "historical charge is missing")
		})
	}
	t.Run("legacy price identity survives repricing", func(t *testing.T) {
		legacy := invoice.NewInvoiceLineItemBuilder(line).Build()
		legacy.SubscriptionLineItemID = nil
		replacement := billingScheduleLineRequest(legacy)
		replacement.Amount = decimal.NewFromInt(35)
		require.NoError(t, validateCorrectionCharges(&invoice.Invoice{LineItems: []*invoice.InvoiceLineItem{legacy}}, []dto.CreateInvoiceLineItemRequest{replacement}))
	})
}

func TestInvoiceCorrectionKeepsZeroUsageRows(t *testing.T) {
	suite := &InvoiceServiceSuite{}
	suite.SetT(t)
	suite.SetupSuite()
	suite.SetupTest()
	t.Cleanup(suite.TearDownTest)
	ctx := suite.GetContext()
	inv := suite.computedCycleDraft("sub_correction_zero_usage", "usd", decimal.NewFromInt(10))
	params := suite.service.(*invoiceService).ServiceParams
	sub, err := params.SubRepo.Get(ctx, *inv.SubscriptionID)
	require.NoError(t, err)
	usage := billingScheduleLineRequest(inv.LineItems[0])
	usage.PriceType = lo.ToPtr(string(types.PRICE_TYPE_USAGE))
	usage.Amount, usage.Quantity = decimal.Zero, decimal.Zero
	billing := NewBillingService(params)
	for _, tc := range []struct {
		name  string
		ctx   context.Context
		count int
	}{
		{"ordinary invoice still omits zero usage", ctx, 0},
		{"correction keeps a zero result for its source check", withCorrectionCharges(ctx), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, err := billing.CreateInvoiceRequestForCharges(tc.ctx, &dto.CreateInvoiceRequestForChargesParams{Subscription: sub,
				PeriodStart: *inv.PeriodStart, PeriodEnd: *inv.PeriodEnd, ReferencePoint: types.ReferencePointPeriodEnd,
				Result: &dto.BillingCalculationResult{Currency: "usd", UsageCharges: []dto.CreateInvoiceLineItemRequest{usage}}})
			require.NoError(t, err)
			require.Len(t, request.LineItems, tc.count)
			if tc.count > 0 {
				require.NoError(t, validateCorrectionCharges(inv, request.LineItems))
			}
		})
	}
}
