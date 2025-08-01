package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

func main() {
	// Example of creating a subscription with addons
	subscriptionRequest := dto.CreateSubscriptionRequest{
		CustomerID:         "cust_123",
		PlanID:             "plan_456",
		Currency:           "USD",
		StartDate:          time.Now(),
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		Addons: []dto.SubscriptionAddonRequest{
			{
				AddonID:           "addon_789",
				StartDate:         nil, // Will use current time
				ProrationBehavior: types.ProrationBehaviorCreateProrations,
				Metadata: map[string]interface{}{
					"source": "subscription_creation",
				},
			},
			{
				AddonID:           "addon_101",
				StartDate:         &time.Time{}, // Will be set to current time
				ProrationBehavior: types.ProrationBehaviorNone,
				Metadata: map[string]interface{}{
					"priority": "high",
				},
			},
		},
	}

	// Validate the request
	if err := subscriptionRequest.Validate(); err != nil {
		fmt.Printf("Validation error: %v\n", err)
		return
	}

	// Convert to subscription domain model
	subscription := subscriptionRequest.ToSubscription(nil)
	fmt.Printf("Subscription ID: %s\n", subscription.ID)

	// Show the addon requests
	for i, addonReq := range subscriptionRequest.Addons {
		fmt.Printf("Addon %d:\n", i+1)
		fmt.Printf("  AddonID: %s\n", addonReq.AddonID)
		fmt.Printf("  StartDate: %v\n", addonReq.StartDate)
		fmt.Printf("  ProrationBehavior: %s\n", addonReq.ProrationBehavior)

		// Convert to domain model
		subscriptionAddon := addonReq.ToDomain(nil, subscription.ID)
		fmt.Printf("  SubscriptionAddon ID: %s\n", subscriptionAddon.ID)
		fmt.Printf("  Status: %s\n", subscriptionAddon.AddonStatus)
	}

	// Example JSON output
	jsonData, _ := json.MarshalIndent(subscriptionRequest, "", "  ")
	fmt.Printf("\nJSON Request:\n%s\n", string(jsonData))
}
