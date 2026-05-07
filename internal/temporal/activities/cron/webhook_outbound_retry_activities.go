package cron

import (
	"context"

	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/webhook"
)

// WebhookOutboundRetryActivities runs scheduled outbound webhook recovery.
type WebhookOutboundRetryActivities struct {
	webhookService *webhook.WebhookService
	logger         *logger.Logger
}

// NewWebhookOutboundRetryActivities constructs WebhookOutboundRetryActivities.
func NewWebhookOutboundRetryActivities(webhookService *webhook.WebhookService, log *logger.Logger) *WebhookOutboundRetryActivities {
	return &WebhookOutboundRetryActivities{
		webhookService: webhookService,
		logger:         log,
	}
}

// RetryStaleOutboundWebhooksActivity delegates to WebhookService.RetryStalePendingWebhooks.
func (a *WebhookOutboundRetryActivities) RetryStaleOutboundWebhooksActivity(ctx context.Context) (*cronModels.OutboundWebhookStaleRetryWorkflowResult, error) {
	a.logger.Infow("starting stale outbound webhook retry cron job")

	res, err := a.webhookService.RetryStalePendingWebhooks(ctx)
	if err != nil {
		a.logger.Errorw("stale outbound webhook retry activity failed", "error", err)
		return nil, err
	}

	out := &cronModels.OutboundWebhookStaleRetryWorkflowResult{
		Total:     res.Total,
		Succeeded: res.Succeeded,
		Failed:    res.Failed,
	}

	a.logger.Infow("completed stale outbound webhook retry cron job",
		"total", out.Total,
		"succeeded", out.Succeeded,
		"failed", out.Failed,
	)
	return out, nil
}
