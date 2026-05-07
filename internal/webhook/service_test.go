package webhook

import (
	"encoding/json"
	"testing"
	"time"

	flexent "github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSystemEventToWebhookEvent(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	se := &flexent.SystemEvent{
		ID:            "sev_test123",
		TenantID:      "ten_1",
		EnvironmentID: "env_1",
		EventName:     types.WebhookEventCustomerCreated,
		EntityType:    string(types.SystemEntityTypeCustomer),
		EntityID:      "cus_1",
		CreatedBy:     "user_1",
		CreatedAt:     created,
		Payload:       map[string]interface{}{"customer_id": "cus_1", "tenant_id": "ten_1"},
	}

	ev, err := SystemEventToWebhookEvent(se)
	require.NoError(t, err)
	require.Equal(t, se.ID, ev.ID)
	require.Equal(t, se.EventName, ev.EventName)
	require.Equal(t, se.TenantID, ev.TenantID)
	require.Equal(t, se.EnvironmentID, ev.EnvironmentID)
	require.Equal(t, se.CreatedBy, ev.UserID)
	require.True(t, ev.Timestamp.Equal(created.UTC()))
	require.Equal(t, types.SystemEntityTypeCustomer, ev.EntityType)
	require.Equal(t, se.EntityID, ev.EntityID)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(ev.Payload, &got))
	require.Equal(t, "cus_1", got["customer_id"])
}

func TestSystemEventToWebhookEvent_NilPayload(t *testing.T) {
	t.Parallel()

	se := &flexent.SystemEvent{
		ID:            "sev_empty",
		TenantID:      "ten_1",
		EnvironmentID: "env_1",
		EventName:     types.WebhookEventCustomerUpdated,
		CreatedAt:     time.Now().UTC(),
	}

	ev, err := SystemEventToWebhookEvent(se)
	require.NoError(t, err)
	require.Empty(t, ev.Payload)
}

func TestSystemEventToWebhookEvent_NilRow(t *testing.T) {
	t.Parallel()

	_, err := SystemEventToWebhookEvent(nil)
	require.Error(t, err)
}


