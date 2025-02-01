package payload

import (
	"context"
	"encoding/json"
)

// PayloadBuilder interface for building event-specific payloads
type PayloadBuilder interface {
	BuildPayload(ctx context.Context, eventType string, data interface{}) (json.RawMessage, error)
}
