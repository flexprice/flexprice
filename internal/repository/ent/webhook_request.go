package ent

import (
	"context"
	"time"

	domainWebhookRequest "github.com/flexprice/flexprice/internal/domain/webhookrequest"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
)

type webhookRequestRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

// NewWebhookRequestRepository creates a new Ent-backed webhook request repository.
func NewWebhookRequestRepository(client postgres.IClient, log *logger.Logger) domainWebhookRequest.Repository {
	return &webhookRequestRepository{client: client, log: log}
}

func (r *webhookRequestRepository) Create(ctx context.Context, req *domainWebhookRequest.WebhookRequest) error {
	client := r.client.Writer(ctx)

	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}

	_, err := client.WebhookRequest.Create().
		SetID(req.ID).
		SetTenantID(req.TenantID).
		SetEnvironmentID(req.EnvironmentID).
		SetProvider(req.Provider).
		SetMethod(req.Method).
		SetPath(req.Path).
		SetRequestID(req.RequestID).
		SetHeaders(req.Headers).
		SetBody(req.Body).
		SetCreatedAt(req.CreatedAt).
		Save(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to persist webhook request log").
			WithReportableDetails(map[string]any{
				"provider":       req.Provider,
				"tenant_id":      req.TenantID,
				"environment_id": req.EnvironmentID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}
