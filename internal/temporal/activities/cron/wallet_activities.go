package cron

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"go.temporal.io/sdk/activity"
)

// WalletCreditExpiryActivities wraps wallet credit expiry logic.
type WalletCreditExpiryActivities struct {
	walletService      service.WalletService
	tenantService      service.TenantService
	environmentService service.EnvironmentService
	logger             *logger.Logger
}

func NewWalletCreditExpiryActivities(
	walletService service.WalletService,
	tenantService service.TenantService,
	environmentService service.EnvironmentService,
	log *logger.Logger,
) *WalletCreditExpiryActivities {
	return &WalletCreditExpiryActivities{
		walletService:      walletService,
		tenantService:      tenantService,
		environmentService: environmentService,
		logger:             log,
	}
}

// ExpireCreditsActivity finds and expires credits that have passed their expiry date across all tenants.
// Logic matches internal/api/cron.WalletCronHandler.ExpireCredits (no-limit tx filter, up to 1000 envs per tenant).
func (a *WalletCreditExpiryActivities) ExpireCreditsActivity(ctx context.Context) (*cronModels.WalletCreditExpiryWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Starting wallet credit expiry activity")
	a.logger.Info(ctx, "starting credit expiry cron job", "time", time.Now().UTC().Format(time.RFC3339))

	tenants, err := a.tenantService.GetAllTenants(ctx)
	if err != nil {
		a.logger.Error(ctx, "failed to get all tenants", "error", err)
		return nil, err
	}

	// Create filter to find expired credits.
	filter := types.NewNoLimitWalletTransactionFilter()
	filter.Type = lo.ToPtr(types.TransactionTypeCredit)
	filter.TransactionStatus = lo.ToPtr(types.TransactionStatusCompleted)
	// Pick credits already past expiry that still hold available credit. No grace buffer: ExpireCredits
	// now tries to consume them into invoices first (best-effort), then expires whatever remains.
	filter.ExpiryDateBefore = lo.ToPtr(time.Now().UTC())
	filter.CreditsAvailableGT = lo.ToPtr(decimal.Zero)

	result := &cronModels.WalletCreditExpiryWorkflowResult{}

	for _, tenant := range tenants {
		tenantCtx := context.WithValue(ctx, types.CtxTenantID, tenant.ID)
		envFilter := types.GetDefaultFilter()
		envFilter.Limit = 1000
		environments, err := a.environmentService.GetEnvironments(tenantCtx, envFilter)
		if err != nil {
			a.logger.Error(ctx, "failed to get all environments", "error", err)
			return nil, err
		}

		for _, environment := range environments.Environments {
			envCtx := context.WithValue(tenantCtx, types.CtxEnvironmentID, environment.ID)

			transactions, err := a.walletService.ListWalletTransactionsByFilter(envCtx, filter)
			if err != nil {
				a.logger.Error(ctx, "failed to list expired credits", "error", err)
				return nil, err
			}

			a.logger.Debug(ctx, "found expired credits", "count", len(transactions.Items))

			for i, tx := range transactions.Items {
				if i%100 == 0 {
					activity.RecordHeartbeat(ctx, "processed tenant "+tenant.ID+" env "+environment.ID)
				}
				result.Total++

				txCtx := context.WithValue(envCtx, types.CtxUserID, tx.CreatedBy)
				expireResult, err := a.walletService.ExpireCredits(txCtx, tx.ID)
				if err != nil {
					a.logger.Error(ctx, "failed to expire credits", "transaction_id", tx.ID, "error", err)
					result.Failed++
					continue
				}
				if expireResult.AmountConsumedIntoInvoices.GreaterThan(decimal.Zero) {
					result.CreditsConsumedIntoInvoices++
					result.AmountConsumedIntoInvoices = result.AmountConsumedIntoInvoices.Add(expireResult.AmountConsumedIntoInvoices)
				}
				if expireResult.Expired {
					result.Succeeded++
					amountExpired := tx.CreditsAvailable.Sub(expireResult.AmountConsumedIntoInvoices)
					a.logger.Info(ctx, "expired credits successfully",
						"transaction_id", tx.ID, "wallet_id", tx.WalletID, "amount", amountExpired)
				}
			}
		}
	}

	a.logger.Info(ctx, "completed credit expiry cron job")
	log.Info("Completed wallet credit expiry activity",
		"total", result.Total,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
		"credits_consumed_into_invoices", result.CreditsConsumedIntoInvoices,
		"amount_consumed_into_invoices", result.AmountConsumedIntoInvoices,
	)
	return result, nil
}
