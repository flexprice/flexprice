package chargebee

import (
	"math"
	"testing"

	"github.com/chargebee/chargebee-go/v3/enum"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestConvertTiersForChargebee_PackageTier(t *testing.T) {
	tiers := convertTiersForChargebee([]*types.PriceTier{
		{UpTo: lo.ToPtr(uint64(1000)), UnitAmount: decimal.NewFromInt(1)},
		{UnitAmount: decimal.NewFromInt(10), TransformQuantity: &types.TransformQuantity{DivideBy: 100}},
	}, "usd")

	require.Nil(t, tiers[0].PackageSize)
	require.Equal(t, int64(100), lo.FromPtr(tiers[1].PackageSize))
}

func TestBuildCreateTierParams_PackageTier(t *testing.T) {
	params, err := buildCreateTierParams([]ChargebeeTier{
		{StartingUnit: 1, EndingUnit: lo.ToPtr(int64(1000)), Price: 100},
		{StartingUnit: 1001, Price: 1000, PackageSize: lo.ToPtr(int64(100))},
	})
	require.NoError(t, err)

	require.Empty(t, params[0].PricingType)
	require.Nil(t, params[0].PackageSize)
	require.Equal(t, enum.PricingTypePackage, params[1].PricingType)
	require.Equal(t, int32(100), lo.FromPtr(params[1].PackageSize))
}

func TestBuildCreateTierParams_RejectsPackageSizeAboveInt32(t *testing.T) {
	_, err := buildCreateTierParams([]ChargebeeTier{
		{StartingUnit: 1, Price: 1000, PackageSize: lo.ToPtr(int64(math.MaxInt32) + 1)},
	})
	require.Error(t, err)
}
