package service

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/temporal/client"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
)

// BillingScheduleProvisioner ensures durable due delivery before an owner transaction starts.
type BillingScheduleProvisioner interface{ Ensure(context.Context) error }

type billingScheduleProvisioner struct{ client client.TemporalClient }

// NewBillingScheduleProvisioner binds schedule readiness to the configured Temporal client.
func NewBillingScheduleProvisioner(client client.TemporalClient) BillingScheduleProvisioner {
	return &billingScheduleProvisioner{client: client}
}
func (p *billingScheduleProvisioner) Ensure(ctx context.Context) error {
	if p == nil {
		return ierr.NewError("billing schedule delivery is unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	return temporalservice.EnsureBillingSchedule(ctx, p.client)
}

type billingScheduleReadyKey struct{}
type billingScheduleReadyScope struct{ tenantID, environmentID string }

func prepareBillingSchedule(ctx context.Context, provisioner BillingScheduleProvisioner) (context.Context, error) {
	if ctx == nil {
		return nil, ierr.Validation("billing schedule context is required")
	}
	scope := billingScheduleReadyScope{tenantID: types.GetTenantID(ctx), environmentID: types.GetEnvironmentID(ctx)}
	if scope.tenantID == "" || scope.environmentID == "" {
		return nil, ierr.Validation("billing schedule requires tenant and environment")
	}
	if existing, ok := ctx.Value(billingScheduleReadyKey{}).(billingScheduleReadyScope); ok && existing == scope {
		return ctx, nil
	}
	if ctx.Value(types.CtxDBTransaction) != nil {
		return nil, ierr.Validation("prepare billing schedule delivery before the owner transaction")
	}
	if provisioner == nil {
		return nil, ierr.NewError("billing schedule provisioning is unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	if err := provisioner.Ensure(ctx); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, billingScheduleReadyKey{}, scope), nil
}
