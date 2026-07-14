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

// AlertActivities hosts the usage-driven alert evaluation activities. They are
// thin adapters over the AlertService methods; all real work lives in the service
// layer so the activities stay trivial and the service methods can be called
// directly (e.g. from a REST test endpoint) without Temporal.
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

// prepare loads the customer and injects tenant/environment IDs into the
// activity context. Returns (nil, nil) when the customer no longer exists — a
// no-op is the right response since the whole workflow is customer-scoped.
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

// SpendAlertsActivity evaluates subscription / line-item / group spend
// alerts for the customer end to end (fetch subs + configs + usage + charges,
// compare thresholds, log alerts). Self-contained.
func (a *AlertActivities) SpendAlertsActivity(ctx context.Context, input models.UsageAlertActivityInput) error {
	log := activity.GetLogger(ctx)
	log.Info("SpendAlertsActivity started",
		"tenant_id", input.TenantID,
		"customer_id", input.CustomerID,
	)

	ctx, cust, err := a.prepare(ctx, input.TenantID, input.EnvironmentID, input.CustomerID)
	if err != nil {
		return err
	}
	if cust == nil {
		log.Info("customer not found, spend alerts activity is a no-op", "customer_id", input.CustomerID)
		return nil
	}

	return service.NewAlertService(a.serviceParams).EvaluateSpendAlertsForCustomer(ctx, cust, nil, nil)
}

// WalletAlertsActivity evaluates wallet-balance / feature-wallet-balance
// alerts and auto-topup for the customer end to end (fetch wallets +
// alert config + real-time balance, compare thresholds, log alerts, trigger
// topups). Self-contained.
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

	return service.NewAlertService(a.serviceParams).EvaluateWalletAlertsForCustomer(ctx, cust)
}
