package ent

import (
	"context"

	domainIncomingWebhookEvent "github.com/flexprice/flexprice/internal/domain/incomingwebhookevent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
)

type incomingWebhookEventRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

// NewIncomingWebhookEventRepository creates a new Ent-backed incoming webhook event repository.
func NewIncomingWebhookEventRepository(client postgres.IClient, log *logger.Logger) domainIncomingWebhookEvent.Repository {
	return &incomingWebhookEventRepository{client: client, log: log}
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
