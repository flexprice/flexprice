package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestTrialPaymentActivationUsesOuterCommit(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		collection types.CollectionMethod
		behavior   types.PaymentBehavior
		flow       types.InvoiceFlowType
		unpaid     bool
	}{
		{name: "send zero gated", collection: types.CollectionMethodSendInvoice, behavior: types.PaymentBehaviorDefaultIncomplete, flow: types.InvoiceFlowRenewal},
		{name: "send unpaid default active", collection: types.CollectionMethodSendInvoice, behavior: types.PaymentBehaviorDefaultActive, flow: types.InvoiceFlowRenewal, unpaid: true},
		{name: "automatic zero allow incomplete", collection: types.CollectionMethodChargeAutomatically, behavior: types.PaymentBehaviorAllowIncomplete, flow: types.InvoiceFlowRenewal},
		{name: "automatic zero error if incomplete", collection: types.CollectionMethodChargeAutomatically, behavior: types.PaymentBehaviorErrorIfIncomplete, flow: types.InvoiceFlowSubscriptionCreation},
		{name: "automatic unpaid default active", collection: types.CollectionMethodChargeAutomatically, behavior: types.PaymentBehaviorDefaultActive, flow: types.InvoiceFlowRenewal, unpaid: true},
		{name: "manual zero", collection: types.CollectionMethodSendInvoice, behavior: types.PaymentBehaviorDefaultIncomplete, flow: types.InvoiceFlowManual},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s, svc, parent, source, _ := newTrialRecoveryFixture(t)
			setTrialRecoveryPastPeriod(t, s, parent, source)
			parent.SubscriptionType = types.SubscriptionTypeParent
			parent.CollectionMethod, parent.PaymentBehavior = string(scenario.collection), string(scenario.behavior)
			child := *parent
			child.ID, child.SubscriptionType, child.ParentSubscriptionID = "trial_payment_child", types.SubscriptionTypeInherited, &parent.ID
			require.NoError(t, svc.SubRepo.Create(s.GetContext(), &child))
			source.PaymentStatus = types.PaymentStatusPending
			source.AmountPaid = decimal.Zero
			if scenario.unpaid {
				source.AmountRemaining = source.AmountDue
			} else {
				source.AmountDue, source.AmountRemaining, source.Subtotal, source.Total = decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero
			}
			require.NoError(t, svc.InvoiceRepo.Update(s.GetContext(), source))
			ctx := types.WithPostCommitHooks(context.WithValue(s.GetContext(), types.CtxDBTransaction, &ent.Tx{}))
			processor := NewSubscriptionPaymentProcessor(&svc.ServiceParams)
			require.NoError(t, processor.HandlePaymentBehavior(ctx, parent, dto.NewInvoiceResponse(source), scenario.behavior, scenario.flow))
			for _, id := range []string{parent.ID, child.ID} {
				stored, err := svc.SubRepo.Get(s.GetContext(), id)
				require.NoError(t, err)
				require.Equal(t, types.SubscriptionStatusIncomplete, stored.SubscriptionStatus, "activation waits for the invoice transaction")
			}
			require.Equal(t, types.SubscriptionStatusIncomplete, parent.SubscriptionStatus, "the caller snapshot must also remain unchanged")
			require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
			types.RunPostCommitHooks(ctx)
			for _, id := range []string{parent.ID, child.ID} {
				stored, err := svc.SubRepo.Get(s.GetContext(), id)
				require.NoError(t, err)
				require.Equal(t, types.SubscriptionStatusActive, stored.SubscriptionStatus)
			}
			require.Len(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events(), 2)
		})
	}
}

func TestTrialPaymentActivationRejectsInvalidPolicyOrLifecycle(t *testing.T) {
	for _, scenario := range []string{"override only", "uncomputed default active", "unknown collection", "paused", "cancelled", "failed gated"} {
		t.Run(scenario, func(t *testing.T) {
			s, svc, sub, source, _ := newTrialRecoveryFixture(t)
			setTrialRecoveryPastPeriod(t, s, sub, source)
			sub.CollectionMethod, sub.PaymentBehavior = string(types.CollectionMethodSendInvoice), string(types.PaymentBehaviorDefaultActive)
			source.PaymentStatus, source.AmountPaid, source.AmountRemaining = types.PaymentStatusPending, decimal.Zero, source.AmountDue
			behavior := types.PaymentBehaviorDefaultActive
			switch scenario {
			case "override only":
				sub.PaymentBehavior = string(types.PaymentBehaviorAllowIncomplete)
			case "uncomputed default active":
				source.LastComputedAt = nil
			case "unknown collection":
				sub.CollectionMethod = "unknown"
			case "paused":
				sub.SubscriptionStatus, sub.PauseStatus = types.SubscriptionStatusPaused, types.PauseStatusActive
			case "cancelled":
				sub.SubscriptionStatus = types.SubscriptionStatusCancelled
			case "failed gated":
				sub.CollectionMethod, sub.PaymentBehavior = string(types.CollectionMethodChargeAutomatically), string(types.PaymentBehaviorAllowIncomplete)
				behavior = types.PaymentBehaviorAllowIncomplete
			}
			require.NoError(t, svc.InvoiceRepo.Update(s.GetContext(), source))
			before := sub.SubscriptionStatus
			processor := NewSubscriptionPaymentProcessor(&svc.ServiceParams)
			err := processor.HandlePaymentBehavior(s.GetContext(), sub, dto.NewInvoiceResponse(source), behavior, types.InvoiceFlowRenewal)
			if scenario == "unknown collection" {
				require.Error(t, err)
				changed, err := svc.reconcileTrialActivation(s.GetContext(), sub.ID, time.Now().UTC())
				require.NoError(t, err)
				require.False(t, changed, "durable recovery must also reject unknown collection policy")
			} else {
				require.NoError(t, err)
			}
			stored, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
			require.NoError(t, err)
			require.Equal(t, before, stored.SubscriptionStatus)
			require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
		})
	}
}

func TestTrialPaymentOtherInvoiceReasonsUnchanged(t *testing.T) {
	s, svc, sub, source, _ := newTrialRecoveryFixture(t)
	sub.CollectionMethod, sub.PaymentBehavior = string(types.CollectionMethodSendInvoice), string(types.PaymentBehaviorDefaultActive)
	source.BillingReason = string(types.InvoiceBillingReasonSubscriptionCycle)
	ctx := types.WithPostCommitHooks(context.WithValue(s.GetContext(), types.CtxDBTransaction, &ent.Tx{}))
	processor := NewSubscriptionPaymentProcessor(&svc.ServiceParams)
	require.NoError(t, processor.HandlePaymentBehavior(ctx, sub, dto.NewInvoiceResponse(source), types.PaymentBehaviorDefaultActive, types.InvoiceFlowRenewal))
	stored, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusActive, stored.SubscriptionStatus)
}

func TestTrialRecoveryRechecksCancellationAfterEligibility(t *testing.T) {
	s, svc, sub, source, _ := newTrialRecoveryFixture(t)
	setTrialRecoveryPastPeriod(t, s, sub, source)
	observation := sub.CurrentPeriodStart.Add(time.Hour)
	sub.EndDate = lo.ToPtr(observation.Add(time.Hour))
	called := false
	svc.BillingTermsStore = &trialRecoveryStore{hook: func() { called = true }}
	changed, err := svc.reconcileTrialActivation(s.GetContext(), sub.ID, observation)
	require.NoError(t, err)
	require.True(t, called, "the supplied observation was eligible before acquiring the guard")
	require.False(t, changed, "the final lifecycle check must use current time after the guard")
	stored, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusIncomplete, stored.SubscriptionStatus)
	require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
}
