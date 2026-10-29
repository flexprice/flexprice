package billingterms

import (
	"context"

	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	models "github.com/flexprice/flexprice/internal/temporal/models/billingterms"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
)

type activities struct {
	service dueService
}

type dueService interface {
	Prepare(context.Context, core.DueBatchRequest) (*core.DueBatch, error)
	CreateOrRecover(context.Context, core.DueInvoiceRequest) (*core.DueInvoice, error)
}

// Activities exposes the two registered delivery operations.
type Activities interface {
	PrepareBillingScheduleBatchActivity(context.Context, models.PrepareInput) (*core.DueBatch, error)
	CreateOrRecoverBillingScheduleInvoiceActivity(context.Context, models.CreateInput) (*core.DueInvoice, error)
}

// NewActivities connects the two scoped delivery operations to Temporal.
func NewActivities(service dueService) Activities {
	return &activities{service: service}
}

// PrepareBillingScheduleBatchActivity resolves fallbacks and selects due or interrupted sources.
func (a *activities) PrepareBillingScheduleBatchActivity(ctx context.Context, input models.PrepareInput) (*core.DueBatch, error) {
	scope := models.DueInput{TenantID: input.TenantID, EnvironmentID: input.EnvironmentID, UserID: input.UserID, AsOf: input.Request.AsOf, Cursor: input.Request.Cursor, BatchSize: input.Request.Limit}
	if err := scope.Validate(); err != nil {
		return nil, activityError(err)
	}
	if input.Request.AsOf.IsZero() || input.Request.Limit < 1 {
		return nil, activityError(ierr.Validation("billing preparation requires a fixed cutoff and positive limit"))
	}
	if a == nil || a.service == nil {
		return nil, temporal.NewNonRetryableApplicationError("billing schedule service is unavailable", "BillingScheduleUnavailable", nil)
	}
	result, err := a.service.Prepare(scopedContext(ctx, scope), input.Request)
	return result, activityError(err)
}

// CreateOrRecoverBillingScheduleInvoiceActivity commits a source once or recovers its live invoice.
func (a *activities) CreateOrRecoverBillingScheduleInvoiceActivity(ctx context.Context, input models.CreateInput) (*core.DueInvoice, error) {
	scope := models.DueInput{TenantID: input.TenantID, EnvironmentID: input.EnvironmentID, UserID: input.UserID, AsOf: input.Request.AsOf}
	if err := scope.Validate(); err != nil {
		return nil, activityError(err)
	}
	if input.Request.AsOf.IsZero() {
		return nil, activityError(ierr.Validation("billing delivery requires a fixed cutoff"))
	}
	if a == nil || a.service == nil {
		return nil, temporal.NewNonRetryableApplicationError("billing schedule service is unavailable", "BillingScheduleUnavailable", nil)
	}
	result, err := a.service.CreateOrRecover(scopedContext(ctx, scope), input.Request)
	return result, activityError(err)
}

func scopedContext(ctx context.Context, scope models.DueInput) context.Context {
	ctx = types.SetTenantID(ctx, scope.TenantID)
	ctx = types.SetEnvironmentID(ctx, scope.EnvironmentID)
	ctx = types.SetUserID(ctx, scope.UserID)
	ctx = types.SetActor(ctx, types.Actor{Type: types.ActorTypeSystem, ID: "billing_schedule"}, types.SourceWorkflow)
	return types.WithForceWriter(ctx)
}

func activityError(err error) error {
	if ierr.IsValidation(err) || ierr.IsInvalidOperation(err) {
		return temporal.NewNonRetryableApplicationError("invalid billing schedule operation", "BillingScheduleInvalid", err)
	}
	return err
}
