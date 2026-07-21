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

// marketplaceReportingCurrency is the currency both AWS and GCP Marketplace bill in — neither
// offers seller-facing currency selection — so every usage record reported here must already be in
// USD. This is a marketplace-specific fact; usage_records itself stores each record's native
// subscription currency, since the table is shared across marketplaces and Azure may not require USD.
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
// No customer-mapping index is needed: consumerId comes entirely from the subscription mapping's
// usageReportingId.
type gcpConnectionMappings struct {
	usageReportingIDBySubscription map[string]string
	plan                           map[string]gcpPlanMapping
}

// ReportActivities reports usage records that have not yet been synced to their marketplace. For
// every published marketplace connection (AWS or GCP) it reads the connection's unsynced usage
// records, skips any not already in USD or not a positive amount, reports the rest, and records the
// outcome on each record the marketplace accepts. Records a marketplace does not accept are left
// unsynced so the next run retries them; there is no terminal failure state.
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

// MarketplaceUsageReportActivity is the activity entrypoint. It processes every tenant/environment
// that has a published marketplace connection, AWS or GCP; each connection is handled independently
// so a failure in one does not stop the others.
func (a *ReportActivities) MarketplaceUsageReportActivity(
	ctx context.Context,
	_ temporalModels.MarketplaceUsageReportWorkflowInput,
) (*temporalModels.MarketplaceUsageReportWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Starting MarketplaceUsageReportActivity")

	result := &temporalModels.MarketplaceUsageReportWorkflowResult{}

	for _, providerType := range marketplaceProviderTypes {
		conns, err := a.connectionRepo.ListPublishedByProvider(ctx, providerType)
		if err != nil {
			a.logger.Error(ctx, "marketplace usage report failed", "provider_type", providerType, "error", err, "stage", "list_connections")
			continue
		}

		for _, conn := range conns {
			envCtx := context.WithValue(context.WithValue(ctx, types.CtxTenantID, conn.TenantID), types.CtxEnvironmentID, conn.EnvironmentID)
			a.processConnection(envCtx, conn, result)
		}
	}

	log.Info("Completed MarketplaceUsageReportActivity",
		"total", result.Total, "succeeded", result.Succeeded, "failed", result.Failed)
	return result, nil
}

// processConnection is the shared per-connection driver for both providers: it lists this
// connection's unsynced records (stopping before touching any secret if there's nothing to do),
// then authenticates and loads that provider's mappings, and dispatches the resolved records to the
// provider-specific reporting method. Auth/mapping failures are logged inside authAWSConnection/
// authGCPConnection at the specific stage that failed, so this method doesn't re-log them.
func (a *ReportActivities) processConnection(envCtx context.Context, conn *connection.Connection, result *temporalModels.MarketplaceUsageReportWorkflowResult) {
	tenantID := conn.TenantID
	environmentID := conn.EnvironmentID

	records, err := a.usageRecordRepo.ListUnsyncedByConnection(envCtx, tenantID, environmentID, conn.ID)
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID, "error", err, "stage", "list_unsynced")
		return
	}
	if len(records) == 0 {
		return
	}

	switch conn.ProviderType {
	case types.SecretProviderAWSMarketplace:
		creds, region, mappings, err := a.authAWSConnection(envCtx, conn)
		if err != nil {
			return
		}
		a.reportToAWSMarketplace(envCtx, records, creds, region, mappings, result)
	case types.SecretProviderGCPMarketplace:
		svc, mappings, err := a.authGCPConnection(envCtx, conn)
		if err != nil {
			return
		}
		a.reportToGCPMarketplace(envCtx, records, svc, mappings, result)
	}
}

// isEligibleForReport applies the validation common to both providers, before any payload is built:
// only USD is accepted (a non-USD record stays unsynced, retried once currency conversion lands),
// and only a positive amount is accepted — both AWS and GCP reject non-positive quantities, and a
// billing correction can legitimately net a window's amount to zero or negative.
func (a *ReportActivities) isEligibleForReport(envCtx context.Context, rec *usagerecord.UsageRecord) bool {
	if !types.IsMatchingCurrency(rec.Currency, marketplaceReportingCurrency) {
		a.logger.Debug(envCtx, "skipping marketplace usage record, currency not usd",
			"subscription_id", rec.SubscriptionID, "usage_record_id", rec.ID, "currency", rec.Currency)
		return false
	}
	if rec.Amount.IsNegative() {
		a.logger.Info(envCtx, "skipping marketplace usage record, non-positive amount",
			"subscription_id", rec.SubscriptionID, "usage_record_id", rec.ID)
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// AWS
// ---------------------------------------------------------------------------

// authAWSConnection decrypts a connection's AWS secret, loads its plan/subscription/customer
// mappings, and assumes the tenant's role once — the "auth, mappings" step processConnection
// dispatches through before handing off to reportToAWSMarketplace.
func (a *ReportActivities) authAWSConnection(envCtx context.Context, conn *connection.Connection) (awssdk.Credentials, string, *awsConnectionMappings, error) {
	tenantID := conn.TenantID
	environmentID := conn.EnvironmentID

	if conn.EncryptedSecretData.AWSMarketplace == nil {
		err := ierr.NewError("connection has no aws_marketplace secret data").Mark(ierr.ErrValidation)
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID,
			"error", err, "stage", "read_connection")
		return awssdk.Credentials{}, "", nil, err
	}

	// The region is saved on the connection at creation time; it selects the AWS Marketplace
	// Metering endpoint and is required.
	region := ""
	if meta, ok := conn.Metadata["aws_marketplace"].(map[string]interface{}); ok {
		region, _ = meta["region"].(string)
	}
	if region == "" {
		err := ierr.NewError("connection has no region in metadata").Mark(ierr.ErrValidation)
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID,
			"error", err, "stage", "read_connection")
		return awssdk.Credentials{}, "", nil, err
	}

	roleArn, err := a.encryptionService.Decrypt(conn.EncryptedSecretData.AWSMarketplace.RoleArn)
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "error", err, "stage", "decrypt_role_arn")
		return awssdk.Credentials{}, "", nil, err
	}
	externalID, err := a.encryptionService.Decrypt(conn.EncryptedSecretData.AWSMarketplace.ExternalID)
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "error", err, "stage", "decrypt_external_id")
		return awssdk.Credentials{}, "", nil, err
	}

	mappings, err := a.loadAWSMappings(envCtx)
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "error", err, "stage", "load_mappings")
		return awssdk.Credentials{}, "", nil, err
	}

	// Assume the tenant's role once; the same static credentials are reused for every record in
	// this connection's loop below (they are a one-time snapshot, not a live auto-refreshing
	// provider). A busy connection's loop can run close to the activity's 30-minute
	// StartToCloseTimeout, so the session must outlive that — 1 hour is requested because every IAM
	// role supports it without the tenant needing to raise MaxSessionDuration on their side.
	creds, err := a.awsClient.AssumeRole(envCtx, roleArn, externalID, time.Hour)
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "error", err, "stage", "assume_role")
		return awssdk.Credentials{}, "", nil, err
	}

	return creds, region, mappings, nil
}

// reportToAWSMarketplace reports each of one AWS connection's already-resolved records to AWS. A
// record AWS accepts is marked synced with its metering id; a record AWS does not process (or one
// whose mapping is missing) is left unsynced so the next run retries it.
func (a *ReportActivities) reportToAWSMarketplace(
	envCtx context.Context,
	records []*usagerecord.UsageRecord,
	creds awssdk.Credentials,
	region string,
	mappings *awsConnectionMappings,
	result *temporalModels.MarketplaceUsageReportWorkflowResult,
) {
	for _, rec := range records {
		if !a.isEligibleForReport(envCtx, rec) {
			continue
		}
		result.Total++
		a.reportAWSRecord(envCtx, rec, mappings, creds, region, result)
	}
}

// reportAWSRecord reports a single usage record to AWS and records the outcome on it.
func (a *ReportActivities) reportAWSRecord(
	envCtx context.Context,
	rec *usagerecord.UsageRecord,
	mappings *awsConnectionMappings,
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

	// Only called for records already filtered to currency == usd (see isEligibleForReport), so
	// rec.Amount is used as-is. AWS only accepts a whole number, so it's sent in USD cents rather
	// than dollars — the tenant prices their dimension per cent (see setup docs), which keeps
	// sub-dollar amounts from rounding away. Turning cents into a charge is AWS's job: it bills
	// quantity x the dimension's rate.
	quantity := types.ToSmallestUnit(rec.Amount, marketplaceReportingCurrency)
	if quantity > math.MaxInt32 {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"amount", rec.Amount, "currency", rec.Currency, "quantity", quantity,
			"error", "quantity exceeds the maximum aws accepts", "stage", "convert_quantity")
		result.Failed++
		return
	}

	res, err := a.awsClient.BatchMeterUsage(envCtx, creds, region, awsmarketplace.UsageRecordInput{
		CustomerAWSAccountID: customerAWSAccountID,
		LicenseArn:           licenseArn,
		ProductCode:          productCode,
		Dimension:            plan.dimension,
		Quantity:             int32(quantity),
		// PeriodEnd is the timestamp so a retry sends an identical record and AWS de-duplicates it.
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
		a.logger.Info(envCtx, "marketplace usage record not processed by aws, will retry next run",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"license_arn", licenseArn, "dimension", plan.dimension, "amount", rec.Amount)
		result.Failed++
		return
	}

	// A present result is not the same as an accepted record — AWS's Status must be checked. Per
	// AWS's docs, only StatusSuccess means the record was honored; the other two statuses are
	// distinct failure modes with different causes, so each gets its own log line rather than one
	// generic "rejected" message.
	switch res.Status {
	case awsmarketplace.StatusSuccess:
		// falls through to the sync below
	case awsmarketplace.StatusCustomerNotSubscribed:
		// The buyer (identified by customer_aws_account_id + license_arn — we don't use AWS's
		// separate CustomerIdentifier field) has no active agreement for this product, or their AWS
		// account was suspended. This resolves itself once the buyer (re)subscribes, so it's
		// expected to keep retrying until then rather than needing manual action.
		a.logger.Error(envCtx, "marketplace usage report rejected by aws: customer not subscribed, will retry next run",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"customer_id", rec.CustomerID, "license_arn", licenseArn, "dimension", plan.dimension, "amount", rec.Amount,
			"error", "customer_not_subscribed")
		result.Failed++
		return
	case awsmarketplace.StatusDuplicateRecord:
		// NOT "AWS already has this exact record, safe to skip" — AWS has a DIFFERENT record for
		// the same customer+dimension+timestamp already on file, and rejected this one. Retrying
		// with the same (unchanged) amount will hit the same rejection every time; this does not
		// self-heal and needs a human to find and fix the source of the mismatch.
		a.logger.Error(envCtx, "marketplace usage report rejected by aws: conflicts with a different record already on file, needs manual investigation",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"customer_id", rec.CustomerID, "license_arn", licenseArn, "dimension", plan.dimension, "amount", rec.Amount,
			"period_end", rec.PeriodEnd, "error", "duplicate_record")
		result.Failed++
		return
	default:
		a.logger.Error(envCtx, "marketplace usage report rejected by aws: unrecognized status, will retry next run",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"license_arn", licenseArn, "dimension", plan.dimension, "amount", rec.Amount,
			"aws_status", res.Status, "error", "unrecognized_aws_status")
		result.Failed++
		return
	}

	if err := a.usageRecordRepo.MarkSynced(envCtx, rec.ID, res.MeteringRecordID); err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"license_arn", licenseArn, "dimension", plan.dimension, "amount", rec.Amount, "error", err, "stage", "write_sync_result")
		result.Failed++
		return
	}
	result.Succeeded++
}

// loadAWSMappings loads the subscription, customer and plan mappings for the current tenant/
// environment in one pass each and indexes them for lookup while reporting.
func (a *ReportActivities) loadAWSMappings(envCtx context.Context) (*awsConnectionMappings, error) {
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

// authGCPConnection decrypts a connection's GCP secret, loads its plan/subscription mappings, and
// performs the WIF exchange once — the "auth, mappings" step processConnection dispatches through
// before handing off to reportToGCPMarketplace.
func (a *ReportActivities) authGCPConnection(envCtx context.Context, conn *connection.Connection) (*servicecontrol.Service, *gcpConnectionMappings, error) {
	tenantID := conn.TenantID
	environmentID := conn.EnvironmentID

	if conn.EncryptedSecretData.GCPMarketplace == nil {
		err := ierr.NewError("connection has no gcp_marketplace secret data").Mark(ierr.ErrValidation)
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "connection_id", conn.ID,
			"error", err, "stage", "read_connection")
		return nil, nil, err
	}

	credentialsJSON, err := a.encryptionService.Decrypt(conn.EncryptedSecretData.GCPMarketplace.CredentialsJSON)
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "error", err, "stage", "decrypt_credentials_json")
		return nil, nil, err
	}

	mappings, err := a.loadGCPMappings(envCtx)
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "error", err, "stage", "load_mappings")
		return nil, nil, err
	}

	// One WIF exchange per connection; the resulting client is reused for every record below,
	// mirroring the single AssumeRole-per-connection pattern the AWS path uses above.
	svc, err := a.gcpClient.WifSession(envCtx, credentialsJSON)
	if err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "error", err, "stage", "wif_session")
		return nil, nil, err
	}

	return svc, mappings, nil
}

// reportToGCPMarketplace reports each of one GCP connection's already-resolved records via
// services.report. A record GCP accepts is marked synced; one GCP rejects (or whose mapping is
// missing) is left unsynced so the next run retries it.
func (a *ReportActivities) reportToGCPMarketplace(
	envCtx context.Context,
	records []*usagerecord.UsageRecord,
	svc *servicecontrol.Service,
	mappings *gcpConnectionMappings,
	result *temporalModels.MarketplaceUsageReportWorkflowResult,
) {
	for _, rec := range records {
		if !a.isEligibleForReport(envCtx, rec) {
			continue
		}
		result.Total++
		a.reportGCPRecord(envCtx, rec, mappings, svc, result)
	}
}

// reportGCPRecord reports a single usage record to GCP and records the outcome on it.
func (a *ReportActivities) reportGCPRecord(
	envCtx context.Context,
	rec *usagerecord.UsageRecord,
	mappings *gcpConnectionMappings,
	svc *servicecontrol.Service,
	result *temporalModels.MarketplaceUsageReportWorkflowResult,
) {
	tenantID := types.GetTenantID(envCtx)
	environmentID := types.GetEnvironmentID(envCtx)

	consumerID := mappings.usageReportingIDBySubscription[rec.SubscriptionID]
	plan, planFound := mappings.plan[rec.PlanID]
	if consumerID == "" || !planFound || plan.serviceName == "" || plan.metricName == "" {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"customer_id", rec.CustomerID, "plan_id", rec.PlanID,
			"error", "missing usage_reporting_id or plan service_name/metric_name mapping", "stage", "resolve_record")
		result.Failed++
		return
	}

	// Only called for records already filtered to currency == usd (see isEligibleForReport). GCP
	// only accepts int64Value with a DELTA-kind metric — ToSmallestUnit already returns an int64, so
	// unlike AWS's int32 Quantity there is no overflow guard needed here.
	cents := types.ToSmallestUnit(rec.Amount, marketplaceReportingCurrency)

	reportResult, err := a.gcpClient.Report(envCtx, svc, gcpmarketplace.UsageReportInput{
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
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"error", err, "stage", "services_report")
		result.Failed++
		return
	}

	// HTTP 200 is not the same as accepted — reportErrors must be checked. Per GCP's docs, common
	// codes: 5=NOT_FOUND (consumer inactive), 7=PERMISSION_DENIED, 3=INVALID_ARGUMENT.
	if !reportResult.Accepted {
		a.logger.Error(envCtx, "marketplace usage report rejected by gcp, will retry next run",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"error", "rejected_by_gcp", "error_code", reportResult.ErrorCode, "error_message", reportResult.ErrorMessage)
		result.Failed++
		return
	}

	// GCP returns no per-record receipt of its own; store the operationId echoed back on
	// reportResult (== rec.ID, since that's what was sent, but read from the result rather than
	// re-deriving it a second time) so marketplace_report_id is populated for every row regardless
	// of provider.
	if err := a.usageRecordRepo.MarkSynced(envCtx, rec.ID, reportResult.OperationID); err != nil {
		a.logger.Error(envCtx, "marketplace usage report failed",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"error", err, "stage", "write_sync_result")
		result.Failed++
		return
	}
	result.Succeeded++
}

// loadGCPMappings loads the subscription and plan mappings for the current tenant/environment in
// one pass each and indexes them for lookup while reporting.
func (a *ReportActivities) loadGCPMappings(envCtx context.Context) (*gcpConnectionMappings, error) {
	providerType := string(types.SecretProviderGCPMarketplace)

	subMappings, err := a.entityIntegrationMappingRepo.List(envCtx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypeSubscription,
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
