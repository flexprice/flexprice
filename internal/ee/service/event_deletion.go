package service

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

const (
	// How long a consumer caches the deletions in progress, and so how long the run
	// waits after committing the state row before it resolves ids.
	suppressionCacheTTL = 30 * time.Second
	// Outer bound on the detached run. Past it the mutations are left to the server
	// and the state row is closed as failed.
	deletionRunDeadline = 45 * time.Minute
	// When a row still DELETING is treated as abandoned. The margin over the run
	// deadline covers clock skew and the terminal status commit.
	staleDeletionAge = deletionRunDeadline + 15*time.Minute
)

// marketplaceProviderTypes have already reported and billed the usage outside
// Flexprice, so there is no invoice to point at.
var marketplaceProviderTypes = []string{
	string(types.IntegrationProviderTypeAWSMarketplace),
	string(types.IntegrationProviderTypeGCPMarketplace),
	string(types.IntegrationProviderTypeAzureMarketplace),
}

// EventDeletionService hard-deletes ingested events and the meter usage they
// produced, recording every deleted event first.
type EventDeletionService interface {
	// Delete runs the whole deletion inside the request (Phase 1): guards, record,
	// both deletes, then the dedup-lock release and the draft recompute.
	Delete(ctx context.Context, req *dto.DeleteEventsRequest) (*dto.DeleteEventsResponse, error)
}

type eventDeletionService struct {
	ServiceParams
	// suppressionWait is how long the run waits after committing the state row
	// before resolving, so no consumer is still holding a stale window.
	suppressionWait time.Duration
}

func NewEventDeletionService(params ServiceParams) EventDeletionService {
	return &eventDeletionService{ServiceParams: params, suppressionWait: suppressionCacheTTL}
}

// deletionPlan is what the guards resolved, carried into execution.
type deletionPlan struct {
	scope *events.MeterUsageQueryParams
	stamp events.DeletionStamp
	// rowIDs are the state rows this request opened, and what closes them.
	rowIDs []string
	// affectedSubscriptionIDs own the draft invoices the deletion makes stale.
	affectedSubscriptionIDs []string
}

func (s *eventDeletionService) Delete(ctx context.Context, req *dto.DeleteEventsRequest) (*dto.DeleteEventsResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	p, err := s.openRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "event deletion started",
		"request_id", p.stamp.RequestID,
		"customer_count", len(p.scope.ExternalCustomerIDs),
		"meter_count", len(p.scope.MeterIDs),
		"period_start", p.scope.StartTime,
		"period_end", p.scope.EndTime,
	)

	// Detached from the caller: a connection dropped between the two mutations would
	// leave meter_usage deleted and events intact.
	runCtx, cancelRun := context.WithTimeout(context.WithoutCancel(ctx), deletionRunDeadline)
	defer cancelRun()

	response, reason, err := s.run(runCtx, p)

	// Terminal status on every exit path, on an uncancelled context, so a dropped
	// connection never leaves a customer's invoicing blocked.
	closeCtx := context.WithoutCancel(ctx)
	if err != nil {
		if runCtx.Err() != nil {
			reason = types.EventDeletionReasonDeadlineExceeded
		}
		s.Logger.Error(closeCtx, "event deletion failed",
			"error", err,
			"request_id", p.stamp.RequestID,
			"reason", reason,
		)
		if closeErr := s.EventDeletionRequestRepo.MarkTerminal(closeCtx, p.rowIDs, types.EventDeletionStatusFailed, reason); closeErr != nil {
			s.Logger.Error(closeCtx, "failed to close the event deletion request as failed",
				"error", closeErr,
				"request_id", p.stamp.RequestID,
			)
		}
		return nil, err
	}

	if closeErr := s.EventDeletionRequestRepo.MarkTerminal(closeCtx, p.rowIDs, types.EventDeletionStatusCompleted, ""); closeErr != nil {
		s.Logger.Error(closeCtx, "failed to close the event deletion request as completed",
			"error", closeErr,
			"request_id", p.stamp.RequestID,
		)
		return nil, closeErr
	}

	s.Logger.Info(ctx, "event deletion completed",
		"request_id", p.stamp.RequestID,
		"deleted_events", response.DeletedEvents,
		"deleted_meter_usage_rows", response.DeletedMeterUsageRows,
	)
	return response, nil
}

// openRequest resolves the request, runs every guard and commits the state rows in
// one transaction. On return the invoicing hold and the suppression window are live.
func (s *eventDeletionService) openRequest(ctx context.Context, req *dto.DeleteEventsRequest) (*deletionPlan, error) {
	meterIDs, err := s.resolveMeterIDs(ctx, req.FeatureIDs)
	if err != nil {
		return nil, err
	}

	customers, err := s.resolveCustomers(ctx, req.ExternalCustomerIDs)
	if err != nil {
		return nil, err
	}
	customerIDs := lo.Map(customers, func(c *customer.Customer, _ int) string { return c.ID })

	subs, err := s.resolveSubscriptions(ctx, customerIDs)
	if err != nil {
		return nil, err
	}

	customerIDsToCheck := lo.Uniq(append(customerIDs,
		lo.Map(subs, func(sub *subscription.Subscription, _ int) string { return sub.CustomerID })...))

	affected := affectedSubscriptions(subs, meterIDs)

	// The fence must cover the same customers the guard checks. A subscription
	// owner pulled in by the parent hop is guarded but was not named, so without
	// this its invoice could finalize while its usage is being deleted.
	fenced, err := s.resolveCustomersByID(ctx, customerIDsToCheck)
	if err != nil {
		return nil, err
	}
	externalIDByCustomerID := lo.SliceToMap(fenced, func(c *customer.Customer) (string, string) {
		return c.ID, c.ExternalID
	})
	fencedExternalIDs := lo.Uniq(lo.Compact(lo.Values(externalIDByCustomerID)))
	sort.Strings(fencedExternalIDs)

	// The HTTP request id, so an audit row points back at the call that wrote it.
	// Generated only when there is none, because an empty request_id would make
	// MarkTerminal and the audit read match every request at once.
	requestID := types.GetRequestID(ctx)
	if requestID == "" {
		requestID = types.GenerateUUID()
		ctx = types.WithRequestID(ctx, requestID)
	}
	startedAt := time.Now().UTC()
	var rowIDs []string

	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// At READ COMMITTED this guard read and finalization's gate read can both
		// pass without seeing the other's uncommitted write. Both sides take the
		// same key; sorted, so two holders cannot wait on each other.
		for _, externalCustomerID := range fencedExternalIDs {
			key := deletionLockKey(txCtx, externalCustomerID)
			if lockErr := s.DB.LockWithWait(txCtx, postgres.LockRequest{Key: key}); lockErr != nil {
				return ierr.WithError(lockErr).
					WithHint("Could not serialize the deletion against invoice finalization").
					WithReportableDetails(map[string]interface{}{"external_customer_id": externalCustomerID}).
					Mark(ierr.ErrDatabase)
			}
		}

		inProgress, listErr := s.EventDeletionRequestRepo.ListAll(txCtx,
			inProgressDeletionFilter(fencedExternalIDs))
		if listErr != nil {
			return listErr
		}
		// The fence ignores an abandoned row, but the unique partial index does not,
		// so it would refuse the insert below. Closed here rather than by a sweeper:
		// a tenant retrying the deletion is exactly when the stale row is in the way.
		abandoned, listErr := s.EventDeletionRequestRepo.ListAll(txCtx,
			abandonedDeletionFilter(fencedExternalIDs))
		if listErr != nil {
			return listErr
		}
		if len(abandoned) > 0 {
			abandonedIDs := lo.Map(abandoned, func(row *events.DeletionRequest, _ int) string { return row.ID })
			s.Logger.Error(txCtx, "closing event deletion requests abandoned by a dead process",
				"error", "event deletion request still DELETING past staleDeletionAge",
				"request_count", len(abandonedIDs),
			)
			if markErr := s.EventDeletionRequestRepo.MarkTerminal(txCtx, abandonedIDs,
				types.EventDeletionStatusFailed, types.EventDeletionReasonAbandoned); markErr != nil {
				return markErr
			}
		}

		if len(inProgress) > 0 {
			return ierr.NewError("a deletion is already in progress for this customer").
				WithHint("Wait for the running deletion to finish before starting another one").
				WithReportableDetails(map[string]interface{}{
					"reason":               types.EventDeletionReasonDeletionInProgress,
					"external_customer_id": inProgress[0].ExternalCustomerID,
					"request_id":           inProgress[0].RequestID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}

		if guardErr := s.guardMarketplace(txCtx, customerIDsToCheck, externalIDByCustomerID); guardErr != nil {
			return guardErr
		}

		if guardErr := s.guardFinalizedInvoices(txCtx, req, meterIDs, customerIDsToCheck, affected); guardErr != nil {
			return guardErr
		}

		rows := make([]*events.DeletionRequest, 0, len(fencedExternalIDs))
		for _, externalCustomerID := range fencedExternalIDs {
			rows = append(rows, &events.DeletionRequest{
				ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_EVENT_DELETION_REQUEST),
				RequestID:          requestID,
				ExternalCustomerID: externalCustomerID,
				PeriodStart:        req.PeriodStart.UTC(),
				PeriodEnd:          req.PeriodEnd.UTC(),
				DeletionStatus:     types.EventDeletionStatusDeleting,
				StartedAt:          startedAt,
				SkipPayload:        lo.FromPtrOr(req.SkipPayload, true),
			})
		}
		if createErr := s.EventDeletionRequestRepo.CreateBulk(txCtx, rows); createErr != nil {
			return createErr
		}
		rowIDs = lo.Map(rows, func(row *events.DeletionRequest, _ int) string { return row.ID })
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &deletionPlan{
		scope: &events.MeterUsageQueryParams{
			TenantID:            types.GetTenantID(ctx),
			EnvironmentID:       types.GetEnvironmentID(ctx),
			ExternalCustomerIDs: req.ExternalCustomerIDs,
			MeterIDs:            meterIDs,
			IDs:                 req.EventIDs,
			StartTime:           req.PeriodStart.UTC(),
			EndTime:             req.PeriodEnd.UTC(),
		},
		rowIDs:                  rowIDs,
		affectedSubscriptionIDs: lo.Map(affected, func(sub *subscription.Subscription, _ int) string { return sub.ID }),
		stamp: events.DeletionStamp{
			RequestID:   requestID,
			DeletedBy:   types.GetUserID(ctx),
			DeletedAt:   startedAt,
			SkipPayload: lo.FromPtrOr(req.SkipPayload, true),
		},
	}, nil
}

// run executes past the guards: resolve, record, delete, aftermath. The returned
// reason is what gets stamped on the state row when it errors.
func (s *eventDeletionService) run(ctx context.Context, p *deletionPlan) (*dto.DeleteEventsResponse, types.EventDeletionFailureReason, error) {
	// A consumer whose cache predates the state row can still write an in-period
	// event that no later step removes. Waiting the interval out closes that.
	select {
	case <-ctx.Done():
		return nil, types.EventDeletionReasonDeadlineExceeded, ctx.Err()
	case <-time.After(s.suppressionWait):
	}

	targets, err := s.EventDeletionLogRepo.ResolveEventIDs(ctx, p.scope)
	if err != nil {
		return nil, types.EventDeletionReasonMutationFailed, err
	}

	eventIDs := append(append(make([]string, 0, len(targets.Metered)+len(targets.Unmetered)),
		targets.Metered...), targets.Unmetered...)
	if len(eventIDs) == 0 {
		s.Logger.Info(ctx, "event deletion matched nothing", "request_id", p.stamp.RequestID)
		return &dto.DeleteEventsResponse{}, "", nil
	}

	if len(eventIDs) > events.MaxEventsPerRequest {
		return nil, types.EventDeletionReasonTooManyEvents,
			ierr.NewError("too many events in scope").
				WithHint("Narrow the period and retry").
				WithReportableDetails(map[string]interface{}{
					"reason":      types.EventDeletionReasonTooManyEvents,
					"event_count": len(eventIDs),
					"limit":       events.MaxEventsPerRequest,
				}).
				Mark(ierr.ErrInvalidOperation)
	}

	if len(p.scope.MeterIDs) > 0 {
		stranded, err := s.EventDeletionLogRepo.FindStrandedMeters(ctx, p.scope, eventIDs)
		if err != nil {
			return nil, types.EventDeletionReasonMutationFailed, err
		}
		if stranded.EventCount > 0 {
			return nil, types.EventDeletionReasonMultipleMetersPerEvent,
				ierr.NewError("the matched events also carry usage on a meter this request did not name").
					WithHint("Name every feature these events feed, or delete by event id instead").
					WithReportableDetails(map[string]interface{}{
						"reason":          types.EventDeletionReasonMultipleMetersPerEvent,
						"event_count":     stranded.EventCount,
						"meters_involved": stranded.MetersInvolved,
					}).
					Mark(ierr.ErrInvalidOperation)
		}
	}

	if err := s.EventDeletionLogRepo.RecordDeletedEvents(ctx, p.scope, targets.Metered, targets.Unmetered, p.stamp); err != nil {
		return nil, types.EventDeletionReasonMutationFailed, err
	}

	// meter_usage first. A surviving events row has no reader, a surviving
	// meter_usage row keeps billing.
	if err := s.EventDeletionLogRepo.DeleteMeterUsage(ctx, p.scope, eventIDs); err != nil {
		return nil, mutationReason(err), err
	}
	if err := s.EventDeletionLogRepo.DeleteEvents(ctx, p.scope, eventIDs); err != nil {
		return nil, mutationReason(err), err
	}

	// After the mutations: until then a redelivery of a deleted id must stay
	// dedup-skipped.
	s.releaseDedupLocks(ctx, eventIDs)
	s.recomputeDrafts(ctx, p)

	return &dto.DeleteEventsResponse{
		DeletedEvents:         len(eventIDs),
		DeletedMeterUsageRows: targets.MeterUsageRows,
	}, "", nil
}

// mutationReason tells the two mutation failures apart: only the timeout is
// still running server side.
func mutationReason(err error) types.EventDeletionFailureReason {
	if errors.Is(err, types.ErrMutationTimeout) {
		return types.EventDeletionReasonMutationTimeout
	}
	return types.EventDeletionReasonMutationFailed
}

// releaseDedupLocks drops the dedup lock for every deleted id, so a
// re-ingest under the same id lands. Best effort: a surviving lock only
// delays the re-ingest until its TTL.
func (s *eventDeletionService) releaseDedupLocks(ctx context.Context, eventIDs []string) {
	// Also covers a nil Locker, which is how a deployment without Redis arrives
	// here: dedup is off there, so no lock was ever taken.
	releaser, ok := s.Locker.(cache.KeyReleaser)
	if !ok {
		return
	}

	keys := lo.Map(eventIDs, func(id string, _ int) string {
		return cache.GenerateKey(ctx, cache.PrefixEvent, id)
	})
	if err := releaser.ReleaseKeys(ctx, keys...); err != nil {
		s.Logger.Error(ctx, "failed to release the deduplication locks for the deleted events",
			"error", err,
			"event_count", len(eventIDs),
		)
		return
	}

	s.Logger.Info(ctx, "released the deduplication locks for the deleted events", "event_count", len(eventIDs))
}

// recomputeDrafts recomputes the drafts the deletion made stale. The workflow
// covers only the current period, so older overlapping drafts are computed
// directly. Best effort: the daily cron picks up whatever is missed.
func (s *eventDeletionService) recomputeDrafts(ctx context.Context, p *deletionPlan) {
	if len(p.affectedSubscriptionIDs) == 0 {
		return
	}

	subscriptionService := NewSubscriptionService(s.ServiceParams)
	invoiceService := NewInvoiceService(s.ServiceParams)
	for _, subscriptionID := range p.affectedSubscriptionIDs {
		if _, err := subscriptionService.TriggerSubscriptionDraftAndComputeWorkflow(ctx, subscriptionID); err != nil {
			s.Logger.Error(ctx, "failed to trigger the draft recompute after deletion",
				"error", err,
				"subscription_id", subscriptionID,
			)
		}

		filter := types.NewNoLimitInvoiceFilter()
		filter.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusDraft}
		filter.SubscriptionID = subscriptionID
		filter.PeriodStartLT = lo.ToPtr(p.scope.EndTime)
		filter.PeriodEndGT = lo.ToPtr(p.scope.StartTime)
		filter.SkipLineItems = true
		drafts, err := s.InvoiceRepo.List(ctx, filter)
		if err != nil {
			s.Logger.Error(ctx, "failed to list the drafts to recompute after deletion",
				"error", err,
				"subscription_id", subscriptionID,
			)
			continue
		}

		for _, draft := range drafts {
			if _, _, err := invoiceService.ComputeInvoice(ctx, draft.ID, nil); err != nil {
				s.Logger.Error(ctx, "failed to recompute a draft after deletion",
					"error", err,
					"subscription_id", subscriptionID,
					"invoice_id", draft.ID,
				)
			}
		}
	}
}

// resolveMeterIDs maps the named features to their meters. A request naming only
// unmetered features is refused rather than silently widening to the customer.
func (s *eventDeletionService) resolveMeterIDs(ctx context.Context, featureIDs []string) ([]string, error) {
	if len(featureIDs) == 0 {
		return nil, nil
	}

	filter := types.NewDefaultFeatureFilter()
	filter.FeatureIDs = featureIDs
	features, err := s.FeatureRepo.ListAll(ctx, filter)
	if err != nil {
		return nil, err
	}

	found := lo.SliceToMap(features, func(f *feature.Feature) (string, string) { return f.ID, f.MeterID })
	missing := lo.Filter(featureIDs, func(id string, _ int) bool {
		_, ok := found[id]
		return !ok
	})
	if len(missing) > 0 {
		return nil, ierr.NewError("unknown feature").
			WithHint("Every feature_id must exist in this environment").
			WithReportableDetails(map[string]interface{}{"feature_ids": missing}).
			Mark(ierr.ErrValidation)
	}

	meterIDs := lo.Uniq(lo.Compact(lo.Values(found)))
	if len(meterIDs) == 0 {
		return nil, ierr.NewError("none of the named features is metered").
			WithHint("Only metered features produce usage that can be deleted").
			WithReportableDetails(map[string]interface{}{"feature_ids": featureIDs}).
			Mark(ierr.ErrValidation)
	}
	return meterIDs, nil
}

// resolveCustomers reads the named customers. ListAll, not List: List carries
// Limit 50, which would truncate a 100-customer request and leave the rest unguarded.
func (s *eventDeletionService) resolveCustomers(ctx context.Context, externalCustomerIDs []string) ([]*customer.Customer, error) {
	filter := types.NewCustomerFilter()
	filter.ExternalIDs = externalCustomerIDs
	customers, err := s.CustomerRepo.ListAll(ctx, filter)
	if err != nil {
		return nil, err
	}

	found := lo.SliceToMap(customers, func(c *customer.Customer) (string, struct{}) {
		return c.ExternalID, struct{}{}
	})
	missing := lo.Filter(externalCustomerIDs, func(id string, _ int) bool {
		_, ok := found[id]
		return !ok
	})
	if len(missing) > 0 {
		return nil, ierr.NewError("unknown customer").
			WithHint("Every external_customer_id must exist in this environment").
			WithReportableDetails(map[string]interface{}{"external_customer_ids": missing}).
			Mark(ierr.ErrValidation)
	}
	return customers, nil
}

// resolveCustomersByID reads customers already known to exist, so no validation
// is needed. ListAll, not List, for the same pagination reason as above.
func (s *eventDeletionService) resolveCustomersByID(ctx context.Context, customerIDs []string) ([]*customer.Customer, error) {
	if len(customerIDs) == 0 {
		return nil, nil
	}
	filter := types.NewCustomerFilter()
	filter.CustomerIDs = customerIDs
	return s.CustomerRepo.ListAll(ctx, filter)
}

// resolveSubscriptions reads the customers' subscriptions with line items, then
// pulls in the parents, because an inherited child owns no line items. Every status,
// because the repository reads active only otherwise and a cancelled or paused
// subscription still holds the finalized invoices the guard has to see. Narrowing
// happens on the invoice, not here.
func (s *eventDeletionService) resolveSubscriptions(ctx context.Context, customerIDs []string) ([]*subscription.Subscription, error) {
	filter := types.NewSubscriptionFilter()
	filter.CustomerIDs = customerIDs
	filter.SubscriptionStatus = types.AllSubscriptionStatuses()
	filter.WithLineItems = true
	subs, err := s.SubRepo.ListAll(ctx, filter)
	if err != nil {
		return nil, err
	}

	known := lo.SliceToMap(subs, func(sub *subscription.Subscription) (string, struct{}) {
		return sub.ID, struct{}{}
	})
	parentIDs := make([]string, 0)
	for _, sub := range subs {
		parentID := lo.FromPtr(sub.ParentSubscriptionID)
		if parentID == "" {
			continue
		}
		if _, ok := known[parentID]; ok {
			continue
		}
		parentIDs = append(parentIDs, parentID)
	}
	parentIDs = lo.Uniq(parentIDs)
	if len(parentIDs) == 0 {
		return subs, nil
	}

	parentFilter := types.NewSubscriptionFilter()
	parentFilter.SubscriptionIDs = parentIDs
	parentFilter.SubscriptionStatus = types.AllSubscriptionStatuses()
	parentFilter.WithLineItems = true
	parents, err := s.SubRepo.ListAll(ctx, parentFilter)
	if err != nil {
		return nil, err
	}
	return append(subs, parents...), nil
}

// affectedSubscriptions narrows to the subscriptions carrying a meter in scope.
// With no meter set there is nothing to narrow with, so all of them are.
func affectedSubscriptions(subs []*subscription.Subscription, meterIDs []string) []*subscription.Subscription {
	if len(meterIDs) == 0 {
		return subs
	}
	return lo.Filter(subs, func(sub *subscription.Subscription, _ int) bool {
		return lo.SomeBy(sub.LineItems, func(item *subscription.SubscriptionLineItem) bool {
			return lo.Contains(meterIDs, item.GetMeterID())
		})
	})
}

// guardMarketplace refuses the request when any customer in scope reports usage
// to a marketplace.
func (s *eventDeletionService) guardMarketplace(ctx context.Context, customerIDs []string, externalIDByCustomerID map[string]string) error {
	if len(customerIDs) == 0 {
		return nil
	}

	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityType = types.IntegrationEntityTypeCustomer
	filter.EntityIDs = customerIDs
	filter.ProviderTypes = marketplaceProviderTypes
	mappings, err := s.EntityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return err
	}
	if len(mappings) == 0 {
		return nil
	}

	blocking := mappings[0]
	return ierr.NewError("deletion is not allowed for a marketplace customer").
		WithHint("Usage for marketplace customers has already been reported and billed outside Flexprice").
		WithReportableDetails(map[string]interface{}{
			"reason":               types.EventDeletionReasonMarketplaceCustomer,
			"customer_id":          blocking.EntityID,
			"external_customer_id": externalIDByCustomerID[blocking.EntityID],
			"provider_type":        blocking.ProviderType,
		}).
		Mark(ierr.ErrInvalidOperation)
}

// guardFinalizedInvoices refuses the request when the period touches a finalized
// invoice. Subscription-level when features were named, customer-level otherwise.
func (s *eventDeletionService) guardFinalizedInvoices(
	ctx context.Context,
	req *dto.DeleteEventsRequest,
	meterIDs []string,
	customerIDs []string,
	affected []*subscription.Subscription,
) error {
	if len(meterIDs) > 0 {
		for _, sub := range affected {
			filter := s.finalizedInvoiceFilter(req)
			filter.SubscriptionID = sub.ID
			invoices, err := s.InvoiceRepo.List(ctx, filter)
			if err != nil {
				return err
			}
			if len(invoices) > 0 {
				return errFinalizedInvoiceBlocksDeletion(map[string]interface{}{
					"subscription_id": sub.ID,
					"customer_id":     sub.CustomerID,
					"invoice_id":      invoices[0].ID,
					"period_start":    invoices[0].PeriodStart,
					"period_end":      invoices[0].PeriodEnd,
				})
			}
		}
		return nil
	}

	for _, customerID := range customerIDs {
		filter := s.finalizedInvoiceFilter(req)
		filter.CustomerID = customerID
		invoices, err := s.InvoiceRepo.List(ctx, filter)
		if err != nil {
			return err
		}
		if len(invoices) > 0 {
			return errFinalizedInvoiceBlocksDeletion(map[string]interface{}{
				"customer_id":  customerID,
				"invoice_id":   invoices[0].ID,
				"period_start": invoices[0].PeriodStart,
				"period_end":   invoices[0].PeriodEnd,
			})
		}
	}

	// The finalization gate keys on both customer columns, so a delegated-invoicing
	// invoice billed to another customer must block here too. Without this the
	// guard is narrower than the gate and such an invoice would not stop the delete.
	subscriptionCustomerFilter := s.finalizedInvoiceFilter(req)
	subscriptionCustomerFilter.SubscriptionCustomerIDs = customerIDs
	invoices, err := s.InvoiceRepo.List(ctx, subscriptionCustomerFilter)
	if err != nil {
		return err
	}
	if len(invoices) > 0 {
		return errFinalizedInvoiceBlocksDeletion(map[string]interface{}{
			"customer_id":  lo.FromPtr(invoices[0].SubscriptionCustomerID),
			"invoice_id":   invoices[0].ID,
			"period_start": invoices[0].PeriodStart,
			"period_end":   invoices[0].PeriodEnd,
		})
	}
	return nil
}

// finalizedInvoiceFilter matches every finalized invoice whose period overlaps the
// requested one. Strict on both sides, because periods are half-open: [Jan, Feb)
// does not overlap a [Feb, Mar) deletion.
func (s *eventDeletionService) finalizedInvoiceFilter(req *dto.DeleteEventsRequest) *types.InvoiceFilter {
	filter := types.NewNoLimitInvoiceFilter()
	filter.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusFinalized}
	filter.PeriodStartLT = lo.ToPtr(req.PeriodEnd.UTC())
	filter.PeriodEndGT = lo.ToPtr(req.PeriodStart.UTC())
	filter.SkipLineItems = true
	return filter
}

func errFinalizedInvoiceBlocksDeletion(details map[string]interface{}) error {
	details["reason"] = types.EventDeletionReasonFinalizedInvoiceExists
	return ierr.NewError("finalized invoice exists in the requested period").
		WithHint("Deletion is not allowed for a period that has already been invoiced").
		WithReportableDetails(details).
		Mark(ierr.ErrInvalidOperation)
}

// deletionLockKey is taken by both the deletion guard and the invoice finalization
// gate. It is the only thing that serializes their reads.
func deletionLockKey(ctx context.Context, externalCustomerID string) string {
	return "evtdel:" + types.GetTenantID(ctx) + ":" + types.GetEnvironmentID(ctx) + ":" + externalCustomerID
}

// inProgressDeletionFilter is the fence read: rows still DELETING, optionally
// narrowed to some customers. Rows past staleDeletionAge are left out, so
// ingestion and invoicing stop honouring an abandoned one at the same moment.
func inProgressDeletionFilter(externalCustomerIDs []string) *types.EventDeletionRequestFilter {
	filter := types.NewNoLimitEventDeletionRequestFilter()
	filter.DeletionStatus = types.EventDeletionStatusDeleting
	filter.ExternalCustomerIDs = externalCustomerIDs
	filter.StartedAtGT = lo.ToPtr(time.Now().UTC().Add(-staleDeletionAge))
	return filter
}

// abandonedDeletionFilter is the complement: rows the fence has stopped honouring
// and which nothing will ever close.
func abandonedDeletionFilter(externalCustomerIDs []string) *types.EventDeletionRequestFilter {
	filter := types.NewNoLimitEventDeletionRequestFilter()
	filter.DeletionStatus = types.EventDeletionStatusDeleting
	filter.ExternalCustomerIDs = externalCustomerIDs
	filter.StartedAtLT = lo.ToPtr(time.Now().UTC().Add(-staleDeletionAge))
	return filter
}
