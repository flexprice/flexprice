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

type recordingRepo struct {
	domainActivity.Repository
	inserted []*domainActivity.Log
}

func (r *recordingRepo) Insert(_ context.Context, logs []*domainActivity.Log) error {
	r.inserted = append(r.inserted, logs...)
	return nil
}

type fakeLookup map[string]string

func (f fakeLookup) GetLinkedCustomerID(_ context.Context, id string) (string, error) {
	if c, ok := f[id]; ok {
		return c, nil
	}
	return "", errors.New("not found")
}

func TestConsumer_LinkedCustomer(t *testing.T) {
	repo := &recordingRepo{}
	customers := LinkedCustomers{
		types.SystemEntityTypeRefund:  fakeLookup{"ref_1": "cust_inv"},
		types.SystemEntityTypeInvoice: fakeLookup{},
	}
	c := NewConsumer(Registrations(), repo, customers, types.ActivityPubSub{}, logger.NewNoopLogger())
	payload, err := json.Marshal(Message{SchemaVersion: SchemaVersion, TenantID: "t", EnvironmentID: "e", Events: []MessageEvent{
		{ID: "act_1", EntityType: "refund", EntityID: "ref_1", Action: "refund.created", State: json.RawMessage(`{"invoice_id":"inv_1"}`)},
		{ID: "act_2", EntityType: "refund", EntityID: "ref_2", Action: "refund.created", State: json.RawMessage(`{"invoice_id":"inv_missing"}`)},
		{ID: "act_3", EntityType: "invoice", EntityID: "inv_1", Action: "invoice.created", State: json.RawMessage(`{"customer_id":"cust_state"}`)},
		{ID: "act_4", EntityType: "plan", EntityID: "plan_1", Action: "plan.created", State: json.RawMessage(`{"name":"Pro"}`)},
		{ID: "act_5", EntityType: "customer", EntityID: "cust_1", Action: "customer.created", State: json.RawMessage(`{"id":"cust_1"}`)},
	}})
	require.NoError(t, err)

	require.ErrorContains(t, c.process(context.Background(), message.NewMessage("m1", payload)), "act_2")
	got := map[string]string{}
	for _, l := range repo.inserted {
		got[l.ID] = l.CustomerID
	}
	require.Equal(t, map[string]string{"act_1": "cust_inv", "act_3": "cust_state", "act_4": "", "act_5": "cust_1"}, got)
}

func RegistrationEntry(t *testing.T, entityType types.SystemEntityType) *Registration {
	t.Helper()
	r, ok := Registrations().ByType(entityType)
	require.True(t, ok)
	return r
}
