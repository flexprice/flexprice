package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func trialEndCollectionFixture(t *testing.T) (*commitmentTermsFixture, *invoice.Invoice, context.Context) {
	t.Helper()
	f := newCommitmentTermsFixture(t)
	f.sub.SubscriptionStatus = types.SubscriptionStatusIncomplete
	f.sub.TrialStart = lo.ToPtr(f.sub.StartDate)
	f.sub.TrialEnd = lo.ToPtr(f.commitment.StartDate)
	f.sub.CurrentPeriodStart = *f.sub.TrialEnd
	f.sub.CurrentPeriodEnd = f.sub.CurrentPeriodStart.AddDate(0, 1, 0)
	inv := &invoice.Invoice{
		ID: "inv_trial_end", CustomerID: f.sub.GetInvoicingCustomerID(),
		SubscriptionID: lo.ToPtr(f.sub.ID), Currency: f.sub.Currency,
		InvoiceType: types.InvoiceTypeSubscription, InvoiceStatus: types.InvoiceStatusDraft,
		BillingReason: string(types.InvoiceBillingReasonSubscriptionTrialEnd),
		PeriodStart:   lo.ToPtr(f.sub.CurrentPeriodStart), PeriodEnd: lo.ToPtr(f.sub.CurrentPeriodEnd),
		EnvironmentID: f.sub.EnvironmentID, BaseModel: types.GetDefaultBaseModel(f.ctx),
	}
	ctx := withTrialEndCollection(f.ctx, f.sub, *f.sub.TrialEnd)
	return f, inv, ctx
}

func TestTrialEndCollection_RequiresBoundInvoice(t *testing.T) {
	f, inv, transitionCtx := trialEndCollectionFixture(t)
	for _, ctx := range []context.Context{f.ctx, transitionCtx, revalidateTrialEndCollection(transitionCtx, inv)} {
		owner, err := f.adapter.Snapshot(ctx, f.owner)
		require.NoError(t, err)
		require.False(t, owner.CanCollect)
		require.Equal(t, "subscription_pending", owner.DeferredReason)
	}
	boundCtx := bindTrialEndCollection(transitionCtx, inv)
	for i := 0; i < 2; i++ {
		owner, err := f.adapter.Snapshot(boundCtx, f.owner)
		require.NoError(t, err)
		require.True(t, owner.CanCollect)
		require.Empty(t, owner.DeferredReason)
	}
	for _, cleared := range []context.Context{withoutTrialEndCollection(transitionCtx), withoutTrialEndCollection(boundCtx)} {
		owner, err := f.adapter.Snapshot(bindTrialEndCollection(cleared, inv), f.owner)
		require.NoError(t, err)
		require.False(t, owner.CanCollect, "later invoice processing cannot restore the authorization")
	}
	ordinary, err := f.adapter.Snapshot(f.ctx, f.owner)
	require.NoError(t, err)
	require.False(t, ordinary.CanCollect, "authorization must not mutate or activate the subscription")
	require.Equal(t, types.SubscriptionStatusIncomplete, f.sub.SubscriptionStatus)
}

func TestTrialEndCollection_InvoiceBoundary(t *testing.T) {
	tests := []struct {
		name   string
		change func(*invoice.Invoice)
	}{
		{"missing id", func(inv *invoice.Invoice) { inv.ID = "" }},
		{"tenant", func(inv *invoice.Invoice) { inv.TenantID = "other" }},
		{"environment", func(inv *invoice.Invoice) { inv.EnvironmentID = "other" }},
		{"subscription", func(inv *invoice.Invoice) { inv.SubscriptionID = lo.ToPtr("other") }},
		{"missing subscription", func(inv *invoice.Invoice) { inv.SubscriptionID = nil }},
		{"recipient", func(inv *invoice.Invoice) { inv.CustomerID = "other" }},
		{"currency", func(inv *invoice.Invoice) { inv.Currency = "eur" }},
		{"trial preview", func(inv *invoice.Invoice) {
			inv.BillingReason = string(types.InvoiceBillingReasonSubscriptionTrialStart)
		}},
		{"cycle", func(inv *invoice.Invoice) { inv.BillingReason = string(types.InvoiceBillingReasonSubscriptionCycle) }},
		{"paused", func(inv *invoice.Invoice) { inv.BillingReason = string(types.InvoiceBillingReasonSubscriptionPaused) }},
		{"separate invoice", func(inv *invoice.Invoice) { inv.InvoiceType = types.InvoiceTypeOneOff }},
		{"skipped", func(inv *invoice.Invoice) { inv.InvoiceStatus = types.InvoiceStatusSkipped }},
		{"finalized", func(inv *invoice.Invoice) { inv.InvoiceStatus = types.InvoiceStatusFinalized }},
		{"voided", func(inv *invoice.Invoice) { inv.InvoiceStatus = types.InvoiceStatusVoided }},
		{"archived", func(inv *invoice.Invoice) { inv.Status = types.StatusArchived }},
		{"missing start", func(inv *invoice.Invoice) { inv.PeriodStart = nil }},
		{"missing end", func(inv *invoice.Invoice) { inv.PeriodEnd = nil }},
		{"wrong start", func(inv *invoice.Invoice) { inv.PeriodStart = lo.ToPtr(inv.PeriodStart.Add(time.Second)) }},
		{"wrong end", func(inv *invoice.Invoice) { inv.PeriodEnd = lo.ToPtr(inv.PeriodEnd.Add(time.Second)) }},
	}
	for _, tt := range tests {
		for _, phase := range []string{"draft binding", "locked invoice"} {
			t.Run(tt.name+"/"+phase, func(t *testing.T) {
				f, inv, ctx := trialEndCollectionFixture(t)
				if phase == "locked invoice" {
					ctx = bindTrialEndCollection(ctx, inv)
				}
				tt.change(inv)
				if phase == "draft binding" {
					ctx = bindTrialEndCollection(ctx, inv)
				} else {
					ctx = revalidateTrialEndCollection(ctx, inv)
				}
				owner, err := f.adapter.Snapshot(ctx, f.owner)
				require.NoError(t, err)
				require.False(t, owner.CanCollect)
			})
		}
	}
}

func TestTrialEndCollection_FreshOwnerState(t *testing.T) {
	tests := []struct {
		name      string
		change    func(*commitmentTermsFixture)
		wantError bool
	}{
		{"invalid protected settlement", func(f *commitmentTermsFixture) { f.commitment.Metadata = types.Metadata{commitmentSettlementKey: "{}"} }, true},
		{"pause", func(f *commitmentTermsFixture) { f.sub.PauseStatus = types.PauseStatusActive }, false},
		{"paused status", func(f *commitmentTermsFixture) { f.sub.SubscriptionStatus = types.SubscriptionStatusPaused }, false},
		{"trialing", func(f *commitmentTermsFixture) { f.sub.SubscriptionStatus = types.SubscriptionStatusTrialing }, false},
		{"draft", func(f *commitmentTermsFixture) { f.sub.SubscriptionStatus = types.SubscriptionStatusDraft }, false},
		{"cancelled", func(f *commitmentTermsFixture) { f.sub.SubscriptionStatus = types.SubscriptionStatusCancelled }, false},
		{"cancellation recorded", func(f *commitmentTermsFixture) { f.sub.CancelledAt = f.sub.TrialEnd }, false},
		{"cancellation due", func(f *commitmentTermsFixture) { f.sub.CancelAt = f.sub.TrialEnd }, false},
		{"terminated owner", func(f *commitmentTermsFixture) { f.commitment.TerminatedAt = f.sub.TrialEnd }, false},
		{"separate delivery", func(f *commitmentTermsFixture) {
			f.commitment.InstalmentInvoicing = lo.ToPtr(string(types.BillingTermSeparateInvoice))
		}, false},
		{"recipient changed", func(f *commitmentTermsFixture) { f.sub.InvoicingCustomerID = lo.ToPtr("new_customer") }, false},
		{"trial end changed", func(f *commitmentTermsFixture) { f.sub.TrialEnd = lo.ToPtr(f.sub.TrialEnd.Add(time.Second)) }, false},
		{"no trial", func(f *commitmentTermsFixture) { f.sub.TrialStart = nil; f.sub.TrialEnd = nil }, false},
		{"later period", func(f *commitmentTermsFixture) { f.sub.CurrentPeriodStart = f.sub.CurrentPeriodEnd }, false},
		{"period end changed", func(f *commitmentTermsFixture) { f.sub.CurrentPeriodEnd = f.sub.CurrentPeriodEnd.Add(time.Second) }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, inv, ctx := trialEndCollectionFixture(t)
			ctx = bindTrialEndCollection(ctx, inv)
			tt.change(f)
			owner, err := f.adapter.Snapshot(ctx, f.owner)
			if tt.wantError {
				require.Error(t, err)
				require.Nil(t, owner)
				return
			}
			require.NoError(t, err)
			require.False(t, owner.CanCollect)
		})
	}
}

func TestTrialEndCollection_RejectsRebindingAndScopeChanges(t *testing.T) {
	f, inv, ctx := trialEndCollectionFixture(t)
	boundCtx := bindTrialEndCollection(ctx, inv)
	inv.ID = "another_invoice"
	owner, err := f.adapter.Snapshot(bindTrialEndCollection(boundCtx, inv), f.owner)
	require.NoError(t, err)
	require.False(t, owner.CanCollect)

	for _, changed := range []context.Context{
		types.SetTenantID(boundCtx, "other"),
		types.SetEnvironmentID(boundCtx, "other"),
	} {
		require.False(t, trialEndCollectionAllowed(changed, f.sub, types.BillingTermSubscriptionInvoice))
	}
}

func TestTrialEndCollection_FutureTrialAndDetachedDates(t *testing.T) {
	f, inv, _ := trialEndCollectionFixture(t)
	ctx := withTrialEndCollection(f.ctx, f.sub, f.sub.TrialEnd.Add(-time.Microsecond))
	require.False(t, trialEndCollectionAllowed(bindTrialEndCollection(ctx, inv), f.sub, types.BillingTermSubscriptionInvoice))
	ctx = withTrialEndCollection(f.ctx, f.sub, *f.sub.TrialEnd)
	originalStart := f.sub.CurrentPeriodStart
	*f.sub.TrialEnd = f.sub.TrialEnd.Add(time.Hour)
	require.False(t, trialEndCollectionAllowed(bindTrialEndCollection(ctx, inv), f.sub, types.BillingTermSubscriptionInvoice))
	*f.sub.TrialEnd = originalStart
	require.True(t, trialEndCollectionAllowed(bindTrialEndCollection(ctx, inv), f.sub, types.BillingTermSubscriptionInvoice))
}
