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

func TestUnmatchedEventPayloadBuilder_BuildPayload(t *testing.T) {
	b := NewUnmatchedEventPayloadBuilder(nil)

	internal := webhookDto.InternalUnmatchedEvent{
		Reason: types.UnmatchedEventReasonNoMatchingMeter,
		Event: webhookDto.UnmatchedEventData{
			ID:                 "evt_1",
			EventName:          "api_kall", // typo'd name
			ExternalCustomerID: "cust_ext_9",
			Source:             "sdk",
			Properties:         map[string]interface{}{"tokens": float64(10)},
		},
	}
	data, err := json.Marshal(internal)
	require.NoError(t, err)

	out, err := b.BuildPayload(context.Background(), types.WebhookEventEventUnmatched, data)
	require.NoError(t, err)

	var payload webhookDto.UnmatchedEventWebhookPayload
	require.NoError(t, json.Unmarshal(out, &payload))

	assert.Equal(t, types.WebhookEventEventUnmatched, payload.EventType)
	assert.Equal(t, types.UnmatchedEventReasonNoMatchingMeter, payload.Reason)
	assert.Equal(t, "evt_1", payload.Event.ID)
	assert.Equal(t, "api_kall", payload.Event.EventName)
	assert.Equal(t, "cust_ext_9", payload.Event.ExternalCustomerID)
}

func TestUnmatchedEventPayloadBuilder_BuildPayload_InvalidData(t *testing.T) {
	b := NewUnmatchedEventPayloadBuilder(nil)

	// malformed JSON
	_, err := b.BuildPayload(context.Background(), types.WebhookEventEventUnmatched, json.RawMessage(`{`))
	assert.Error(t, err)

	// missing required event id/name
	data, _ := json.Marshal(webhookDto.InternalUnmatchedEvent{
		Reason: types.UnmatchedEventReasonNoMatchingMeter,
		Event:  webhookDto.UnmatchedEventData{},
	})
	_, err = b.BuildPayload(context.Background(), types.WebhookEventEventUnmatched, data)
	assert.Error(t, err)
}
