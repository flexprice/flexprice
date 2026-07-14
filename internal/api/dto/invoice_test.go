package dto

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestCreateInvoiceRequest_ZeroOutAmounts(t *testing.T) {
	req := CreateInvoiceRequest{
		Subtotal:  decimal.NewFromInt(99),
		Total:     decimal.NewFromInt(99),
		AmountDue: decimal.NewFromInt(99),
		LineItems: []CreateInvoiceLineItemRequest{
			{Amount: decimal.NewFromInt(99), Quantity: decimal.NewFromInt(2)},
			{Amount: decimal.NewFromInt(49), Quantity: decimal.NewFromInt(1)},
		},
	}

	req.ZeroOutAmounts()

	assert.True(t, req.Subtotal.IsZero(), "Subtotal must be zero")
	assert.True(t, req.Total.IsZero(), "Total must be zero")
	assert.True(t, req.AmountDue.IsZero(), "AmountDue must be zero")

	for i, li := range req.LineItems {
		assert.True(t, li.Amount.IsZero(), "line item %d Amount must be zero", i)
		// Quantity is deliberately preserved — it shows the pricing skeleton.
		assert.False(t, li.Quantity.IsZero(), "line item %d Quantity must be preserved", i)
	}
}

func TestCreateInvoiceRequest_ZeroOutAmounts_EmptyLineItems(t *testing.T) {
	req := CreateInvoiceRequest{
		Subtotal:  decimal.NewFromInt(50),
		Total:     decimal.NewFromInt(50),
		AmountDue: decimal.NewFromInt(50),
	}
	req.ZeroOutAmounts() // must not panic on nil/empty LineItems
	assert.True(t, req.Subtotal.IsZero())
	assert.True(t, req.Total.IsZero())
	assert.True(t, req.AmountDue.IsZero())
}

func TestCreateDraftInvoiceRequest_ToDraftInvoice_Defaults(t *testing.T) {
	req := CreateDraftInvoiceRequest{
		CustomerID:  "cust_1",
		InvoiceType: types.InvoiceTypeOneOff,
		Currency:    "usd",
	}
	inv, err := req.ToDraftInvoice(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, types.CollectionMethodChargeAutomatically, inv.CollectionMethod)
	assert.Equal(t, types.PaymentBehaviorDefaultActive, inv.PaymentBehavior)
}

func TestCreateDraftInvoiceRequest_ToDraftInvoice_ExplicitValues(t *testing.T) {
	cm := types.CollectionMethodSendInvoice
	pb := types.PaymentBehaviorDefaultIncomplete
	req := CreateDraftInvoiceRequest{
		CustomerID:       "cust_1",
		InvoiceType:      types.InvoiceTypeOneOff,
		Currency:         "usd",
		CollectionMethod: &cm,
		PaymentBehavior:  &pb,
	}
	inv, err := req.ToDraftInvoice(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, types.CollectionMethodSendInvoice, inv.CollectionMethod)
	assert.Equal(t, types.PaymentBehaviorDefaultIncomplete, inv.PaymentBehavior)
}

func TestCreateDraftInvoiceRequest_Validate_RejectsInvalidCombo(t *testing.T) {
	cm := types.CollectionMethodSendInvoice
	pb := types.PaymentBehaviorAllowIncomplete // invalid for send_invoice
	req := CreateDraftInvoiceRequest{
		CustomerID:       "cust_1",
		InvoiceType:      types.InvoiceTypeOneOff,
		Currency:         "usd",
		CollectionMethod: &cm,
		PaymentBehavior:  &pb,
	}
	err := req.Validate()
	assert.Error(t, err)
}

func TestCreateDraftInvoiceRequest_Validate_AllowsValidCombo(t *testing.T) {
	cm := types.CollectionMethodSendInvoice
	pb := types.PaymentBehaviorDefaultActive
	req := CreateDraftInvoiceRequest{
		CustomerID:       "cust_1",
		InvoiceType:      types.InvoiceTypeOneOff,
		Currency:         "usd",
		CollectionMethod: &cm,
		PaymentBehavior:  &pb,
	}
	err := req.Validate()
	assert.NoError(t, err)
}

func TestCreateInvoiceRequest_ToDraftRequest_ThreadsCollectionMethodAndPaymentBehavior(t *testing.T) {
	cm := types.CollectionMethodSendInvoice
	pb := types.PaymentBehaviorDefaultIncomplete
	req := CreateInvoiceRequest{
		CustomerID:       "cust_1",
		Currency:         "usd",
		CollectionMethod: &cm,
		PaymentBehavior:  &pb,
	}
	draft := req.ToDraftRequest()
	assert.Equal(t, &cm, draft.CollectionMethod)
	assert.Equal(t, &pb, draft.PaymentBehavior)
}

func TestCreateInvoiceRequest_ToInvoice_Defaults(t *testing.T) {
	req := CreateInvoiceRequest{
		CustomerID: "cust_1",
		Currency:   "usd",
		AmountDue:  decimal.NewFromInt(10),
		Total:      decimal.NewFromInt(10),
		Subtotal:   decimal.NewFromInt(10),
	}
	inv, err := req.ToInvoice(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, types.CollectionMethodChargeAutomatically, inv.CollectionMethod)
	assert.Equal(t, types.PaymentBehaviorDefaultActive, inv.PaymentBehavior)
}
