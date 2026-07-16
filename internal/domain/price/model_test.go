package price

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestPrice_Validate_MinQuantity(t *testing.T) {
	base := func() *Price {
		return &Price{
			Amount:         decimal.NewFromInt(10),
			Currency:       "usd",
			BillingModel:   types.BILLING_MODEL_FLAT_FEE,
			BillingCadence: types.BILLING_CADENCE_RECURRING,
			BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
			InvoiceCadence: types.InvoiceCadenceAdvance,
			EntityType:     types.PRICE_ENTITY_TYPE_PLAN,
		}
	}

	t.Run("nil min_quantity is valid", func(t *testing.T) {
		p := base()
		p.MinQuantity = nil
		assert.NoError(t, p.Validate())
	})

	t.Run("zero min_quantity is valid", func(t *testing.T) {
		p := base()
		p.MinQuantity = lo.ToPtr(decimal.Zero)
		assert.NoError(t, p.Validate())
	})

	t.Run("positive min_quantity is valid", func(t *testing.T) {
		p := base()
		p.MinQuantity = lo.ToPtr(decimal.NewFromInt(5))
		assert.NoError(t, p.Validate())
	})

	t.Run("negative min_quantity is rejected", func(t *testing.T) {
		p := base()
		p.MinQuantity = lo.ToPtr(decimal.NewFromInt(-1))
		err := p.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "min_quantity must be non-negative")
	})
}

func TestValidateQuantityNonNegative(t *testing.T) {
	tests := []struct {
		name    string
		qty     *decimal.Decimal
		wantErr bool
	}{
		{name: "nil is allowed (omitted)", qty: nil, wantErr: false},
		{name: "zero is allowed", qty: lo.ToPtr(decimal.Zero), wantErr: false},
		{name: "positive is allowed", qty: lo.ToPtr(decimal.NewFromInt(3)), wantErr: false},
		{name: "negative is rejected", qty: lo.ToPtr(decimal.NewFromInt(-1)), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateQuantityNonNegative(tt.qty)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "quantity must be non-negative")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestApplyQuantityDefault(t *testing.T) {
	makePrice := func(minQty *decimal.Decimal, priceType types.PriceType) *Price {
		p := &Price{Type: priceType}
		p.MinQuantity = minQty
		return p
	}
	toPtr := func(d decimal.Decimal) *decimal.Decimal { return &d }

	tests := []struct {
		name    string
		qty     decimal.Decimal
		price   *Price
		wantQty decimal.Decimal
	}{
		{
			name:    "non-zero qty returned as-is",
			qty:     decimal.NewFromInt(3),
			price:   makePrice(toPtr(decimal.NewFromInt(5)), types.PRICE_TYPE_FIXED),
			wantQty: decimal.NewFromInt(3),
		},
		{
			name:    "zero qty with min_quantity returns min_quantity",
			qty:     decimal.Zero,
			price:   makePrice(toPtr(decimal.NewFromInt(5)), types.PRICE_TYPE_FIXED),
			wantQty: decimal.NewFromInt(5),
		},
		{
			name:    "zero qty with nil min_quantity returns default (1 for fixed)",
			qty:     decimal.Zero,
			price:   makePrice(nil, types.PRICE_TYPE_FIXED),
			wantQty: decimal.NewFromInt(1),
		},
		{
			name:    "zero qty with zero min_quantity falls through to default",
			qty:     decimal.Zero,
			price:   makePrice(toPtr(decimal.Zero), types.PRICE_TYPE_FIXED),
			wantQty: decimal.NewFromInt(1),
		},
		{
			name:    "zero qty with nil price returns 1",
			qty:     decimal.Zero,
			price:   nil,
			wantQty: decimal.NewFromInt(1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyQuantityDefault(tt.qty, tt.price)
			assert.True(t, tt.wantQty.Equal(got), "want %s got %s", tt.wantQty, got)
		})
	}
}
