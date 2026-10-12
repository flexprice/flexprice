package activity

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type fakeCustomerResolver struct {
	customers map[string]string
}

func (f fakeCustomerResolver) CustomerID(_ context.Context, t types.SystemEntityType, id string) (string, error) {
	if c, ok := f.customers[string(t)+":"+id]; ok {
		return c, nil
	}
	return "", errors.New("parent not found")
}

func TestResolveCustomer(t *testing.T) {
	customers := fakeCustomerResolver{customers: map[string]string{"invoice:inv_1": "cust_inv", "subscription:sub_1": "cust_sub"}}
	payment := RegistrationEntry(t, types.SystemEntityTypePayment).Customer
	price := RegistrationEntry(t, types.SystemEntityTypePrice).Customer

	tests := []struct {
		name    string
		ref     CustomerRef
		state   map[string]any
		want    string
		wantErr bool
	}{
		{"self", Self(), map[string]any{"id": "cust_1"}, "cust_1", false},
		{"field", Field("customer_id"), map[string]any{"customer_id": "cust_2"}, "cust_2", false},
		{"parent found", Parent(types.SystemEntityTypeInvoice, "invoice_id"), map[string]any{"invoice_id": "inv_1"}, "cust_inv", false},
		{"parent missing", Parent(types.SystemEntityTypeInvoice, "invoice_id"), map[string]any{"invoice_id": "inv_x"}, "", true},
		{"when unmatched", Field("customer_id").When("kind", "A"), map[string]any{"customer_id": "c", "kind": "B"}, "", false},
		{"payment on invoice", payment, map[string]any{"destination_type": "INVOICE", "destination_id": "inv_1"}, "cust_inv", false},
		{"payment on customer", payment, map[string]any{"destination_type": "CUSTOMER", "destination_id": "cust_9"}, "cust_9", false},
		{"override price", price, map[string]any{"entity_type": "SUBSCRIPTION", "entity_id": "sub_1"}, "cust_sub", false},
		{"plan price", price, map[string]any{"entity_type": "PLAN", "entity_id": "plan_1"}, "", false},
		{"none", None(), map[string]any{"id": "x"}, "", false},
		{"no state", Field("customer_id"), nil, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveCustomer(context.Background(), tt.ref, tt.state, customers)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
