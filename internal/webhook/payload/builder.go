package payload

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/types"
)

// PayloadBuilder interface for building event-specific payloads
type PayloadBuilder interface {
	BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error)
}
