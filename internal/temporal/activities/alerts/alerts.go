package alerts

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"

	"go.temporal.io/sdk/activity"
)

// AlertActivities hosts the meter-usage-driven alert-check activities.
// They are thin adapters over service.CheckSpendBreachForCustomer and
// service.CheckWalletBalanceForCustomer; all real work lives in the service
// layer so the activities stay trivial and the service functions can be reused
// outside Temporal if needed.
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

// setTenantContext injects tenant and environment IDs into the activity context so
// downstream repository queries scope correctly. Activities run outside HTTP
// middleware so the framework never populates this for us.
func setTenantContext(ctx context.Context, tenantID, environmentID string) context.Context {
	ctx = types.SetTenantID(ctx, tenantID)
	ctx = types.SetEnvironmentID(ctx, environmentID)
	return ctx
}

// CheckSpendBreachActivity evaluates every subscription-level, line-item-level,
// and group-level spend threshold configured for the customer. Runs once per
// debounce window per customer; errors during a single subscription evaluation
// are logged and skipped by the underlying service so one bad subscription can't
// block the rest.
func (a *AlertActivities) CheckSpendBreachActivity(ctx context.Context, input models.MeterUsageAlertActivityInput) error {
	logger := activity.GetLogger(ctx)
	logger.Info("CheckSpendBreachActivity started",
		"tenant_id", input.TenantID,
		"customer_id", input.CustomerID,
	)

	ctx = setTenantContext(ctx, input.TenantID, input.EnvironmentID)

	cust, err := a.serviceParams.CustomerRepo.Get(ctx, input.CustomerID)
	if err != nil {
		if ierr.IsNotFound(err) {
			logger.Info("customer not found, skipping spend-breach check", "customer_id", input.CustomerID)
			return nil
		}
		return err
	}

	service.CheckSpendBreachForCustomer(ctx, a.serviceParams, cust)
	return nil
}

// CheckWalletBalanceActivity re-runs the wallet balance alert check for the
// customer, force-calculating the balance to bypass the in-memory throttle in
// walletService.CheckWalletBalanceAlert — Temporal already guarantees at-most-one
// invocation per debounce window per customer via the workflow-ID dedupe, so an
// extra layer of throttling would just re-introduce the "trailing event ignored"
// bug the debouncer exists to fix.
func (a *AlertActivities) CheckWalletBalanceActivity(ctx context.Context, input models.MeterUsageAlertActivityInput) error {
	logger := activity.GetLogger(ctx)
	logger.Info("CheckWalletBalanceActivity started",
		"tenant_id", input.TenantID,
		"customer_id", input.CustomerID,
	)

	ctx = setTenantContext(ctx, input.TenantID, input.EnvironmentID)

	cust, err := a.serviceParams.CustomerRepo.Get(ctx, input.CustomerID)
	if err != nil {
		if ierr.IsNotFound(err) {
			logger.Info("customer not found, skipping wallet-balance check", "customer_id", input.CustomerID)
			return nil
		}
		return err
	}

	return service.CheckWalletBalanceForCustomer(ctx, a.serviceParams, cust)
}
