package marketplace

import (
	"context"
	"math"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/usagerecord"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration/awsmarketplace"
	"github.com/flexprice/flexprice/internal/integration/azuremarketplace"
	"github.com/flexprice/flexprice/internal/integration/gcpmarketplace"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
	servicecontrol "google.golang.org/api/servicecontrol/v1"
)

// marketplaceReportingCurrency is the currency all three marketplaces bill in; none offers
// seller-facing currency selection.
const marketplaceReportingCurrency = "usd"

// MarketplaceReporter owns everything needed to authenticate a marketplace connection and report one
// usage record to it. It is deliberately not a Temporal activity: it is the shared implementation
// behind both the scheduled reporting cron (ReportActivities) and the one-shot cancellation flush
// (FlushActivities), so neither can drift from the other in how it talks to a provider. Both hold
// the same instance rather than one activity reaching into the other.
type MarketplaceReporter struct {
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	encryptionService            security.EncryptionService
	awsClient                    awsmarketplace.Client
	gcpClient                    gcpmarketplace.Client
	azureClient                  azuremarketplace.Client
	logger                       *logger.Logger
}

// NewMarketplaceReporter builds the shared reporter; registration creates one and hands the same
// instance to both ReportActivities and FlushActivities.
func NewMarketplaceReporter(
	params service.ServiceParams,
	awsClient awsmarketplace.Client,
	gcpClient gcpmarketplace.Client,
	azureClient azuremarketplace.Client,
	log *logger.Logger,
) *MarketplaceReporter {
	return &MarketplaceReporter{
		entityIntegrationMappingRepo: params.EntityIntegrationMappingRepo,
		encryptionService:            params.EncryptionService,
		awsClient:                    awsClient,
		gcpClient:                    gcpClient,
		azureClient:                  azureClient,
		logger:                       log,
	}
}

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

// azurePlanMapping holds the plan-level Azure configuration resolved from the plan's entity mapping.
type azurePlanMapping struct {
	planID    string
	dimension string
}

// azureConnectionMappings holds the Azure identifiers for a connection, indexed by Flexprice entity id.
type azureConnectionMappings struct {
	resourceIDBySubscription map[string]string
	plan                     map[string]azurePlanMapping
}

// marketplaceConnection is an authenticated connection with its entity mappings loaded, ready to report
// records through. Only the fields for conn.ProviderType are populated.
type marketplaceConnection struct {
	conn *connection.Connection

	awsCreds    awssdk.Credentials
	awsRegion   string
	awsMappings *awsConnectionMappings

	gcpSvc      *servicecontrol.Service
	gcpMappings *gcpConnectionMappings

	azureToken    azuremarketplace.Token
	azureMappings *azureConnectionMappings
}

// hasAgreementFor reports whether the subscription holds an agreement on this connection's
// marketplace, which is what makes a usage record for it reportable here.
//
// A subscription is never mapped to a connection: an entity mapping records the subscription's
// identifier on a marketplace (license_arn, usage_reporting_id, resource_id) against a provider type,
// and a connection is only the credentials for talking to that provider. Replacing the connection
// leaves the agreement untouched.
func (marketplaceConn *marketplaceConnection) hasAgreementFor(subscriptionID string) bool {
	switch marketplaceConn.conn.ProviderType {
	case types.SecretProviderAWSMarketplace:
		return marketplaceConn.awsMappings != nil && marketplaceConn.awsMappings.licenseArnBySubscription[subscriptionID] != ""
	case types.SecretProviderGCPMarketplace:
		return marketplaceConn.gcpMappings != nil && marketplaceConn.gcpMappings.usageReportingIDBySubscription[subscriptionID] != ""
	case types.SecretProviderAzureMarketplace:
		return marketplaceConn.azureMappings != nil && marketplaceConn.azureMappings.resourceIDBySubscription[subscriptionID] != ""
	}
	return false
}

// reportRecordToMarketplaces reports a record to every marketplace its subscription holds an
// agreement on that does not already hold a sync entry, and marks it synced once all of them do.
//
// The record is updated in place but never persisted: the final flush record is reported before it
// exists in the table, so storing the outcome is left to the caller.
//
// Returns the marketplaces that accepted the record on this call, for the caller to log; the entry
// itself is on the record. Skips are excluded, since they are logged where the decision is made and
// reporting one as accepted would claim something was sent when nothing was.
func (r *MarketplaceReporter) reportRecordToMarketplaces(
	ctx context.Context,
	rec *usagerecord.UsageRecord,
	reportableConns []*marketplaceConnection,
) (reportedMarketplaces []string) {
	if rec.Syncs == nil {
		rec.Syncs = map[string]types.UsageRecordSyncEntry{}
	}

	for _, marketplaceConn := range reportableConns {
		marketplace := string(marketplaceConn.conn.ProviderType)
		// A skip leaves an entry as well, so a marketplace that skipped this record is treated as
		// resolved and never attempted again: the amount will not change on a retry.
		if entry, ok := rec.Syncs[marketplace]; ok && entry.IsSynced() {
			continue
		}

		var entry types.UsageRecordSyncEntry
		var ok bool
		switch marketplaceConn.conn.ProviderType {
		case types.SecretProviderAWSMarketplace:
			entry, ok = r.reportAWSRecord(ctx, rec, marketplaceConn)
		case types.SecretProviderGCPMarketplace:
			entry, ok = r.reportGCPRecord(ctx, rec, marketplaceConn)
		case types.SecretProviderAzureMarketplace:
			entry, ok = r.reportAzureRecord(ctx, rec, marketplaceConn)
		}
		if !ok {
			continue // already logged inside the provider method
		}

		rec.Syncs[marketplace] = entry
		if !entry.Skipped {
			reportedMarketplaces = append(reportedMarketplaces, marketplace)
		}
	}

	// Synced means every marketplace with an agreement is resolved, whether it accepted or skipped
	// it. A skip is permanent — the amount will not change on a retry — so requiring an acceptance
	// here would leave the record pending forever.
	rec.Synced = true
	for _, marketplaceConn := range reportableConns {
		if _, ok := rec.Syncs[string(marketplaceConn.conn.ProviderType)]; !ok {
			rec.Synced = false
			break
		}
	}

	return reportedMarketplaces
}

// prepareMarketplaceConnection authenticates one connection and loads its provider's entity mappings, returning
// a handle ready to report records through. Called once per connection per run, before any record is
// touched, so a failure here defers every record for that connection.
func (r *MarketplaceReporter) prepareMarketplaceConnection(ctx context.Context, conn *connection.Connection) (*marketplaceConnection, error) {
	switch conn.ProviderType {
	case types.SecretProviderAWSMarketplace:
		creds, region, mappings, err := r.authAWSConnection(ctx, conn)
		if err != nil {
			return nil, err
		}
		return &marketplaceConnection{conn: conn, awsCreds: creds, awsRegion: region, awsMappings: mappings}, nil
	case types.SecretProviderGCPMarketplace:
		svc, mappings, err := r.authGCPConnection(ctx, conn)
		if err != nil {
			return nil, err
		}
		return &marketplaceConnection{conn: conn, gcpSvc: svc, gcpMappings: mappings}, nil
	case types.SecretProviderAzureMarketplace:
		token, mappings, err := r.authAzureConnection(ctx, conn)
		if err != nil {
			return nil, err
		}
		return &marketplaceConnection{conn: conn, azureToken: token, azureMappings: mappings}, nil
	}
	return nil, ierr.NewErrorf("unsupported marketplace provider type %q", conn.ProviderType).Mark(ierr.ErrValidation)
}

// isEligibleForReport applies the validation common to every provider before any payload is built:
// only USD is accepted (a non-USD record stays unsynced, retried once currency conversion lands),
// and a negative amount is never valid on any provider — it means an upstream billing computation
// produced a bad value, not a marketplace rejection, so it is left unsynced for investigation rather
// than sent. A zero amount passes this check; whether it is reportable is provider-specific and is
// decided in reportAzureRecord.
func (r *MarketplaceReporter) isEligibleForReport(ctx context.Context, rec *usagerecord.UsageRecord) bool {
	if !types.IsMatchingCurrency(rec.Currency, marketplaceReportingCurrency) {
		r.logger.Info(ctx, "marketplace usage report: skipping usage record, currency is not usd",
			"subscription_id", rec.SubscriptionID, "usage_record_id", rec.ID, "currency", rec.Currency)
		return false
	}
	if rec.Amount.IsNegative() {
		r.logger.Error(ctx, "marketplace usage report: skipping usage record, amount is negative",
			"subscription_id", rec.SubscriptionID, "usage_record_id", rec.ID, "amount", rec.Amount,
			"error", "negative_amount")
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// AWS
// ---------------------------------------------------------------------------

// authAWSConnection decrypts a connection's AWS secret, loads its mappings, and assumes the tenant's
// role once.
func (r *MarketplaceReporter) authAWSConnection(ctx context.Context, conn *connection.Connection) (awssdk.Credentials, string, *awsConnectionMappings, error) {
	tenantID := conn.TenantID
	environmentID := conn.EnvironmentID

	if conn.EncryptedSecretData.AWSMarketplace == nil {
		err := ierr.NewError("connection has no aws_marketplace secret data").Mark(ierr.ErrValidation)
		r.logger.Error(ctx, "marketplace usage report: failed to read connection configuration", "marketplace", conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "read_connection")
		return awssdk.Credentials{}, "", nil, err
	}

	// The region is saved on the connection at creation time (sync_config.aws_marketplace.region,
	// same home as S3's bucket/region); it selects the AWS Marketplace Metering endpoint and is required.
	if conn.SyncConfig == nil || conn.SyncConfig.AWSMarketplace == nil || conn.SyncConfig.AWSMarketplace.Region == "" {
		err := ierr.NewError("connection has no region in sync_config").Mark(ierr.ErrValidation)
		r.logger.Error(ctx, "marketplace usage report: failed to read connection configuration", "marketplace", conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "read_connection")
		return awssdk.Credentials{}, "", nil, err
	}
	region := conn.SyncConfig.AWSMarketplace.Region

	roleArn, err := r.encryptionService.Decrypt(conn.EncryptedSecretData.AWSMarketplace.RoleArn)
	if err != nil {
		r.logger.Error(ctx, "marketplace usage report: failed to decrypt aws role arn", "marketplace", conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "decrypt_role_arn")
		return awssdk.Credentials{}, "", nil, err
	}
	externalID, err := r.encryptionService.Decrypt(conn.EncryptedSecretData.AWSMarketplace.ExternalID)
	if err != nil {
		r.logger.Error(ctx, "marketplace usage report: failed to decrypt aws external id", "marketplace", conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "decrypt_external_id")
		return awssdk.Credentials{}, "", nil, err
	}

	mappings, err := r.loadAWSMappings(ctx)
	if err != nil {
		r.logger.Error(ctx, "marketplace usage report: failed to load entity mappings", "marketplace", conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "load_mappings")
		return awssdk.Credentials{}, "", nil, err
	}

	// Assume the tenant's role once; the same static credentials are reused for every record this
	// connection reports (they are a one-time snapshot, not a live auto-refreshing provider). A busy
	// tenant's loop can run close to the activity's 30-minute StartToCloseTimeout, so the session
	// must outlive that — 1 hour is requested because every IAM role supports it without the tenant
	// needing to raise MaxSessionDuration.
	creds, err := r.awsClient.AssumeRole(ctx, roleArn, externalID, time.Hour)
	if err != nil {
		// err is already redacted of the role ARN and external ID by AssumeRole.
		r.logger.Error(ctx, "marketplace usage report: failed to assume the tenant's aws role",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID,
			"region", region,
			"error", err, "stage", "assume_role")
		return awssdk.Credentials{}, "", nil, err
	}

	return creds, region, mappings, nil
}

// reportAWSRecord reports a single usage record to AWS and returns the sync entry to persist on
// success.
func (r *MarketplaceReporter) reportAWSRecord(ctx context.Context, rec *usagerecord.UsageRecord, marketplaceConn *marketplaceConnection) (types.UsageRecordSyncEntry, bool) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	mappings := marketplaceConn.awsMappings

	licenseArn := mappings.licenseArnBySubscription[rec.SubscriptionID]
	customerAWSAccountID := mappings.awsAccountByCustomer[rec.CustomerID]
	plan, planFound := mappings.plan[rec.PlanID]
	if licenseArn == "" || customerAWSAccountID == "" || !planFound || plan.dimension == "" {
		r.logger.Error(ctx, "marketplace usage report: failed to resolve the record's marketplace identifiers", "marketplace", marketplaceConn.conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "customer_id", rec.CustomerID, "plan_id", rec.PlanID, "connection_id", marketplaceConn.conn.ID,
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
	// sent in USD cents — the tenant prices their dimension per cent. Turning cents into a charge is
	// AWS's job: it bills quantity x the dimension's rate.
	quantity := types.ToSmallestUnit(rec.Amount, marketplaceReportingCurrency)
	if quantity > math.MaxInt32 || quantity < 0 {
		r.logger.Error(ctx, "marketplace usage report: failed to convert amount to an aws quantity", "marketplace", marketplaceConn.conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "connection_id", marketplaceConn.conn.ID, "amount", rec.Amount, "currency", rec.Currency, "quantity", quantity,
			"error", "quantity out of the range aws accepts", "stage", "convert_quantity")
		return types.UsageRecordSyncEntry{}, false
	}

	res, err := r.awsClient.BatchMeterUsage(ctx, marketplaceConn.awsCreds, marketplaceConn.awsRegion, awsmarketplace.UsageRecordInput{
		CustomerAWSAccountID: customerAWSAccountID,
		LicenseArn:           licenseArn,
		ProductCode:          productCode,
		Dimension:            plan.dimension,
		Quantity:             int32(quantity), // #nosec G115 -- bounds checked above
		// PeriodEnd is the timestamp so a retry sends an identical record and AWS de-duplicates it.
		Timestamp: rec.PeriodEnd,
	})
	if err != nil {
		r.logger.Error(ctx, "marketplace usage report: aws batch meter usage call failed", "marketplace", marketplaceConn.conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "connection_id", marketplaceConn.conn.ID, "license_arn", licenseArn, "dimension", plan.dimension, "amount", rec.Amount,
			"error", err, "stage", "batch_meter_usage")
		return types.UsageRecordSyncEntry{}, false
	}
	if res == nil {
		// AWS returned the record as unprocessed; leaving it unsynced retries it next run.
		r.logger.Info(ctx, "marketplace usage report: aws did not process the usage record, will retry next run", "marketplace", marketplaceConn.conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "connection_id", marketplaceConn.conn.ID, "license_arn", licenseArn, "dimension", plan.dimension, "amount", rec.Amount)
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
		r.logger.Error(ctx, "marketplace usage report: aws rejected the usage record, customer not subscribed, will retry next run", "marketplace", marketplaceConn.conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "connection_id", marketplaceConn.conn.ID, "customer_id", rec.CustomerID, "license_arn", licenseArn,
			"dimension", plan.dimension, "amount", rec.Amount, "error", "customer_not_subscribed")
		return types.UsageRecordSyncEntry{}, false
	case awsmarketplace.StatusDuplicateRecord:
		// NOT "AWS already has this exact record, safe to skip" — AWS has a DIFFERENT record for the
		// same customer+dimension+timestamp already on file, and rejected this one. Retrying with the
		// same amount hits the same rejection every time; this needs a human to fix the mismatch.
		r.logger.Error(ctx, "marketplace usage report: aws rejected the usage record, it conflicts with a different record already on file, needs manual investigation", "marketplace", marketplaceConn.conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "connection_id", marketplaceConn.conn.ID, "customer_id", rec.CustomerID, "license_arn", licenseArn,
			"dimension", plan.dimension, "amount", rec.Amount, "period_end", rec.PeriodEnd, "error", "duplicate_record")
		return types.UsageRecordSyncEntry{}, false
	default:
		r.logger.Error(ctx, "marketplace usage report: aws returned an unrecognized status, will retry next run", "marketplace", marketplaceConn.conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "connection_id", marketplaceConn.conn.ID, "license_arn", licenseArn, "dimension", plan.dimension, "amount", rec.Amount,
			"aws_status", res.Status, "error", "unrecognized_aws_status")
		return types.UsageRecordSyncEntry{}, false
	}

	return types.UsageRecordSyncEntry{
		AgreementID:  licenseArn,
		ReportingID:  res.MeteringRecordID,
		SyncedAt:     time.Now().UTC(),
		ConnectionID: marketplaceConn.conn.ID,
	}, true
}

// loadAWSMappings loads the subscription, customer and plan mappings for the current tenant/
// environment and indexes them for lookup while reporting.
func (r *MarketplaceReporter) loadAWSMappings(ctx context.Context) (*awsConnectionMappings, error) {
	providerType := string(types.SecretProviderAWSMarketplace)

	subMappings, err := r.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypeSubscription,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		return nil, err
	}
	customerMappings, err := r.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypeCustomer,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		return nil, err
	}
	planMappings, err := r.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
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
func (r *MarketplaceReporter) authGCPConnection(ctx context.Context, conn *connection.Connection) (*servicecontrol.Service, *gcpConnectionMappings, error) {
	tenantID := conn.TenantID
	environmentID := conn.EnvironmentID

	if conn.EncryptedSecretData.GCPMarketplace == nil {
		err := ierr.NewError("connection has no gcp_marketplace secret data").Mark(ierr.ErrValidation)
		r.logger.Error(ctx, "marketplace usage report: failed to read connection configuration", "marketplace", conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "read_connection")
		return nil, nil, err
	}

	credentialsJSON, err := r.encryptionService.Decrypt(conn.EncryptedSecretData.GCPMarketplace.CredentialsJSON)
	if err != nil {
		r.logger.Error(ctx, "marketplace usage report: failed to decrypt gcp credentials", "marketplace", conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "decrypt_credentials_json")
		return nil, nil, err
	}

	mappings, err := r.loadGCPMappings(ctx)
	if err != nil {
		r.logger.Error(ctx, "marketplace usage report: failed to load entity mappings", "marketplace", conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "load_mappings")
		return nil, nil, err
	}

	// One WIF exchange per connection; the resulting client is reused for every record this
	// connection reports, mirroring the single AssumeRole-per-connection pattern on the AWS path.
	svc, err := r.gcpClient.WifSession(ctx, credentialsJSON)
	if err != nil {
		r.logger.Error(ctx, "marketplace usage report: failed to establish the gcp workload identity session", "marketplace", conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "wif_session")
		return nil, nil, err
	}

	return svc, mappings, nil
}

// reportGCPRecord reports a single usage record to GCP and returns the sync entry to persist on
// success.
func (r *MarketplaceReporter) reportGCPRecord(ctx context.Context, rec *usagerecord.UsageRecord, marketplaceConn *marketplaceConnection) (types.UsageRecordSyncEntry, bool) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	mappings := marketplaceConn.gcpMappings

	consumerID := mappings.usageReportingIDBySubscription[rec.SubscriptionID]
	plan, planFound := mappings.plan[rec.PlanID]
	if consumerID == "" || !planFound || plan.serviceName == "" || plan.metricName == "" {
		r.logger.Error(ctx, "marketplace usage report: failed to resolve the record's marketplace identifiers", "marketplace", marketplaceConn.conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "customer_id", rec.CustomerID, "plan_id", rec.PlanID, "connection_id", marketplaceConn.conn.ID,
			"error", "missing usage_reporting_id or plan service_name/metric_name mapping", "stage", "resolve_record")
		return types.UsageRecordSyncEntry{}, false
	}

	// Only called for USD records (see isEligibleForReport). GCP accepts an int64 value, and
	// ToSmallestUnit already returns int64, so unlike AWS's int32 Quantity there's no overflow guard.
	cents := types.ToSmallestUnit(rec.Amount, marketplaceReportingCurrency)

	reportResult, err := r.gcpClient.Report(ctx, marketplaceConn.gcpSvc, gcpmarketplace.UsageReportInput{
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
		r.logger.Error(ctx, "marketplace usage report: gcp services report call failed", "marketplace", marketplaceConn.conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "connection_id", marketplaceConn.conn.ID, "error", err, "stage", "services_report")
		return types.UsageRecordSyncEntry{}, false
	}

	// HTTP 200 is not the same as accepted — reportErrors must be checked. Common codes:
	// 5=NOT_FOUND (consumer inactive), 7=PERMISSION_DENIED, 3=INVALID_ARGUMENT.
	if !reportResult.Accepted {
		r.logger.Error(ctx, "marketplace usage report: gcp rejected the usage record, will retry next run", "marketplace", marketplaceConn.conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "connection_id", marketplaceConn.conn.ID, "error", "rejected_by_gcp", "error_code", reportResult.ErrorCode,
			"error_message", reportResult.ErrorMessage)
		return types.UsageRecordSyncEntry{}, false
	}

	// GCP returns no per-record receipt; the operationId echoed back (== rec.ID) becomes this
	// connection's reporting_id, read from the result rather than re-derived.
	return types.UsageRecordSyncEntry{
		AgreementID:  consumerID,
		ReportingID:  reportResult.OperationID,
		SyncedAt:     time.Now().UTC(),
		ConnectionID: marketplaceConn.conn.ID,
	}, true
}

// loadGCPMappings loads the subscription and plan mappings for the current tenant/environment and
// indexes them for lookup while reporting.
func (r *MarketplaceReporter) loadGCPMappings(ctx context.Context) (*gcpConnectionMappings, error) {
	providerType := string(types.SecretProviderGCPMarketplace)

	subMappings, err := r.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypeSubscription,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		return nil, err
	}
	planMappings, err := r.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
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

// ---------------------------------------------------------------------------
// Azure
// ---------------------------------------------------------------------------

// authAzureConnection decrypts a connection's Azure secret, loads its mappings, and requests a
// client_credentials token once.
func (r *MarketplaceReporter) authAzureConnection(ctx context.Context, conn *connection.Connection) (azuremarketplace.Token, *azureConnectionMappings, error) {
	tenantID := conn.TenantID
	environmentID := conn.EnvironmentID

	if conn.EncryptedSecretData.AzureMarketplace == nil {
		err := ierr.NewError("connection has no azure_marketplace secret data").Mark(ierr.ErrValidation)
		r.logger.Error(ctx, "marketplace usage report: failed to read connection configuration", "marketplace", conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "read_connection")
		return azuremarketplace.Token{}, nil, err
	}

	azureTenantID, err := r.encryptionService.Decrypt(conn.EncryptedSecretData.AzureMarketplace.TenantID)
	if err != nil {
		r.logger.Error(ctx, "marketplace usage report: failed to decrypt azure tenant id", "marketplace", conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "decrypt_tenant_id")
		return azuremarketplace.Token{}, nil, err
	}
	clientID, err := r.encryptionService.Decrypt(conn.EncryptedSecretData.AzureMarketplace.ClientID)
	if err != nil {
		r.logger.Error(ctx, "marketplace usage report: failed to decrypt azure client id", "marketplace", conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "decrypt_client_id")
		return azuremarketplace.Token{}, nil, err
	}
	clientSecret, err := r.encryptionService.Decrypt(conn.EncryptedSecretData.AzureMarketplace.ClientSecret)
	if err != nil {
		r.logger.Error(ctx, "marketplace usage report: failed to decrypt azure client secret", "marketplace", conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "decrypt_client_secret")
		return azuremarketplace.Token{}, nil, err
	}

	mappings, err := r.loadAzureMappings(ctx)
	if err != nil {
		r.logger.Error(ctx, "marketplace usage report: failed to load entity mappings", "marketplace", conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "load_mappings")
		return azuremarketplace.Token{}, nil, err
	}

	// One token request per connection; the resulting token is reused for every record this
	// connection reports, mirroring the AssumeRole/WifSession pattern on the AWS/GCP paths.
	token, err := r.azureClient.GetToken(ctx, azureTenantID, clientID, clientSecret)
	if err != nil {
		r.logger.Error(ctx, "marketplace usage report: failed to obtain an azure access token", "marketplace", conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "get_token")
		return azuremarketplace.Token{}, nil, err
	}

	return token, mappings, nil
}

// reportAzureRecord reports a single usage record to Azure and returns the sync entry to persist on
// success. A row whose reportable quantity is zero cents is never sent — Azure treats a quantity
// of zero as invalid — and instead resolves this connection with Skipped=true, so a record is not
// permanently blocked from reaching synced=true just because Azure cannot accept a zero quantity.
func (r *MarketplaceReporter) reportAzureRecord(ctx context.Context, rec *usagerecord.UsageRecord, marketplaceConn *marketplaceConnection) (types.UsageRecordSyncEntry, bool) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	mappings := marketplaceConn.azureMappings

	resourceID := mappings.resourceIDBySubscription[rec.SubscriptionID]
	plan, planFound := mappings.plan[rec.PlanID]
	if resourceID == "" || !planFound || plan.planID == "" || plan.dimension == "" {
		r.logger.Error(ctx, "marketplace usage report: failed to resolve the record's marketplace identifiers", "marketplace", marketplaceConn.conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "customer_id", rec.CustomerID, "plan_id", rec.PlanID, "connection_id", marketplaceConn.conn.ID,
			"error", "missing resource_id or plan plan_id/dimension mapping", "stage", "resolve_record")
		return types.UsageRecordSyncEntry{}, false
	}

	// Only called for USD records (see isEligibleForReport). Azure's quantity is a double, but is
	// always sent as a whole number of cents — the tenant prices their dimension per cent.
	cents := types.ToSmallestUnit(rec.Amount, marketplaceReportingCurrency)

	// Skip on cents, not rec.Amount: a positive sub-cent amount rounds to zero cents and Azure
	// rejects a zero quantity. Negatives are already filtered upstream (isEligibleForReport).
	if cents == 0 {
		r.logger.Info(ctx, "marketplace usage report: skipping usage record for azure, it does not accept a zero quantity", "marketplace", marketplaceConn.conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "connection_id", marketplaceConn.conn.ID, "amount", rec.Amount)
		return types.UsageRecordSyncEntry{
			AgreementID: resourceID,
			// SyncedAt stays zero: nothing was sent, so nothing was synced. Azure documents a quantity
			// of zero as invalid, so this is never sent and never retried.
			ConnectionID: marketplaceConn.conn.ID,
			Skipped:      true,
			SkipReason:   types.SkipReasonZeroAmountNotSupported,
		}, true
	}

	res, err := r.azureClient.ReportUsageEvent(ctx, marketplaceConn.azureToken, azuremarketplace.UsageEventInput{
		ResourceID: resourceID,
		Dimension:  plan.dimension,
		PlanID:     plan.planID,
		Quantity:   float64(cents),
		// PeriodEnd is effectiveStartTime so a retry sends an identical record.
		EffectiveStartTime: rec.PeriodEnd,
	})
	// Azure has no "present result, check its status" step: the single-event endpoint's 200 response
	// is unconditionally Accepted, and every rejection (Duplicate, Expired, invalid resource,
	// malformed request) comes back as a distinct non-2xx status, which the client already turns into
	// an error. Duplicate is ambiguous — nothing in a 409 confirms the existing event is ours — so any
	// error here, rejection or transient failure alike, is left unsynced and retried next run, never
	// resolved by inference.
	if err != nil {
		r.logger.Error(ctx, "marketplace usage report: azure usage event call failed", "marketplace", marketplaceConn.conn.ProviderType,
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "connection_id", marketplaceConn.conn.ID, "resource_id", resourceID, "dimension", plan.dimension, "amount", rec.Amount,
			"error", err, "stage", "usage_event")
		return types.UsageRecordSyncEntry{}, false
	}

	return types.UsageRecordSyncEntry{
		AgreementID:  resourceID,
		ReportingID:  res.UsageEventID,
		SyncedAt:     time.Now().UTC(),
		ConnectionID: marketplaceConn.conn.ID,
	}, true
}

// loadAzureMappings loads the subscription and plan mappings for the current tenant/environment and
// indexes them for lookup while reporting.
func (r *MarketplaceReporter) loadAzureMappings(ctx context.Context) (*azureConnectionMappings, error) {
	providerType := string(types.SecretProviderAzureMarketplace)

	subMappings, err := r.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypeSubscription,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		return nil, err
	}
	planMappings, err := r.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypePlan,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		return nil, err
	}

	m := &azureConnectionMappings{
		resourceIDBySubscription: make(map[string]string, len(subMappings)),
		plan:                     make(map[string]azurePlanMapping, len(planMappings)),
	}
	for _, sm := range subMappings {
		m.resourceIDBySubscription[sm.EntityID] = sm.ProviderEntityID
	}
	for _, pm := range planMappings {
		dimension, _ := pm.Metadata["dimension"].(string)
		m.plan[pm.EntityID] = azurePlanMapping{
			planID:    pm.ProviderEntityID,
			dimension: dimension,
		}
	}
	return m, nil
}
