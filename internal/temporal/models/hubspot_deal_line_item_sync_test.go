package models

import "testing"

func TestHubSpotDealLineItemSyncWorkflowInput_Validate(t *testing.T) {
	tests := []struct {
		name    string
		input   HubSpotDealLineItemSyncWorkflowInput
		wantErr bool
	}{
		{
			name: "valid created",
			input: HubSpotDealLineItemSyncWorkflowInput{
				SubscriptionID: "sub_1", CustomerID: "cust_1", DealID: "deal_1",
				LineItemID: "li_1", Operation: HubSpotLineItemSyncOperationCreated,
				TenantID: "tenant_1", EnvironmentID: "env_1",
			},
			wantErr: false,
		},
		{
			name: "missing operation",
			input: HubSpotDealLineItemSyncWorkflowInput{
				SubscriptionID: "sub_1", CustomerID: "cust_1", DealID: "deal_1",
				LineItemID: "li_1", TenantID: "tenant_1", EnvironmentID: "env_1",
			},
			wantErr: true,
		},
		{
			name: "missing line_item_id",
			input: HubSpotDealLineItemSyncWorkflowInput{
				SubscriptionID: "sub_1", CustomerID: "cust_1", DealID: "deal_1",
				Operation: HubSpotLineItemSyncOperationDeleted,
				TenantID:  "tenant_1", EnvironmentID: "env_1",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}
