package cron

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
)

// EmailDispatchActivities wraps the customer email dispatch cron job.
type EmailDispatchActivities struct {
	emailService service.EmailNotificationService
	logger       *logger.Logger
}

// NewEmailDispatchActivities builds activities for the email dispatch cron workflow.
func NewEmailDispatchActivities(emailService service.EmailNotificationService, log *logger.Logger) *EmailDispatchActivities {
	return &EmailDispatchActivities{
		emailService: emailService,
		logger:       log,
	}
}

// DispatchEmailsActivity sends queued customer emails from every environment.
func (a *EmailDispatchActivities) DispatchEmailsActivity(ctx context.Context) (*cronModels.EmailDispatchWorkflowResult, error) {
	res, err := a.emailService.DispatchEmails(ctx)
	if err != nil {
		a.logger.Error(ctx, "DispatchEmailsActivity failed", "error", err)
		return nil, err
	}
	return &cronModels.EmailDispatchWorkflowResult{Sent: res.Sent, Failed: res.Failed, Deferred: res.Deferred}, nil
}
