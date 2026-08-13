package events

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/stretchr/testify/require"
)

func testDealDispatchCtx() context.Context {
	return context.Background()
}

func TestDispatchHubSpotDealLineItemSync_NonFixedPrice_Skips(t *testing.T) {
	payload, err := json.Marshal(webhookDto.InternalSubscriptionLineItemEvent{
		SubscriptionID: "sub_1",
		LineItemID:     "li_1",
		CustomerID:     "cust_1",
		PriceType:      types.PRICE_TYPE_USAGE,
		TenantID:       "tenant_test",
		EnvironmentID:  "env_test",
	})
	require.NoError(t, err)

	event := &types.WebhookEvent{Payload: payload}
	cfg := &config.Configuration{IntegrationEvents: config.IntegrationEventsConfig{Enabled: true}}

	err = DispatchHubSpotDealLineItemSync(testDealDispatchCtx(), cfg, nil, nil, nil, testLogger(), event, "msg_1", types.WebhookEventSubscriptionLineItemCreated)
	require.NoError(t, err, "a USAGE-price event must be a silent no-op, not an error")
}

func TestDispatchHubSpotDealLineItemSync_InvalidPayload_Skips(t *testing.T) {
	event := &types.WebhookEvent{Payload: json.RawMessage(`not valid json`)}
	cfg := &config.Configuration{IntegrationEvents: config.IntegrationEventsConfig{Enabled: true}}

	err := DispatchHubSpotDealLineItemSync(testDealDispatchCtx(), cfg, nil, nil, nil, testLogger(), event, "msg_1", types.WebhookEventSubscriptionLineItemCreated)
	require.NoError(t, err, "an invalid payload must be a silent no-op (already logged), not an error that triggers Kafka retry")
}

func TestDispatchHubSpotDealLineItemSync_IntegrationEventsDisabled_Skips(t *testing.T) {
	payload, err := json.Marshal(webhookDto.InternalSubscriptionLineItemEvent{
		SubscriptionID: "sub_1", LineItemID: "li_1", CustomerID: "cust_1",
		PriceType: types.PRICE_TYPE_FIXED, TenantID: "tenant_test", EnvironmentID: "env_test",
	})
	require.NoError(t, err)
	event := &types.WebhookEvent{Payload: payload}
	cfg := &config.Configuration{IntegrationEvents: config.IntegrationEventsConfig{Enabled: false}}

	err = DispatchHubSpotDealLineItemSync(testDealDispatchCtx(), cfg, nil, nil, nil, testLogger(), event, "msg_1", types.WebhookEventSubscriptionLineItemCreated)
	require.NoError(t, err)
}
