package marketplace

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/usagerecord"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	temporalModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/activity"
)

// ReportActivities reports usage records that have not yet reached every marketplace their
// subscription is mapped to. For every tenant/environment with a published marketplace connection it
// authenticates each connection once, reads that tenant's unsynced usage records once, and reports
// each record to whichever marketplaces it has not already reached. A record a marketplace rejects is
// left unsynced so the next run retries it; there is no terminal failure state.
//
// The per-provider mechanics live on the shared MarketplaceReporter, the same instance the
// cancellation flush holds, so the two cannot drift apart.
type ReportActivities struct {
	reporter        *MarketplaceReporter
	connectionRepo  connection.Repository
	usageRecordRepo usagerecord.Repository
	logger          *logger.Logger
}

// NewReportActivities builds the activity set. The reporter is constructed once at registration and
// shared with the cancellation flush.
func NewReportActivities(params service.ServiceParams, reporter *MarketplaceReporter, log *logger.Logger) *ReportActivities {
	return &ReportActivities{
		reporter:        reporter,
		connectionRepo:  params.ConnectionRepo,
		usageRecordRepo: params.UsageRecordRepo,
		logger:          log,
	}
}

// MarketplaceUsageReportActivity is the activity entrypoint. A record's reportable connections and its
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
			a.logger.Error(ctx, "marketplace usage report: failed to list marketplace connections", "marketplace", providerType, "error", err, "stage", "list_connections")
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

	// Emitted through the structured logger, not the workflow logger, so a completed run is greppable
	// under the same prefix as this activity's failures.
	a.logger.Info(ctx, "marketplace usage report: completed",
		"total", result.Total, "succeeded", result.Succeeded,
		"failed", result.Failed)
	return result, nil
}

// reportForTenant authenticates each of this tenant/environment's published connections once,
// fetches its unsynced usage records once, and reports each record to the marketplaces it holds an
// agreement on. Both lists
// are fixed for the whole call — nothing is re-queried mid-run, so a connection deleted while this
// run executes only takes effect from the next scheduled run.
func (a *ReportActivities) reportForTenant(
	ctx context.Context,
	tenantID, environmentID string,
	conns []*connection.Connection,
	result *temporalModels.MarketplaceUsageReportWorkflowResult,
) {
	marketplaceConns := make([]*marketplaceConnection, 0, len(conns))
	for _, conn := range conns {
		marketplaceConn, err := a.reporter.prepareMarketplaceConnection(ctx, conn)
		if err != nil {
			continue // already logged inside prepareMarketplaceConnection at the stage that failed
		}
		marketplaceConns = append(marketplaceConns, marketplaceConn)
	}
	if len(marketplaceConns) == 0 {
		return
	}

	records, err := a.usageRecordRepo.ListUnsynced(ctx, tenantID, environmentID)
	if err != nil {
		a.logger.Error(ctx, "marketplace usage report: failed to list unsynced usage records",
			"tenant_id", tenantID, "environment_id", environmentID, "error", err, "stage", "list_unsynced")
		return
	}

	for _, rec := range records {
		if a.reporter.isEligibleForReport(ctx, rec) {
			a.reportRecord(ctx, rec, marketplaceConns, result)
		}
	}
}

// reportRecord reports one record to every marketplace its subscription holds an agreement on,
// persists the outcome, and classifies the record as succeeded, failed or skipped.
func (a *ReportActivities) reportRecord(
	ctx context.Context,
	rec *usagerecord.UsageRecord,
	marketplaceConns []*marketplaceConnection,
	result *temporalModels.MarketplaceUsageReportWorkflowResult,
) {
	var reportableConns []*marketplaceConnection
	for _, marketplaceConn := range marketplaceConns {
		if marketplaceConn.hasAgreementFor(rec.SubscriptionID) {
			reportableConns = append(reportableConns, marketplaceConn)
		}
	}
	if len(reportableConns) == 0 {
		return // the subscription holds no agreement on any connected marketplace right now
	}
	reportedMarketplaces := a.reporter.reportRecordToMarketplaces(ctx, rec, reportableConns)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	for _, marketplace := range reportedMarketplaces {
		entry := rec.Syncs[marketplace]
		a.logger.Info(ctx, "marketplace usage report: usage record synced",
			"tenant_id", tenantID, "environment_id", environmentID, "subscription_id", rec.SubscriptionID,
			"usage_record_id", rec.ID, "marketplace", marketplace,
			"agreement_id", entry.AgreementID, "reporting_id", entry.ReportingID)
	}

	if err := a.usageRecordRepo.MarkSynced(ctx, rec.ID, rec.Syncs, rec.Synced); err != nil {
		a.logger.Error(ctx, "marketplace usage report: failed to record usage sync state",
			"tenant_id", tenantID, "environment_id", environmentID,
			"subscription_id", rec.SubscriptionID, "usage_record_id", rec.ID, "error", err, "stage", "mark_synced")
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

	result.Total++
	switch {
	case !rec.Synced:
		result.Failed++
	case anyAccepted:
		result.Succeeded++
	default:
		result.Skipped++
	}
}
