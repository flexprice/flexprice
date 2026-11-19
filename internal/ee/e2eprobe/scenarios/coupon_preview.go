package scenarios

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
)

// couponPreview previews a clone created with the shared coupon and checks the
// coupon is applied with a positive discount.
type couponPreview struct{}

func newCouponPreview(scenario.Deps) *couponPreview { return &couponPreview{} }

func (*couponPreview) Name() string               { return "coupon-preview" }
func (*couponPreview) Kind() e2eprobe.Kind        { return e2eprobe.KindFlow }
func (*couponPreview) Requires() []e2eprobe.Trait { return []e2eprobe.Trait{e2eprobe.TraitCoupon} }

func (*couponPreview) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	if gap := sc.Persona().Gap(e2eprobe.TraitCoupon); gap != "" {
		return sc.Skip("gap", gap)
	}
	couponID := sc.World().Fixtures().CouponID()
	if couponID == "" {
		return sc.Skip("fixtures", "shared coupon not provisioned")
	}
	eph, err := sc.Clone(ctx)
	if err != nil {
		return sc.Fail("clone", err)
	}
	noteClone(sc, eph)
	inv, err := previewOf(ctx, sc, eph.SubscriptionID())
	if err != nil {
		return sc.Fail("preview", err)
	}
	applied := false
	for _, ca := range inv.CouponApplications {
		if ca.CouponID == nil || *ca.CouponID != couponID {
			continue
		}
		applied = true
		if ca.DiscountedAmount != nil {
			sc.Truth(fmt.Sprintf("shared coupon discount %s is positive", *ca.DiscountedAmount), decimalOf(ca.DiscountedAmount).IsPositive())
		}
	}
	sc.Truth("preview applies the shared coupon", applied)
	return sc.Result("assert_coupon_preview")
}
