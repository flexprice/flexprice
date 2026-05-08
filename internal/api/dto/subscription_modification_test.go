package dto_test

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/stretchr/testify/require"
)

func TestExecuteSubscriptionModifyRequest_Validate_GroupedInvoicing(t *testing.T) {
	tests := []struct {
		name    string
		req     dto.ExecuteSubscriptionModifyRequest
		wantErr bool
	}{
		{
			name: "grouped_invoicing_add with valid params",
			req: dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeGroupedInvoicingAdd,
				GroupedInvoicingParams: &dto.SubModifyGroupedInvoicingParams{
					ParentSubscriptionID: "parent_123",
					ChildSubscriptionIDs: []string{"child_1", "child_2"},
				},
			},
		},
		{
			name: "grouped_invoicing_add missing parent_subscription_id",
			req: dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeGroupedInvoicingAdd,
				GroupedInvoicingParams: &dto.SubModifyGroupedInvoicingParams{
					ChildSubscriptionIDs: []string{"child_1"},
				},
			},
			wantErr: true,
		},
		{
			name: "grouped_invoicing_add missing params",
			req: dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeGroupedInvoicingAdd,
			},
			wantErr: true,
		},
		{
			name: "grouped_invoicing_remove with valid params — parent not required",
			req: dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeGroupedInvoicingRemove,
				GroupedInvoicingParams: &dto.SubModifyGroupedInvoicingParams{
					ChildSubscriptionIDs: []string{"child_1"},
				},
			},
		},
		{
			name: "grouped_invoicing_remove missing child_subscription_ids",
			req: dto.ExecuteSubscriptionModifyRequest{
				Type:                   dto.SubscriptionModifyTypeGroupedInvoicingRemove,
				GroupedInvoicingParams: &dto.SubModifyGroupedInvoicingParams{},
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
