package ent

import (
	"context"
	"testing"
	"time"

	itemdb "github.com/flexprice/flexprice/ent/billingscheduleitem"
	invoicedb "github.com/flexprice/flexprice/ent/invoice"
	linedb "github.com/flexprice/flexprice/ent/invoicelineitem"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	domaininvoice "github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestBillingScheduleClaimIntegrityPostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx := billingTestContext("claim_tenant", "claim_environment")
	for _, test := range []struct {
		name   string
		change func(*domaininvoice.Invoice, *domaininvoice.InvoiceLineItem)
	}{
		{"amount", func(_ *domaininvoice.Invoice, line *domaininvoice.InvoiceLineItem) {
			line.Amount = decimal.NewFromInt(1)
		}},
		{"quantity", func(_ *domaininvoice.Invoice, line *domaininvoice.InvoiceLineItem) {
			line.Quantity = decimal.NewFromInt(2)
		}},
		{"line_customer", func(_ *domaininvoice.Invoice, line *domaininvoice.InvoiceLineItem) {
			line.CustomerID = "other_customer"
		}},
		{"invoice_customer", func(inv *domaininvoice.Invoice, _ *domaininvoice.InvoiceLineItem) { inv.CustomerID = "other_customer" }},
		{"currency", func(inv *domaininvoice.Invoice, line *domaininvoice.InvoiceLineItem) {
			inv.Currency, line.Currency = "eur", "eur"
		}},
		{"invoice_currency", func(inv *domaininvoice.Invoice, _ *domaininvoice.InvoiceLineItem) { inv.Currency = "eur" }},
		{"service_period", func(_ *domaininvoice.Invoice, line *domaininvoice.InvoiceLineItem) {
			end := line.PeriodEnd.Add(time.Hour)
			line.PeriodEnd = &end
		}},
		{"missing_period", func(_ *domaininvoice.Invoice, line *domaininvoice.InvoiceLineItem) { line.PeriodStart = nil }},
		{"subscription", func(_ *domaininvoice.Invoice, line *domaininvoice.InvoiceLineItem) {
			id := "other_subscription"
			line.SubscriptionID = &id
		}},
		{"commitment", func(_ *domaininvoice.Invoice, line *domaininvoice.InvoiceLineItem) {
			id := "other_commitment"
			line.CommitmentID = &id
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, item := f.createSource(t, ctx)
			inv := newBillingTestInvoice(ctx)
			line := newBillingTestLine(ctx, inv, item)
			test.change(inv, line)
			require.NoError(t, f.invs.Create(ctx, inv))
			require.NoError(t, f.lines.Create(ctx, line))
			claimed, err := f.items.Claim(ctx, item.ID, inv.ID, line.ID)
			require.Error(t, err)
			require.False(t, claimed)
			stored, err := f.items.Get(ctx, item.ID)
			require.NoError(t, err)
			require.Equal(t, types.BillingScheduleScheduled, stored.ScheduleStatus)
			require.Nil(t, stored.InvoiceID)
			require.Nil(t, stored.InvoiceLineItemID)
		})
	}
}

func TestInvoiceBillingSourceScopePostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx := billingTestContext("source_tenant", "source_environment")
	for _, foreign := range []context.Context{
		billingTestContext("foreign_tenant", "source_environment"),
		billingTestContext("source_tenant", "foreign_environment"),
	} {
		for _, method := range []string{"create", "bulk", "with_invoice", "add"} {
			t.Run(types.GetTenantID(foreign)+"/"+types.GetEnvironmentID(foreign)+"/"+method, func(t *testing.T) {
				_, source := f.createSource(t, foreign)
				inv := newBillingTestInvoice(ctx)
				line := newBillingTestLine(ctx, inv, source)
				ordinary := domaininvoice.NewInvoiceLineItemBuilder(line).WithBillingScheduleItemID(nil).Build()
				ordinary.ID = types.GenerateUUIDWithPrefix("invli")
				if method != "with_invoice" {
					require.NoError(t, f.invs.Create(ctx, inv))
				}
				var err error
				switch method {
				case "create":
					err = f.lines.Create(ctx, line)
				case "bulk":
					err = f.lines.CreateBulk(ctx, []*domaininvoice.InvoiceLineItem{ordinary, line})
				case "with_invoice":
					inv = domaininvoice.NewInvoiceBuilder(inv).WithLineItems([]*domaininvoice.InvoiceLineItem{ordinary, line}).Build()
					err = f.invs.CreateWithLineItems(ctx, inv)
				case "add":
					err = f.invs.AddLineItems(ctx, inv.ID, []*domaininvoice.InvoiceLineItem{ordinary, line})
				}
				require.Error(t, err)
				count, err := f.client.Writer(ctx).InvoiceLineItem.Query().Where(linedb.TenantID(types.GetTenantID(ctx)), linedb.EnvironmentID(types.GetEnvironmentID(ctx)), linedb.InvoiceID(inv.ID)).Count(ctx)
				require.NoError(t, err)
				require.Zero(t, count, "bulk failure must not leave ordinary lines behind")
				if method == "with_invoice" {
					exists, err := f.client.Writer(ctx).Invoice.Query().Where(invoicedb.TenantID(types.GetTenantID(ctx)), invoicedb.EnvironmentID(types.GetEnvironmentID(ctx)), invoicedb.ID(inv.ID)).Exist(ctx)
					require.NoError(t, err)
					require.False(t, exists)
				}
			})
		}
	}
}

func TestBillingScheduleRecipientScopePostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx := billingTestContext("recipient_tenant", "recipient_environment")
	_, local := f.createSource(t, ctx)
	for _, foreign := range []context.Context{
		billingTestContext("foreign_tenant", "recipient_environment"),
		billingTestContext("recipient_tenant", "foreign_environment"),
	} {
		_, source := f.createSource(t, foreign)
		subscriptionID := types.GenerateUUIDWithPrefix("sub")
		_, err := f.client.Writer(foreign).Subscription.Create().SetID(subscriptionID).
			SetTenantID(types.GetTenantID(foreign)).SetEnvironmentID(types.GetEnvironmentID(foreign)).
			SetCustomerID(source.CustomerID).SetPlanID("plan").SetCurrency("usd").
			SetBillingCadence(types.BILLING_CADENCE_RECURRING).SetBillingPeriod(types.BILLING_PERIOD_MONTHLY).Save(foreign)
		require.NoError(t, err)
		for _, recipient := range []string{"customer", "subscription"} {
			t.Run(types.GetTenantID(foreign)+"/"+types.GetEnvironmentID(foreign)+"/"+recipient, func(t *testing.T) {
				item := domain.NewScheduleItemBuilder(local).WithID(types.GenerateUUIDWithPrefix("bsitem")).Build()
				if recipient == "customer" {
					item.CustomerID = source.CustomerID
				} else {
					item.SubscriptionID = &subscriptionID
				}
				require.Error(t, f.items.Create(ctx, item))
				exists, err := f.client.Writer(ctx).BillingScheduleItem.Query().Where(itemdb.TenantID(types.GetTenantID(ctx)), itemdb.EnvironmentID(types.GetEnvironmentID(ctx)), itemdb.ID(item.ID)).Exist(ctx)
				require.NoError(t, err)
				require.False(t, exists)
			})
		}
	}
}
