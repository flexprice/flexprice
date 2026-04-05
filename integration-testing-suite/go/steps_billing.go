package main

import (
	"context"
	"fmt"

	"github.com/flexprice/go-sdk/v2/models/types"
)

// runBillingSteps executes Phase 2: Entitlements & Billing Entities.
func (r *SanityRunner) runBillingSteps(ctx context.Context) {
	r.setPhase("PHASE 2: Entitlements & Billing Entities")
	r.printPhaseHeader(r.phase)

	// ── Create Entitlement for Feature A on Plan ────────────────────────
	// SDK: client.Entitlements.CreateEntitlement(ctx, types.CreateEntitlementRequest{...})

	if !r.require(r.planID, "Create Plan", "Create Entitlement (Feature A)") ||
		!r.require(r.featureAID, "Feature A", "Create Entitlement (Feature A)") {
		r.skip("Verify Plan Entitlements", "depends on entitlement creation")
		goto skipTax
	}

	r.run("Create Entitlement (Feature A)", "Entitlements.CreateEntitlement", false, func() error {
		usageReset := types.EntitlementUsageResetPeriodMonthly
		req := types.CreateEntitlementRequest{
			FeatureID:   r.featureAID,
			FeatureType: types.FeatureTypeMetered,
			PlanID:      strPtr(r.planID),
			IsEnabled:   boolPtr(true),
			UsageLimit:  int64Ptr(1000),
			UsageResetPeriod: &usageReset,
			IsSoftLimit: boolPtr(true),
			EntityType:  types.EntitlementEntityTypePlan.ToPointer(),
			EntityID:    strPtr(r.planID),
		}

		resp, err := r.client.Entitlements.CreateEntitlement(ctx, req)
		if err != nil {
			return err
		}
		if resp == nil {
			return fmt.Errorf("create entitlement returned nil response")
		}
		ent := resp.EntitlementResponse
		if ent == nil || ent.ID == nil {
			return fmt.Errorf("create entitlement returned no body")
		}
		r.entitlementID = *ent.ID
		r.lastResult().EntityID = *ent.ID
		r.lastResult().Details = fmt.Sprintf("ent_id=%s, plan-level, metered, limit=1000 tokens, soft limit", *ent.ID)
		return nil
	})

	// ── Verify Plan Entitlements ────────────────────────────────────────
	// SDK: client.Entitlements.GetPlanEntitlements(ctx, planID)
	// SDK v2.0.16: Swagger fixed to return dto.ListEntitlementsResponse (was PlanResponse).

	r.run("Verify Plan Entitlements", "Entitlements.GetPlanEntitlements", false, func() error {
		// Primary: SDK GetPlanEntitlements — v2.0.16 returns DtoListEntitlementsResponse.
		sdkErr := func() error {
			resp, err := r.client.Entitlements.GetPlanEntitlements(ctx, r.planID)
			if err != nil {
				return fmt.Errorf("SDK GetPlanEntitlements call failed: %w", err)
			}
			if resp == nil {
				return fmt.Errorf("SDK GetPlanEntitlements returned nil response")
			}
			list := resp.ListEntitlementsResponse
			if list == nil || len(list.Items) == 0 {
				return fmt.Errorf("SDK GetPlanEntitlements returned 0 entitlements for plan %s", r.planID)
			}
			for _, ent := range list.Items {
				if ent.FeatureID != nil && *ent.FeatureID == r.featureAID {
					limit := int64(0)
					if ent.UsageLimit != nil {
						limit = *ent.UsageLimit
					}
					r.lastResult().Details = fmt.Sprintf("found Feature A entitlement via SDK, limit=%d", limit)
					return nil
				}
			}
			return fmt.Errorf("Feature A not found in %d entitlements", len(list.Items))
		}()
		if sdkErr == nil {
			return nil
		}

		// Diagnostic fallback: raw HTTP to determine if it's an SDK bug or server bug.
		r.markSDKFallback("Entitlements.GetPlanEntitlements", sdkErr)

		rawResp, _, err := r.raw.Get(ctx, fmt.Sprintf("/plans/%s/entitlements", r.planID))
		if err != nil {
			return fmt.Errorf("SDK failed: %v | raw HTTP also failed: %w", sdkErr, err)
		}

		items := getSlice(rawResp, "items")
		if len(items) == 0 {
			return fmt.Errorf("SDK failed: %v | raw HTTP returned 0 entitlements for plan %s", sdkErr, r.planID)
		}

		found := false
		for _, item := range items {
			if ent, ok := item.(map[string]interface{}); ok {
				if getString(ent, "feature_id") == r.featureAID {
					found = true
					limit := getFloat(ent, "usage_limit")
					r.lastResult().Details += fmt.Sprintf("\n        → found Feature A entitlement via raw HTTP (SDK BROKEN), limit=%.0f", limit)
					break
				}
			}
		}
		if !found {
			return fmt.Errorf("entitlement for Feature A (%s) not found on plan via any method", r.featureAID)
		}
		return nil
	})

skipTax:
	// ── Create Tax Rate ─────────────────────────────────────────────────
	// SDK: client.TaxRates.CreateTaxRate(ctx, types.CreateTaxRateRequest{...})

	r.run("Create Tax Rate (18% GST)", "TaxRates.CreateTaxRate", false, func() error {
		taxCode := fmt.Sprintf("GST18_%d", ts())
		taxRateType := types.TaxRateTypePercentage
		scope := types.TaxRateScopeInternal

		req := types.CreateTaxRateRequest{
			Name:            fmt.Sprintf("GST 18%% %d", ts()),
			Code:            taxCode,
			PercentageValue: strPtr("18.00"),
			TaxRateType:     &taxRateType,
			Description:     strPtr("Goods and Services Tax"),
			Scope:           &scope,
		}

		resp, err := r.client.TaxRates.CreateTaxRate(ctx, req)
		if err != nil {
			return err
		}
		if resp == nil {
			return fmt.Errorf("create tax rate returned nil response")
		}
		taxRate := resp.TaxRateResponse
		if taxRate == nil || taxRate.ID == nil {
			return fmt.Errorf("create tax rate returned no body")
		}
		r.taxRateID = *taxRate.ID
		r.taxRateCode = taxCode
		r.lastResult().EntityID = *taxRate.ID
		r.lastResult().Details = fmt.Sprintf("tax_id=%s, code=%s, rate=18%%", *taxRate.ID, taxCode)
		return nil
	})

	// ── Create Coupon ───────────────────────────────────────────────────
	// SDK: client.Coupons.CreateCoupon(ctx, types.CreateCouponRequest{...})

	r.run("Create Coupon (10% off)", "Coupons.CreateCoupon", false, func() error {
		req := types.CreateCouponRequest{
			Name:          fmt.Sprintf("Sanity 10pct Off %d", ts()),
			Type:          types.CouponTypePercentage,
			Cadence:       types.CouponCadenceOnce,
			PercentageOff: strPtr("10"),
		}

		resp, err := r.client.Coupons.CreateCoupon(ctx, req)
		if err != nil {
			return err
		}
		if resp == nil {
			return fmt.Errorf("create coupon returned nil response")
		}
		coupon := resp.Coupon
		if coupon == nil || coupon.ID == nil {
			return fmt.Errorf("create coupon returned no body")
		}
		r.couponID = *coupon.ID
		r.lastResult().EntityID = *coupon.ID
		r.lastResult().Details = fmt.Sprintf("coupon_id=%s, type=percentage, 10%% off", *coupon.ID)
		return nil
	})
}
