package ent

import (
	"context"

	domainIncomingWebhookEvent "github.com/flexprice/flexprice/internal/domain/incomingwebhookevent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
)

type incomingWebhookEventRepository struct {
	client postgres.IClient
}

// NewIncomingWebhookEventRepository creates a new Ent-backed incoming webhook event repository.
func NewIncomingWebhookEventRepository(client postgres.IClient) domainIncomingWebhookEvent.Repository {
	return &incomingWebhookEventRepository{client: client}
}

func (r *incomingWebhookEventRepository) Create(ctx context.Context, event *domainIncomingWebhookEvent.IncomingWebhookEvent) error {
	client := r.client.Writer(ctx)

	_, err := client.IncomingWebhookEvent.Create().
		SetID(event.ID).
		SetTenantID(event.TenantID).
		SetEnvironmentID(event.EnvironmentID).
		SetProvider(event.Provider).
		SetMethod(event.Method).
		SetPath(event.Path).
		SetRequestID(event.RequestID).
		SetHeaders(event.Headers).
		SetBody(event.Body).
		Save(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to persist incoming webhook event log").
			WithReportableDetails(map[string]any{
				"provider":       event.Provider,
				"tenant_id":      event.TenantID,
				"environment_id": event.EnvironmentID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}
