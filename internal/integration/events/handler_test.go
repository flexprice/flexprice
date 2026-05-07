package events

import (
	"encoding/json"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"go.uber.org/zap"
)

func testLogger() *logger.Logger {
	return &logger.Logger{SugaredLogger: zap.NewNop().Sugar()}
}

func TestProcessMessage_IgnoresUnknownEvent(t *testing.T) {
	h := &handler{
		deps: Deps{
			Logger: testLogger(),
			Config: &config.Configuration{},
		},
		processors: map[types.WebhookEventName]eventProcessor{},
	}

	ev := types.WebhookEvent{
		EventName: "unknown.event",
		TenantID:  "ten_1",
		Payload:   json.RawMessage(`{"x":"y"}`),
	}
	b, _ := json.Marshal(ev)
	msg := message.NewMessage("msg-1", b)

	if err := h.processMessage(msg); err != nil {
		t.Fatalf("expected nil error for unknown event, got %v", err)
	}
}

func TestProcessMessage_CustomerCreatedDispatchError(t *testing.T) {
	h, ok := NewHandler(Deps{
		Logger: testLogger(),
		Config: &config.Configuration{
			IntegrationEvents: config.IntegrationEventsConfig{Enabled: true},
		},
	}).(*handler)
	if !ok {
		t.Fatalf("expected concrete *handler")
	}

	ev := types.WebhookEvent{
		EventName:     types.WebhookEventCustomerCreated,
		TenantID:      "ten_1",
		EnvironmentID: "env_1",
		UserID:        "usr_1",
		Payload:       json.RawMessage(`{"customer_id":"cus_1"}`),
	}
	b, _ := json.Marshal(ev)
	msg := message.NewMessage("msg-2", b)

	if err := h.processMessage(msg); err == nil {
		t.Fatalf("expected error when temporal service is unavailable")
	}
}
