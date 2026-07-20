package alerts

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"

	"go.temporal.io/sdk/activity"
)

// AlertActivities hosts usage-driven alert evaluation activities. Thin adapters
// over AlertService; all logic lives in the service layer.
type AlertActivities struct {
	serviceParams service.ServiceParams
	logger        *logger.Logger
}

func NewAlertActivities(serviceParams service.ServiceParams, logger *logger.Logger) *AlertActivities {
	return &AlertActivities{
		serviceParams: serviceParams,
		logger:        logger,
	}
}

// prepare loads the customer + injects tenant/env into ctx. Returns (nil, nil)
// when the customer no longer exists (activity becomes a no-op).
func (a *AlertActivities) prepare(ctx context.Context, tenantID, environmentID, customerID string) (context.Context, *customer.Customer, error) {
	ctx = types.SetTenantID(ctx, tenantID)
	ctx = types.SetEnvironmentID(ctx, environmentID)

	cust, err := a.serviceParams.CustomerRepo.Get(ctx, customerID)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ctx, nil, nil
		}
		return ctx, nil, err
	}
	return ctx, cust, nil
}

// SpendAndEntitlementAlertsActivity evaluates spend alerts (subscription /
// line-item / group) and per-grant exhaustion in one pass. Idempotent under
// Temporal retries via LogAlert state-transition dedup + UpdateSnapshot.
func (a *AlertActivities) SpendAndEntitlementAlertsActivity(ctx context.Context, input models.UsageAlertActivityInput) error {
	log := activity.GetLogger(ctx)
	log.Info("SpendAndEntitlementAlertsActivity started",
		"tenant_id", input.TenantID,
		"customer_id", input.CustomerID,
	)

	ctx, cust, err := a.prepare(ctx, input.TenantID, input.EnvironmentID, input.CustomerID)
	if err != nil {
		return err
	}
	if cust == nil {
		log.Info("customer not found, fused alerts activity is a no-op", "customer_id", input.CustomerID)
		return nil
	}

	return service.NewAlertService(a.serviceParams).EvaluateSpendAndEntitlementAlertsForCustomer(ctx, cust)
}

// WalletAlertsActivity evaluates wallet-balance alerts and auto-topup.
func (a *AlertActivities) WalletAlertsActivity(ctx context.Context, input models.UsageAlertActivityInput) error {
	log := activity.GetLogger(ctx)
	log.Info("WalletAlertsActivity started",
		"tenant_id", input.TenantID,
		"customer_id", input.CustomerID,
	)

	ctx, cust, err := a.prepare(ctx, input.TenantID, input.EnvironmentID, input.CustomerID)
	if err != nil {
		return err
	}
	if cust == nil {
		log.Info("customer not found, wallet alerts activity is a no-op", "customer_id", input.CustomerID)
		return nil
	}

	// Run-id-seeded idempotency: retries within the same firing dedupe to one topup.
	autoTopupSeed := activity.GetInfo(ctx).WorkflowExecution.RunID
	return service.NewAlertService(a.serviceParams).EvaluateWalletAlertsForCustomer(ctx, cust, autoTopupSeed)
}
