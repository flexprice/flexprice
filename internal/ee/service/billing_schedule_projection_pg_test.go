package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/wallet"
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
