package email

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
)

type EmailActivities struct {
	params service.ServiceParams
}

func NewEmailActivities(params service.ServiceParams) *EmailActivities {
	return &EmailActivities{params: params}
}

func scopedCtx(ctx context.Context, in models.SendEmailWorkflowInput) context.Context {
	ctx = types.SetTenantID(ctx, in.TenantID)
	ctx = types.SetEnvironmentID(ctx, in.EnvironmentID)
	ctx = types.SetUserID(ctx, in.UserID)
	return types.WithForceWriter(ctx)
}

func (a *EmailActivities) DeliverEmail(ctx context.Context, in models.DeliverEmailActivityInput) error {
	err := service.NewEmailNotificationService(a.params).DeliverEmail(scopedCtx(ctx, in.SendEmailWorkflowInput), in.EmailLogID, in.AllowMissingAttachments)
	if err != nil && ierr.IsValidation(err) {
		return temporal.NewNonRetryableApplicationError(err.Error(), "EmailRejected", err)
	}
	return err
}

func (a *EmailActivities) MarkEmailFailed(ctx context.Context, in models.SendEmailWorkflowInput) error {
	return service.NewEmailNotificationService(a.params).MarkEmailFailed(scopedCtx(ctx, in), in.EmailLogID, "The email could not be sent after several attempts")
}
