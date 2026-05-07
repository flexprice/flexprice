package systemevent

import (
	"context"
	"time"

	flexent "github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// ListStaleUndeliveredWebhooksParams configures the stale-webhook query.
type ListStaleUndeliveredWebhooksParams struct {
	OlderThan         time.Time
	Limit             int
	MaxAttempts       int
	ExcludedTenants   []string
	AllowedEventTypes []string
}

// Repository defines the data-access contract for system events.
type Repository interface {
	GetByID(ctx context.Context, tenantID, environmentID, id string) (*flexent.SystemEvent, error)
	ListStaleUndeliveredWebhooks(ctx context.Context, params ListStaleUndeliveredWebhooksParams) ([]*flexent.SystemEvent, error)
	OnConsumed(ctx context.Context, event *types.WebhookEvent) error
	OnDelivered(ctx context.Context, eventID string, webhookMessageID *string) error
	OnFailed(ctx context.Context, eventID, reason string) error
}
