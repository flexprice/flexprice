package incomingwebhookevent

import "context"

// Repository defines the persistence interface for incoming webhook event audit logs.
type Repository interface {
	Create(ctx context.Context, event *IncomingWebhookEvent) error
}
