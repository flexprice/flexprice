package marketplace

import (
	"context"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/usagerecord"
	"github.com/flexprice/flexprice/internal/integration/awsmarketplace"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	temporalModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"go.temporal.io/sdk/activity"
)

// ReportActivities reports usage records that have not yet been synced to their marketplace. For
// every published aws_marketplace connection it reads the connection's unsynced usage records,
// reports them to AWS in batches, and records the returned metering id on each record AWS accepts.
// Records AWS does not accept are left unsynced so the next run retries them; there is no terminal
// failure state.
type ReportActivities struct {
	connectionRepo               connection.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	usageRecordRepo              usagerecord.Repository
	encryptionService            security.EncryptionService
	awsClient                    awsmarketplace.Client
	logger                       *logger.Logger
}

func NewReportActivities(
	connectionRepo connection.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	usageRecordRepo usagerecord.Repository,
	encryptionService security.EncryptionService,
	awsClient awsmarketplace.Client,
	log *logger.Logger,
) *ReportActivities {
	return &ReportActivities{
		connectionRepo:               connectionRepo,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		usageRecordRepo:              usageRecordRepo,
		encryptionService:            encryptionService,
		awsClient:                    awsClient,
		logger:                       log,
	}
}

// MarketplaceUsageReportActivity is the activity entrypoint. It processes every tenant/environment
// that has a published aws_marketplace connection; each connection is handled independently so a
// failure in one does not stop the others.
func (a *ReportActivities) MarketplaceUsageReportActivity(
	ctx context.Context,
	_ temporalModels.MarketplaceUsageReportWorkflowInput,
) (*temporalModels.MarketplaceUsageReportWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Starting MarketplaceUsageReportActivity")

	result := &temporalModels.MarketplaceUsageReportWorkflowResult{}

	conns, err := a.connectionRepo.ListPublishedByProvider(ctx, types.SecretProviderAWSMarketplace)
	if err != nil {
		a.logger.Error(ctx, "marketplace usage report failed", "error", err, "stage", "list_connections")
		return nil, err
	}

	for _, conn := range conns {
		envCtx := context.WithValue(context.WithValue(ctx, types.CtxTenantID, conn.TenantID), types.CtxEnvironmentID, conn.EnvironmentID)
		a.reportConnectionUsage(envCtx, conn, result)
	}

	log.Info("Completed MarketplaceUsageReportActivity",
		"total", result.Total, "succeeded", result.Succeeded, "failed", result.Failed)
	return result, nil
}

// reportConnectionUsage reports each of one connection's unsynced usage records to AWS. A record
// AWS accepts is marked synced with its metering id; a record AWS does not process (or one whose
// mapping is missing) is left unsynced so the next run retries it.
func (a *ReportActivities) reportConnectionUsage(envCtx context.Context, conn *connection.Connection, result *temporalModels.MarketplaceUsageReportWorkflowResult) {
	tenantID := conn.TenantID
	environmentID := conn.EnvironmentID

	if conn.EncryptedSecretData.AWSMarketplace == nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID,
			"error", "connection has no aws_marketplace secret data", "stage", "read_connection")
		return
	}

	// The region is saved on the connection at creation time; it selects the AWS Marketplace
	// Metering endpoint and is required.
	region := ""
	if meta, ok := conn.Metadata["aws_marketplace"].(map[string]interface{}); ok {
		region, _ = meta["region"].(string)
	}
	if region == "" {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID,
			"error", "connection has no region in metadata", "stage", "read_connection")
		return
	}

	roleArn, err := a.encryptionService.Decrypt(conn.EncryptedSecretData.AWSMarketplace.RoleArn)
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "error", err, "stage", "decrypt_role_arn")
		return
	}
	externalID, err := a.encryptionService.Decrypt(conn.EncryptedSecretData.AWSMarketplace.ExternalID)
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "error", err, "stage", "decrypt_external_id")
		return
	}

	records, err := a.usageRecordRepo.ListUnsynced(envCtx, tenantID, environmentID)
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "error", err, "stage", "list_unsynced")
		return
	}
	if len(records) == 0 {
		return
	}

	mappings, err := a.loadMappings(envCtx)
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "error", err, "stage", "load_mappings")
		return
	}

	// Assume the tenant's role once; the short-lived credentials cover one run's records.
	creds, err := a.awsClient.AssumeRole(envCtx, roleArn, externalID)
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "error", err, "stage", "assume_role")
		return
	}

	for _, rec := range records {
		result.Total++
		a.reportRecord(envCtx, conn.ID, rec, mappings, creds, region, result)
	}
}

// reportRecord reports a single usage record to AWS and records the outcome on it.
func (a *ReportActivities) reportRecord(
	envCtx context.Context,
	connectionID string,
	rec *usagerecord.UsageRecord,
	mappings *connectionMappings,
	creds awssdk.Credentials,
	region string,
	result *temporalModels.MarketplaceUsageReportWorkflowResult,
) {
	tenantID := types.GetTenantID(envCtx)
	environmentID := types.GetEnvironmentID(envCtx)

	licenseArn := mappings.licenseArnBySubscription[rec.SubscriptionID]
	customerAWSAccountID := mappings.awsAccountByCustomer[rec.CustomerID]
	plan, planFound := mappings.plan[rec.PlanID]
	if licenseArn == "" || customerAWSAccountID == "" || !planFound || plan.dimension == "" {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"customer_id", rec.CustomerID, "plan_id", rec.PlanID,
			"error", "missing license_arn, customer_aws_account_id, or plan dimension mapping", "stage", "resolve_record")
		result.Failed++
		return
	}

	// ProductCode is sent only for legacy products; for concurrent-agreements products the license
	// identifies the product and ProductCode must be omitted.
	productCode := plan.productCode
	if plan.concurrentAgreements {
		productCode = ""
	}

	res, err := a.awsClient.BatchMeterUsage(envCtx, creds, region, awsmarketplace.UsageRecordInput{
		CustomerAWSAccountID: customerAWSAccountID,
		LicenseArn:           licenseArn,
		ProductCode:          productCode,
		Dimension:            plan.dimension,
		// AWS Quantity is an integer, so the dollar amount is reported as whole units (rate is one
		// unit per dollar). PeriodEnd is the timestamp so a retry sends an identical record and AWS
		// de-duplicates it.
		Quantity:  int32(rec.Amount.Round(0).IntPart()),
		Timestamp: rec.PeriodEnd,
	})
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"license_arn", licenseArn, "dimension", plan.dimension, "amount", rec.Amount, "error", err, "stage", "batch_meter_usage")
		result.Failed++
		return
	}
	if res == nil {
		// AWS returned the record as unprocessed; leaving it unsynced retries it next run.
		a.logger.Warn(envCtx, "marketplace usage record not processed by aws, will retry next run",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"license_arn", licenseArn, "dimension", plan.dimension, "amount", rec.Amount)
		result.Failed++
		return
	}

	entry := usagerecord.MarketplaceSyncEntry{
		ConnectionID:        connectionID,
		SyncedAt:            lo.ToPtr(time.Now().UTC()),
		MarketplaceReportID: res.MeteringRecordID,
	}
	// AWS is the only marketplace in scope, so a record is fully synced once its AWS entry exists.
	if err := a.usageRecordRepo.UpdateSyncResult(envCtx, rec.ID, usagerecord.MarketplaceAWS, entry, true); err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"license_arn", licenseArn, "dimension", plan.dimension, "amount", rec.Amount, "error", err, "stage", "write_sync_result")
		result.Failed++
		return
	}
	result.Succeeded++
}

// planMapping holds the plan-level AWS configuration resolved from the plan's entity mapping.
type planMapping struct {
	productCode          string
	dimension            string
	concurrentAgreements bool
}

// connectionMappings holds the AWS identifiers for a connection, indexed by Flexprice entity id.
type connectionMappings struct {
	licenseArnBySubscription map[string]string
	awsAccountByCustomer     map[string]string
	plan                     map[string]planMapping
}

// loadMappings loads the subscription, customer and plan mappings for the current tenant/
// environment in one pass each and indexes them for lookup while reporting.
func (a *ReportActivities) loadMappings(envCtx context.Context) (*connectionMappings, error) {
	providerType := string(types.SecretProviderAWSMarketplace)

	subMappings, err := a.entityIntegrationMappingRepo.List(envCtx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypeSubscription,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		return nil, err
	}
	customerMappings, err := a.entityIntegrationMappingRepo.List(envCtx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypeCustomer,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		return nil, err
	}
	planMappings, err := a.entityIntegrationMappingRepo.List(envCtx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypePlan,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		return nil, err
	}

	m := &connectionMappings{
		licenseArnBySubscription: make(map[string]string, len(subMappings)),
		awsAccountByCustomer:     make(map[string]string, len(customerMappings)),
		plan:                     make(map[string]planMapping, len(planMappings)),
	}
	for _, sm := range subMappings {
		m.licenseArnBySubscription[sm.EntityID] = sm.ProviderEntityID
	}
	for _, cm := range customerMappings {
		m.awsAccountByCustomer[cm.EntityID] = cm.ProviderEntityID
	}
	for _, pm := range planMappings {
		concurrent, _ := pm.Metadata["concurrent_agreements"].(bool)
		dimension, _ := pm.Metadata["dimension"].(string)
		m.plan[pm.EntityID] = planMapping{
			productCode:          pm.ProviderEntityID,
			dimension:            dimension,
			concurrentAgreements: concurrent,
		}
	}
	return m, nil
}
