package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseValidCouponRequest returns a minimal CreateCouponRequest that passes
// Validate(), so tests can vary a single field in isolation.
func baseValidCouponRequest() CreateCouponRequest {
	pct := decimal.NewFromInt(10)
	return CreateCouponRequest{
		Name:          "Test Coupon",
		Type:          types.CouponTypePercentage,
		Cadence:       types.CouponCadenceOnce,
		PercentageOff: &pct,
	}
}

// TestCreateCouponRequest_MaxRedemptions covers the VAPT finding that
// max_redemptions had no upper bound (impractically large values accepted).
func TestCreateCouponRequest_MaxRedemptions(t *testing.T) {
	cases := []struct {
		name    string
		max     *int
		wantErr bool
	}{
		{name: "nil is allowed (unlimited)", max: nil, wantErr: false},
		{name: "one is allowed", max: lo.ToPtr(1), wantErr: false},
		{name: "at the limit is allowed", max: lo.ToPtr(MaxCouponRedemptionsLimit), wantErr: false},
		{name: "zero is rejected", max: lo.ToPtr(0), wantErr: true},
		{name: "negative is rejected", max: lo.ToPtr(-5), wantErr: true},
		{name: "just over the limit is rejected", max: lo.ToPtr(MaxCouponRedemptionsLimit + 1), wantErr: true},
		{name: "the VAPT value 100000000000 is rejected", max: lo.ToPtr(100_000_000_000), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := baseValidCouponRequest()
			req.MaxRedemptions = tc.max
			err := req.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMaxCouponRedemptionsLimit(t *testing.T) {
	// Guard against accidental changes to the business cap.
	assert.Equal(t, 1_000_000, MaxCouponRedemptionsLimit)
}
