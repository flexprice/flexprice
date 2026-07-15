package marketplace

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/usagerecord"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	temporalModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/activity"
)

// SnapshotActivities creates the usage records that will later be reported to a marketplace. For
// every published aws_marketplace connection it computes each mapped subscription's usage for the
// reporting window, using the same commitment- and overage-aware computation as real invoicing,
// and writes one usage record per subscription in the subscription's own billing currency —
// marketplace-mandated currency conversion (e.g. AWS requires USD) happens per-marketplace at
// report time, not here, since this table is shared across marketplaces.
type SnapshotActivities struct {
	subscriptionService          service.SubscriptionService
	billingService               service.BillingService
	connectionRepo               connection.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	subscriptionRepo             subscription.Repository
	customerRepo                 customer.Repository
	usageRecordRepo              usagerecord.Repository
	logger                       *logger.Logger
}

func NewSnapshotActivities(
	subscriptionService service.SubscriptionService,
	billingService service.BillingService,
	connectionRepo connection.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	subscriptionRepo subscription.Repository,
	customerRepo customer.Repository,
	usageRecordRepo usagerecord.Repository,
	log *logger.Logger,
) *SnapshotActivities {
	return &SnapshotActivities{
		subscriptionService:          subscriptionService,
		billingService:               billingService,
		connectionRepo:               connectionRepo,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		subscriptionRepo:             subscriptionRepo,
		customerRepo:                 customerRepo,
		usageRecordRepo:              usageRecordRepo,
		logger:                       log,
	}
}

// MarketplaceUsageSnapshotActivity is the activity entrypoint. The reporting window
// (PeriodStart/PeriodEnd) is computed by the workflow and passed in unchanged. It processes every
// tenant/environment that has a published aws_marketplace connection.
func (a *SnapshotActivities) MarketplaceUsageSnapshotActivity(
	ctx context.Context,
	input temporalModels.MarketplaceUsageSnapshotActivityInput,
) (*temporalModels.MarketplaceUsageSnapshotWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Starting MarketplaceUsageSnapshotActivity", "period_start", input.PeriodStart, "period_end", input.PeriodEnd)

	result := &temporalModels.MarketplaceUsageSnapshotWorkflowResult{}

	conns, err := a.connectionRepo.ListPublishedByProvider(ctx, types.SecretProviderAWSMarketplace)
	if err != nil {
		a.logger.Error(ctx, "marketplace usage snapshot failed", "error", err, "stage", "list_connections")
		return nil, err
	}

	for _, conn := range conns {
		envCtx := context.WithValue(context.WithValue(ctx, types.CtxTenantID, conn.TenantID), types.CtxEnvironmentID, conn.EnvironmentID)
		a.snapshotConnectionUsage(envCtx, conn.TenantID, conn.EnvironmentID, input, result)
	}

	log.Info("Completed MarketplaceUsageSnapshotActivity",
		"total", result.Total, "succeeded", result.Succeeded, "failed", result.Failed)
	return result, nil
}

// snapshotConnectionUsage writes a usage record for each of the connection's mapped subscriptions.
// It resolves the connection's mapped customers first, then snapshots each mapped subscription that
// belongs to one of them.
func (a *SnapshotActivities) snapshotConnectionUsage(
	envCtx context.Context,
	tenantID, environmentID string,
	input temporalModels.MarketplaceUsageSnapshotActivityInput,
	result *temporalModels.MarketplaceUsageSnapshotWorkflowResult,
) {
	providerType := string(types.SecretProviderAWSMarketplace)

	customerMappings, err := a.entityIntegrationMappingRepo.List(envCtx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypeCustomer,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage snapshot failed",
			"tenant_id", tenantID, "environment_id", environmentID, "error", err, "stage", "list_customer_mappings")
		return
	}
	mappedCustomerIDs := make(map[string]bool, len(customerMappings))
	for _, m := range customerMappings {
		mappedCustomerIDs[m.EntityID] = true
	}
	if len(mappedCustomerIDs) == 0 {
		return
	}

	subscriptionMappings, err := a.entityIntegrationMappingRepo.List(envCtx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypeSubscription,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage snapshot failed",
			"tenant_id", tenantID, "environment_id", environmentID, "error", err, "stage", "list_subscription_mappings")
		return
	}

	for _, subMapping := range subscriptionMappings {
		a.snapshotSubscription(envCtx, tenantID, environmentID, subMapping.EntityID, mappedCustomerIDs, input, result)
	}
}

// snapshotSubscription computes one subscription's usage for the window and writes a usage record.
func (a *SnapshotActivities) snapshotSubscription(
	envCtx context.Context,
	tenantID, environmentID, subscriptionID string,
	mappedCustomerIDs map[string]bool,
	input temporalModels.MarketplaceUsageSnapshotActivityInput,
	result *temporalModels.MarketplaceUsageSnapshotWorkflowResult,
) {
	sub, err := a.subscriptionRepo.Get(envCtx, subscriptionID)
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage snapshot failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", subscriptionID,
			"period_start", input.PeriodStart, "period_end", input.PeriodEnd, "error", err, "stage", "get_subscription")
		result.Failed++
		return
	}

	// Only report subscriptions whose customer also has an aws_marketplace mapping.
	if !mappedCustomerIDs[sub.CustomerID] {
		return
	}

	result.Total++

	// The reporting window is deterministic per scheduled run, so a matching row means an earlier
	// Temporal attempt for this exact activity call already wrote it (activity retries re-run the
	// whole loop, including subscriptions a prior attempt already finished). Skip re-computing and
	// re-inserting it — this is what makes the activity safe to retry.
	alreadyExists, err := a.usageRecordRepo.ExistsForPeriod(envCtx, sub.ID, input.PeriodStart, input.PeriodEnd)
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage snapshot failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", sub.ID, "customer_id", sub.CustomerID,
			"period_start", input.PeriodStart, "period_end", input.PeriodEnd, "error", err, "stage", "check_existing")
		result.Failed++
		return
	}
	if alreadyExists {
		result.Succeeded++
		return
	}

	usageResp, err := a.subscriptionService.GetMeterUsageBySubscription(envCtx, &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: sub.ID,
		StartTime:      input.PeriodStart,
		EndTime:        input.PeriodEnd,
		Source:         string(types.UsageSourceInvoiceCreation),
	})
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage snapshot failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", sub.ID, "customer_id", sub.CustomerID,
			"period_start", input.PeriodStart, "period_end", input.PeriodEnd, "error", err, "stage", "get_meter_usage")
		result.Failed++
		return
	}

	_, totalAmount, err := a.billingService.CalculateMeterUsageCharges(
		envCtx, sub, usageResp, input.PeriodStart, input.PeriodEnd, types.UsageSourceInvoiceCreation,
	)
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage snapshot failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", sub.ID, "customer_id", sub.CustomerID,
			"period_start", input.PeriodStart, "period_end", input.PeriodEnd, "error", err, "stage", "calculate_charges")
		result.Failed++
		return
	}

	// UsageRecord stores the subscription's native currency as the source of truth — this table is
	// shared across marketplaces (AWS/Azure/GCP), so any marketplace-mandated currency conversion
	// (AWS requires USD; Azure/GCP may not) happens per-marketplace at report time, not here.
	customerExternalID := ""
	if cust, custErr := a.customerRepo.Get(envCtx, sub.CustomerID); custErr == nil && cust != nil {
		customerExternalID = cust.ExternalID
	}

	rec := &usagerecord.UsageRecord{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_USAGE_RECORD),
		CustomerID:         sub.CustomerID,
		CustomerExternalID: customerExternalID,
		SubscriptionID:     sub.ID,
		PlanID:             sub.PlanID,
		Amount:             totalAmount,
		Currency:           usageResp.Currency,
		PeriodStart:        input.PeriodStart,
		PeriodEnd:          input.PeriodEnd,
		Syncs:              map[usagerecord.Marketplace]usagerecord.MarketplaceSyncEntry{},
		AllProvidersSynced: false,
		EnvironmentID:      environmentID,
		BaseModel:          types.GetDefaultBaseModel(envCtx),
	}

	if err := a.usageRecordRepo.Create(envCtx, rec); err != nil {
		// The ExistsForPeriod check above is a fast pre-check, not the source of truth — the unique
		// index on (subscription_id, period_start, period_end) is. A concurrent execution can still
		// win the race between the check and this insert; that shows up here as ErrAlreadyExists,
		// and means the record is already written, so it's a success, not a failure.
		if ierr.IsAlreadyExists(err) {
			result.Succeeded++
			return
		}
		a.logger.Error(envCtx, "marketplace usage snapshot failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", sub.ID, "customer_id", sub.CustomerID,
			"period_start", input.PeriodStart, "period_end", input.PeriodEnd, "error", err, "stage", "create_usage_record")
		result.Failed++
		return
	}

	result.Succeeded++
}
