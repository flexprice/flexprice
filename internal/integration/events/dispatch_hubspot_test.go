package events

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"go.uber.org/zap"
)

func testLoggerEvents() *logger.Logger {
	return &logger.Logger{SugaredLogger: zap.NewNop().Sugar()}
}

func TestDispatchHubSpotDealLineItemSync_NoConnection_SkipsSilently(t *testing.T) {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, "ten_1")
	ctx = types.SetEnvironmentID(ctx, "env_1")

	connRepo := testutil.NewInMemoryConnectionStore()
	custRepo := testutil.NewInMemoryCustomerStore()
	eimRepo := testutil.NewInMemoryEntityIntegrationMappingStore()

	ev := &types.WebhookEvent{
		EventName:     types.WebhookEventSubscriptionLineItemCreated,
		TenantID:      "ten_1",
		EnvironmentID: "env_1",
		Payload:       json.RawMessage(`{"subscription_id":"sub_1","line_item_id":"li_1","customer_id":"cus_1","price_type":"FIXED"}`),
	}

	err := DispatchHubSpotDealLineItemSync(ctx, &config.Configuration{IntegrationEvents: config.IntegrationEventsConfig{Enabled: true}},
		connRepo, custRepo, eimRepo, testLoggerEvents(), ev, "msg-1", types.WebhookEventSubscriptionLineItemCreated)
	if err != nil {
		t.Fatalf("expected nil error when no HubSpot connection exists, got %v", err)
	}
}
