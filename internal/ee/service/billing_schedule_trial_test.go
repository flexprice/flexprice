package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
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
	for _, ctx := range []context.Context{f.ctx, transitionCtx, revalidatePendingCollection(transitionCtx, inv)} {
		owner, err := f.adapter.Snapshot(ctx, f.owner)
		require.NoError(t, err)
		require.False(t, owner.CanCollect)
		require.Equal(t, "subscription_pending", owner.DeferredReason)
	}
	boundCtx := bindPendingCollection(transitionCtx, inv)
	for i := 0; i < 2; i++ {
		owner, err := f.adapter.Snapshot(boundCtx, f.owner)
		require.NoError(t, err)
		require.True(t, owner.CanCollect)
		require.Empty(t, owner.DeferredReason)
	}
	for _, cleared := range []context.Context{withoutPendingCollection(transitionCtx), withoutPendingCollection(boundCtx)} {
		owner, err := f.adapter.Snapshot(bindPendingCollection(cleared, inv), f.owner)
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
					ctx = bindPendingCollection(ctx, inv)
				}
				tt.change(inv)
				if phase == "draft binding" {
					ctx = bindPendingCollection(ctx, inv)
				} else {
					ctx = revalidatePendingCollection(ctx, inv)
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
			ctx = bindPendingCollection(ctx, inv)
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
	boundCtx := bindPendingCollection(ctx, inv)
	inv.ID = "another_invoice"
	owner, err := f.adapter.Snapshot(bindPendingCollection(boundCtx, inv), f.owner)
	require.NoError(t, err)
	require.False(t, owner.CanCollect)

	for _, changed := range []context.Context{
		types.SetTenantID(boundCtx, "other"),
		types.SetEnvironmentID(boundCtx, "other"),
	} {
		require.False(t, pendingCollectionAllowed(changed, f.sub, types.BillingTermSubscriptionInvoice))
	}
}

func TestTrialEndCollection_FutureTrialAndDetachedDates(t *testing.T) {
	f, inv, _ := trialEndCollectionFixture(t)
	ctx := withTrialEndCollection(f.ctx, f.sub, f.sub.TrialEnd.Add(-time.Microsecond))
	require.False(t, pendingCollectionAllowed(bindPendingCollection(ctx, inv), f.sub, types.BillingTermSubscriptionInvoice))
	ctx = withTrialEndCollection(f.ctx, f.sub, *f.sub.TrialEnd)
	originalStart := f.sub.CurrentPeriodStart
	*f.sub.TrialEnd = f.sub.TrialEnd.Add(time.Hour)
	require.False(t, pendingCollectionAllowed(bindPendingCollection(ctx, inv), f.sub, types.BillingTermSubscriptionInvoice))
	*f.sub.TrialEnd = originalStart
	require.True(t, pendingCollectionAllowed(bindPendingCollection(ctx, inv), f.sub, types.BillingTermSubscriptionInvoice))
}

type trialEndInvoiceProbe struct {
	InvoiceService
	create func(context.Context, *dto.CreateSubscriptionInvoiceRequest) (*dto.InvoiceResponse, *subscription.Subscription, error)
}

func (p *trialEndInvoiceProbe) CreateSubscriptionInvoice(ctx context.Context, req *dto.CreateSubscriptionInvoiceRequest, _ *dto.PaymentParameters, _ types.InvoiceFlowType, _ bool) (*dto.InvoiceResponse, *subscription.Subscription, error) {
	return p.create(ctx, req)
}

func (s *SubscriptionTrialProrationSuite) TestTrialEndCollectionAuthorization() {
	f, inv, _ := trialEndCollectionFixture(s.T())
	ctx := types.SetEnvironmentID(s.GetContext(), "env_trial")
	f.sub.TenantID, f.sub.EnvironmentID = types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	f.sub.SubscriptionStatus = types.SubscriptionStatusTrialing
	s.Require().NoError(s.params.SubRepo.Create(ctx, f.sub))
	called := false
	probe := &trialEndInvoiceProbe{create: func(invoiceCtx context.Context, req *dto.CreateSubscriptionInvoiceRequest) (*dto.InvoiceResponse, *subscription.Subscription, error) {
		called = true
		fresh, err := s.params.SubRepo.Get(invoiceCtx, f.sub.ID)
		s.Require().NoError(err)
		s.Equal(types.SubscriptionStatusIncomplete, fresh.SubscriptionStatus)
		s.False(pendingCollectionAllowed(invoiceCtx, fresh, types.BillingTermSubscriptionInvoice))
		inv.TenantID, inv.EnvironmentID = fresh.TenantID, fresh.EnvironmentID
		inv.PeriodStart, inv.PeriodEnd = &req.PeriodStart, &req.PeriodEnd
		boundCtx := bindPendingCollection(invoiceCtx, inv)
		s.True(pendingCollectionAllowed(boundCtx, fresh, types.BillingTermSubscriptionInvoice))
		s.False(pendingCollectionAllowed(boundCtx, fresh, types.BillingTermSeparateInvoice))
		return dto.NewInvoiceResponse(inv), fresh, nil
	}}
	_, err := s.subSvc.(*subscriptionService).processSubscriptionTrialEnd(ctx, f.sub, probe, *f.sub.TrialEnd)
	s.Require().NoError(err)
	s.True(called)
	s.False(pendingCollectionAllowed(ctx, f.sub, types.BillingTermSubscriptionInvoice))
}
