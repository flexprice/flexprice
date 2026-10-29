package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestBillingScheduleProjection_PostgresReadsDoNotClaim(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	f := newInvoiceCollectionFixture(t, db)
	f.invoice.LastComputedAt = nil
	require.NoError(t, f.service.InvoiceRepo.Update(f.ctx, f.invoice))
	sub, err := f.service.SubRepo.Get(f.ctx, *f.invoice.SubscriptionID)
	require.NoError(t, err)
	service := &walletService{ServiceParams: f.service.ServiceParams}
	w := &wallet.Wallet{CustomerID: f.invoice.CustomerID, Currency: "usd"}
	usage := map[string]currentPeriodUsage{sub.ID: {periodStart: sub.CurrentPeriodStart, amount: decimal.Zero}}
	for _, allowed := range []types.WalletConfigPriceType{"", types.WalletConfigPriceTypeUsage, types.WalletConfigPriceTypeFixed, types.WalletConfigPriceTypeAll} {
		w.Config.AllowedPriceTypes = nil
		if allowed != "" {
			w.Config.AllowedPriceTypes = []types.WalletConfigPriceType{allowed}
		}
		pending, _, err := service.pendingCharges(f.ctx, w, usage)
		require.NoError(t, err)
		require.Equal(t, lo.Ternary(allowed == types.WalletConfigPriceTypeUsage, "0", "100"), pending.String())
	}
	preview := dto.CreateInvoiceRequest{CustomerID: f.invoice.CustomerID, Currency: "usd", SubscriptionID: &sub.ID, InvoiceType: types.InvoiceTypeSubscription,
		PeriodStart: f.invoice.PeriodStart, PeriodEnd: f.invoice.PeriodEnd}
	require.NoError(t, f.service.projectSubscriptionBillingSchedule(f.ctx, sub, &preview))
	require.Len(t, preview.LineItems, 1)
	require.Equal(t, "100", preview.Subtotal.String())
	item, err := f.items.Get(f.ctx, f.itemID)
	require.NoError(t, err)
	require.Equal(t, types.BillingScheduleScheduled, item.ScheduleStatus)
	require.Nil(t, item.InvoiceID)
	for _, status := range []types.SubscriptionStatus{types.SubscriptionStatusPaused, types.SubscriptionStatusTrialing} {
		sub.SubscriptionStatus = status
		require.NoError(t, f.service.SubRepo.Update(f.ctx, sub))
		pending, _, err := service.pendingCharges(f.ctx, w, usage)
		require.NoError(t, err)
		require.True(t, pending.IsZero(), "deferred subscription principal must not reserve the wallet")
	}
	sub.SubscriptionStatus = types.SubscriptionStatusActive
	require.NoError(t, f.service.SubRepo.Update(f.ctx, sub))

	_, _, err = f.service.ComputeInvoice(f.ctx, f.invoice.ID, nil)
	require.NoError(t, err)
	pending, _, err := service.pendingCharges(f.ctx, w, usage)
	require.NoError(t, err)
	require.Equal(t, "100", pending.String(), "the current draft must retain the source exactly once")
	preview.LineItems, preview.Subtotal, preview.Total, preview.AmountDue = nil, decimal.Zero, decimal.Zero, decimal.Zero
	require.NoError(t, f.service.projectSubscriptionBillingSchedule(f.ctx, sub, &preview))
	require.Len(t, preview.LineItems, 1)
	require.Equal(t, "100", preview.Subtotal.String())

	inv, err := f.service.InvoiceRepo.Get(f.ctx, f.invoice.ID)
	require.NoError(t, err)
	inv.InvoiceStatus = types.InvoiceStatusFinalized
	require.NoError(t, f.service.InvoiceRepo.Update(f.ctx, inv))
	pending, _, err = service.pendingCharges(f.ctx, w, usage)
	require.NoError(t, err)
	require.Equal(t, "140", pending.String(), "finalized invoice totals include ordinary charges and the source once")
}

func TestBillingSchedulePreviewPostgresMatchesCollectionBoundary(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	for _, reason := range []types.InvoiceBillingReason{
		types.InvoiceBillingReasonSubscriptionCreate, types.InvoiceBillingReasonSubscriptionUpdate,
		types.InvoiceBillingReasonSubscriptionResume, types.InvoiceBillingReasonSubscriptionTrialEnd,
		types.InvoiceBillingReasonSubscriptionCycle, types.InvoiceBillingReasonProration,
	} {
		t.Run(string(reason), func(t *testing.T) {
			f := newInvoiceCollectionFixture(t, db)
			sub, err := f.service.SubRepo.Get(f.ctx, *f.invoice.SubscriptionID)
			require.NoError(t, err)
			item, err := f.items.Get(f.ctx, f.itemID)
			require.NoError(t, err)
			owner := domain.OwnerRef{Type: item.OwnerType, ID: item.OwnerID}
			state, err := f.service.BillingTerms.Get(f.ctx, owner)
			require.NoError(t, err)
			var rules []core.RuleInput
			for _, date := range []time.Time{sub.CurrentPeriodStart, sub.CurrentPeriodStart.AddDate(0, 0, 10), time.Now().UTC().AddDate(0, 0, 1), sub.CurrentPeriodEnd} {
				rules = append(rules, core.RuleInput{Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Value: lo.ToPtr(decimal.NewFromInt(25)), BillOn: lo.ToPtr(date)})
			}
			state, err = f.service.BillingTerms.Replace(f.ctx, core.ReplaceRequest{Owner: owner, ExpectedState: state.StateToken(), Rows: rules, Reason: "Test invoice boundary"})
			require.NoError(t, err)
			f.invoice.BillingReason = string(reason)
			require.NoError(t, f.service.InvoiceRepo.Update(f.ctx, f.invoice))
			preview := dto.CreateInvoiceRequest{CustomerID: f.invoice.CustomerID, Currency: "usd", SubscriptionID: &sub.ID,
				InvoiceType: types.InvoiceTypeSubscription, BillingReason: reason, PeriodStart: f.invoice.PeriodStart, PeriodEnd: f.invoice.PeriodEnd}
			require.NoError(t, f.service.projectSubscriptionBillingSchedule(f.ctx, sub, &preview))
			want := "25"
			if reason == types.InvoiceBillingReasonSubscriptionCycle || reason == types.InvoiceBillingReasonProration {
				want = "50"
			}
			require.Equal(t, want, preview.Subtotal.String(), "only sources due by both the invoice boundary and now are previewed")
			for _, item := range state.Items() {
				current, err := f.items.Get(f.ctx, item.ID)
				require.NoError(t, err)
				require.Nil(t, current.InvoiceID, "preview cannot claim sources")
			}
			inv, _, err := f.service.ComputeInvoice(f.ctx, f.invoice.ID, nil)
			require.NoError(t, err)
			principal := decimal.Zero
			for _, line := range inv.LineItems {
				if line.BillingScheduleItemID != nil {
					principal = principal.Add(line.Amount)
				}
			}
			require.Equal(t, want, principal.String())
			preview.LineItems, preview.Subtotal, preview.Total, preview.AmountDue = nil, decimal.Zero, decimal.Zero, decimal.Zero
			require.NoError(t, f.service.projectSubscriptionBillingSchedule(f.ctx, sub, &preview))
			require.Equal(t, want, preview.Subtotal.String(), "assigned sources remain represented once on repeated previews")
		})
	}
}
