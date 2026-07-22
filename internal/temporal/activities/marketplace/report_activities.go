package marketplace

import (
	"context"
	"math"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/usagerecord"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration/awsmarketplace"
	"github.com/flexprice/flexprice/internal/integration/gcpmarketplace"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	temporalModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/activity"
	servicecontrol "google.golang.org/api/servicecontrol/v1"
)

// marketplaceReportingCurrency is the currency both AWS and GCP Marketplace bill i,
// neither offers seller-facing currency selection
const marketplaceReportingCurrency = "usd"

// awsPlanMapping holds the plan-level AWS configuration resolved from the plan's entity mapping.
type awsPlanMapping struct {
	productCode          string
	dimension            string
	concurrentAgreements bool
}

// awsConnectionMappings holds the AWS identifiers for a connection, indexed by Flexprice entity id.
type awsConnectionMappings struct {
	licenseArnBySubscription map[string]string
	awsAccountByCustomer     map[string]string
	plan                     map[string]awsPlanMapping
}

// gcpPlanMapping holds the plan-level GCP configuration resolved from the plan's entity mapping.
type gcpPlanMapping struct {
	serviceName string
	metricName  string
}

// gcpConnectionMappings holds the GCP identifiers for a connection, indexed by Flexprice entity id.
// consumerId comes entirely from the subscription mapping, so no customer index is needed.
type gcpConnectionMappings struct {
	usageReportingIDBySubscription map[string]string
	plan                           map[string]gcpPlanMapping
}

// preparedConnection is a published marketplace connection that's been authenticated and had its
// mappings loaded, ready to report records through. Exactly one provider's fields are set, matching
// conn.ProviderType.
type preparedConnection struct {
	conn *connection.Connection

	awsCreds    awssdk.Credentials
	awsRegion   string
	awsMappings *awsConnectionMappings

	gcpSvc      *servicecontrol.Service
	gcpMappings *gcpConnectionMappings
}

// isRelevantForSubscription reports whether this connection is mapped to subscriptionID — i.e.
// whether a usage record for that subscription needs to reach it at all.
func (pc *preparedConnection) isRelevantForSubscription(subscriptionID string) bool {
	switch pc.conn.ProviderType {
	case types.SecretProviderAWSMarketplace:
		return pc.awsMappings != nil && pc.awsMappings.licenseArnBySubscription[subscriptionID] != ""
	case types.SecretProviderGCPMarketplace:
		return pc.gcpMappings != nil && pc.gcpMappings.usageReportingIDBySubscription[subscriptionID] != ""
	}
	return false
}

// ReportActivities reports usage records that have not yet reached every marketplace connection
// relevant to them. For every tenant/environment with a published marketplace connection (AWS or
// GCP), it authenticates each connection once, reads that tenant's unsynced usage records once, and
// reports each record to whichever relevant connections it hasn't already reached. A record a
// marketplace rejects is left unsynced so the next run retries it; there is no terminal failure state.
type ReportActivities struct {
	connectionRepo               connection.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	usageRecordRepo              usagerecord.Repository
	encryptionService            security.EncryptionService
	awsClient                    awsmarketplace.Client
	gcpClient                    gcpmarketplace.Client
	logger                       *logger.Logger
}

func NewReportActivities(
	connectionRepo connection.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	usageRecordRepo usagerecord.Repository,
	encryptionService security.EncryptionService,
	awsClient awsmarketplace.Client,
	gcpClient gcpmarketplace.Client,
	log *logger.Logger,
) *ReportActivities {
	return &ReportActivities{
		connectionRepo:               connectionRepo,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		usageRecordRepo:              usageRecordRepo,
		encryptionService:            encryptionService,
		awsClient:                    awsClient,
		gcpClient:                    gcpClient,
		logger:                       log,
	}
}

// MarketplaceUsageReportActivity is the activity entrypoint. A record's relevant connections and its
// unsynced status are both tenant/environment-scoped, so it groups every published marketplace
// connection by tenant/environment and reports each group independently — a failure in one tenant
// does not stop the others.
func (a *ReportActivities) MarketplaceUsageReportActivity(
	ctx context.Context,
	_ temporalModels.MarketplaceUsageReportWorkflowInput,
) (*temporalModels.MarketplaceUsageReportWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Starting MarketplaceUsageReportActivity")

	result := &temporalModels.MarketplaceUsageReportWorkflowResult{}

	type tenantEnv struct{ tenantID, environmentID string }
	connsByTenant := make(map[tenantEnv][]*connection.Connection)
	for _, providerType := range marketplaceProviderTypes {
		conns, err := a.connectionRepo.ListPublishedByProvider(ctx, providerType)
		if err != nil {
			a.logger.Error(ctx, "marketplace usage report failed", "provider_type", providerType, "error", err, "stage", "list_connections")
			continue
		}
		for _, conn := range conns {
			key := tenantEnv{conn.TenantID, conn.EnvironmentID}
			connsByTenant[key] = append(connsByTenant[key], conn)
		}
	}

	for key, conns := range connsByTenant {
		ctx := types.SetTenantID(ctx, key.tenantID)
		ctx = types.SetEnvironmentID(ctx, key.environmentID)
		a.reportForTenant(ctx, key.tenantID, key.environmentID, conns, result)
	}

	log.Info("Completed MarketplaceUsageReportActivity",
		"total", result.Total, "succeeded", result.Succeeded, "failed", result.Failed)
	return result, nil
}

// reportForTenant authenticates each of this tenant/environment's published connections once,
// fetches its unsynced usage records once, and reports each record to the relevant ones. Both lists
// are fixed for the whole call — nothing is re-queried mid-run, so a connection deleted while this
// run executes only takes effect from the next scheduled run.
func (a *ReportActivities) reportForTenant(
	ctx context.Context,
	tenantID, environmentID string,
	conns []*connection.Connection,
	result *temporalModels.MarketplaceUsageReportWorkflowResult,
) {
	prepared := make([]*preparedConnection, 0, len(conns))
	for _, conn := range conns {
		pc, err := a.prepareConnection(ctx, conn)
		if err != nil {
			continue // already logged inside prepareConnection at the stage that failed
		}
		prepared = append(prepared, pc)
	}
	if len(prepared) == 0 {
		return
	}

	records, err := a.usageRecordRepo.ListUnsynced(ctx, tenantID, environmentID)
	if err != nil {
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "error", err, "stage", "list_unsynced")
		return
	}

	for _, rec := range records {
		if a.isEligibleForReport(ctx, rec) {
			a.reportRecord(ctx, rec, prepared, result)
		}
	}
}

// reportRecord reports one record to every connection that's relevant to its subscription and
// doesn't already have a syncs entry, then persists the updated map once — with synced=true only if
// every relevant connection now has an entry. A relevant connection that rejects the record leaves
// it unsynced for the next run.
func (a *ReportActivities) reportRecord(
	ctx context.Context,
	rec *usagerecord.UsageRecord,
	prepared []*preparedConnection,
	result *temporalModels.MarketplaceUsageReportWorkflowResult,
) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	var relevant []*preparedConnection
	for _, pc := range prepared {
		if pc.isRelevantForSubscription(rec.SubscriptionID) {
			relevant = append(relevant, pc)
		}
	}
	if len(relevant) == 0 {
		return // no published connection is mapped to this subscription right now
	}

	result.Total++

	added := false
	for _, pc := range relevant {
		if _, done := rec.Syncs[pc.conn.ID]; done {
			continue // already reported to this connection on an earlier run
		}

		var entry types.UsageRecordSyncEntry
		var ok bool
		switch pc.conn.ProviderType {
		case types.SecretProviderAWSMarketplace:
			entry, ok = a.reportAWSRecord(ctx, rec, pc)
		case types.SecretProviderGCPMarketplace:
			entry, ok = a.reportGCPRecord(ctx, rec, pc)
		}
		if !ok {
			continue // already logged inside the provider method
		}

		rec.Syncs[pc.conn.ID] = entry
		added = true
		a.logger.Info(ctx, "marketplace usage record synced",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "connection_id", pc.conn.ID, "marketplace", pc.conn.ProviderType,
			"reporting_id", entry.ReportingID)
	}

	// Fully synced only when every relevant connection has an entry.
	synced := true
	for _, pc := range relevant {
		if _, done := rec.Syncs[pc.conn.ID]; !done {
			synced = false
			break
		}
	}

	// Nothing new reported and still not fully synced: leave the row as-is for the next run.
	if !added && !synced {
		result.Failed++
		return
	}

	if err := a.usageRecordRepo.MarkSynced(ctx, rec.ID, rec.Syncs, synced); err != nil {
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "error", err, "stage", "mark_synced")
		result.Failed++
		return
	}

	if synced {
		result.Succeeded++
	} else {
		result.Failed++
	}
}

// prepareConnection decrypts and authenticates one connection and loads its provider's mappings once.
func (a *ReportActivities) prepareConnection(ctx context.Context, conn *connection.Connection) (*preparedConnection, error) {
	switch conn.ProviderType {
	case types.SecretProviderAWSMarketplace:
		creds, region, mappings, err := a.authAWSConnection(ctx, conn)
		if err != nil {
			return nil, err
		}
		return &preparedConnection{conn: conn, awsCreds: creds, awsRegion: region, awsMappings: mappings}, nil
	case types.SecretProviderGCPMarketplace:
		svc, mappings, err := a.authGCPConnection(ctx, conn)
		if err != nil {
			return nil, err
		}
		return &preparedConnection{conn: conn, gcpSvc: svc, gcpMappings: mappings}, nil
	}
	return nil, ierr.NewErrorf("unsupported marketplace provider type %q", conn.ProviderType).Mark(ierr.ErrValidation)
}

// isEligibleForReport applies the validation common to both providers before any payload is built:
// only USD is accepted (a non-USD record stays unsynced, retried once currency conversion lands),
// and only a positive amount is accepted — both AWS and GCP reject non-positive quantities, and a
// billing correction can legitimately net a window's amount to zero or negative.
func (a *ReportActivities) isEligibleForReport(ctx context.Context, rec *usagerecord.UsageRecord) bool {
	if !types.IsMatchingCurrency(rec.Currency, marketplaceReportingCurrency) {
		a.logger.Debug(ctx, "skipping marketplace usage record, currency not usd",
			"subscription_id", rec.SubscriptionID, "usage_record_id", rec.ID, "currency", rec.Currency)
		return false
	}
	if rec.Amount.IsNegative() {
		a.logger.Info(ctx, "skipping marketplace usage record, non-positive amount",
			"subscription_id", rec.SubscriptionID, "usage_record_id", rec.ID)
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// AWS
// ---------------------------------------------------------------------------

// authAWSConnection decrypts a connection's AWS secret, loads its mappings, and assumes the tenant's
// role once.
func (a *ReportActivities) authAWSConnection(ctx context.Context, conn *connection.Connection) (awssdk.Credentials, string, *awsConnectionMappings, error) {
	tenantID := conn.TenantID
	environmentID := conn.EnvironmentID

	if conn.EncryptedSecretData.AWSMarketplace == nil {
		err := ierr.NewError("connection has no aws_marketplace secret data").Mark(ierr.ErrValidation)
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "read_connection")
		return awssdk.Credentials{}, "", nil, err
	}

	// The region is saved on the connection at creation time (sync_config.aws_marketplace.region,
	// same home as S3's bucket/region); it selects the AWS Marketplace Metering endpoint and is required.
	if conn.SyncConfig == nil || conn.SyncConfig.AWSMarketplace == nil || conn.SyncConfig.AWSMarketplace.Region == "" {
		err := ierr.NewError("connection has no region in sync_config").Mark(ierr.ErrValidation)
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "read_connection")
		return awssdk.Credentials{}, "", nil, err
	}
	region := conn.SyncConfig.AWSMarketplace.Region

	roleArn, err := a.encryptionService.Decrypt(conn.EncryptedSecretData.AWSMarketplace.RoleArn)
	if err != nil {
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "decrypt_role_arn")
		return awssdk.Credentials{}, "", nil, err
	}
	externalID, err := a.encryptionService.Decrypt(conn.EncryptedSecretData.AWSMarketplace.ExternalID)
	if err != nil {
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "decrypt_external_id")
		return awssdk.Credentials{}, "", nil, err
	}

	mappings, err := a.loadAWSMappings(ctx)
	if err != nil {
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "load_mappings")
		return awssdk.Credentials{}, "", nil, err
	}

	// Assume the tenant's role once; the same static credentials are reused for every record this
	// connection reports (they are a one-time snapshot, not a live auto-refreshing provider). A busy
	// tenant's loop can run close to the activity's 30-minute StartToCloseTimeout, so the session
	// must outlive that — 1 hour is requested because every IAM role supports it without the tenant
	// needing to raise MaxSessionDuration.
	creds, err := a.awsClient.AssumeRole(ctx, roleArn, externalID, time.Hour)
	if err != nil {
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "assume_role")
		return awssdk.Credentials{}, "", nil, err
	}

	return creds, region, mappings, nil
}

// reportAWSRecord reports a single usage record to AWS and returns the sync entry to persist on
// success.
func (a *ReportActivities) reportAWSRecord(ctx context.Context, rec *usagerecord.UsageRecord, pc *preparedConnection) (types.UsageRecordSyncEntry, bool) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	mappings := pc.awsMappings

	licenseArn := mappings.licenseArnBySubscription[rec.SubscriptionID]
	customerAWSAccountID := mappings.awsAccountByCustomer[rec.CustomerID]
	plan, planFound := mappings.plan[rec.PlanID]
	if licenseArn == "" || customerAWSAccountID == "" || !planFound || plan.dimension == "" {
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"customer_id", rec.CustomerID, "plan_id", rec.PlanID, "connection_id", pc.conn.ID,
			"error", "missing license_arn, customer_aws_account_id, or plan dimension mapping", "stage", "resolve_record")
		return types.UsageRecordSyncEntry{}, false
	}

	// ProductCode is sent only for legacy products; for concurrent-agreements products the license
	// identifies the product and ProductCode must be omitted.
	productCode := plan.productCode
	if plan.concurrentAgreements {
		productCode = ""
	}

	// Only called for USD records (see isEligibleForReport). AWS only accepts a whole number, so it's
	// sent in USD cents — the tenant prices their dimension per cent (see setup docs), which keeps
	// sub-dollar amounts from rounding away. Turning cents into a charge is AWS's job: it bills
	// quantity x the dimension's rate.
	quantity := types.ToSmallestUnit(rec.Amount, marketplaceReportingCurrency)
	if quantity > math.MaxInt32 {
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"connection_id", pc.conn.ID, "amount", rec.Amount, "currency", rec.Currency, "quantity", quantity,
			"error", "quantity exceeds the maximum aws accepts", "stage", "convert_quantity")
		return types.UsageRecordSyncEntry{}, false
	}

	res, err := a.awsClient.BatchMeterUsage(ctx, pc.awsCreds, pc.awsRegion, awsmarketplace.UsageRecordInput{
		CustomerAWSAccountID: customerAWSAccountID,
		LicenseArn:           licenseArn,
		ProductCode:          productCode,
		Dimension:            plan.dimension,
		Quantity:             int32(quantity),
		// PeriodEnd is the timestamp so a retry sends an identical record and AWS de-duplicates it.
		Timestamp: rec.PeriodEnd,
	})
	if err != nil {
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"connection_id", pc.conn.ID, "license_arn", licenseArn, "dimension", plan.dimension, "amount", rec.Amount,
			"error", err, "stage", "batch_meter_usage")
		return types.UsageRecordSyncEntry{}, false
	}
	if res == nil {
		// AWS returned the record as unprocessed; leaving it unsynced retries it next run.
		a.logger.Info(ctx, "marketplace usage record not processed by aws, will retry next run",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"connection_id", pc.conn.ID, "license_arn", licenseArn, "dimension", plan.dimension, "amount", rec.Amount)
		return types.UsageRecordSyncEntry{}, false
	}

	// A present result is not the same as an accepted record — AWS's Status must be checked. Only
	// StatusSuccess means the record was honored; the other two are distinct failure modes with
	// different causes, so each gets its own log line.
	switch res.Status {
	case awsmarketplace.StatusSuccess:
		// falls through to the return below
	case awsmarketplace.StatusCustomerNotSubscribed:
		// The buyer has no active agreement for this product, or their AWS account was suspended.
		// Resolves itself once the buyer (re)subscribes, so it keeps retrying rather than needing
		// manual action.
		a.logger.Error(ctx, "marketplace usage report rejected by aws: customer not subscribed, will retry next run",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"connection_id", pc.conn.ID, "customer_id", rec.CustomerID, "license_arn", licenseArn,
			"dimension", plan.dimension, "amount", rec.Amount, "error", "customer_not_subscribed")
		return types.UsageRecordSyncEntry{}, false
	case awsmarketplace.StatusDuplicateRecord:
		// NOT "AWS already has this exact record, safe to skip" — AWS has a DIFFERENT record for the
		// same customer+dimension+timestamp already on file, and rejected this one. Retrying with the
		// same amount hits the same rejection every time; this needs a human to fix the mismatch.
		a.logger.Error(ctx, "marketplace usage report rejected by aws: conflicts with a different record already on file, needs manual investigation",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"connection_id", pc.conn.ID, "customer_id", rec.CustomerID, "license_arn", licenseArn,
			"dimension", plan.dimension, "amount", rec.Amount, "period_end", rec.PeriodEnd, "error", "duplicate_record")
		return types.UsageRecordSyncEntry{}, false
	default:
		a.logger.Error(ctx, "marketplace usage report rejected by aws: unrecognized status, will retry next run",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"connection_id", pc.conn.ID, "license_arn", licenseArn, "dimension", plan.dimension, "amount", rec.Amount,
			"aws_status", res.Status, "error", "unrecognized_aws_status")
		return types.UsageRecordSyncEntry{}, false
	}

	return types.UsageRecordSyncEntry{
		Marketplace: types.SecretProviderAWSMarketplace,
		ReportingID: res.MeteringRecordID,
		SyncedAt:    time.Now().UTC(),
	}, true
}

// loadAWSMappings loads the subscription, customer and plan mappings for the current tenant/
// environment and indexes them for lookup while reporting.
func (a *ReportActivities) loadAWSMappings(ctx context.Context) (*awsConnectionMappings, error) {
	providerType := string(types.SecretProviderAWSMarketplace)

	subMappings, err := a.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypeSubscription,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		return nil, err
	}
	customerMappings, err := a.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypeCustomer,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		return nil, err
	}
	planMappings, err := a.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypePlan,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		return nil, err
	}

	m := &awsConnectionMappings{
		licenseArnBySubscription: make(map[string]string, len(subMappings)),
		awsAccountByCustomer:     make(map[string]string, len(customerMappings)),
		plan:                     make(map[string]awsPlanMapping, len(planMappings)),
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
		m.plan[pm.EntityID] = awsPlanMapping{
			productCode:          pm.ProviderEntityID,
			dimension:            dimension,
			concurrentAgreements: concurrent,
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// GCP
// ---------------------------------------------------------------------------

// authGCPConnection decrypts a connection's GCP secret, loads its mappings, and performs the WIF
// exchange once.
func (a *ReportActivities) authGCPConnection(ctx context.Context, conn *connection.Connection) (*servicecontrol.Service, *gcpConnectionMappings, error) {
	tenantID := conn.TenantID
	environmentID := conn.EnvironmentID

	if conn.EncryptedSecretData.GCPMarketplace == nil {
		err := ierr.NewError("connection has no gcp_marketplace secret data").Mark(ierr.ErrValidation)
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "read_connection")
		return nil, nil, err
	}

	credentialsJSON, err := a.encryptionService.Decrypt(conn.EncryptedSecretData.GCPMarketplace.CredentialsJSON)
	if err != nil {
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "decrypt_credentials_json")
		return nil, nil, err
	}

	mappings, err := a.loadGCPMappings(ctx)
	if err != nil {
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "load_mappings")
		return nil, nil, err
	}

	// One WIF exchange per connection; the resulting client is reused for every record this
	// connection reports, mirroring the single AssumeRole-per-connection pattern on the AWS path.
	svc, err := a.gcpClient.WifSession(ctx, credentialsJSON)
	if err != nil {
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "wif_session")
		return nil, nil, err
	}

	return svc, mappings, nil
}

// reportGCPRecord reports a single usage record to GCP and returns the sync entry to persist on
// success.
func (a *ReportActivities) reportGCPRecord(ctx context.Context, rec *usagerecord.UsageRecord, pc *preparedConnection) (types.UsageRecordSyncEntry, bool) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	mappings := pc.gcpMappings

	consumerID := mappings.usageReportingIDBySubscription[rec.SubscriptionID]
	plan, planFound := mappings.plan[rec.PlanID]
	if consumerID == "" || !planFound || plan.serviceName == "" || plan.metricName == "" {
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"customer_id", rec.CustomerID, "plan_id", rec.PlanID, "connection_id", pc.conn.ID,
			"error", "missing usage_reporting_id or plan service_name/metric_name mapping", "stage", "resolve_record")
		return types.UsageRecordSyncEntry{}, false
	}

	// Only called for USD records (see isEligibleForReport). GCP accepts an int64 value, and
	// ToSmallestUnit already returns int64, so unlike AWS's int32 Quantity there's no overflow guard.
	cents := types.ToSmallestUnit(rec.Amount, marketplaceReportingCurrency)

	reportResult, err := a.gcpClient.Report(ctx, pc.gcpSvc, gcpmarketplace.UsageReportInput{
		ServiceName: plan.serviceName,
		ConsumerID:  consumerID,
		MetricName:  plan.metricName,
		ValueCents:  cents,
		// OperationID = the record's own id, so a retry sends an identical operation and GCP de-dupes.
		OperationID: rec.ID,
		StartTime:   rec.PeriodStart,
		EndTime:     rec.PeriodEnd,
	})
	if err != nil {
		a.logger.Error(ctx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"connection_id", pc.conn.ID, "error", err, "stage", "services_report")
		return types.UsageRecordSyncEntry{}, false
	}

	// HTTP 200 is not the same as accepted — reportErrors must be checked. Common codes:
	// 5=NOT_FOUND (consumer inactive), 7=PERMISSION_DENIED, 3=INVALID_ARGUMENT.
	if !reportResult.Accepted {
		a.logger.Error(ctx, "marketplace usage report rejected by gcp, will retry next run",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"connection_id", pc.conn.ID, "error", "rejected_by_gcp", "error_code", reportResult.ErrorCode,
			"error_message", reportResult.ErrorMessage)
		return types.UsageRecordSyncEntry{}, false
	}

	// GCP returns no per-record receipt; the operationId echoed back (== rec.ID) becomes this
	// connection's reporting_id, read from the result rather than re-derived.
	return types.UsageRecordSyncEntry{
		Marketplace: types.SecretProviderGCPMarketplace,
		ReportingID: reportResult.OperationID,
		SyncedAt:    time.Now().UTC(),
	}, true
}

// loadGCPMappings loads the subscription and plan mappings for the current tenant/environment and
// indexes them for lookup while reporting.
func (a *ReportActivities) loadGCPMappings(ctx context.Context) (*gcpConnectionMappings, error) {
	providerType := string(types.SecretProviderGCPMarketplace)

	subMappings, err := a.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypeSubscription,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		return nil, err
	}
	planMappings, err := a.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypePlan,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		return nil, err
	}

	m := &gcpConnectionMappings{
		usageReportingIDBySubscription: make(map[string]string, len(subMappings)),
		plan:                           make(map[string]gcpPlanMapping, len(planMappings)),
	}
	for _, sm := range subMappings {
		m.usageReportingIDBySubscription[sm.EntityID] = sm.ProviderEntityID
	}
	for _, pm := range planMappings {
		metricName, _ := pm.Metadata["metric_name"].(string)
		m.plan[pm.EntityID] = gcpPlanMapping{
			serviceName: pm.ProviderEntityID,
			metricName:  metricName,
		}
	}
	return m, nil
}
