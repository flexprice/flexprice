package dto

import (
	"testing"

	"github.com/samber/lo"
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

func TestAddLineItemRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     AddLineItemRequest
		wantErr bool
	}{
		{
			name: "valid request",
			req: AddLineItemRequest{
				DisplayName: "Extra seats",
				Amount:      decimal.NewFromInt(100),
				Quantity:    decimal.NewFromInt(2),
			},
			wantErr: false,
		},
		{
			name: "negative amount rejected",
			req: AddLineItemRequest{
				DisplayName: "Extra seats",
				Amount:      decimal.NewFromInt(-100),
				Quantity:    decimal.NewFromInt(2),
			},
			wantErr: true,
		},
		{
			name: "negative quantity rejected",
			req: AddLineItemRequest{
				DisplayName: "Extra seats",
				Amount:      decimal.NewFromInt(100),
				Quantity:    decimal.NewFromInt(-2),
			},
			wantErr: true,
		},
		{
			name: "missing display name rejected",
			req: AddLineItemRequest{
				Amount:   decimal.NewFromInt(100),
				Quantity: decimal.NewFromInt(2),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestUpdateLineItemRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     UpdateLineItemRequest
		wantErr bool
	}{
		{
			name: "valid request with all fields set",
			req: UpdateLineItemRequest{
				DisplayName: lo.ToPtr("Updated name"),
				Amount:      lo.ToPtr(decimal.NewFromInt(100)),
				Quantity:    lo.ToPtr(decimal.NewFromInt(2)),
			},
			wantErr: false,
		},
		{
			name: "valid request with only display name set",
			req: UpdateLineItemRequest{
				DisplayName: lo.ToPtr("Updated name"),
			},
			wantErr: false,
		},
		{
			name:    "empty request rejected",
			req:     UpdateLineItemRequest{},
			wantErr: true,
		},
		{
			name: "negative amount rejected",
			req: UpdateLineItemRequest{
				Amount: lo.ToPtr(decimal.NewFromInt(-1)),
			},
			wantErr: true,
		},
		{
			name: "negative quantity rejected",
			req: UpdateLineItemRequest{
				Quantity: lo.ToPtr(decimal.NewFromInt(-1)),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestApplyCouponRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     ApplyCouponRequest
		wantErr bool
	}{
		{
			name:    "valid invoice-level request",
			req:     ApplyCouponRequest{CouponID: "coupon_123"},
			wantErr: false,
		},
		{
			name:    "valid line-item-level request",
			req:     ApplyCouponRequest{CouponID: "coupon_123", LineItemID: lo.ToPtr("li_123")},
			wantErr: false,
		},
		{
			name:    "missing coupon id rejected",
			req:     ApplyCouponRequest{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestApplyTaxRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     ApplyTaxRequest
		wantErr bool
	}{
		{
			name:    "valid request",
			req:     ApplyTaxRequest{TaxRateID: "tax_123"},
			wantErr: false,
		},
		{
			name:    "missing tax rate id rejected",
			req:     ApplyTaxRequest{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
