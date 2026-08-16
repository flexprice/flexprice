package price

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestBillsIdenticallyTo_SeparatesPricesThatOnlyLookAlike(t *testing.T) {
	base := func() *Price {
		return &Price{
			Amount:             decimal.NewFromInt(20),
			Currency:           "usd",
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			Type:               types.PRICE_TYPE_FIXED,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			InvoiceCadence:     types.InvoiceCadenceAdvance,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
		}
	}

	assert.True(t, base().BillsIdenticallyTo(base()), "two prices that bill the same way are the same service")

	pkgA, pkgB := base(), base()
	pkgA.BillingModel, pkgB.BillingModel = types.BILLING_MODEL_PACKAGE, types.BILLING_MODEL_PACKAGE
	pkgA.TransformQuantity = JSONBTransformQuantity{DivideBy: 100}
	pkgB.TransformQuantity = JSONBTransformQuantity{DivideBy: 500}
	assert.False(t, pkgA.BillsIdenticallyTo(pkgB),
		"$20 per 100 units and $20 per 500 units are a 5x difference, not the same price")

	curA, curB := base(), base()
	curA.Currency, curB.Currency = "usd", "eur"
	assert.False(t, curA.BillsIdenticallyTo(curB), "20 dollars is not 20 euros")

	// One-time vs recurring is carried by BillingPeriod; BillingCadence has a single
	// value today (BILLING_CADENCE_ONETIME was removed) but is compared for when it does not.
	oneA, oneB := base(), base()
	oneB.BillingPeriod = types.BILLING_PERIOD_ONETIME
	assert.False(t, oneA.BillsIdenticallyTo(oneB), "a one-time 20 is not 20 every month")

	unitA, unitB := base(), base()
	credits := "pu_credits"
	unitB.PriceUnitID = &credits
	assert.False(t, unitA.BillsIdenticallyTo(unitB), "20 credits is not 20 dollars")

	useA, useB := base(), base()
	useA.Type, useB.Type = types.PRICE_TYPE_USAGE, types.PRICE_TYPE_USAGE
	useA.MeterID, useB.MeterID = "meter_1", "meter_1"
	assert.False(t, useA.BillsIdenticallyTo(useB),
		"usage prices can differ by filter_values, which this comparison cannot see")

	tierA, tierB := base(), base()
	tierA.Tiers = []PriceTier{{UnitAmount: decimal.NewFromInt(1)}}
	assert.False(t, tierA.BillsIdenticallyTo(tierB), "a tiered ladder is never assumed equal to a flat fee")

	assert.False(t, (*Price)(nil).BillsIdenticallyTo(base()))
	assert.False(t, base().BillsIdenticallyTo(nil))
}
