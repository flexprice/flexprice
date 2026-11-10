package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/emaillog"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/email/render"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceChargeGroups(t *testing.T) {
	money := func(d decimal.Decimal) string { return formatMoney(d, "usd") }
	line := func(name, plan, entityType, priceType string, amount, qty, discount int64) *dto.InvoiceLineItemResponse {
		return &dto.InvoiceLineItemResponse{InvoiceLineItem: invoice.InvoiceLineItem{
			DisplayName:      lo.EmptyableToPtr(name),
			PlanDisplayName:  lo.EmptyableToPtr(plan),
			EntityType:       lo.EmptyableToPtr(entityType),
			PriceType:        lo.EmptyableToPtr(priceType),
			Amount:           decimal.NewFromInt(amount),
			Quantity:         decimal.NewFromInt(qty),
			LineItemDiscount: decimal.NewFromInt(discount),
		}}
	}

	items := []*dto.InvoiceLineItemResponse{
		line("Platform fee", "Growth", "plan", "FIXED", 500, 1, 50),
		line("Free tier", "Growth", "plan", "FIXED", 0, 1, 0),
		line("Support", "", "addon", "FIXED", 150, 3, 0),
		line("API calls", "Growth", "plan", "USAGE", 1800, 1800, 0),
		line("Setup", "", "", "FIXED", 20, 1, 0),
	}

	groups, count, more := invoiceChargeGroups(items, money)
	assert.Equal(t, 4, count, "zero-amount lines are left out")
	assert.Equal(t, 0, more)
	require.Len(t, groups, 3)
	assert.Equal(t, render.ChargeGroup{Name: "Growth", Total: "$2,250.00", Lines: []render.ChargeLine{
		{Name: "Platform fee", Detail: "$500.00 each", Amount: "$450.00"},
		{Name: "API calls", Detail: "1,800 used", Amount: "$1,800.00"},
	}}, groups[0])
	assert.Equal(t, "Support", groups[1].Name, "an add-on is its own group")
	assert.Equal(t, "$50.00 × 3", groups[1].Lines[0].Detail)
	assert.Equal(t, "Other charges", groups[2].Name)

	var many []*dto.InvoiceLineItemResponse
	for range 12 {
		many = append(many, line("Seat", "Team", "plan", "FIXED", 10, 1, 0))
	}
	groups, count, more = invoiceChargeGroups(many, money)
	assert.Equal(t, 12, count)
	assert.Equal(t, 2, more)
	assert.Len(t, groups[0].Lines, maxInvoiceEmailLines)
	assert.Equal(t, "$120.00", groups[0].Total, "group totals count lines left out")
}

func TestInvoiceEmailState(t *testing.T) {
	now := time.Date(2026, 12, 11, 9, 0, 0, 0, time.UTC)
	due := time.Date(2026, 12, 10, 23, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		payment   types.PaymentStatus
		remaining int64
		dueDate   *time.Time
		want      render.InvoiceState
	}{
		{"paid", types.PaymentStatusSucceeded, 0, &due, render.InvoiceStatePaid},
		{"nothing left to pay", types.PaymentStatusPending, 0, nil, render.InvoiceStatePaid},
		{"processing is still owed", types.PaymentStatusProcessing, 0, nil, render.InvoiceStateDue},
		{"past the due day", types.PaymentStatusPending, 100, &due, render.InvoiceStateOverdue},
		{"on the due day", types.PaymentStatusPending, 100, lo.ToPtr(now.Add(-time.Hour)), render.InvoiceStateDue},
		{"no due date", types.PaymentStatusPending, 100, nil, render.InvoiceStateDue},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := &dto.InvoiceResponse{Invoice: invoice.Invoice{
				PaymentStatus:   tt.payment,
				AmountRemaining: decimal.NewFromInt(tt.remaining),
				DueDate:         tt.dueDate,
			}}
			assert.Equal(t, tt.want, invoiceEmailState(inv, now))
		})
	}
}

func TestFormatMoney(t *testing.T) {
	assert.Equal(t, "$1,234,567.50", formatMoney(decimal.RequireFromString("1234567.5"), "usd"))
	assert.Equal(t, "$0.00", formatMoney(decimal.Zero, "USD"))
	assert.Equal(t, "-$12.00", formatMoney(decimal.NewFromInt(-12), "usd"))
	assert.Equal(t, "XYZ 1,000.00", formatMoney(decimal.NewFromInt(1000), "XYZ"))
}

func TestRecipientsAndSender(t *testing.T) {
	assert.Equal(t, []string{"a@x.io", "b@x.io"}, normalizeRecipients([]string{" A@x.io", "b@x.io", "a@x.io", ""}))
	assert.Equal(t, `"Acme, Inc." <invoices+tenant_01abc@mail.flexprice.io>`,
		senderAddress("invoices", "mail.flexprice.io", "tenant_01ABC", "Acme, Inc."))
}

func TestFailIfPermanent(t *testing.T) {
	ctx := testutil.SetupContext()
	store := testutil.NewInMemoryEmailLogStore()
	s := &emailNotificationService{ServiceParams: ServiceParams{EmailLogRepo: store, Logger: logger.NewNoopLogger()}}

	queued := func(id string) *emaillog.EmailLog {
		e := &emaillog.EmailLog{ID: id, EnvironmentID: types.GetEnvironmentID(ctx), DeliveryStatus: types.EmailDeliveryStatusQueued,
			Metadata: types.Metadata{}, BaseModel: types.GetDefaultBaseModel(ctx)}
		require.NoError(t, store.Create(ctx, e))
		return e
	}

	rejected := queued("email_rejected")
	err := s.failIfPermanent(ctx, rejected, ierr.NewError("bad").WithHint("Recipient address is invalid").Mark(ierr.ErrValidation))
	assert.True(t, ierr.IsValidation(err))
	got, _ := store.Get(ctx, rejected.ID)
	assert.Equal(t, types.EmailDeliveryStatusFailed, got.DeliveryStatus)
	assert.Equal(t, "Recipient address is invalid", lo.FromPtr(got.StatusReason))

	throttled := queued("email_throttled")
	err = s.failIfPermanent(ctx, throttled, ierr.NewError("slow down").Mark(ierr.ErrTooManyRequests))
	assert.False(t, ierr.IsValidation(err), "retryable errors pass through for the workflow to retry")
	got, _ = store.Get(ctx, throttled.ID)
	assert.Equal(t, types.EmailDeliveryStatusQueued, got.DeliveryStatus)
}
