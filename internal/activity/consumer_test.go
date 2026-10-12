package activity

import (
	"context"
	"encoding/json"
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

func RegistrationEntry(t *testing.T, entityType types.SystemEntityType) *Registration {
	t.Helper()
	r, ok := Registrations().ByType(entityType)
	require.True(t, ok)
	return r
}
