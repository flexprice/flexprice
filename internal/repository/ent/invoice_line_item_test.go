package ent

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	domainInvoice "github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// newTestInvoiceLineItemRepository and newTestInvoiceRepository reuse the real-Postgres
// harness (newRealPostgresTestClient, defined in coupon_test.go) so this test exercises
// the actual generated SQL - including the ORDER BY added to ListByInvoiceID - rather than
// the in-memory test double used by internal/testutil, which sorts by a different key.
func newTestInvoiceLineItemRepository(t *testing.T) domainInvoice.LineItemRepository {
	t.Helper()
	client := newRealPostgresTestClient(t)
	log, err := newTestLogger(t)
	require.NoError(t, err)
	return NewInvoiceLineItemRepository(client, log)
}

func newTestInvoiceRepository(t *testing.T) domainInvoice.Repository {
	t.Helper()
	client := newRealPostgresTestClient(t)
	log, err := newTestLogger(t)
	require.NoError(t, err)
	return NewInvoiceRepository(client, log, noopRedisCache{})
}

func newTestLogger(t *testing.T) (*logger.Logger, error) {
	t.Helper()
	return logger.NewLogger(&config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	})
}

func testInvoiceLineItemContext() context.Context {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, types.DefaultUserID)
	return ctx
}

func newTestInvoiceForLineItems(id, customerID string) *domainInvoice.Invoice {
	ctx := testInvoiceLineItemContext()
	return &domainInvoice.Invoice{
		ID:            id,
		CustomerID:    customerID,
		InvoiceType:   types.InvoiceTypeOneOff,
		InvoiceStatus: types.InvoiceStatusDraft,
		PaymentStatus: types.PaymentStatusPending,
		Currency:      "usd",
		AmountDue:     decimal.Zero,
		AmountPaid:    decimal.Zero,
		Subtotal:      decimal.Zero,
		Total:         decimal.Zero,
		TotalDiscount: decimal.Zero,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
}

// TestListByInvoiceID_StableCreatedAtOrder proves that ListByInvoiceID returns line items
// ordered ascending by created_at, and that this order is stable across two separate calls -
// which is what spreadPrepaidCreditsAcrossLineItems / CalculateCreditAdjustments
// (internal/ee/service/credit_adjustment.go) rely on to reproduce prior per-line prepaid-credit
// allocation across repeated invocations on the same invoice.
func TestListByInvoiceID_StableCreatedAtOrder(t *testing.T) {
	lineItemRepo := newTestInvoiceLineItemRepository(t)
	invoiceRepo := newTestInvoiceRepository(t)
	ctx := testInvoiceLineItemContext()

	customerID := types.GenerateUUIDWithPrefix("cust")
	inv := newTestInvoiceForLineItems(types.GenerateUUIDWithPrefix("inv"), customerID)
	require.NoError(t, invoiceRepo.Create(ctx, inv))

	// Create line items with explicit, staggered created_at timestamps so the test does not
	// depend on incidental insertion order or clock resolution. Insert in an order that is the
	// REVERSE of created_at, so a query without ORDER BY would have no reason to happen to come
	// back sorted ascending by created_at.
	base := time.Now().UTC().Truncate(time.Second)
	ids := []string{
		types.GenerateUUIDWithPrefix("li"),
		types.GenerateUUIDWithPrefix("li"),
		types.GenerateUUIDWithPrefix("li"),
		types.GenerateUUIDWithPrefix("li"),
	}
	createdAts := []time.Time{
		base.Add(3 * time.Second),
		base.Add(1 * time.Second),
		base.Add(2 * time.Second),
		base.Add(0 * time.Second),
	}

	for i, id := range ids {
		item := &domainInvoice.InvoiceLineItem{
			ID:         id,
			InvoiceID:  inv.ID,
			CustomerID: customerID,
			Amount:     decimal.NewFromInt(int64(i + 1)),
			Quantity:   decimal.NewFromInt(1),
			Currency:   "usd",
			BaseModel:  types.GetDefaultBaseModel(ctx),
		}
		item.CreatedAt = createdAts[i]
		require.NoError(t, lineItemRepo.Create(ctx, item))
	}

	wantOrder := []string{ids[3], ids[1], ids[2], ids[0]} // ascending by created_at

	first, err := lineItemRepo.ListByInvoiceID(ctx, inv.ID)
	require.NoError(t, err)
	require.Len(t, first, len(ids))

	gotOrder := make([]string, len(first))
	for i, item := range first {
		gotOrder[i] = item.ID
	}
	require.Equal(t, wantOrder, gotOrder, "ListByInvoiceID must return items ascending by created_at")

	// Call again to prove the order is stable across separate queries, not an accident of a
	// single query plan.
	second, err := lineItemRepo.ListByInvoiceID(ctx, inv.ID)
	require.NoError(t, err)
	require.Len(t, second, len(ids))

	gotOrder2 := make([]string, len(second))
	for i, item := range second {
		gotOrder2[i] = item.ID
	}
	require.Equal(t, gotOrder, gotOrder2, "order must be stable across separate ListByInvoiceID calls")
}
