package ent

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type selfBillingTestInvoice struct {
	tenantID    string
	status      types.InvoiceStatus
	currency    string
	finalizedAt time.Time
	total       int64
	discount    *int64
}

func createSelfBillingTestInvoice(t *testing.T, client postgres.IClient, in selfBillingTestInvoice) {
	t.Helper()

	ctx := types.SetTenantID(context.Background(), in.tenantID)
	base := types.GetDefaultBaseModel(ctx)

	create := client.Writer(ctx).Invoice.Create().
		SetID(types.GenerateUUIDWithPrefix("inv")).
		SetTenantID(in.tenantID).
		SetEnvironmentID("env_1").
		SetCustomerID("cust_1").
		SetInvoiceType(types.InvoiceTypeOneOff).
		SetInvoiceStatus(in.status).
		SetPaymentStatus(types.PaymentStatusPending).
		SetCurrency(in.currency).
		SetAmountDue(decimal.NewFromInt(in.total)).
		SetAmountPaid(decimal.Zero).
		SetAmountRemaining(decimal.NewFromInt(in.total)).
		SetSubtotal(decimal.NewFromInt(in.total)).
		SetTotal(decimal.NewFromInt(in.total)).
		SetFinalizedAt(in.finalizedAt).
		SetCreatedBy(base.CreatedBy).
		SetUpdatedBy(base.UpdatedBy)
	if in.discount != nil {
		create.SetTotalDiscount(decimal.NewFromInt(*in.discount))
	}

	_, err := create.Save(ctx)
	require.NoError(t, err)
}

func TestSelfBillingRepository_SumInvoicesByTenantHour(t *testing.T) {
	client := newRealPostgresTestClient(t)
	log, err := logger.NewLogger(&config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	})
	require.NoError(t, err)
	repo := NewSelfBillingRepository(client, log)

	// Unique tenants keep rows from other runs on the shared test database out of the assertions.
	tenantA := types.GenerateUUIDWithPrefix("tenant")
	tenantB := types.GenerateUUIDWithPrefix("tenant")
	hour := time.Date(2001, 3, 4, 10, 0, 0, 0, time.UTC)

	invoices := []selfBillingTestInvoice{
		{tenantA, types.InvoiceStatusFinalized, "usd", hour.Add(5 * time.Minute), 100, lo.ToPtr(int64(10))},
		{tenantA, types.InvoiceStatusFinalized, "usd", hour.Add(50 * time.Minute), 50, nil},
		{tenantA, types.InvoiceStatusFinalized, "eur", hour.Add(time.Minute), 7, nil},
		{tenantA, types.InvoiceStatusDraft, "usd", hour.Add(time.Minute), 1000, nil},
		{tenantA, types.InvoiceStatusFinalized, "usd", hour.Add(time.Hour), 1000, nil},
		{tenantB, types.InvoiceStatusFinalized, "usd", hour.Add(-time.Minute), 1000, nil},
		{tenantB, types.InvoiceStatusFinalized, "usd", hour.Add(30 * time.Minute), 3, nil},
	}
	for _, in := range invoices {
		createSelfBillingTestInvoice(t, client, in)
	}

	rows, err := repo.SumInvoicesByTenantHour(context.Background(), hour, hour.Add(time.Hour))
	require.NoError(t, err)

	got := lo.Filter(rows, func(r *domainsb.TenantHourInvoices, _ int) bool {
		return r.TenantID == tenantA || r.TenantID == tenantB
	})
	byKey := lo.KeyBy(got, func(r *domainsb.TenantHourInvoices) string {
		return r.TenantID + ":" + r.Currency
	})

	tests := []struct {
		name         string
		key          string
		wantCount    uint64
		wantTotal    int64
		wantDiscount int64
	}{
		{"same_hour_and_currency_summed", tenantA + ":usd", 2, 150, 10},
		{"currencies_kept_apart", tenantA + ":eur", 1, 7, 0},
		{"tenants_kept_apart", tenantB + ":usd", 1, 3, 0},
	}
	require.Len(t, got, len(tests))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row, ok := byKey[tt.key]
			require.True(t, ok)
			assert.Equal(t, hour, row.Hour)
			assert.Equal(t, tt.wantCount, row.InvoiceCount)
			assert.True(t, decimal.NewFromInt(tt.wantTotal).Equal(row.Total), "total %s", row.Total)
			assert.True(t, decimal.NewFromInt(tt.wantDiscount).Equal(row.TotalDiscount), "discount %s", row.TotalDiscount)
		})
	}
}
