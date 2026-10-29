package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func newPaymentLifecycleFixture(t *testing.T) (*SubscriptionPauseSuite, *subscriptionService, *subscription.Subscription, *invoice.Invoice, time.Time) {
	t.Helper()
	s, svc, sub, source, asOf := newTrialRecoveryFixture(t)
	sub.TrialStart = nil
	sub.TrialEnd = nil
	sub.PaymentBehavior = string(types.PaymentBehaviorAllowIncomplete)
	sub.CollectionMethod = string(types.CollectionMethodChargeAutomatically)
	source.BillingReason = string(types.InvoiceBillingReasonSubscriptionCycle)
	require.NoError(t, svc.InvoiceRepo.Update(s.GetContext(), source))
	return s, svc, sub, source, asOf
}

func TestPaymentLifecycle_ReconcilesPaidAndRejectsStaleSources(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*subscription.Subscription, *invoice.Invoice)
		want   types.SubscriptionStatus
	}{
		{name: "paid", want: types.SubscriptionStatusActive},
		{name: "paused", change: func(s *subscription.Subscription, _ *invoice.Invoice) {
			s.SubscriptionStatus = types.SubscriptionStatusPaused
		}, want: types.SubscriptionStatusPaused},
		{name: "cancelled", change: func(s *subscription.Subscription, _ *invoice.Invoice) {
			s.SubscriptionStatus = types.SubscriptionStatusCancelled
		}, want: types.SubscriptionStatusCancelled},
		{name: "active pause", change: func(s *subscription.Subscription, _ *invoice.Invoice) { s.PauseStatus = types.PauseStatusActive }, want: types.SubscriptionStatusIncomplete},
		{name: "stale period", change: func(s *subscription.Subscription, i *invoice.Invoice) {
			i.PeriodStart = lo.ToPtr(s.CurrentPeriodStart.AddDate(0, -2, 0))
			i.PeriodEnd = lo.ToPtr(s.CurrentPeriodStart.AddDate(0, -1, 0))
		}, want: types.SubscriptionStatusIncomplete},
		{name: "voided", change: func(_ *subscription.Subscription, i *invoice.Invoice) { i.InvoiceStatus = types.InvoiceStatusVoided }, want: types.SubscriptionStatusIncomplete},
		{name: "refunded", change: func(_ *subscription.Subscription, i *invoice.Invoice) { i.PaymentStatus = types.PaymentStatusRefunded }, want: types.SubscriptionStatusIncomplete},
		{name: "trial ending separately", change: func(s *subscription.Subscription, _ *invoice.Invoice) { s.TrialEnd = lo.ToPtr(s.CurrentPeriodStart) }, want: types.SubscriptionStatusIncomplete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, svc, sub, source, asOf := newPaymentLifecycleFixture(t)
			if tc.change != nil {
				tc.change(sub, source)
			}
			require.NoError(t, svc.InvoiceRepo.Update(s.GetContext(), source))
			require.NoError(t, svc.reconcilePaymentLifecycle(s.GetContext(), paymentLifecycleRequest{subscriptionID: sub.ID, invoiceID: source.ID, asOf: asOf}))
			fresh, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
			require.NoError(t, err)
			require.Equal(t, tc.want, fresh.SubscriptionStatus)
		})
	}
}

func TestPaymentLifecycle_GuardThenFreshRows(t *testing.T) {
	s, svc, sub, source, asOf := newPaymentLifecycleFixture(t)
	order := []string{}
	svc.BillingTermsStore = &trialRecoveryStore{order: &order}
	svc.SubRepo = &trialRecoverySubscriptions{Repository: svc.SubRepo, order: &order}
	svc.InvoiceRepo = &trialRecoveryInvoices{Repository: svc.InvoiceRepo, order: &order}
	require.NoError(t, svc.reconcilePaymentLifecycle(s.GetContext(), paymentLifecycleRequest{subscriptionID: sub.ID, invoiceID: source.ID, asOf: asOf}))
	require.Equal(t, []string{"eligibility", "subscription:" + sub.ID, "invoice:" + source.ID}, order)
}

func TestPaymentLifecycle_RechecksPauseAndPaymentAfterDiscovery(t *testing.T) {
	for _, change := range []string{"pause", "unpaid"} {
		t.Run(change, func(t *testing.T) {
			s, svc, sub, source, asOf := newPaymentLifecycleFixture(t)
			if change == "pause" {
				svc.BillingTermsStore = &trialRecoveryStore{hook: func() { sub.SubscriptionStatus = types.SubscriptionStatusPaused }}
			} else {
				svc.InvoiceRepo = &trialRecoveryInvoices{Repository: svc.InvoiceRepo, beforeLock: func(string) {
					source.PaymentStatus = types.PaymentStatusPending
					source.AmountPaid = decimal.Zero
					source.AmountRemaining = source.AmountDue
					require.NoError(t, s.params.InvoiceRepo.Update(s.GetContext(), source))
				}}
			}
			require.NoError(t, svc.reconcilePaymentLifecycle(s.GetContext(), paymentLifecycleRequest{subscriptionID: sub.ID, invoiceID: source.ID, asOf: asOf}))
			fresh, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
			require.NoError(t, err)
			require.NotEqual(t, types.SubscriptionStatusActive, fresh.SubscriptionStatus)
		})
	}
}

func TestPaymentLifecycle_CallbackWaitsForOuterCommit(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			s, svc, sub, source, _ := newPaymentLifecycleFixture(t)
			tx := types.WithTxStore(types.WithPostCommitHooks(context.WithValue(s.GetContext(), types.CtxDBTransaction, &ent.Tx{})))
			require.NoError(t, svc.HandleSubscriptionActivatingInvoicePaid(tx, source))
			fresh, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
			require.NoError(t, err)
			require.Equal(t, types.SubscriptionStatusIncomplete, fresh.SubscriptionStatus)
			if commit {
				types.RunPostCommitHooks(tx)
			} else {
				types.DiscardPostCommitHooks(tx)
			}
			fresh, err = svc.SubRepo.Get(s.GetContext(), sub.ID)
			require.NoError(t, err)
			if commit {
				require.Equal(t, types.SubscriptionStatusActive, fresh.SubscriptionStatus)
			} else {
				require.Equal(t, types.SubscriptionStatusIncomplete, fresh.SubscriptionStatus)
			}
		})
	}
}

func TestPaymentLifecycle_RecoveryWithoutPaymentCallback(t *testing.T) {
	s, svc, sub, _, _ := newPaymentLifecycleFixture(t)
	require.NoError(t, svc.ProcessOverdueSubscriptionInvoices(s.GetContext()))
	fresh, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusActive, fresh.SubscriptionStatus)
	events := len(s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
	require.NoError(t, svc.ProcessOverdueSubscriptionInvoices(s.GetContext()))
	require.Len(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events(), events)
}

func TestPaymentLifecycle_AnotherUnpaidInvoiceBlocksActivation(t *testing.T) {
	s, svc, sub, source, asOf := newPaymentLifecycleFixture(t)
	unpaid := *source
	unpaid.ID = "invoice_unpaid"
	unpaid.PaymentStatus = types.PaymentStatusPending
	unpaid.AmountPaid = decimal.Zero
	unpaid.AmountRemaining = unpaid.AmountDue
	require.NoError(t, svc.InvoiceRepo.Create(s.GetContext(), &unpaid))
	require.NoError(t, svc.reconcilePaymentLifecycle(s.GetContext(), paymentLifecycleRequest{subscriptionID: sub.ID, invoiceID: source.ID, asOf: asOf}))
	fresh, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusIncomplete, fresh.SubscriptionStatus)
}

func TestPaymentLifecycle_LockConflictHasNoStatusSideEffects(t *testing.T) {
	s, svc, sub, source, asOf := newPaymentLifecycleFixture(t)
	svc.InvoiceRepo = &trialRecoveryInvoices{Repository: svc.InvoiceRepo, lockErr: errors.New("invoice busy")}
	require.ErrorContains(t, svc.reconcilePaymentLifecycle(s.GetContext(), paymentLifecycleRequest{subscriptionID: sub.ID, invoiceID: source.ID, asOf: asOf}), "invoice busy")
	fresh, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusIncomplete, fresh.SubscriptionStatus)
	require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
}

func TestPaymentLifecycle_StoredDefaultActiveAndProcessingPayments(t *testing.T) {
	for _, tc := range []struct {
		name     string
		behavior types.PaymentBehavior
		payment  types.PaymentStatus
		want     types.SubscriptionStatus
	}{
		{"default active pending", types.PaymentBehaviorDefaultActive, types.PaymentStatusPending, types.SubscriptionStatusActive},
		{"default active processing", types.PaymentBehaviorDefaultActive, types.PaymentStatusProcessing, types.SubscriptionStatusActive},
		{"gated pending", types.PaymentBehaviorAllowIncomplete, types.PaymentStatusPending, types.SubscriptionStatusIncomplete},
		{"gated processing", types.PaymentBehaviorAllowIncomplete, types.PaymentStatusProcessing, types.SubscriptionStatusIncomplete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, svc, sub, source, asOf := newPaymentLifecycleFixture(t)
			sub.PaymentBehavior = string(tc.behavior)
			source.PaymentStatus = tc.payment
			source.AmountPaid = decimal.Zero
			source.AmountRemaining = source.AmountDue
			require.NoError(t, svc.InvoiceRepo.Update(s.GetContext(), source))
			require.NoError(t, svc.reconcilePaymentLifecycle(s.GetContext(), paymentLifecycleRequest{subscriptionID: sub.ID, invoiceID: source.ID, asOf: asOf}))
			fresh, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
			require.NoError(t, err)
			require.Equal(t, tc.want, fresh.SubscriptionStatus)
		})
	}
}
