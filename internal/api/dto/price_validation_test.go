package dto_test

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validFlatFeeRequest returns a minimal valid CreatePriceRequest for FLAT_FEE billing model.
// Each test mutates a copy of this to exercise the specific scenario.
func validFlatFeeRequest() dto.CreatePriceRequest {
	amount := decimal.NewFromInt(100)
	return dto.CreatePriceRequest{
		Currency:           "USD",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           "plan_test_123",
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		Amount:             &amount,
	}
}

// --- Rejection cases ---

func TestCreatePriceRequest_Validate_RejectsMissingBillingModel(t *testing.T) {
	req := validFlatFeeRequest()
	req.BillingModel = ""

	err := req.Validate()
	if err == nil {
		t.Fatal("expected error for missing BillingModel, got nil")
	}
}

func TestCreatePriceRequest_Validate_RejectsTieredWithNoTiers(t *testing.T) {
	req := validFlatFeeRequest()
	req.BillingModel = types.BILLING_MODEL_TIERED
	req.Tiers = nil

	err := req.Validate()
	if err == nil {
		t.Fatal("expected error for TIERED billing model with no tiers, got nil")
	}
}

func TestCreatePriceRequest_Validate_AcceptsFlatFeeWithZeroAmount(t *testing.T) {
	req := validFlatFeeRequest()
	zero := decimal.Zero
	req.Amount = &zero
	err := req.Validate()
	assert.NoError(t, err, "FLAT_FEE with amount=0 should be valid (e.g. free tier)")
}

func TestCreatePriceRequest_Validate_RejectsFlatFeeWithNilAmount(t *testing.T) {
	req := validFlatFeeRequest()
	req.Amount = nil
	err := req.Validate()
	require.Error(t, err, "FLAT_FEE with nil amount should fail validation")
}

// --- Acceptance cases ---

func TestCreatePriceRequest_Validate_AcceptsFlatFeeWithPositiveAmount(t *testing.T) {
	req := validFlatFeeRequest()

	if err := req.Validate(); err != nil {
		t.Fatalf("expected no error for valid FLAT_FEE request, got: %v", err)
	}
}

func TestCreatePriceRequest_Validate_AcceptsTieredWithValidTiers(t *testing.T) {
	req := validFlatFeeRequest()
	req.BillingModel = types.BILLING_MODEL_TIERED
	req.TierMode = types.BILLING_TIER_VOLUME

	upTo := uint64(1000)
	tier1UnitAmount := decimal.NewFromFloat(0.50)
	tier2UnitAmount := decimal.NewFromFloat(0.30)

	req.Tiers = []dto.CreatePriceTier{
		{
			UpTo:       &upTo,
			UnitAmount: tier1UnitAmount,
		},
		{
			UpTo:       nil, // last tier has no upper bound
			UnitAmount: tier2UnitAmount,
		},
	}
	req.Amount = nil // TIERED does not use Amount

	if err := req.Validate(); err != nil {
		t.Fatalf("expected no error for valid TIERED request, got: %v", err)
	}
}

func TestCreatePriceRequest_Validate_AcceptsPackageWithTransformQuantity(t *testing.T) {
	req := validFlatFeeRequest()
	req.BillingModel = types.BILLING_MODEL_PACKAGE
	req.TransformQuantity = &price.TransformQuantity{
		DivideBy: 10,
		Round:    types.ROUND_UP,
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("expected no error for valid PACKAGE request, got: %v", err)
	}
}

func TestCreatePriceRequest_Validate_PackageTiers(t *testing.T) {
	d := decimal.RequireFromString
	upTo := func(v uint64) *uint64 { return &v }
	pkg := func(size int) *price.TransformQuantity { return &price.TransformQuantity{DivideBy: size} }
	flat := func(v string) *decimal.Decimal { a := d(v); return &a }
	tieredRequest := func(mode types.BillingTier, tiers ...dto.CreatePriceTier) dto.CreatePriceRequest {
		req := validFlatFeeRequest()
		req.Amount = nil
		req.BillingModel = types.BILLING_MODEL_TIERED
		req.TierMode = mode
		req.Tiers = tiers
		return req
	}
	slabCustomer := []dto.CreatePriceTier{
		{UpTo: upTo(200), UnitAmount: d("0"), FlatAmount: flat("10000")},
		{UpTo: upTo(500), UnitAmount: d("0"), FlatAmount: flat("20000")},
		{UpTo: upTo(1000), UnitAmount: d("0"), FlatAmount: flat("40000")},
		{UnitAmount: d("1000"), TransformQuantity: pkg(100)},
	}

	tests := []struct {
		name    string
		req     func() dto.CreatePriceRequest
		wantErr bool
	}{
		{"divide_by 0", func() dto.CreatePriceRequest {
			return tieredRequest(types.BILLING_TIER_SLAB, dto.CreatePriceTier{UnitAmount: d("1"), TransformQuantity: pkg(0)})
		}, true},
		{"divide_by 0 in price unit tiers", func() dto.CreatePriceRequest {
			req := tieredRequest(types.BILLING_TIER_SLAB)
			req.PriceUnitType = types.PRICE_UNIT_TYPE_CUSTOM
			req.PriceUnitConfig = &dto.PriceUnitConfig{
				PriceUnit:      "BTC",
				PriceUnitTiers: []dto.CreatePriceTier{{UnitAmount: d("1"), TransformQuantity: pkg(0)}},
			}
			return req
		}, true},
		{"invalid round", func() dto.CreatePriceRequest {
			return tieredRequest(types.BILLING_TIER_SLAB, dto.CreatePriceTier{
				UnitAmount:        d("1"),
				TransformQuantity: &price.TransformQuantity{DivideBy: 100, Round: "nearest"},
			})
		}, true},
		{"tiers on FLAT_FEE", func() dto.CreatePriceRequest {
			req := validFlatFeeRequest()
			req.Tiers = []dto.CreatePriceTier{{UnitAmount: d("1")}}
			return req
		}, true},
		{"slab customer config", func() dto.CreatePriceRequest {
			return tieredRequest(types.BILLING_TIER_SLAB, slabCustomer...)
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.req()
			err := req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestCreatePriceRequest_ToPrice_TierRoundDefaultsUp(t *testing.T) {
	req := validFlatFeeRequest()
	req.Amount = nil
	req.BillingModel = types.BILLING_MODEL_TIERED
	req.TierMode = types.BILLING_TIER_SLAB
	req.Tiers = []dto.CreatePriceTier{
		{UnitAmount: decimal.NewFromInt(1)},
		{UnitAmount: decimal.NewFromInt(1), TransformQuantity: &price.TransformQuantity{DivideBy: 100}},
		{UnitAmount: decimal.NewFromInt(1), TransformQuantity: &price.TransformQuantity{DivideBy: 100, Round: types.ROUND_DOWN}},
	}

	p, err := req.ToPrice(context.Background())
	require.NoError(t, err)
	assert.Nil(t, p.Tiers[0].TransformQuantity)
	assert.Equal(t, types.ROUND_UP, p.Tiers[1].TransformQuantity.Round)
	assert.Equal(t, types.ROUND_DOWN, p.Tiers[2].TransformQuantity.Round)
	assert.Empty(t, req.Tiers[1].TransformQuantity.Round)
}

func TestCreatePriceRequest_ToPrice_DefaultStartDate(t *testing.T) {
	tests := []struct {
		name          string
		mutate        func(r *dto.CreatePriceRequest)
		wantFixedDate bool
	}{
		{name: "recurring fixed defaults to fixed past date", mutate: func(r *dto.CreatePriceRequest) {}, wantFixedDate: true},
		{name: "onetime fixed defaults to fixed past date", mutate: func(r *dto.CreatePriceRequest) {
			r.BillingPeriod = types.BILLING_PERIOD_ONETIME
		}, wantFixedDate: true},
		{name: "usage defaults to now", mutate: func(r *dto.CreatePriceRequest) {
			r.Type = types.PRICE_TYPE_USAGE
			r.MeterID = "meter_test_123"
			r.InvoiceCadence = types.InvoiceCadenceArrear
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validFlatFeeRequest()
			tt.mutate(&req)
			before := time.Now().UTC()

			p, err := req.ToPrice(context.Background())
			require.NoError(t, err)
			require.NotNil(t, p.StartDate)
			if tt.wantFixedDate {
				assert.True(t, p.StartDate.Equal(price.DefaultFixedPriceStartDate()))
				assert.Len(t, req.StartDateWarnings(), 1)
				return
			}
			assert.False(t, p.StartDate.Before(before))
			assert.Empty(t, req.StartDateWarnings())
		})
	}
}

func TestCreatePriceRequest_ToPrice_ExplicitStartDateKept(t *testing.T) {
	req := validFlatFeeRequest()
	start := time.Date(2026, time.October, 21, 18, 30, 0, 0, time.UTC)
	req.StartDate = &start

	p, err := req.ToPrice(context.Background())
	require.NoError(t, err)
	assert.True(t, p.StartDate.Equal(start))
	assert.Empty(t, req.StartDateWarnings())
}
