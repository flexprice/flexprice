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

// marketplaceProviderTypes are the marketplace SecretProviders both crons iterate. Adding a further
// marketplace (Azure) means adding its constant here — nothing else about either cron's loop shape
// changes, since both were already written to process "every published marketplace connection,"
// not an AWS-specific list.
var marketplaceProviderTypes = []types.SecretProvider{
	types.SecretProviderAWSMarketplace,
	types.SecretProviderGCPMarketplace,
}

// SnapshotActivities creates the usage records that will later be reported to a marketplace. For
// every published marketplace connection (AWS or GCP) it computes each mapped subscription's usage
// for the reporting window, using the same commitment- and overage-aware computation as real
// invoicing, and writes one usage record per subscription in the subscription's own billing currency
// — marketplace-mandated currency conversion (both AWS and GCP require USD) happens per-marketplace
// at report time, not here. A record is provider-agnostic: it does not pin a connection_id, so if a
// subscription is mapped to more than one marketplace, the unique index on (subscription_id,
// period_start, period_end) collapses every connection's attempt into the same one row — the
// reporting cron fans that single row out to every relevant connection itself.
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
// tenant/environment that has a published marketplace connection, AWS or GCP.
func (a *SnapshotActivities) MarketplaceUsageSnapshotActivity(
	ctx context.Context,
	input temporalModels.MarketplaceUsageSnapshotActivityInput,
) (*temporalModels.MarketplaceUsageSnapshotWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Starting MarketplaceUsageSnapshotActivity", "period_start", input.PeriodStart, "period_end", input.PeriodEnd)

	result := &temporalModels.MarketplaceUsageSnapshotWorkflowResult{}

	for _, providerType := range marketplaceProviderTypes {
		conns, err := a.connectionRepo.ListPublishedByProvider(ctx, providerType)
		if err != nil {
			a.logger.Error(ctx, "marketplace usage snapshot failed", "provider_type", providerType, "error", err, "stage", "list_connections")
			continue
		}

		for _, conn := range conns {
			ctx := types.SetTenantID(ctx, conn.TenantID)
			ctx = types.SetEnvironmentID(ctx, conn.EnvironmentID)
			a.processConnection(ctx, conn, input, result)
		}
	}

	log.Info("Completed MarketplaceUsageSnapshotActivity",
		"total", result.Total, "succeeded", result.Succeeded, "failed", result.Failed)
	return result, nil
}

// processConnection writes a usage record for each of the connection's mapped subscriptions. It
// resolves the connection's mapped customers first, then snapshots each mapped subscription that
// belongs to one of them. Subscriptions are scoped to this connection's own provider_type — not "any
// marketplace" — but the resulting row itself is provider-agnostic: if the same subscription is also
// mapped to a different connection, that connection's own pass over this loop resolves to the same
// (subscription_id, period_start, period_end) and is absorbed by the ExistsForPeriod check in
// snapshotSubscription below, rather than writing a second row or losing the second connection's usage.
func (a *SnapshotActivities) processConnection(
	ctx context.Context,
	conn *connection.Connection,
	input temporalModels.MarketplaceUsageSnapshotActivityInput,
	result *temporalModels.MarketplaceUsageSnapshotWorkflowResult,
) {
	tenantID := conn.TenantID
	environmentID := conn.EnvironmentID
	providerType := string(conn.ProviderType)

	customerMappings, err := a.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypeCustomer,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		a.logger.Error(ctx, "marketplace usage snapshot failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "list_customer_mappings")
		return
	}
	mappedCustomerIDs := make(map[string]bool, len(customerMappings))
	for _, m := range customerMappings {
		mappedCustomerIDs[m.EntityID] = true
	}
	if len(mappedCustomerIDs) == 0 {
		return
	}

	subscriptionMappings, err := a.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypeSubscription,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		a.logger.Error(ctx, "marketplace usage snapshot failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "list_subscription_mappings")
		return
	}

	for _, subMapping := range subscriptionMappings {
		a.snapshotSubscription(ctx, tenantID, environmentID, subMapping.EntityID, mappedCustomerIDs, input, result)
	}
}

// snapshotSubscription computes one subscription's usage for the window and writes a usage record.
func (a *SnapshotActivities) snapshotSubscription(
	ctx context.Context,
	tenantID, environmentID, subscriptionID string,
	mappedCustomerIDs map[string]bool,
	input temporalModels.MarketplaceUsageSnapshotActivityInput,
	result *temporalModels.MarketplaceUsageSnapshotWorkflowResult,
) {
	// CalculateMeterUsageCharges iterates sub.LineItems to drive its per-line-item recalculation
	sub, _, err := a.subscriptionRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		a.logger.Error(ctx, "marketplace usage snapshot failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", subscriptionID,
			"period_start", input.PeriodStart, "period_end", input.PeriodEnd, "error", err, "stage", "get_subscription")
		result.Failed++
		return
	}

	// Only report subscriptions whose customer also has a marketplace mapping for this connection's provider.
	if !mappedCustomerIDs[sub.CustomerID] {
		return
	}

	result.Total++

	// The reporting window is deterministic per scheduled run, so a matching row means an earlier
	// Temporal attempt for this exact activity call already wrote it (activity retries re-run the
	// whole loop, including subscriptions a prior attempt already finished). Skip re-computing and
	// re-inserting it — this is what makes the activity safe to retry.
	alreadyExists, err := a.usageRecordRepo.ExistsForPeriod(ctx, sub.ID, input.PeriodStart, input.PeriodEnd)
	if err != nil {
		a.logger.Error(ctx, "marketplace usage snapshot failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", sub.ID, "customer_id", sub.CustomerID,
			"period_start", input.PeriodStart, "period_end", input.PeriodEnd, "error", err, "stage", "check_existing")
		result.Failed++
		return
	}
	if alreadyExists {
		result.Succeeded++
		return
	}

	usageResp, err := a.subscriptionService.GetMeterUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: sub.ID,
		StartTime:      input.PeriodStart,
		EndTime:        input.PeriodEnd,
		Source:         string(types.UsageSourceInvoiceCreation),
	})
	if err != nil {
		a.logger.Error(ctx, "marketplace usage snapshot failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", sub.ID, "customer_id", sub.CustomerID,
			"period_start", input.PeriodStart, "period_end", input.PeriodEnd, "error", err, "stage", "get_meter_usage")
		result.Failed++
		return
	}

	_, totalAmount, err := a.billingService.CalculateMeterUsageCharges(
		ctx, sub, usageResp, input.PeriodStart, input.PeriodEnd, types.UsageSourceInvoiceCreation,
	)
	if err != nil {
		a.logger.Error(ctx, "marketplace usage snapshot failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", sub.ID, "customer_id", sub.CustomerID,
			"period_start", input.PeriodStart, "period_end", input.PeriodEnd, "error", err, "stage", "calculate_charges")
		result.Failed++
		return
	}

	// UsageRecord stores the subscription's native currency as the source of truth — this table is
	// shared across marketplaces (AWS/Azure/GCP), so any marketplace-mandated currency conversion
	// (AWS and GCP both require USD) happens per-marketplace at report time, not here.
	customerExternalID := ""
	if cust, custErr := a.customerRepo.Get(ctx, sub.CustomerID); custErr == nil && cust != nil {
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
		Synced:             false,
		EnvironmentID:      environmentID,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}

	if err := a.usageRecordRepo.Create(ctx, rec); err != nil {
		// The ExistsForPeriod check above is a fast pre-check, not the source of truth — the unique
		// index on (subscription_id, period_start, period_end) is. A concurrent execution can still
		// win the race between the check and this insert; that shows up here as ErrAlreadyExists,
		// and means the record is already written, so it's a success, not a failure.
		if ierr.IsAlreadyExists(err) {
			result.Succeeded++
			return
		}
		a.logger.Error(ctx, "marketplace usage snapshot failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", sub.ID, "customer_id", sub.CustomerID,
			"period_start", input.PeriodStart, "period_end", input.PeriodEnd, "error", err, "stage", "create_usage_record")
		result.Failed++
		return
	}

	result.Succeeded++
}
