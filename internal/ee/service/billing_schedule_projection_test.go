package service

import (
	"context"
	"testing"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type billingProjectionEngine struct {
	core.Service
	state core.State
}

func (s *billingProjectionEngine) Get(context.Context, domain.OwnerRef) (core.State, error) {
	return s.state, nil
}

func TestBillingScheduleProjection_EligibilityAndArchivedSources(t *testing.T) {
	for _, tc := range []struct {
		name     string
		collect  bool
		gap      bool
		future   bool
		archived bool
		assigned bool
		wantDue  int
	}{
		{name: "eligible", collect: true, wantDue: 1},
		{name: "future", collect: true, future: true},
		{name: "paused"},
		{name: "trial"},
		{name: "gap", collect: true, gap: true},
		{name: "archived unassigned", collect: true, archived: true},
		{name: "archived assigned", archived: true, assigned: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCommitmentTermsFixture(t)
			now := time.Now().UTC()
			owner := &domain.OwnerSnapshot{Owner: f.owner, TenantID: types.GetTenantID(f.ctx), EnvironmentID: types.GetEnvironmentID(f.ctx), CustomerID: f.sub.GetInvoicingCustomerID(), Currency: "usd", CanCollect: tc.collect}
			item := &domain.ScheduleItem{ID: "schedule", OwnerID: f.owner.ID, OwnerType: f.owner.Type, CustomerID: owner.CustomerID, Currency: owner.Currency, Amount: decimal.NewFromInt(100), BillOn: lo.ToPtr(now.Add(-time.Hour)),
				ScheduleStatus: types.BillingScheduleScheduled, EnvironmentID: owner.EnvironmentID, BaseModel: types.GetDefaultBaseModel(f.ctx)}
			if tc.future {
				item.BillOn = lo.ToPtr(now.Add(time.Hour))
			}
			if tc.archived {
				item.Status = types.StatusArchived
			}
			if tc.assigned {
				item.ScheduleStatus = types.BillingScheduleInvoiced
			}
			params := ServiceParams{BillingTerms: &billingProjectionEngine{state: &billingAPIState{owner: owner, items: []*domain.ScheduleItem{item}, summary: domain.CollectionSummary{RequiresReplan: tc.gap}}}}
			projection, err := projectBillingSchedule(f.ctx, params, []domain.OwnerRef{f.owner}, owner.CustomerID, owner.Currency, now)
			require.NoError(t, err)
			require.Len(t, projection.due, tc.wantDue)
			require.Len(t, projection.assigned, lo.Ternary(tc.assigned, 1, 0))
		})
	}
}

func TestWalletBillingSchedule_CurrentDraftCreditsCountOnce(t *testing.T) {
	s, svc, sub, source, asOf := newPaymentLifecycleFixture(t)
	ctx := s.GetContext()
	owner := &commitment.Commitment{ID: "commitment_wallet", SubscriptionID: sub.ID, CustomerID: sub.CustomerID, Currency: sub.Currency, Binding: true,
		CollectionMethod: lo.ToPtr(types.CommitmentCollectionBillingTerms), EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}
	_, err := svc.CommitmentRepo.Create(ctx, owner)
	require.NoError(t, err)
	item := &domain.ScheduleItem{ID: "schedule_wallet", OwnerID: owner.ID, OwnerType: types.BillingTermOwnerCommitment, CustomerID: sub.CustomerID, Currency: sub.Currency,
		Amount: decimal.NewFromInt(100), ScheduleStatus: types.BillingScheduleInvoiced, InvoiceID: &source.ID, InvoiceLineItemID: lo.ToPtr("source_line"), EnvironmentID: sub.EnvironmentID, BaseModel: types.GetDefaultBaseModel(ctx)}
	source.InvoiceStatus = types.InvoiceStatusDraft
	source.PaymentStatus = types.PaymentStatusPending
	source.AmountDue, source.AmountRemaining, source.AmountPaid = decimal.NewFromInt(150), decimal.NewFromInt(80), decimal.NewFromInt(70)
	source.TotalPrepaidCreditsApplied = decimal.NewFromInt(70)
	source.LastComputedAt = &asOf
	source.LineItems = []*invoice.InvoiceLineItem{{ID: *item.InvoiceLineItemID, InvoiceID: source.ID, CustomerID: sub.CustomerID, Currency: sub.Currency, BillingScheduleItemID: &item.ID,
		Amount: item.Amount, Quantity: decimal.NewFromInt(1), PeriodStart: &sub.CurrentPeriodStart, PeriodEnd: &sub.CurrentPeriodEnd, EnvironmentID: sub.EnvironmentID, BaseModel: types.GetDefaultBaseModel(ctx)}}
	require.NoError(t, svc.InvoiceRepo.Update(ctx, source))
	svc.BillingTerms = &billingProjectionEngine{state: &billingAPIState{owner: &domain.OwnerSnapshot{Owner: commitmentBillingOwner(owner.ID), CustomerID: sub.CustomerID, Currency: sub.Currency, TenantID: sub.TenantID, EnvironmentID: sub.EnvironmentID}, items: []*domain.ScheduleItem{item}}}
	service := &walletService{ServiceParams: svc.ServiceParams}
	w := &wallet.Wallet{CustomerID: sub.CustomerID, Currency: sub.Currency, Config: types.WalletConfig{AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll}}}
	usage := map[string]currentPeriodUsage{sub.ID: {periodStart: sub.CurrentPeriodStart, amount: decimal.NewFromInt(50)}}
	pending, _, err := service.pendingCharges(ctx, w, usage)
	require.NoError(t, err)
	require.Equal(t, "80", pending.String())
	require.Equal(t, "50", usage[sub.ID].amount.String(), "projection must not mutate live usage")
	w.Config.AllowedPriceTypes = nil
	pending, _, err = service.pendingCharges(ctx, w, usage)
	require.NoError(t, err)
	require.Equal(t, "80", pending.String(), "legacy empty filters must include the draft source once")
	source.LineItems[0].LineItemDiscount = decimal.NewFromInt(10)
	source.LineItems[0].InvoiceLevelDiscount = decimal.NewFromInt(5)
	require.NoError(t, svc.InvoiceRepo.Update(ctx, source))
	pending, _, err = service.pendingCharges(ctx, w, usage)
	require.NoError(t, err)
	require.Equal(t, "65", pending.String())
}

func TestWalletBillingSchedule_EmptyFiltersPreserveOrdinaryCharges(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		usage, fixed, paid, remaining int64
		want                          string
	}{
		{name: "mixed partial payment", usage: 50, fixed: 20, paid: 70, remaining: 100, want: "80"},
		{name: "source only partial payment", paid: 30, remaining: 70, want: "70"},
		{name: "credit reduced invoice", usage: 50, fixed: 20, paid: 70, remaining: 60, want: "60"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, svc, sub, source, _ := newPaymentLifecycleFixture(t)
			ctx := s.GetContext()
			owner := &commitment.Commitment{ID: "commitment_wallet", SubscriptionID: sub.ID, CustomerID: sub.CustomerID, Currency: sub.Currency, Binding: true,
				CollectionMethod: lo.ToPtr(types.CommitmentCollectionBillingTerms), EnvironmentID: sub.EnvironmentID, BaseModel: types.GetDefaultBaseModel(ctx)}
			_, err := svc.CommitmentRepo.Create(ctx, owner)
			require.NoError(t, err)
			source.PaymentStatus = types.PaymentStatusPending
			source.AmountDue = decimal.NewFromInt(tc.paid + tc.remaining)
			source.AmountPaid, source.AmountRemaining = decimal.NewFromInt(tc.paid), decimal.NewFromInt(tc.remaining)
			source.Subtotal, source.Total = decimal.NewFromInt(tc.usage+tc.fixed+100), source.AmountDue
			source.LineItems = []*invoice.InvoiceLineItem{
				{ID: "usage", Amount: decimal.NewFromInt(tc.usage), PriceType: lo.ToPtr(string(types.PRICE_TYPE_USAGE))},
				{ID: "fixed", Amount: decimal.NewFromInt(tc.fixed), PriceType: lo.ToPtr(string(types.PRICE_TYPE_FIXED))},
			}
			var items []*domain.ScheduleItem
			for _, id := range []string{"scheduled_one", "scheduled_two"} {
				item := &domain.ScheduleItem{ID: id, OwnerID: owner.ID, OwnerType: types.BillingTermOwnerCommitment, CustomerID: sub.CustomerID, Currency: sub.Currency,
					Amount: decimal.NewFromInt(50), ScheduleStatus: types.BillingScheduleInvoiced, InvoiceID: &source.ID, InvoiceLineItemID: lo.ToPtr(id + "_line"), EnvironmentID: sub.EnvironmentID, BaseModel: types.GetDefaultBaseModel(ctx)}
				items = append(items, item)
				source.LineItems = append(source.LineItems, &invoice.InvoiceLineItem{ID: *item.InvoiceLineItemID, InvoiceID: source.ID, CustomerID: sub.CustomerID, Currency: sub.Currency,
					BillingScheduleItemID: &item.ID, Amount: item.Amount, Quantity: decimal.NewFromInt(1), PriceType: lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
					PeriodStart: &sub.CurrentPeriodStart, PeriodEnd: &sub.CurrentPeriodEnd, EnvironmentID: sub.EnvironmentID, BaseModel: types.GetDefaultBaseModel(ctx)})
			}
			require.NoError(t, svc.InvoiceRepo.Update(ctx, source))
			svc.BillingTerms = &billingProjectionEngine{state: &billingAPIState{owner: &domain.OwnerSnapshot{Owner: commitmentBillingOwner(owner.ID), CustomerID: sub.CustomerID,
				Currency: sub.Currency, TenantID: sub.TenantID, EnvironmentID: sub.EnvironmentID}, items: items}}
			service := &walletService{ServiceParams: svc.ServiceParams}
			w := &wallet.Wallet{CustomerID: sub.CustomerID, Currency: sub.Currency}
			for range 2 {
				pending, _, err := service.pendingCharges(ctx, w, nil)
				require.NoError(t, err)
				require.Equal(t, tc.want, pending.String())
			}
			w.Config.AllowedPriceTypes = []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll}
			pending, _, err := service.pendingCharges(ctx, w, nil)
			require.NoError(t, err)
			require.Equal(t, decimal.NewFromInt(tc.remaining), pending)
		})
	}
}

func TestBillingScheduleProjection_InvoiceRecipientAndGroupedChildren(t *testing.T) {
	s, svc, sub, _, _ := newPaymentLifecycleFixture(t)
	ctx := s.GetContext()
	parent := subscription.NewSubscriptionBuilder(sub).WithSubscriptionType(types.SubscriptionTypeParent).Build()
	parent.InvoicingCustomerID = lo.ToPtr("invoice_recipient")
	require.NoError(t, svc.SubRepo.Update(ctx, parent))
	child := subscription.NewSubscriptionBuilder(parent).WithSubscriptionType(types.SubscriptionTypeGroupedInvoicing).WithParentSubscriptionID(&parent.ID).Build()
	child.ID, child.CustomerID, child.InvoicingCustomerID = "grouped_child", "usage_customer", nil
	require.NoError(t, svc.SubRepo.Create(ctx, child))
	ids, err := billingProjectionSubscriptions(ctx, svc.ServiceParams, "invoice_recipient")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{parent.ID, child.ID}, ids)
	ids, err = billingProjectionSubscriptions(ctx, svc.ServiceParams, sub.CustomerID)
	require.NoError(t, err)
	require.Empty(t, ids)
}
