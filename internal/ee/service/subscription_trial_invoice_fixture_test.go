package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func persistTrialActivationInvoice(t *testing.T, ctx context.Context, params ServiceParams, sub *subscription.Subscription, source *invoice.Invoice) {
	t.Helper()
	if sub.SubscriptionType == "" {
		sub.SubscriptionType = types.SubscriptionTypeStandalone
		require.NoError(t, params.SubRepo.Update(ctx, sub))
	}
	source.CustomerID = sub.GetInvoicingCustomerID()
	source.SubscriptionID = &sub.ID
	source.InvoiceType = types.InvoiceTypeSubscription
	source.InvoiceStatus = types.InvoiceStatusFinalized
	source.PaymentStatus = types.PaymentStatusSucceeded
	source.BillingReason = string(types.InvoiceBillingReasonSubscriptionTrialEnd)
	source.Currency = sub.Currency
	source.EnvironmentID = types.GetEnvironmentID(ctx)
	source.BaseModel = types.GetDefaultBaseModel(ctx)
	source.PeriodStart, source.PeriodEnd = &sub.CurrentPeriodStart, &sub.CurrentPeriodEnd
	source.FinalizedAt, source.LastComputedAt = sub.TrialEnd, sub.TrialEnd
	if !source.AmountDue.IsPositive() {
		source.AmountDue = decimal.NewFromInt(10)
	}
	source.AmountPaid = source.AmountDue
	source.Subtotal, source.Total = source.AmountDue, source.AmountDue
	source.AmountRemaining = decimal.Zero
	require.NoError(t, params.InvoiceRepo.Create(ctx, source))
}
