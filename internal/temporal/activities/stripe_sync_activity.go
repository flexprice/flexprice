package activities

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/integration"
	"github.com/flexprice/flexprice/internal/domain/stripe"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	stripeRepo "github.com/flexprice/flexprice/internal/repository/stripe"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

// StripeSyncActivities implements Stripe synchronization activities
type StripeSyncActivities struct {
	processedEventRepo       events.ProcessedEventRepository
	customerMappingRepo      integration.EntityIntegrationMappingRepository
	stripeSyncBatchRepo      integration.StripeSyncBatchRepository
	stripeTenantConfigRepo   integration.StripeTenantConfigRepository
	meterProviderMappingRepo integration.MeterProviderMappingRepository
	stripeClient             stripe.Client
	encryptionService        security.EncryptionService
	logger                   *logger.Logger
}

// NewStripeSyncActivities creates a new instance of Stripe sync activities
func NewStripeSyncActivities(
	processedEventRepo events.ProcessedEventRepository,
	customerMappingRepo integration.EntityIntegrationMappingRepository,
	stripeSyncBatchRepo integration.StripeSyncBatchRepository,
	stripeTenantConfigRepo integration.StripeTenantConfigRepository,
	meterProviderMappingRepo integration.MeterProviderMappingRepository,
	stripeClient stripe.Client,
	encryptionService security.EncryptionService,
	logger *logger.Logger,
) *StripeSyncActivities {
	return &StripeSyncActivities{
		processedEventRepo:       processedEventRepo,
		customerMappingRepo:      customerMappingRepo,
		stripeSyncBatchRepo:      stripeSyncBatchRepo,
		stripeTenantConfigRepo:   stripeTenantConfigRepo,
		meterProviderMappingRepo: meterProviderMappingRepo,
		stripeClient:             stripeClient,
		encryptionService:        encryptionService,
		logger:                   logger,
	}
}

// AggregateEventsActivity aggregates billable events from ClickHouse for a specific time window
func (a *StripeSyncActivities) AggregateEventsActivity(ctx context.Context, input models.AggregateEventsActivityInput) (*models.AggregateEventsActivityResult, error) {
	a.logger.Infow("starting event aggregation activity",
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID,
		"window_start", input.WindowStart,
		"window_end", input.WindowEnd)

	if err := input.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid aggregation activity input").
			Mark(ierr.ErrValidation)
	}

	// Check if Stripe sync is enabled for this tenant
	config, err := a.stripeTenantConfigRepo.GetByTenantAndEnvironment(ctx, input.TenantID, input.EnvironmentID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get Stripe tenant configuration").
			Mark(ierr.ErrDatabase)
	}

	if config == nil || !config.SyncEnabled {
		a.logger.Infow("Stripe sync not enabled for tenant", "tenant_id", input.TenantID)
		return &models.AggregateEventsActivityResult{
			Aggregations:  []models.EventAggregation{},
			TotalEvents:   0,
			TotalQuantity: 0,
		}, nil
	}

	// Get processed events for the time window
	// Inject tenant and environment IDs into context so repository filters correctly
	ctx = context.WithValue(ctx, types.CtxTenantID, input.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, input.EnvironmentID)

	params := &events.GetProcessedEventsParams{
		StartTime:  input.WindowStart,
		EndTime:    input.WindowEnd,
		Limit:      input.BatchSize,
		CustomerID: input.CustomerID,
	}

	processedEvents, _, err := a.processedEventRepo.GetProcessedEvents(ctx, params)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve processed events from ClickHouse").
			Mark(ierr.ErrDatabase)
	}

	if len(processedEvents) == 0 {
		a.logger.Infow("No processed events found for time window",
			"tenant_id", input.TenantID,
			"window_start", input.WindowStart,
			"window_end", input.WindowEnd)
		return &models.AggregateEventsActivityResult{
			Aggregations:  []models.EventAggregation{},
			TotalEvents:   0,
			TotalQuantity: 0,
		}, nil
	}

	// Filter events that have customer mappings to Stripe
	billableEvents := []*events.ProcessedEvent{}
	for _, event := range processedEvents {
		// Only include events with billable quantity > 0
		if event.QtyBillable.IsPositive() && event.CustomerID != "" && event.MeterID != "" {
			// Check if customer has Stripe mapping
			mapping, err := a.customerMappingRepo.GetByCustomerAndProvider(ctx, event.CustomerID, integration.ProviderTypeStripe)
			if err != nil {
				a.logger.Warnw("Failed to check customer mapping", "customer_id", event.CustomerID, "error", err)
				continue
			}
			if mapping != nil {
				billableEvents = append(billableEvents, event)
			}
		}
	}

	// Aggregate events by (customer_id, meter_id, event_name)
	aggregationMap := make(map[string]*models.EventAggregation)
	totalEvents := 0
	totalQuantity := 0.0

	for _, event := range billableEvents {
		key := fmt.Sprintf("%s:%s:%s", event.CustomerID, event.MeterID, event.EventName)

		billableQty, _ := event.QtyBillable.Float64()

		if agg, exists := aggregationMap[key]; exists {
			agg.AggregatedQuantity += billableQty
			agg.EventCount += 1
		} else {
			aggregationMap[key] = &models.EventAggregation{
				CustomerID:         event.CustomerID,
				MeterID:            event.MeterID,
				EventType:          event.EventName,
				AggregatedQuantity: billableQty,
				EventCount:         1,
			}
		}

		totalEvents += 1
		totalQuantity += billableQty
	}

	// Convert map to slice
	aggregations := make([]models.EventAggregation, 0, len(aggregationMap))
	for _, agg := range aggregationMap {
		aggregations = append(aggregations, *agg)
	}

	a.logger.Infow("completed event aggregation",
		"tenant_id", input.TenantID,
		"aggregations_count", len(aggregations),
		"total_events", totalEvents,
		"total_quantity", totalQuantity)

	return &models.AggregateEventsActivityResult{
		Aggregations:  aggregations,
		TotalEvents:   totalEvents,
		TotalQuantity: totalQuantity,
	}, nil
}

// SyncToStripeActivity sends aggregated events to Stripe's meter API
func (a *StripeSyncActivities) SyncToStripeActivity(ctx context.Context, input models.SyncToStripeActivityInput) (*models.SyncToStripeActivityResult, error) {
	a.logger.Infow("starting Stripe sync activity",
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID,
		"aggregations_count", len(input.Aggregations))

	if err := input.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid Stripe sync activity input").
			Mark(ierr.ErrValidation)
	}

	// Ensure repository calls see the correct tenant & environment
	ctx = context.WithValue(ctx, types.CtxTenantID, input.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, input.EnvironmentID)

	if len(input.Aggregations) == 0 {
		return nil, ierr.NewError("no aggregations to sync").
			WithHint("Stripe sync activity received zero aggregations, nothing to push").
			Mark(ierr.ErrNotFound)
	}

	// Get Stripe configuration for the tenant
	config, err := a.stripeTenantConfigRepo.GetByTenantAndEnvironment(ctx, input.TenantID, input.EnvironmentID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get Stripe tenant configuration").
			Mark(ierr.ErrDatabase)
	}

	if config == nil {
		return nil, ierr.NewError("stripe configuration not found").
			WithHint("Stripe configuration must be set up for tenant").
			Mark(ierr.ErrNotFound)
	}

	// Decrypt API key (if encryption is configured)
	apiKey := config.APIKeyEncrypted
	if a.encryptionService != nil {
		if dec, derr := a.encryptionService.Decrypt(config.APIKeyEncrypted); derr == nil && dec != "" {
			apiKey = dec
		} else if derr != nil {
			a.logger.Warnw("failed to decrypt API key; falling back to stored value", "error", derr)
		}
	}

	// Create tenant-scoped Stripe client so we use the correct key instead of the placeholder
	stripeHTTP := httpclient.NewDefaultClient()
	tenantStripeClient := stripeRepo.NewStripeAPIClient(stripeHTTP, apiKey, a.logger)

	// Replace activity-wide client for this run
	// (thread-safe because activities are single-run instances)
	a.stripeClient = tenantStripeClient

	results := make([]models.SyncBatchResult, 0, len(input.Aggregations))
	successfulSyncs := 0
	failedSyncs := 0

	for _, agg := range input.Aggregations {
		result := a.syncSingleAggregation(ctx, input.TenantID, input.EnvironmentID, agg, input.WindowStart, input.WindowEnd)
		results = append(results, result)

		if result.Success {
			successfulSyncs++
		} else {
			failedSyncs++
		}
	}

	a.logger.Infow("completed Stripe sync activity",
		"tenant_id", input.TenantID,
		"successful_syncs", successfulSyncs,
		"failed_syncs", failedSyncs)

	return &models.SyncToStripeActivityResult{
		SyncedBatches:   results,
		SuccessfulSyncs: successfulSyncs,
		FailedSyncs:     failedSyncs,
	}, nil
}

// syncSingleAggregation syncs a single aggregation to Stripe
func (a *StripeSyncActivities) syncSingleAggregation(ctx context.Context, tenantID, environmentID string, agg models.EventAggregation, windowStart, windowEnd time.Time) models.SyncBatchResult {
	batchID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_STRIPE_BATCH)

	result := models.SyncBatchResult{
		BatchID:            batchID,
		CustomerID:         agg.CustomerID,
		MeterID:            agg.MeterID,
		EventType:          agg.EventType,
		AggregatedQuantity: agg.AggregatedQuantity,
		EventCount:         agg.EventCount,
		Success:            false,
		RetryCount:         0,
	}

	// Get customer integration mapping to resolve Stripe customer ID
	customerMapping, err := a.customerMappingRepo.GetByCustomerAndProvider(ctx, agg.CustomerID, integration.ProviderTypeStripe)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to get customer mapping: %v", err)
		return result
	}

	if customerMapping == nil {
		result.ErrorMessage = "Customer not mapped to Stripe"
		return result
	}

	// Get meter provider mapping to resolve Stripe meter ID
	meterMapping, err := a.meterProviderMappingRepo.GetByMeterAndProvider(ctx, agg.MeterID, integration.ProviderTypeStripe)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to get meter mapping: %v", err)
		return result
	}

	if meterMapping == nil {
		result.ErrorMessage = "Meter not mapped to Stripe"
		return result
	}

	// Create deterministic idempotency key for Stripe API call that respects Stripe's 100-character limit.
	// The original key exceeded the 100-character constraint (tenantID + customerID + meterID + timestamps).
	// We now hash the full input to keep the key short but still unique & deterministic.

	// Build the raw identifier string (for hashing only)
	rawIdentifier := fmt.Sprintf("%s_%s_%s_%d_%d", tenantID, agg.CustomerID, agg.MeterID, windowStart.Unix(), windowEnd.Unix())

	// Use SHA-256 hash; hex-encoded string is 64 characters.
	// Final identifier will be: "flexprice_" + 64-char hash = 74 characters (<100 Stripe limit).
	hashBytes := sha256.Sum256([]byte(rawIdentifier))
	idempotencyKey := fmt.Sprintf("flexprice_%x", hashBytes[:])

	// Resolve the Stripe meter's event name (required by Stripe API)
	meterEventName := meterMapping.ProviderMeterID // default fallback

	// Attempt to retrieve meter details from Stripe to get the authoritative event_name.
	if meter, err := a.stripeClient.GetMeter(ctx, meterMapping.ProviderMeterID); err == nil && meter != nil {
		if meter.Status == "active" {
			meterEventName = meter.EventName
		} else {
			a.logger.Warnw("Stripe meter is not active", "meter_id", meter.ID, "status", meter.Status)
		}
	} else if err != nil {
		a.logger.Warnw("failed to fetch Stripe meter details; using provider_meter_id as event_name", "meter_id", meterMapping.ProviderMeterID, "error", err)
	}

	// Create Stripe meter event
	stripeEvent := &stripe.StripeEvent{
		EventName: meterEventName,
		Payload: map[string]interface{}{
			"stripe_customer_id": customerMapping.ProviderEntityID,
			"value":              agg.AggregatedQuantity,
		},
		Timestamp:  windowEnd,
		Identifier: idempotencyKey,
	}

	// Send event to Stripe
	stripeResp, err := a.stripeClient.CreateMeterEvent(ctx, &stripe.CreateMeterEventRequest{
		EventName:  stripeEvent.EventName,
		Payload:    stripeEvent.Payload,
		Timestamp:  stripeEvent.Timestamp,
		Identifier: stripeEvent.Identifier,
	})

	if err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to send event to Stripe: %v", err)
		return result
	}

	result.Success = true
	result.StripeEventID = stripeResp.ID

	a.logger.Debugw("successfully synced aggregation to Stripe",
		"batch_id", batchID,
		"customer_id", agg.CustomerID,
		"meter_id", agg.MeterID,
		"stripe_event_id", stripeResp.ID,
		"quantity", agg.AggregatedQuantity)

	return result
}

// TrackSyncBatchActivity tracks sync batch results in the database
func (a *StripeSyncActivities) TrackSyncBatchActivity(ctx context.Context, input models.TrackSyncBatchActivityInput) (*models.TrackSyncBatchActivityResult, error) {
	a.logger.Infow("starting batch tracking activity",
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID,
		"sync_results_count", len(input.SyncResults))

	if err := input.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid batch tracking activity input").
			Mark(ierr.ErrValidation)
	}

	if len(input.SyncResults) == 0 {
		return &models.TrackSyncBatchActivityResult{
			TrackedBatches:   0,
			SuccessfulTracks: 0,
			FailedTracks:     0,
		}, nil
	}

	// Convert sync results to stripe sync batch entities
	batches := make([]*integration.StripeSyncBatch, 0, len(input.SyncResults))
	for _, result := range input.SyncResults {
		status := integration.SyncStatusCompleted
		if !result.Success {
			status = integration.SyncStatusFailed
		}

		// Ensure context has tenant/environment for BaseModel defaults
		batchCtx := context.WithValue(ctx, types.CtxTenantID, input.TenantID)
		batchCtx = context.WithValue(batchCtx, types.CtxEnvironmentID, input.EnvironmentID)

		base := types.GetDefaultBaseModel(batchCtx)

		batch := &integration.StripeSyncBatch{
			ID:                 result.BatchID,
			EntityID:           result.CustomerID,
			EntityType:         integration.EntityTypeCustomer,
			MeterID:            result.MeterID,
			EventType:          result.EventType,
			AggregatedQuantity: result.AggregatedQuantity,
			EventCount:         result.EventCount,
			StripeEventID:      result.StripeEventID,
			SyncStatus:         status,
			RetryCount:         result.RetryCount,
			ErrorMessage:       result.ErrorMessage,
			WindowStart:        input.WindowStart,
			WindowEnd:          input.WindowEnd,
			EnvironmentID:      input.EnvironmentID,
			BaseModel:          base,
		}

		batches = append(batches, batch)
	}

	// Bulk create sync batch records
	err := a.stripeSyncBatchRepo.BulkCreate(ctx, batches)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create sync batch records").
			Mark(ierr.ErrDatabase)
	}

	successfulTracks := 0
	failedTracks := 0
	for _, result := range input.SyncResults {
		if result.Success {
			successfulTracks++
		} else {
			failedTracks++
		}
	}

	a.logger.Infow("completed batch tracking activity",
		"tenant_id", input.TenantID,
		"tracked_batches", len(batches),
		"successful_tracks", successfulTracks,
		"failed_tracks", failedTracks)

	return &models.TrackSyncBatchActivityResult{
		TrackedBatches:   len(batches),
		SuccessfulTracks: successfulTracks,
		FailedTracks:     failedTracks,
	}, nil
}
