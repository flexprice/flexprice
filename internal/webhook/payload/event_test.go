package payload

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRejectedEventPayloadBuilder_BuildPayload(t *testing.T) {
	b := NewRejectedEventPayloadBuilder(nil)

	internal := webhookDto.InternalRejectedEvent{
		Reason: types.RejectedEventReasonNoMatchingMeter,
		Event: webhookDto.RejectedEventData{
			ID:                 "evt_1",
			EventName:          "api_kall", // typo'd name
			ExternalCustomerID: "cust_ext_9",
			Source:             "sdk",
			Properties:         map[string]interface{}{"tokens": float64(10)},
		},
	}
	data, err := json.Marshal(internal)
	require.NoError(t, err)

	out, err := b.BuildPayload(context.Background(), types.WebhookEventEventRejected, data)
	require.NoError(t, err)

	var payload webhookDto.RejectedEventWebhookPayload
	require.NoError(t, json.Unmarshal(out, &payload))

	assert.Equal(t, types.WebhookEventEventRejected, payload.EventType)
	assert.Equal(t, types.RejectedEventReasonNoMatchingMeter, payload.Reason)
	assert.Equal(t, "evt_1", payload.Event.ID)
	assert.Equal(t, "api_kall", payload.Event.EventName)
	assert.Equal(t, "cust_ext_9", payload.Event.ExternalCustomerID)
}

func TestRejectedEventPayloadBuilder_BuildPayload_InvalidData(t *testing.T) {
	b := NewRejectedEventPayloadBuilder(nil)

	// malformed JSON
	_, err := b.BuildPayload(context.Background(), types.WebhookEventEventRejected, json.RawMessage(`{`))
	assert.Error(t, err)

	// missing required event id/name
	data, _ := json.Marshal(webhookDto.InternalRejectedEvent{
		Reason: types.RejectedEventReasonNoMatchingMeter,
		Event:  webhookDto.RejectedEventData{},
	})
	_, err = b.BuildPayload(context.Background(), types.WebhookEventEventRejected, data)
	assert.Error(t, err)
}
