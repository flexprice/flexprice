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

func TestValidateQuantityFloor(t *testing.T) {
	tests := []struct {
		name        string
		qty         *decimal.Decimal
		minQuantity *decimal.Decimal
		wantErr     bool
	}{
		{name: "nil qty is no-op", qty: nil, minQuantity: lo.ToPtr(decimal.NewFromInt(5)), wantErr: false},
		{name: "nil min_quantity is no-op", qty: lo.ToPtr(decimal.Zero), minQuantity: nil, wantErr: false},
		{name: "zero qty below positive floor is rejected", qty: lo.ToPtr(decimal.Zero), minQuantity: lo.ToPtr(decimal.NewFromInt(5)), wantErr: true},
		{name: "qty at floor is allowed", qty: lo.ToPtr(decimal.NewFromInt(5)), minQuantity: lo.ToPtr(decimal.NewFromInt(5)), wantErr: false},
		{name: "qty above floor is allowed", qty: lo.ToPtr(decimal.NewFromInt(6)), minQuantity: lo.ToPtr(decimal.NewFromInt(5)), wantErr: false},
		{name: "qty below floor is rejected", qty: lo.ToPtr(decimal.NewFromInt(4)), minQuantity: lo.ToPtr(decimal.NewFromInt(5)), wantErr: true},
		{name: "zero qty with zero floor is allowed", qty: lo.ToPtr(decimal.Zero), minQuantity: lo.ToPtr(decimal.Zero), wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateQuantityFloor(tt.qty, tt.minQuantity)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "quantity must be greater than or equal to min_quantity")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
