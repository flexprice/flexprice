package types

import "testing"

func TestIntegrationEntityType_SubscriptionLineItem_Validates(t *testing.T) {
	if err := IntegrationEntityTypeSubscriptionLineItem.Validate(); err != nil {
		t.Fatalf("expected subscription_line_item to be a valid entity type, got error: %v", err)
	}
	if IntegrationEntityTypeSubscriptionLineItem.String() != "subscription_line_item" {
		t.Fatalf("expected string value 'subscription_line_item', got %q", IntegrationEntityTypeSubscriptionLineItem.String())
	}
}
