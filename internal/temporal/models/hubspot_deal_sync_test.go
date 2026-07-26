package models

import "testing"

func TestHubSpotDealSyncWorkflowInput_Validate(t *testing.T) {
	base := HubSpotDealSyncWorkflowInput{
		SubscriptionID: "sub_1",
		CustomerID:     "cust_1",
		DealID:         "deal_1",
		TenantID:       "tenant_1",
		EnvironmentID:  "env_1",
		LineItemID:     "li_1",
		Operation:      HubSpotLineItemSyncOperationCreated,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("expected valid input to pass, got: %v", err)
	}

	missingLineItem := base
	missingLineItem.LineItemID = ""
	if err := missingLineItem.Validate(); err == nil {
		t.Fatal("expected missing line_item_id to fail validation")
	}

	badOperation := base
	badOperation.Operation = "not_a_real_operation"
	if err := badOperation.Validate(); err == nil {
		t.Fatal("expected an invalid operation to fail validation")
	}
}
