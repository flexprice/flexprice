package dto

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCreateInvoiceLineItemRequest_BillingScheduleSourceIsServerOnly(t *testing.T) {
	for _, key := range []string{"billing_schedule_item_id", "BillingScheduleItemID", "billingScheduleItemID"} {
		t.Run(key, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{key: "bsi_untrusted", "amount": "25", "quantity": "1"})
			require.NoError(t, err)
			var request CreateInvoiceLineItemRequest
			require.NoError(t, json.Unmarshal(payload, &request))
			require.Nil(t, request.BillingScheduleItemID)
			require.True(t, request.Amount.Equal(decimal.NewFromInt(25)))
			require.Nil(t, request.ToInvoiceLineItem(context.Background(), &invoice.Invoice{Currency: "usd"}).BillingScheduleItemID)

			request.BillingScheduleItemID = lo.ToPtr("bsi_trusted")
			require.NoError(t, json.Unmarshal(payload, &request))
			require.Equal(t, "bsi_trusted", *request.BillingScheduleItemID)
		})
	}
}

func TestCreateInvoiceLineItemRequest_ToInvoiceLineItemPreservesBillingScheduleSource(t *testing.T) {
	ctx := types.SetTenantID(context.Background(), "tenant_source")
	ctx = types.SetEnvironmentID(ctx, "env_source")
	inv := &invoice.Invoice{ID: "inv_source", CustomerID: "customer_source", Currency: "usd"}
	for _, source := range []*string{nil, lo.ToPtr("bsi_source")} {
		name := "ordinary line"
		if source != nil {
			name = "scheduled line"
		}
		t.Run(name, func(t *testing.T) {
			request := CreateInvoiceLineItemRequest{Amount: decimal.NewFromInt(25), Quantity: decimal.NewFromInt(1), BillingScheduleItemID: source}
			got := request.ToInvoiceLineItem(ctx, inv)
			require.Equal(t, source, got.BillingScheduleItemID)
			require.Equal(t, inv.ID, got.InvoiceID)
			require.Equal(t, inv.CustomerID, got.CustomerID)
			require.Equal(t, "tenant_source", got.TenantID)
			require.Equal(t, "env_source", got.EnvironmentID)

			payload, err := json.Marshal(request)
			require.NoError(t, err)
			require.NotContains(t, string(payload), "billing_schedule_item_id")
			require.NotContains(t, string(payload), "bsi_source")
		})
	}
}
