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
//
// Deprecated: kept for backward compatibility with pre-FLE-959 workflow
// definitions still running through Temporal's history replay. New workflow
// runs use SpendAndEntitlementAlertsActivity, which fuses spend and grant
// evaluation to share subscription/customer setup and cover FLE-959 grants
// in the same activity.
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

// SpendAndEntitlementAlertsActivity is the FLE-959 fused evaluator: subscription /
// line-item / group spend alerts AND per-grant threshold + exhaustion alerts,
// running in one activity so subscription and customer setup are shared.
// Wallet alerts remain on their own activity (WalletAlertsActivity) — the
// wallet path has its own throttle + balance-cache semantics that don't fuse
// cleanly here.
//
// The heavy lifting is in the service layer; this activity is a thin adapter.
// Idempotent under Temporal retries via LogAlert's state-transition dedup and
// UpdateSnapshot's upsert semantics.
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

	// Anchor the auto-topup idempotency key to the Temporal workflow run id so
	// activity retries within the same debounce firing collapse into a single
	// top-up per wallet (fresh UUIDs on each retry would double-topup).
	autoTopupSeed := activity.GetInfo(ctx).WorkflowExecution.RunID
	return service.NewAlertService(a.serviceParams).EvaluateWalletAlertsForCustomer(ctx, cust, autoTopupSeed)
}
