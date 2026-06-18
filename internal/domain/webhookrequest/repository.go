package webhookrequest

import "context"

// Repository defines the persistence interface for webhook request audit logs.
type Repository interface {
	Create(ctx context.Context, req *WebhookRequest) error
}
