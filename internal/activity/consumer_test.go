package activity

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	domainActivity "github.com/flexprice/flexprice/internal/domain/activity"
	"github.com/flexprice/flexprice/internal/logger"
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

type recordingRepo struct {
	domainActivity.Repository
	inserted []*domainActivity.Log
}

func (r *recordingRepo) Insert(_ context.Context, logs []*domainActivity.Log) error {
	r.inserted = append(r.inserted, logs...)
	return nil
}

func TestConsumer_UnresolvedCustomerRetriesMessage(t *testing.T) {
	repo := &recordingRepo{}
	c := NewConsumer(Registrations(), repo, fakeCustomerResolver{customers: map[string]string{"invoice:inv_1": "cust_inv"}}, types.ActivityPubSub{}, logger.NewNoopLogger())
	payload, err := json.Marshal(Message{TenantID: "t", EnvironmentID: "e", Events: []MessageEvent{
		{ID: "act_1", EntityType: "refund", EntityID: "ref_1", Action: "refund.created", State: json.RawMessage(`{"invoice_id":"inv_1"}`)},
		{ID: "act_2", EntityType: "refund", EntityID: "ref_2", Action: "refund.created", State: json.RawMessage(`{"invoice_id":"inv_missing"}`)},
	}})
	require.NoError(t, err)

	require.ErrorContains(t, c.process(context.Background(), message.NewMessage("m1", payload)), "act_2")
	require.Len(t, repo.inserted, 1)
	require.Equal(t, "act_1", repo.inserted[0].ID)
	require.Equal(t, "cust_inv", repo.inserted[0].CustomerID)
}

func TestLabel(t *testing.T) {
	customer := RegistrationEntry(t, types.SystemEntityTypeCustomer)
	require.Equal(t, "ext", label(customer, map[string]any{"name": "", "external_id": "ext"}))
	require.Equal(t, "Ada", label(customer, map[string]any{"name": "Ada", "external_id": "ext"}))
	subscription := RegistrationEntry(t, types.SystemEntityTypeSubscription)
	require.Equal(t, "", label(subscription, map[string]any{"name": "x"}))
}

func RegistrationEntry(t *testing.T, entityType types.SystemEntityType) *Registration {
	t.Helper()
	r, ok := Registrations().ByType(entityType)
	require.True(t, ok)
	return r
}
