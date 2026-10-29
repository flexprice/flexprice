package ent

import (
	"context"
	"testing"

	invoicedb "github.com/flexprice/flexprice/ent/invoice"
	linedb "github.com/flexprice/flexprice/ent/invoicelineitem"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	domaininvoice "github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestBillingTermsExecutionPostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx := billingTestContext("execution_tenant", "execution_environment")
	store := NewBillingTermsExecutionStore(f.client)

	for _, test := range []struct {
		name   string
		change func(*domaininvoice.Invoice, *domaininvoice.InvoiceLineItem)
		valid  bool
	}{
		{name: "draft principal is assigned", valid: true},
		{name: "finalized principal is assigned", valid: true, change: func(i *domaininvoice.Invoice, _ *domaininvoice.InvoiceLineItem) {
			i.InvoiceStatus = types.InvoiceStatusFinalized
		}},
		{name: "different invoice destination subscription is allowed", valid: true, change: func(i *domaininvoice.Invoice, _ *domaininvoice.InvoiceLineItem) {
			i.SubscriptionID = lo.ToPtr("parent_subscription")
		}},
		{name: "different recipient fails closed", change: func(i *domaininvoice.Invoice, _ *domaininvoice.InvoiceLineItem) { i.CustomerID = "another_customer" }},
		{name: "edited principal fails closed", change: func(_ *domaininvoice.Invoice, l *domaininvoice.InvoiceLineItem) { l.Amount = decimal.NewFromInt(90) }},
		{name: "different currency fails closed", change: func(i *domaininvoice.Invoice, l *domaininvoice.InvoiceLineItem) {
			i.Currency, l.Currency = "eur", "eur"
		}},
		{name: "different quantity fails closed", change: func(_ *domaininvoice.Invoice, l *domaininvoice.InvoiceLineItem) { l.Quantity = decimal.NewFromInt(2) }},
		{name: "missing service period fails closed", change: func(_ *domaininvoice.Invoice, l *domaininvoice.InvoiceLineItem) { l.PeriodEnd = nil }},
		{name: "changed subscription attribution fails closed", change: func(_ *domaininvoice.Invoice, l *domaininvoice.InvoiceLineItem) {
			l.SubscriptionID = lo.ToPtr("another_subscription")
		}},
		{name: "skipped invoice cannot retain principal", change: func(i *domaininvoice.Invoice, _ *domaininvoice.InvoiceLineItem) {
			i.InvoiceStatus = types.InvoiceStatusSkipped
		}},
		{name: "custom currency requires explicit support", change: func(_ *domaininvoice.Invoice, l *domaininvoice.InvoiceLineItem) {
			l.CustomCurrency = &types.CustomCurrencyLineItem{Amount: decimal.NewFromInt(100)}
		}},
		{name: "frozen FX preserves original principal", valid: true, change: func(i *domaininvoice.Invoice, l *domaininvoice.InvoiceLineItem) {
			i.Currency, l.Currency, l.Amount = "eur", "eur", decimal.NewFromInt(90)
			i.FxConversion = &types.FxConversion{ChargeCurrency: "usd", BillingCurrency: "eur", Rate: decimal.RequireFromString("0.90")}
			l.FxConversion = &types.FxConversion{ChargeCurrency: "usd", Source: types.FxConversionSource{Subtotal: decimal.NewFromInt(100)}}
		}},
		{name: "FX snapshot principal mismatch fails closed", change: func(i *domaininvoice.Invoice, l *domaininvoice.InvoiceLineItem) {
			i.Currency, l.Currency, l.Amount = "eur", "eur", decimal.NewFromInt(90)
			i.FxConversion = &types.FxConversion{ChargeCurrency: "usd", BillingCurrency: "eur"}
			l.FxConversion = &types.FxConversion{ChargeCurrency: "usd", Source: types.FxConversionSource{Subtotal: decimal.NewFromInt(99)}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, item := f.createSource(t, ctx)
			inv := newBillingTestInvoice(ctx)
			inv.CustomerID = item.CustomerID
			line := domaininvoice.NewInvoiceLineItemBuilder(newBillingTestLine(ctx, inv, item)).
				WithPeriodStart(&item.ServicePeriodStart).WithPeriodEnd(&item.ServicePeriodEnd).Build()
			if test.change != nil {
				test.change(inv, line)
			}
			require.NoError(t, f.invs.Create(ctx, inv))
			require.NoError(t, f.lines.Create(ctx, line))
			bound := domain.NewScheduleItemBuilder(item).WithInvoiceID(&inv.ID).WithInvoiceLineItemID(&line.ID).WithScheduleStatus(types.BillingScheduleInvoiced).Build()
			states, err := store.BindingStates(ctx, []*domain.ScheduleItem{bound})
			if !test.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, states, 1)
			require.Equal(t, item.ID, states[0].ItemID)
			require.Equal(t, inv.InvoiceStatus, states[0].InvoiceStatus)
			_, err = store.BindingStates(billingTestContext("execution_tenant", "other_environment"), []*domain.ScheduleItem{bound})
			require.Error(t, err)
			forged := domain.NewScheduleItemBuilder(bound).Build()
			forged.EnvironmentID = "other_environment"
			_, err = store.BindingStates(billingTestContext("execution_tenant", "other_environment"), []*domain.ScheduleItem{forged})
			require.Error(t, err, "a fabricated scoped item cannot read another environment's invoice")
		})
	}

	t.Run("archived source still reserves principal until void", func(t *testing.T) {
		rule, item := f.createSource(t, ctx)
		inv := newBillingTestInvoice(ctx)
		inv.CustomerID = item.CustomerID
		line := domaininvoice.NewInvoiceLineItemBuilder(newBillingTestLine(ctx, inv, item)).WithPeriodStart(&item.ServicePeriodStart).WithPeriodEnd(&item.ServicePeriodEnd).Build()
		require.NoError(t, f.invs.Create(ctx, inv))
		require.NoError(t, f.lines.Create(ctx, line))
		claimed, err := f.items.Claim(ctx, item.ID, inv.ID, line.ID)
		require.NoError(t, err)
		require.True(t, claimed)
		owner := domain.OwnerRef{Type: rule.OwnerType, ID: rule.OwnerID}
		require.NoError(t, f.rules.Archive(ctx, owner))
		require.NoError(t, f.items.Archive(ctx, owner))
		bound, err := f.items.Get(ctx, item.ID)
		require.NoError(t, err)
		states, err := store.BindingStates(ctx, []*domain.ScheduleItem{bound})
		require.NoError(t, err)
		require.Equal(t, types.InvoiceStatusDraft, states[0].InvoiceStatus)
		_, err = f.client.Writer(ctx).Invoice.Update().Where(invoicedb.ID(inv.ID), invoicedb.TenantID(types.GetTenantID(ctx)), invoicedb.EnvironmentID(types.GetEnvironmentID(ctx))).SetStatus(string(types.StatusArchived)).Save(ctx)
		require.NoError(t, err)
		states, err = store.BindingStates(ctx, []*domain.ScheduleItem{bound})
		require.NoError(t, err)
		require.True(t, states[0].InvoiceArchived, "archiving a non-void invoice must not free assigned principal")
		_, err = f.client.Writer(ctx).InvoiceLineItem.Update().Where(linedb.ID(line.ID), linedb.TenantID(types.GetTenantID(ctx)), linedb.EnvironmentID(types.GetEnvironmentID(ctx))).SetStatus(string(types.StatusArchived)).Save(ctx)
		require.NoError(t, err)
		_, err = store.BindingStates(ctx, []*domain.ScheduleItem{bound})
		require.Error(t, err)
		_, err = f.client.Writer(ctx).Invoice.Update().Where(invoicedb.ID(inv.ID), invoicedb.TenantID(types.GetTenantID(ctx)), invoicedb.EnvironmentID(types.GetEnvironmentID(ctx))).SetInvoiceStatus(types.InvoiceStatusVoided).Save(ctx)
		require.NoError(t, err)
		states, err = store.BindingStates(ctx, []*domain.ScheduleItem{bound})
		require.NoError(t, err)
		require.Equal(t, types.InvoiceStatusVoided, states[0].InvoiceStatus)
	})
}

func TestBillingTermsPauseSourceIsolationPostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx := billingTestContext("pause_tenant", "pause_environment")
	repo := NewSubscriptionRepository(f.client, logger.NewNoopLogger(), newFakeRedisCache())
	sub := subscriptionTestFixture(ctx, types.GenerateUUIDWithPrefix("subs"), types.SubscriptionStatusActive, nil, nil)
	require.NoError(t, repo.Create(ctx, sub))
	createPause := func(pauseCtx context.Context) string {
		id := types.GenerateUUIDWithPrefix("pause")
		_, err := f.client.Writer(pauseCtx).SubscriptionPause.Create().SetID(id).SetTenantID(types.GetTenantID(pauseCtx)).SetEnvironmentID(types.GetEnvironmentID(pauseCtx)).SetSubscriptionID(sub.ID).SetPauseStatus("active").Save(pauseCtx)
		require.NoError(t, err)
		return id
	}
	local := createPause(ctx)
	foreignCtx := billingTestContext("pause_tenant", "other_environment")
	foreign := createPause(foreignCtx)
	_, err := repo.GetPause(ctx, local)
	require.NoError(t, err)
	_, err = repo.GetPause(ctx, foreign)
	require.Error(t, err)
	_, err = repo.GetPause(foreignCtx, local)
	require.Error(t, err)
	_, pauses, err := repo.GetWithPauses(ctx, sub.ID)
	require.NoError(t, err)
	require.Len(t, pauses, 1)
	require.Equal(t, local, pauses[0].ID)
}
