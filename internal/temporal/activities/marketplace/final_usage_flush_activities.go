package marketplace

import (
	"context"
	"time"

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
	"github.com/samber/lo"
	"go.temporal.io/sdk/activity"
)

// reportTimestampSafetyMargin is subtracted from the final record's period_end when it is reported,
// never when it is stored. GCP requires the reported timestamp to be strictly earlier than the
// cancellation instant it holds, and cancelAt is exactly that instant, so a second satisfies the
// comparison without materially under-billing. Applied to every provider rather than branched.
//
// Storing the exact cancelAt keeps the record's window identical on every attempt, so a retry finds
// the same row rather than computing a second one a second wide.
const reportTimestampSafetyMargin = 1 * time.Second

// backlogSubmissionWindow bounds the backlog to rows a marketplace can still accept. None of the
// three take a report older than this, so there is no point fetching one.
const backlogSubmissionWindow = 24 * time.Hour

// FlushActivities computes and reports a cancelled subscription's final marketplace usage, then
// archives its marketplace mappings. It runs once per cancellation rather than on a schedule because
// AWS and GCP accept a final report for only about an hour after cancellation, far tighter than the
// reporting crons' cadence.
//
// The per-provider mechanics live on the shared reporter, the same instance the reporting cron holds,
// so the two report paths cannot drift apart.
type FlushActivities struct {
	subscriptionService          service.SubscriptionService
	billingService               service.BillingService
	connectionRepo               connection.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	subscriptionRepo             subscription.Repository
	customerRepo                 customer.Repository
	usageRecordRepo              usagerecord.Repository
	mktplaceReporter             *MarketplaceReporter
	logger                       *logger.Logger
}

// NewFlushActivities builds the activity set. The reporter is constructed once at registration and
// shared with the reporting cron.
func NewFlushActivities(params service.ServiceParams, mktplaceReporter *MarketplaceReporter, log *logger.Logger) *FlushActivities {
	return &FlushActivities{
		subscriptionService:          service.NewSubscriptionService(params),
		billingService:               service.NewBillingService(params),
		connectionRepo:               params.ConnectionRepo,
		entityIntegrationMappingRepo: params.EntityIntegrationMappingRepo,
		subscriptionRepo:             params.SubRepo,
		customerRepo:                 params.CustomerRepo,
		usageRecordRepo:              params.UsageRecordRepo,
		mktplaceReporter:             mktplaceReporter,
		logger:                       log,
	}
}

// subscriptionMarketplaceMappings returns the subscription's published mappings for the marketplace
// providers.
func (a *FlushActivities) subscriptionMarketplaceMappings(ctx context.Context, subscriptionID string) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	providerTypes := make([]string, len(marketplaceProviderTypes))
	for i, p := range marketplaceProviderTypes {
		providerTypes[i] = string(p)
	}
	return a.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityID:      subscriptionID,
		EntityType:    types.IntegrationEntityTypeSubscription,
		ProviderTypes: providerTypes,
	})
}

// MarketplaceSubscriptionFinalUsageFlushActivity reports the subscription's outstanding usage in two
// phases — first the backlog of unsynced records, then the final window up to the cancellation — and
// archives its marketplace mappings. The phases are kept separate, and logged separately, so a run's
// logs say which one a line belongs to.
//
// Archiving only happens once every record in both phases has fully synced and every mapped
// connection resolved. On any failure the mappings stay published: that is what keeps the
// subscription's backlog visible to the reporting cron, which retries it independently of this
// activity. Archiving early would strand whatever failed with no way to reach it again.
func (a *FlushActivities) MarketplaceSubscriptionFinalUsageFlushActivity(
	ctx context.Context,
	input temporalModels.MarketplaceSubscriptionFinalUsageFlushActivityInput,
) (*temporalModels.MarketplaceSubscriptionFinalUsageFlushWorkflowResult, error) {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	log := activity.GetLogger(ctx)
	tenantID := input.TenantID
	environmentID := input.EnvironmentID
	result := &temporalModels.MarketplaceSubscriptionFinalUsageFlushWorkflowResult{
		SubscriptionID: input.SubscriptionID,
	}

	subMappings, err := a.subscriptionMarketplaceMappings(ctx, input.SubscriptionID)
	if err != nil {
		a.logger.Error(ctx, "marketplace subscription flush: failed to list subscription marketplace mappings",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", input.SubscriptionID,
			"error", err, "stage", "list_subscription_mappings")
		return nil, err
	}
	if len(subMappings) == 0 {
		log.Info("marketplace subscription flush: subscription has no marketplace mapping, nothing to flush", "subscription_id", input.SubscriptionID)
		return result, nil
	}

	// One unresolvable connection must not stop the others from being reported, but it does block
	// archiving below: that provider was never attempted, so nothing can be concluded about it.
	marketplaceConns := make([]*marketplaceConnection, 0, len(subMappings))
	connectionResolutionFailed := false
	for _, m := range subMappings {
		conn, err := a.connectionRepo.GetByProvider(ctx, types.SecretProvider(m.ProviderType))
		if err != nil {
			a.logger.Error(ctx, "marketplace subscription flush: failed to resolve marketplace connection",
				"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", input.SubscriptionID,
				"provider_type", m.ProviderType, "error", err, "stage", "get_connection")
			connectionResolutionFailed = true
			continue
		}
		prepared, err := a.mktplaceReporter.prepareMarketplaceConnection(ctx, conn)
		if err != nil {
			// already logged inside prepareMarketplaceConnection at the stage that failed
			connectionResolutionFailed = true
			continue
		}
		marketplaceConns = append(marketplaceConns, prepared)
	}

	cancelAt := input.CancelAt.UTC()

	// Phase 1: the backlog written by earlier snapshots. Bounded below by the submission window, since
	// an older row can no longer be accepted by any provider, and above by cancelAt, which excludes
	// the final record — phase 2 owns that one, and reporting it here would send it without the
	// timestamp margin the providers require.
	backlog, err := a.usageRecordRepo.List(ctx, &types.UsageRecordFilter{
		QueryFilter: &types.QueryFilter{
			Sort:  lo.ToPtr("period_end"),
			Order: lo.ToPtr(types.OrderAsc),
		},
		SubscriptionID: input.SubscriptionID,
		Synced:         lo.ToPtr(false),
		Filters: []*types.FilterCondition{
			{
				Field:    lo.ToPtr("period_end"),
				Operator: lo.ToPtr(types.GREATER_THAN_EQUAL),
				DataType: lo.ToPtr(types.DataTypeDate),
				Value:    &types.Value{Date: lo.ToPtr(time.Now().UTC().Add(-backlogSubmissionWindow))},
			},
			{
				Field:    lo.ToPtr("period_end"),
				Operator: lo.ToPtr(types.LESS_THAN),
				DataType: lo.ToPtr(types.DataTypeDate),
				Value:    &types.Value{Date: lo.ToPtr(cancelAt)},
			},
		},
	})
	if err != nil {
		a.logger.Error(ctx, "marketplace subscription flush: failed to list backlog usage records",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", input.SubscriptionID,
			"error", err, "stage", "list_backlog")
		return nil, err
	}

	for _, rec := range backlog {
		if !a.mktplaceReporter.isEligibleForReport(ctx, rec) {
			continue
		}
		a.reportRecord(ctx, rec, marketplaceConns, result)
	}

	// Phase 2: the single record covering everything from the last computed point up to cancellation.
	computedThrough, err := a.lastComputedPeriodEnd(ctx, input.SubscriptionID, cancelAt, subMappings)
	if err != nil {
		a.logger.Error(ctx, "marketplace subscription flush: failed to determine last computed period",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", input.SubscriptionID,
			"error", err, "stage", "last_computed_period_end")
		return nil, err
	}

	finalUsageFlush, err := a.finalUsageRecord(ctx, input.SubscriptionID, computedThrough, cancelAt, environmentID)
	if err != nil {
		return nil, err
	}

	// An ineligible record — non-USD, or a negative amount — can never be accepted, so it is not
	// reported. The eligibility check logs which of the two it was.
	finalUsageFlushFailed := false
	if finalUsageFlush != nil && a.mktplaceReporter.isEligibleForReport(ctx, finalUsageFlush) {
		result.FinalRecordID = finalUsageFlush.ID
		result.PeriodStart = finalUsageFlush.PeriodStart
		result.PeriodEnd = finalUsageFlush.PeriodEnd

		// The providers receive cancelAt less the margin; the stored row keeps the true instant, and
		// reporting never writes period_end back.
		finalUsageFlush.PeriodEnd = cancelAt.Add(-reportTimestampSafetyMargin)
		finalUsageFlushFailed = a.reportRecord(ctx, finalUsageFlush, marketplaceConns, result)
		finalUsageFlush.PeriodEnd = cancelAt
	}

	if connectionResolutionFailed || finalUsageFlushFailed || result.RecordsFailed > 0 {
		// Each cause is reported separately: an unresolved connection and an unreported final record
		// both fail the run while leaving the failed-record count at zero, so that count alone would
		// describe the failure as affecting no records and hide what actually went wrong.
		//
		// Returning an error retries the whole activity. That is safe to repeat: connections already
		// holding a sync entry are not attempted again.
		return result, ierr.NewErrorf("marketplace subscription flush failed for subscription %s", input.SubscriptionID).
			WithReportableDetails(map[string]any{
				"subscription_id":              input.SubscriptionID,
				"connection_resolution_failed": connectionResolutionFailed,
				"final_usage_flush_failed":     finalUsageFlushFailed,
				"records_failed":               result.RecordsFailed,
				"records_succeeded":            result.RecordsSucceeded,
				"records_skipped":              result.RecordsSkipped,
			}).
			Mark(ierr.ErrInternal)
	}

	delinkedIDs, err := a.delinkSubscriptionMappings(ctx, subMappings)
	if err != nil {
		return result, err
	}
	result.MappingsDelinked = len(delinkedIDs)

	// Emitted through the structured logger, not the workflow logger, so a completed run is greppable
	// under the same prefix as this activity's failures.
	a.logger.Info(ctx, "marketplace subscription flush: completed",
		"tenant_id", tenantID, "environment_id", environmentID,
		"subscription_id", input.SubscriptionID,
		"final_record_id", result.FinalRecordID,
		"records_succeeded", result.RecordsSucceeded,
		"records_skipped", result.RecordsSkipped,
		"mappings_delinked", result.MappingsDelinked)
	return result, nil
}

// reportRecord reports one record to every marketplace its subscription holds an agreement on, persists the
// outcome, and records it on result. It reports whether the record was left unreported, which is what
// keeps the mappings published: archiving a subscription whose records never reached a marketplace
// would put them beyond the reporting cron's reach, since that cron also needs the mapping to decide
// a record belongs to a marketplace.
func (a *FlushActivities) reportRecord(
	ctx context.Context,
	rec *usagerecord.UsageRecord,
	marketplaceConns []*marketplaceConnection,
	result *temporalModels.MarketplaceSubscriptionFinalUsageFlushWorkflowResult,
) (failed bool) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	var reportableConns []*marketplaceConnection
	for _, marketplaceConn := range marketplaceConns {
		if marketplaceConn.hasAgreementFor(rec.SubscriptionID) {
			reportableConns = append(reportableConns, marketplaceConn)
		}
	}
	if len(reportableConns) == 0 {
		// No provider can be reached, so the record stays exactly as it is: unreported and unsynced.
		// A connection resolved for the subscription's marketplace, but the subscription holds no
		// agreement there: its mapping carries no provider entity id.
		a.logger.Error(ctx, "marketplace subscription flush: usage record has no connection to report to",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "connections_resolved", len(marketplaceConns),
			"error", "subscription holds no agreement on any resolved marketplace",
			"stage", "relevant_connections")
		result.RecordsFailed++
		return true
	}

	reportedMarketplaces := a.mktplaceReporter.reportRecordToMarketplaces(ctx, rec, reportableConns)
	a.logReported(ctx, rec, reportedMarketplaces)

	if err := a.usageRecordRepo.MarkSynced(ctx, rec.ID, rec.Syncs, rec.Synced); err != nil {
		a.logger.Error(ctx, "marketplace subscription flush: failed to record usage sync state",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "error", err, "stage", "mark_synced")
		rec.Synced = false
	}

	// A record that reached every marketplace it has an agreement on but was skipped by all of them is not a
	// success: nothing was posted anywhere.
	anyAccepted := false
	for _, marketplaceConn := range reportableConns {
		if entry, ok := rec.Syncs[string(marketplaceConn.conn.ProviderType)]; ok && entry.IsSynced() {
			anyAccepted = true
			break
		}
	}

	switch {
	case !rec.Synced:
		result.RecordsFailed++
		return true
	case anyAccepted:
		result.RecordsSucceeded++
	default:
		result.RecordsSkipped++
	}
	return false
}

// logReported logs one line per connection that accepted this record on this run.
func (a *FlushActivities) logReported(ctx context.Context, rec *usagerecord.UsageRecord, reportedMarketplaces []string) {
	for _, marketplace := range reportedMarketplaces {
		entry := rec.Syncs[marketplace]
		a.logger.Info(ctx, "marketplace subscription flush: usage record synced",
			"tenant_id", types.GetTenantID(ctx), "environment_id", types.GetEnvironmentID(ctx),
			"subscription_id", rec.SubscriptionID, "usage_record_id", rec.ID,
			"marketplace", marketplace, "agreement_id", entry.AgreementID,
			"reporting_id", entry.ReportingID)
	}
}

// delinkSubscriptionMappings archives the subscription's marketplace mappings and returns the ids it
// archived. It soft-deletes through the repository directly, since the rows are already resolved.
func (a *FlushActivities) delinkSubscriptionMappings(ctx context.Context, subMappings []*entityintegrationmapping.EntityIntegrationMapping) ([]string, error) {
	delinked := make([]string, 0, len(subMappings))
	for _, m := range subMappings {
		if err := a.entityIntegrationMappingRepo.Delete(ctx, m); err != nil {
			a.logger.Error(ctx, "marketplace subscription flush: failed to archive marketplace mapping",
				"tenant_id", types.GetTenantID(ctx), "environment_id", types.GetEnvironmentID(ctx),
				"subscription_id", m.EntityID, "provider_type", m.ProviderType,
				"error", err, "stage", "delink_mapping")
			return delinked, err
		}
		delinked = append(delinked, m.ID)
	}
	return delinked, nil
}

// lastComputedPeriodEnd returns the point this subscription's usage has already been computed up to,
// which is where the final window starts: the latest period_end across every published row, whatever
// its sync state. Taking the latest unsynced row instead would re-report a span an already-synced row
// covers, billing it twice.
//
// Rows ending at or after cancelAt are excluded so the final record cannot move this point past
// itself. Without that, a retry would measure from the record the previous attempt wrote and compute a
// second, narrower window — a different period_start, so the unique index would not catch it.
//
// With no usage record yet, it falls back to the earliest marketplace mapping's creation time — the
// point the subscription became reportable, before which nothing could have been owed.
func (a *FlushActivities) lastComputedPeriodEnd(ctx context.Context, subscriptionID string, cancelAt time.Time, subMappings []*entityintegrationmapping.EntityIntegrationMapping) (time.Time, error) {
	rows, err := a.usageRecordRepo.List(ctx, &types.UsageRecordFilter{
		QueryFilter: &types.QueryFilter{
			Sort:  lo.ToPtr("period_end"),
			Order: lo.ToPtr(types.OrderDesc),
			Limit: lo.ToPtr(1),
		},
		SubscriptionID: subscriptionID,
		Filters: []*types.FilterCondition{{
			Field:    lo.ToPtr("period_end"),
			Operator: lo.ToPtr(types.LESS_THAN),
			DataType: lo.ToPtr(types.DataTypeDate),
			Value:    &types.Value{Date: lo.ToPtr(cancelAt)},
		}},
	})
	if err != nil {
		return time.Time{}, err
	}
	if len(rows) > 0 {
		return rows[0].PeriodEnd, nil
	}

	earliest := subMappings[0].CreatedAt
	for _, m := range subMappings[1:] {
		if m.CreatedAt.Before(earliest) {
			earliest = m.CreatedAt
		}
	}
	return earliest, nil
}

// finalUsageRecord returns the stored record covering [windowStart, cancelAt), computing and writing
// it if this is the first attempt. Returns nil when cancelAt is at or before windowStart and there is
// nothing left to compute, which is what a backdated cancellation looks like.
//
// A retry recomputes the same window, since the starting point ignores this record, so the insert
// collides with the unique key and the existing row is returned instead. The record therefore keeps
// one identity across every attempt, which is what lets a provider recognise a resend as the same
// usage rather than new usage.
func (a *FlushActivities) finalUsageRecord(ctx context.Context, subscriptionID string, windowStart, cancelAt time.Time, environmentID string) (*usagerecord.UsageRecord, error) {
	if !cancelAt.After(windowStart) {
		return nil, nil
	}

	tenantID := types.GetTenantID(ctx)

	// Looked up before any computation, not just before the insert: on a retry an earlier attempt has
	// already written this exact window, and there is no reason to recompute usage and charges only to
	// discard them a moment later. The unique key over the window means at most one row can match.
	existing, err := a.usageRecordRepo.List(ctx, &types.UsageRecordFilter{
		QueryFilter:    types.NewNoLimitQueryFilter(),
		SubscriptionID: subscriptionID,
		PeriodStart:    &windowStart,
		PeriodEnd:      &cancelAt,
	})
	if err != nil {
		a.logger.Error(ctx, "marketplace subscription flush: failed to load the existing final usage record",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", subscriptionID,
			"error", err, "stage", "get_existing_usage_record")
		return nil, err
	}
	if len(existing) > 0 {
		a.logger.Info(ctx, "marketplace subscription flush: final usage record already exists, reporting it",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", subscriptionID,
			"usage_record_id", existing[0].ID, "period_start", existing[0].PeriodStart,
			"period_end", existing[0].PeriodEnd, "synced", existing[0].Synced)
		return existing[0], nil
	}

	sub, _, err := a.subscriptionRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		a.logger.Error(ctx, "marketplace subscription flush: failed to load subscription",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", subscriptionID,
			"error", err, "stage", "get_subscription")
		return nil, err
	}

	usageResp, err := a.subscriptionService.GetMeterUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: subscriptionID,
		StartTime:      windowStart,
		EndTime:        cancelAt,
		Source:         string(types.UsageSourceInvoiceCreation),
	})
	if err != nil {
		a.logger.Error(ctx, "marketplace subscription flush: failed to compute meter usage",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", subscriptionID,
			"customer_id", sub.CustomerID, "error", err, "stage", "get_meter_usage")
		return nil, err
	}

	_, totalAmount, err := a.billingService.CalculateMeterUsageCharges(
		ctx, sub, usageResp, windowStart, cancelAt, types.UsageSourceInvoiceCreation,
	)
	if err != nil {
		a.logger.Error(ctx, "marketplace subscription flush: failed to calculate charges",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", subscriptionID,
			"customer_id", sub.CustomerID, "error", err, "stage", "calculate_charges")
		return nil, err
	}

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
		PeriodStart:        windowStart,
		PeriodEnd:          cancelAt,
		EnvironmentID:      environmentID,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}

	// An already-exists error here means a concurrent attempt won the race against the check above.
	// It is returned like any other failure: the next attempt's check finds that row and reports it.
	if err := a.usageRecordRepo.Create(ctx, rec); err != nil {
		a.logger.Error(ctx, "marketplace subscription flush: failed to create final usage record",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", subscriptionID,
			"customer_id", sub.CustomerID, "usage_record_id", rec.ID, "error", err,
			"stage", "create_usage_record")
		return nil, err
	}

	a.logger.Info(ctx, "marketplace subscription flush: final usage record created",
		"tenant_id", tenantID, "environment_id", environmentID,
		"subscription_id", rec.SubscriptionID, "customer_id", rec.CustomerID, "usage_record_id", rec.ID,
		"period_start", rec.PeriodStart, "period_end", rec.PeriodEnd, "amount", rec.Amount)
	return rec, nil
}
