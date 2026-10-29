package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type trialRecoveryTraversal struct {
	subscription.Repository
	boundaries []string
}

func (r *trialRecoveryTraversal) GetSubscriptionsForBillingPeriodUpdate(ctx context.Context, filter *types.SubscriptionFilter) ([]*subscription.Subscription, error) {
	r.boundaries = append(r.boundaries, filter.AfterID)
	return r.Repository.GetSubscriptionsForBillingPeriodUpdate(ctx, filter)
}

func TestTrialRecoveryCron_AdvancesPastUnpaidCandidates(t *testing.T) {
	s, seed := newSubscriptionUpdateFixture(t)
	ctx := s.GetContext()
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	trialEnd := start.AddDate(0, 0, 14)
	var last *subscription.Subscription
	for i := 0; i < 101; i++ {
		candidate := *seed
		candidate.ID = fmt.Sprintf("recovery_%03d", i)
		candidate.SubscriptionStatus = types.SubscriptionStatusIncomplete
		candidate.TrialStart, candidate.TrialEnd = &start, &trialEnd
		candidate.CurrentPeriodStart = trialEnd
		candidate.CurrentPeriodEnd = trialEnd.AddDate(0, 1, 0)
		require.NoError(t, s.params.SubRepo.Create(ctx, &candidate))
		last = &candidate
	}
	source := &invoice.Invoice{
		ID: "inv_recovered", SubscriptionID: &last.ID, CustomerID: last.GetInvoicingCustomerID(),
		InvoiceType: types.InvoiceTypeSubscription, InvoiceStatus: types.InvoiceStatusFinalized,
		BillingReason: string(types.InvoiceBillingReasonSubscriptionTrialEnd), PaymentStatus: types.PaymentStatusSucceeded,
		Currency: last.Currency, AmountDue: decimal.NewFromInt(100), AmountPaid: decimal.NewFromInt(100),
		Subtotal: decimal.NewFromInt(100), Total: decimal.NewFromInt(100),
		PeriodStart: &last.CurrentPeriodStart, PeriodEnd: &last.CurrentPeriodEnd,
		FinalizedAt: &trialEnd, LastComputedAt: &trialEnd, IdempotencyKey: lo.ToPtr("inv_recovered"),
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	require.NoError(t, s.params.InvoiceRepo.Create(ctx, source))
	svc := s.service.(*subscriptionService)
	traversal := &trialRecoveryTraversal{Repository: s.params.SubRepo}
	svc.SubRepo = traversal
	result, err := svc.ProcessTrialEndDue(ctx)
	require.NoError(t, err)
	require.Zero(t, result.TotalFailed)
	require.Equal(t, 101, result.TotalSuccess)
	require.Equal(t, []string{"", "recovery_099"}, traversal.boundaries)
	stored, err := s.params.SubRepo.Get(ctx, last.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusActive, stored.SubscriptionStatus)
	unchanged, err := s.params.SubRepo.Get(ctx, "recovery_000")
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusIncomplete, unchanged.SubscriptionStatus)

	traversal.boundaries = nil
	result, err = svc.ProcessTrialEndDue(ctx)
	require.NoError(t, err)
	require.Zero(t, result.TotalFailed)
	require.Equal(t, 100, result.TotalSuccess)
	require.Equal(t, []string{"", "recovery_099"}, traversal.boundaries)
}
