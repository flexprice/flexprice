package types

import "testing"

func TestIntegrationEntityType_SubscriptionLineItem_Validates(t *testing.T) {
	if err := IntegrationEntityTypeSubscriptionLineItem.Validate(); err != nil {
		t.Fatalf("expected IntegrationEntityTypeSubscriptionLineItem to validate, got: %v", err)
	}
}
