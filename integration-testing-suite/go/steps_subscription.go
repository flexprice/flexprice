package main

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/go-sdk/v2/models/types"
)

// runSubscriptionSteps executes Phase 3: Customer & Subscription.
func (r *SanityRunner) runSubscriptionSteps(ctx context.Context) {
	r.setPhase("PHASE 3: Customer & Subscription")
	r.printPhaseHeader(r.phase)

	// ── Create Customer ─────────────────────────────────────────────────
	// SDK: client.Customers.CreateCustomer(ctx, types.CreateCustomerRequest{...})

	r.run("Create Customer", "Customers.CreateCustomer", false, func() error {
		r.externalCustID = fmt.Sprintf("sanity-cust-%d", ts())

		req := types.CreateCustomerRequest{
			ExternalID: r.externalCustID,
			Name:       strPtr(fmt.Sprintf("Sanity Test Customer %d", ts())),
			Email:      strPtr(fmt.Sprintf("sanity-%d@test.flexprice.io", ts())),
			Metadata:   map[string]string{"source": "sanity_test"},
		}

		resp, err := r.client.Customers.CreateCustomer(ctx, req)
		if err != nil {
			return err
		}
		customer := resp.CustomerResponse
		if customer == nil || customer.ID == nil {
			return fmt.Errorf("create customer returned no body")
		}
		r.customerID = *customer.ID
		r.lastResult().EntityID = *customer.ID
		r.lastResult().Details = fmt.Sprintf("cust_id=%s, external_id=%s", *customer.ID, r.externalCustID)
		return nil
	})

	// ── Create Subscription ─────────────────────────────────────────────
	// SDK: client.Subscriptions.CreateSubscription(ctx, types.CreateSubscriptionRequest{...})

	if !r.require(r.customerID, "Create Customer", "Create Subscription") ||
		!r.require(r.planID, "Create Plan", "Create Subscription") {
		r.skip("Verify Subscription Active", "depends on Create Subscription")
		r.skip("Verify Subscription Entitlements", "depends on Create Subscription")
		r.skip("Verify Customer Entitlements", "depends on Create Subscription")
		return
	}

	r.run("Create Subscription", "Subscriptions.CreateSubscription", false, func() error {
		startDate := time.Now()
		billingCycle := types.BillingCycleAnniversary

		req := types.CreateSubscriptionRequest{
			CustomerID:         strPtr(r.customerID),
			PlanID:             r.planID,
			Currency:           "usd",
			BillingPeriod:      types.BillingPeriodMonthly,
			BillingPeriodCount: int64Ptr(1),
			BillingCycle:       &billingCycle,
			StartDate:          &startDate,
			Metadata:           map[string]string{"source": "sanity_test"},
		}

		// Attach coupon if available.
		if r.couponID != "" {
			req.Coupons = []string{r.couponID}
		}

		// Attach tax rate override if available (uses code + currency, both required).
		if r.taxRateCode != "" {
			req.TaxRateOverrides = []types.TaxRateOverride{
				{
					TaxRateCode: r.taxRateCode,
					Currency:    "usd",
					AutoApply:   boolPtr(true),
					Priority:    int64Ptr(1),
				},
			}
		}

		resp, err := r.client.Subscriptions.CreateSubscription(ctx, req)
		if err != nil {
			return err
		}
		sub := resp.SubscriptionResponse
		if sub == nil || sub.ID == nil {
			return fmt.Errorf("create subscription returned no body")
		}
		r.subscriptionID = *sub.ID
		r.lastResult().EntityID = *sub.ID

		details := fmt.Sprintf("sub_id=%s, plan=%s, customer=%s", *sub.ID, r.planID, r.customerID)
		if r.couponID != "" {
			details += ", coupon=" + r.couponID
		}
		if r.taxRateID != "" {
			details += ", tax=" + r.taxRateCode
		}
		r.lastResult().Details = details
		return nil
	})

	// ── Verify Subscription Active ──────────────────────────────────────
	// SDK: client.Subscriptions.GetSubscription(ctx, subID)

	if !r.require(r.subscriptionID, "Create Subscription", "Verify Subscription Active") {
		r.skip("Verify Subscription Entitlements", "depends on subscription")
		r.skip("Verify Customer Entitlements", "depends on subscription")
		return
	}

	r.run("Verify Subscription Active", "Subscriptions.GetSubscription", false, func() error {
		resp, err := r.client.Subscriptions.GetSubscription(ctx, r.subscriptionID)
		if err != nil {
			return err
		}
		sub := resp.SubscriptionResponse
		if sub == nil {
			return fmt.Errorf("get subscription returned no body")
		}

		status := ""
		if sub.SubscriptionStatus != nil {
			status = string(*sub.SubscriptionStatus)
		}

		// If DRAFT, activate it.
		if status == "draft" || status == "DRAFT" {
			activateReq := types.ActivateDraftSubscriptionRequest{
				StartDate: time.Now(),
			}
			_, err := r.client.Subscriptions.ActivateSubscription(ctx, r.subscriptionID, activateReq)
			if err != nil {
				return fmt.Errorf("activate subscription: %w", err)
			}
			// Re-fetch to confirm.
			resp, err = r.client.Subscriptions.GetSubscription(ctx, r.subscriptionID)
			if err != nil {
				return fmt.Errorf("re-fetch after activate: %w", err)
			}
			sub = resp.SubscriptionResponse
			if sub != nil && sub.SubscriptionStatus != nil {
				status = string(*sub.SubscriptionStatus)
			}
		}

		if status != "active" && status != "ACTIVE" {
			return fmt.Errorf("expected subscription status ACTIVE, got %s", status)
		}

		r.lastResult().Details = "status=ACTIVE"
		return nil
	})

	// ── Verify Subscription Entitlements ─────────────────────────────────
	// SDK: client.Subscriptions.GetSubscriptionEntitlements(ctx, subID, featureIds)

	r.run("Verify Subscription Entitlements", "Subscriptions.GetSubscriptionEntitlements", false, func() error {
		resp, err := r.client.Subscriptions.GetSubscriptionEntitlements(ctx, r.subscriptionID, nil)
		if err != nil {
			return err
		}
		entResp := resp.SubscriptionEntitlementsResponse
		if entResp == nil {
			return fmt.Errorf("get subscription entitlements returned no body")
		}

		features := entResp.Features
		if len(features) == 0 {
			return fmt.Errorf("expected at least 1 entitlement on subscription, got 0")
		}

		found := false
		for _, af := range features {
			if af.Feature != nil && af.Feature.ID != nil && *af.Feature.ID == r.featureAID {
				found = true
				details := "Feature A entitlement found"
				if af.Entitlement != nil && af.Entitlement.UsageLimit != nil {
					details += fmt.Sprintf(", limit=%d", *af.Entitlement.UsageLimit)
				}
				r.lastResult().Details = details
				break
			}
		}
		if !found {
			r.lastResult().Details = fmt.Sprintf("features count=%d, looking for feature_id=%s", len(features), r.featureAID)
			return fmt.Errorf("Feature A entitlement not inherited to subscription")
		}
		return nil
	})

	// ── Verify Customer Entitlements ────────────────────────────────────
	// SDK: client.Customers.GetCustomerEntitlements(ctx, customerID)

	r.run("Verify Customer Entitlements", "Customers.GetCustomerEntitlements", false, func() error {
		resp, err := r.client.Customers.GetCustomerEntitlements(ctx, r.customerID)
		if err != nil {
			return err
		}
		entResp := resp.CustomerEntitlementsResponse
		if entResp == nil {
			return fmt.Errorf("get customer entitlements returned no body")
		}

		features := entResp.Features
		if len(features) == 0 {
			return fmt.Errorf("expected at least 1 customer entitlement, got 0")
		}

		found := false
		for _, af := range features {
			if af.Feature != nil && af.Feature.ID != nil && *af.Feature.ID == r.featureAID {
				found = true
				r.lastResult().Details = "Feature A entitlement visible on customer"
				break
			}
		}
		if !found {
			return fmt.Errorf("Feature A entitlement not visible on customer")
		}
		return nil
	})
}
