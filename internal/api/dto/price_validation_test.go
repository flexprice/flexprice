package dto_test

import (
	"testing"

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
