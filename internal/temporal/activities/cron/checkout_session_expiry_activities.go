package cron

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
)

// CheckoutSessionExpiryActivities wraps checkout session expiry logic.
type CheckoutSessionExpiryActivities struct {
	checkoutSvc service.CheckoutSessionService
	logger      *logger.Logger
}

func NewCheckoutSessionExpiryActivities(
	checkoutSvc service.CheckoutSessionService,
	log *logger.Logger,
) *CheckoutSessionExpiryActivities {
	return &CheckoutSessionExpiryActivities{checkoutSvc: checkoutSvc, logger: log}
}

// ExpireCheckoutSessionsActivity archives all expired checkout sessions across every tenant and environment.
func (a *CheckoutSessionExpiryActivities) ExpireCheckoutSessionsActivity(ctx context.Context) (*cronModels.CheckoutSessionExpiryWorkflowResult, error) {
	now := time.Now().UTC()
	r, err := a.checkoutSvc.CleanupAllExpiredSessions(ctx, &now)
	if err != nil {
		return nil, err
	}
	return &cronModels.CheckoutSessionExpiryWorkflowResult{
		Total:     r.Total,
		Succeeded: r.Succeeded,
		Failed:    r.Failed,
	}, nil
}
