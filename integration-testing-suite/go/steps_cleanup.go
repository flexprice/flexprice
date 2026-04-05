package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/flexprice/go-sdk/v2/models/types"
)

// runCleanupSteps executes Phase 7: Cleanup.
// Deletes all entities created during the test in correct dependency order.
// Cleanup failures are reported honestly as [FAIL] but do NOT cause non-zero exit.
func (r *SanityRunner) runCleanupSteps(ctx context.Context) {
	r.setPhase("PHASE 7: Cleanup")
	r.printPhaseHeader(r.phase)

	// ── 1. Void invoice ──────────────────────────────────────────────────
	if r.invoiceID != "" {
		r.run("Cleanup: Void Invoice", "Invoices.VoidInvoice", false, func() error {
			_, err := r.client.Invoices.VoidInvoice(ctx, r.invoiceID)
			if err != nil {
				if strings.Contains(err.Error(), "already voided") || strings.Contains(err.Error(), "VOIDED") {
					r.lastResult().Details = fmt.Sprintf("invoice_id=%s, already voided", r.invoiceID)
					return nil
				}
				return fmt.Errorf("void invoice: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("invoice_id=%s, voided", r.invoiceID)
			return nil
		})
	}

	// ── 2. Cancel subscription ───────────────────────────────────────────
	if r.subscriptionID != "" && !r.subscriptionCancelled {
		r.run("Cleanup: Cancel Subscription", "Subscriptions.CancelSubscription", false, func() error {
			req := types.CancelSubscriptionRequest{
				CancellationType: types.CancellationTypeImmediate,
			}
			_, err := r.client.Subscriptions.CancelSubscription(ctx, r.subscriptionID, req)
			if err != nil {
				errStr := err.Error()
				if strings.Contains(errStr, "already cancelled") ||
					strings.Contains(errStr, "CANCELLED") ||
					strings.Contains(errStr, "cancelled") ||
					strings.Contains(errStr, "not active") {
					r.lastResult().Details = fmt.Sprintf("sub_id=%s, already cancelled (expected)", r.subscriptionID)
					return nil
				}
				return fmt.Errorf("cancel subscription: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("sub_id=%s, cancelled", r.subscriptionID)
			return nil
		})
	}

	// ── 3. Delete entitlement ────────────────────────────────────────────
	if r.entitlementID != "" {
		r.run("Cleanup: Delete Entitlement", "Entitlements.DeleteEntitlement", false, func() error {
			_, err := r.client.Entitlements.DeleteEntitlement(ctx, r.entitlementID)
			if err != nil {
				return fmt.Errorf("delete entitlement: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("ent_id=%s, deleted", r.entitlementID)
			return nil
		})
	}

	// ── 4. Delete prices ─────────────────────────────────────────────────
	priceIDs := []struct {
		id   string
		name string
	}{
		{r.priceUsageB, "usage price B"},
		{r.priceUsageA, "usage price A"},
		{r.priceRecurr2, "recurring price 2"},
		{r.priceRecurr1, "recurring price 1"},
	}
	for _, p := range priceIDs {
		if p.id == "" {
			continue
		}
		priceID := p.id
		priceName := p.name
		r.run(fmt.Sprintf("Cleanup: Delete Price (%s)", priceName), "Prices.DeletePrice", false, func() error {
			deleteReq := types.DeletePriceRequest{}
			_, err := r.client.Prices.DeletePrice(ctx, priceID, deleteReq)
			if err != nil {
				return fmt.Errorf("delete price %s: %w", priceName, err)
			}
			r.lastResult().Details = fmt.Sprintf("price_id=%s, deleted", priceID)
			return nil
		})
	}

	// ── 5. Delete plan ───────────────────────────────────────────────────
	if r.planID != "" {
		r.run("Cleanup: Delete Plan", "Plans.DeletePlan", false, func() error {
			_, err := r.client.Plans.DeletePlan(ctx, r.planID)
			if err != nil {
				return fmt.Errorf("delete plan: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("plan_id=%s, deleted", r.planID)
			return nil
		})
	}

	// ── 6. Delete features ───────────────────────────────────────────────
	for _, fid := range []string{r.featureBID, r.featureAID} {
		if fid == "" {
			continue
		}
		featureID := fid
		r.run("Cleanup: Delete Feature", "Features.DeleteFeature", false, func() error {
			_, err := r.client.Features.DeleteFeature(ctx, featureID)
			if err != nil {
				return fmt.Errorf("delete feature: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("feat_id=%s, deleted", featureID)
			return nil
		})
	}

	// ── 7. Terminate wallet(s) for customer ──────────────────────────────
	if r.walletID != "" {
		r.run("Cleanup: Terminate Wallet", "Wallets.TerminateWallet", false, func() error {
			_, err := r.client.Wallets.TerminateWallet(ctx, r.walletID)
			if err != nil {
				if strings.Contains(err.Error(), "already terminated") || strings.Contains(err.Error(), "TERMINATED") {
					r.lastResult().Details = fmt.Sprintf("wallet_id=%s, already terminated", r.walletID)
					return nil
				}
				return fmt.Errorf("terminate wallet: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("wallet_id=%s, terminated", r.walletID)
			return nil
		})
	}

	// ── 8. Delete customer ───────────────────────────────────────────────
	if r.customerID != "" {
		r.run("Cleanup: Delete Customer", "Customers.DeleteCustomer", false, func() error {
			_, err := r.client.Customers.DeleteCustomer(ctx, r.customerID)
			if err != nil {
				return fmt.Errorf("delete customer: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("cust_id=%s, deleted", r.customerID)
			return nil
		})
	}

	// ── 9. Delete coupon ─────────────────────────────────────────────────
	if r.couponID != "" {
		r.run("Cleanup: Delete Coupon", "Coupons.DeleteCoupon", false, func() error {
			_, err := r.client.Coupons.DeleteCoupon(ctx, r.couponID)
			if err != nil {
				return fmt.Errorf("delete coupon: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("coupon_id=%s, deleted", r.couponID)
			return nil
		})
	}

	// ── 10. Delete tax rate ──────────────────────────────────────────────
	if r.taxRateID != "" {
		r.run("Cleanup: Delete Tax Rate", "TaxRates.DeleteTaxRate", false, func() error {
			_, err := r.client.TaxRates.DeleteTaxRate(ctx, r.taxRateID)
			if err != nil {
				return fmt.Errorf("delete tax rate: %w", err)
			}
			r.lastResult().Details = fmt.Sprintf("tax_id=%s, deleted", r.taxRateID)
			return nil
		})
	}

	// ── 11. Delete groups ────────────────────────────────────────────────
	// SDK v2.0.16: Backend now returns 204, SDK expects 204. Both bugs fixed.
	for _, gid := range []string{r.priceGroupID, r.featureGroupID} {
		if gid == "" {
			continue
		}
		groupID := gid
		r.run("Cleanup: Delete Group", "Groups.DeleteGroup", false, func() error {
			// Primary: SDK DeleteGroup — should work with v2.0.16 (backend returns 204).
			_, sdkErr := r.client.Groups.DeleteGroup(ctx, groupID)
			if sdkErr == nil {
				r.lastResult().Details = fmt.Sprintf("group_id=%s, deleted", groupID)
				return nil
			}

			// Diagnostic fallback: raw HTTP to determine if it's an SDK bug or server bug.
			r.markSDKFallback("Groups.DeleteGroup", sdkErr)

			_, rawErr := r.raw.Delete(ctx, fmt.Sprintf("/groups/%s", groupID))
			if rawErr != nil {
				return fmt.Errorf("delete group — SDK: %v | raw HTTP: %w", sdkErr, rawErr)
			}
			r.lastResult().Details += fmt.Sprintf("\n        → group_id=%s, deleted via raw HTTP (SDK BROKEN)", groupID)
			return nil
		})
	}
}
