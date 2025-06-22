package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	integration "github.com/flexprice/flexprice/internal/domain/integration"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/client"
)

// StripeIntegrationService defines the interface for Stripe integration operations
type StripeIntegrationService interface {
	// Configuration Management
	CreateTenantConfig(ctx context.Context, req CreateStripeTenantConfigRequest) (*StripeTenantConfigResponse, error)
	GetTenantConfig(ctx context.Context, tenantID, environmentID string) (*StripeTenantConfigResponse, error)
	UpdateTenantConfig(ctx context.Context, tenantID, environmentID string, req UpdateStripeTenantConfigRequest) (*StripeTenantConfigResponse, error)
	DeleteTenantConfig(ctx context.Context, tenantID, environmentID string) error
	TestStripeConnection(ctx context.Context, tenantID, environmentID string) (*StripeConnectionTestResponse, error)

	// Sync Status Monitoring
	GetSyncStatus(ctx context.Context, filter *StripeSyncStatusFilter) (*StripeSyncStatusResponse, error)
	GetSyncBatches(ctx context.Context, filter *integration.StripeSyncBatchFilter) (*ListStripeSyncBatchesResponse, error)
	GetSyncBatch(ctx context.Context, id string) (*StripeSyncBatchResponse, error)

	// Manual Sync Operations
	TriggerManualSync(ctx context.Context, req TriggerManualSyncRequest) (*ManualSyncResponse, error)
	RetryFailedBatches(ctx context.Context, req RetryFailedBatchesRequest) (*RetryFailedBatchesResponse, error)

	// Meter Mapping Management
	CreateMeterMapping(ctx context.Context, req CreateMeterMappingRequest) (*MeterMappingResponse, error)
	GetMeterMapping(ctx context.Context, meterID string, providerType integration.ProviderType) (*MeterMappingResponse, error)
	UpdateMeterMapping(ctx context.Context, meterID string, providerType integration.ProviderType, req UpdateMeterMappingRequest) (*MeterMappingResponse, error)
	DeleteMeterMapping(ctx context.Context, meterID string, providerType integration.ProviderType) error
	ListMeterMappings(ctx context.Context, filter *integration.MeterProviderMappingFilter) (*ListMeterMappingsResponse, error)
}

// DTO types for Stripe Integration Service

// CreateStripeTenantConfigRequest represents a request to create Stripe tenant configuration
type CreateStripeTenantConfigRequest struct {
	APIKey                   string                 `json:"api_key" validate:"required"`
	SyncEnabled              bool                   `json:"sync_enabled"`
	AggregationWindowMinutes int                    `json:"aggregation_window_minutes" validate:"min=5,max=60"`
	WebhookConfig            *StripeWebhookConfig   `json:"webhook_config,omitempty"`
	Metadata                 map[string]interface{} `json:"metadata,omitempty"`
}

// UpdateStripeTenantConfigRequest represents a request to update Stripe tenant configuration
type UpdateStripeTenantConfigRequest struct {
	APIKey                   *string                `json:"api_key,omitempty"`
	SyncEnabled              *bool                  `json:"sync_enabled,omitempty"`
	AggregationWindowMinutes *int                   `json:"aggregation_window_minutes,omitempty" validate:"omitempty,min=5,max=60"`
	WebhookConfig            *StripeWebhookConfig   `json:"webhook_config,omitempty"`
	Metadata                 map[string]interface{} `json:"metadata,omitempty"`
}

// StripeWebhookConfig represents webhook configuration for Stripe
type StripeWebhookConfig struct {
	EndpointURL string `json:"endpoint_url"`
	Secret      string `json:"secret"`
	Enabled     bool   `json:"enabled"`
}

// StripeTenantConfigResponse represents the response for Stripe tenant configuration
type StripeTenantConfigResponse struct {
	TenantID                 string                 `json:"tenant_id"`
	EnvironmentID            string                 `json:"environment_id"`
	SyncEnabled              bool                   `json:"sync_enabled"`
	AggregationWindowMinutes int                    `json:"aggregation_window_minutes"`
	WebhookConfig            *StripeWebhookConfig   `json:"webhook_config,omitempty"`
	Metadata                 map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt                time.Time              `json:"created_at"`
	UpdatedAt                time.Time              `json:"updated_at"`
}

// StripeConnectionTestResponse represents the response for testing Stripe connection
type StripeConnectionTestResponse struct {
	Success   bool      `json:"success"`
	Message   string    `json:"message"`
	LatencyMs int64     `json:"latency_ms"`
	TestedAt  time.Time `json:"tested_at"`
}

// StripeSyncStatusFilter represents filtering options for sync status queries
type StripeSyncStatusFilter struct {
	EntityIDs     []string                 `json:"entity_ids,omitempty"`
	EntityTypes   []integration.EntityType `json:"entity_types,omitempty"`
	MeterIDs      []string                 `json:"meter_ids,omitempty"`
	SyncStatuses  []integration.SyncStatus `json:"sync_statuses,omitempty"`
	TimeRangeFrom *time.Time               `json:"time_range_from,omitempty"`
	TimeRangeTo   *time.Time               `json:"time_range_to,omitempty"`
}

// StripeSyncStatusResponse represents the response for sync status monitoring
type StripeSyncStatusResponse struct {
	Summary SyncStatusSummary         `json:"summary"`
	Metrics SyncMetrics               `json:"metrics"`
	Recent  []StripeSyncBatchResponse `json:"recent_batches"`
}

// SyncStatusSummary represents summary statistics for sync status
type SyncStatusSummary struct {
	TotalBatches      int                            `json:"total_batches"`
	SuccessfulBatches int                            `json:"successful_batches"`
	FailedBatches     int                            `json:"failed_batches"`
	PendingBatches    int                            `json:"pending_batches"`
	StatusBreakdown   map[integration.SyncStatus]int `json:"status_breakdown"`
}

// SyncMetrics represents performance metrics for sync operations
type SyncMetrics struct {
	SuccessRate     float64       `json:"success_rate"`
	AverageLatency  time.Duration `json:"average_latency"`
	TotalEvents     int64         `json:"total_events"`
	EventsPerSecond float64       `json:"events_per_second"`
	LastSyncTime    *time.Time    `json:"last_sync_time"`
}

// ListStripeSyncBatchesResponse represents the response for listing sync batches
type ListStripeSyncBatchesResponse struct {
	Items      []StripeSyncBatchResponse `json:"items"`
	Pagination types.PaginationResponse  `json:"pagination"`
}

// StripeSyncBatchResponse represents a sync batch in the response
type StripeSyncBatchResponse struct {
	ID                 string                 `json:"id"`
	EntityID           string                 `json:"entity_id"`
	EntityType         integration.EntityType `json:"entity_type"`
	MeterID            string                 `json:"meter_id"`
	EventType          string                 `json:"event_type"`
	AggregatedQuantity float64                `json:"aggregated_quantity"`
	EventCount         int                    `json:"event_count"`
	StripeEventID      string                 `json:"stripe_event_id"`
	SyncStatus         integration.SyncStatus `json:"sync_status"`
	RetryCount         int                    `json:"retry_count"`
	ErrorMessage       string                 `json:"error_message"`
	WindowStart        time.Time              `json:"window_start"`
	WindowEnd          time.Time              `json:"window_end"`
	SyncedAt           *time.Time             `json:"synced_at"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
}

// TriggerManualSyncRequest represents a request to trigger manual sync
type TriggerManualSyncRequest struct {
	EntityID   string                 `json:"entity_id" validate:"required"`
	EntityType integration.EntityType `json:"entity_type" validate:"required"`
	MeterID    *string                `json:"meter_id,omitempty"`
	TimeFrom   time.Time              `json:"time_from" validate:"required"`
	TimeTo     time.Time              `json:"time_to" validate:"required"`
	ForceRerun bool                   `json:"force_rerun"`
}

// ManualSyncResponse represents the response for manual sync trigger
type ManualSyncResponse struct {
	WorkflowID   string    `json:"workflow_id"`
	RunID        string    `json:"run_id"`
	TriggeredAt  time.Time `json:"triggered_at"`
	EstimatedETA time.Time `json:"estimated_eta"`
}

// RetryFailedBatchesRequest represents a request to retry failed batches
type RetryFailedBatchesRequest struct {
	BatchIDs    []string       `json:"batch_ids,omitempty"`
	MaxRetryAge *time.Duration `json:"max_retry_age,omitempty"`
	EntityID    *string        `json:"entity_id,omitempty"`
	MeterID     *string        `json:"meter_id,omitempty"`
}

// RetryFailedBatchesResponse represents the response for retry failed batches
type RetryFailedBatchesResponse struct {
	RetriedCount int      `json:"retried_count"`
	SkippedCount int      `json:"skipped_count"`
	BatchIDs     []string `json:"batch_ids"`
}

// Meter mapping DTOs

// CreateMeterMappingRequest represents a request to create meter mapping
type CreateMeterMappingRequest struct {
	MeterID         string                   `json:"meter_id" validate:"required"`
	ProviderType    integration.ProviderType `json:"provider_type" validate:"required"`
	ProviderMeterID string                   `json:"provider_meter_id" validate:"required"`
	SyncEnabled     bool                     `json:"sync_enabled"`
	Configuration   map[string]interface{}   `json:"configuration,omitempty"`
}

// UpdateMeterMappingRequest represents a request to update meter mapping
type UpdateMeterMappingRequest struct {
	ProviderMeterID *string                `json:"provider_meter_id,omitempty"`
	SyncEnabled     *bool                  `json:"sync_enabled,omitempty"`
	Configuration   map[string]interface{} `json:"configuration,omitempty"`
}

// MeterMappingResponse represents a meter mapping in the response
type MeterMappingResponse struct {
	ID              string                   `json:"id"`
	MeterID         string                   `json:"meter_id"`
	ProviderType    integration.ProviderType `json:"provider_type"`
	ProviderMeterID string                   `json:"provider_meter_id"`
	SyncEnabled     bool                     `json:"sync_enabled"`
	Configuration   map[string]interface{}   `json:"configuration"`
	TenantID        string                   `json:"tenant_id"`
	EnvironmentID   string                   `json:"environment_id"`
	CreatedAt       time.Time                `json:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at"`
}

// ListMeterMappingsResponse represents the response for listing meter mappings
type ListMeterMappingsResponse struct {
	Items      []MeterMappingResponse   `json:"items"`
	Pagination types.PaginationResponse `json:"pagination"`
}

// Implementation

type stripeIntegrationService struct {
	ServiceParams
	temporalService *temporal.Service
}

// NewStripeIntegrationService creates a new Stripe integration service
func NewStripeIntegrationService(params ServiceParams, temporalService *temporal.Service) StripeIntegrationService {
	return &stripeIntegrationService{
		ServiceParams:   params,
		temporalService: temporalService,
	}
}

// Configuration Management Implementation

func (s *stripeIntegrationService) CreateTenantConfig(ctx context.Context, req CreateStripeTenantConfigRequest) (*StripeTenantConfigResponse, error) {
	// Check if Stripe integration is enabled globally
	if !s.Config.Stripe.Enabled {
		return nil, ierr.NewError("Stripe integration is disabled").
			WithHint("Enable Stripe integration in configuration").
			Mark(ierr.ErrValidation)
	}

	if err := s.validateCreateTenantConfigRequest(req); err != nil {
		return nil, err
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Check if config already exists
	existing, err := s.StripeTenantConfigRepo.GetByTenantAndEnvironment(ctx, tenantID, environmentID)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}
	if existing != nil {
		return nil, ierr.NewError("tenant configuration already exists").
			WithHint("Use update endpoint to modify existing configuration").
			Mark(ierr.ErrValidation)
	}

	// Test Stripe connection with provided API key
	if err := s.testStripeAPIKey(ctx, req.APIKey); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid Stripe API key or connection failed").
			Mark(ierr.ErrValidation)
	}

	// Create configuration
	config := &integration.StripeTenantConfig{
		ID:                       types.GenerateUUIDWithPrefix("stripe_config"),
		APIKeyEncrypted:          s.encryptAPIKey(req.APIKey),
		SyncEnabled:              req.SyncEnabled,
		AggregationWindowMinutes: req.AggregationWindowMinutes,
		WebhookConfig:            s.mapWebhookConfig(req.WebhookConfig),
		EnvironmentID:            environmentID,
		BaseModel:                types.GetDefaultBaseModel(ctx),
	}

	if err := s.StripeTenantConfigRepo.Create(ctx, config); err != nil {
		return nil, err
	}

	return s.mapTenantConfigToResponse(config), nil
}

func (s *stripeIntegrationService) GetTenantConfig(ctx context.Context, tenantID, environmentID string) (*StripeTenantConfigResponse, error) {
	if tenantID == "" || environmentID == "" {
		return nil, ierr.NewError("tenant_id and environment_id are required").
			WithHint("Both tenant ID and environment ID must be provided").
			Mark(ierr.ErrValidation)
	}

	config, err := s.StripeTenantConfigRepo.GetByTenantAndEnvironment(ctx, tenantID, environmentID)
	if err != nil {
		return nil, err
	}

	return s.mapTenantConfigToResponse(config), nil
}

func (s *stripeIntegrationService) UpdateTenantConfig(ctx context.Context, tenantID, environmentID string, req UpdateStripeTenantConfigRequest) (*StripeTenantConfigResponse, error) {
	if err := s.validateUpdateTenantConfigRequest(req); err != nil {
		return nil, err
	}

	config, err := s.StripeTenantConfigRepo.GetByTenantAndEnvironment(ctx, tenantID, environmentID)
	if err != nil {
		return nil, err
	}

	// Update fields
	if req.APIKey != nil {
		// Test new API key
		if err := s.testStripeAPIKey(ctx, *req.APIKey); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Invalid Stripe API key or connection failed").
				Mark(ierr.ErrValidation)
		}
		config.APIKeyEncrypted = s.encryptAPIKey(*req.APIKey)
	}

	if req.SyncEnabled != nil {
		config.SyncEnabled = *req.SyncEnabled
	}

	if req.AggregationWindowMinutes != nil {
		config.AggregationWindowMinutes = *req.AggregationWindowMinutes
	}

	if req.WebhookConfig != nil {
		config.WebhookConfig = s.mapWebhookConfig(req.WebhookConfig)
	}

	// Note: StripeTenantConfig doesn't have Metadata field in the domain model
	// Metadata can be stored in WebhookConfig if needed

	config.UpdatedAt = time.Now().UTC()
	config.UpdatedBy = types.GetUserID(ctx)

	if err := s.StripeTenantConfigRepo.Update(ctx, config); err != nil {
		return nil, err
	}

	return s.mapTenantConfigToResponse(config), nil
}

func (s *stripeIntegrationService) DeleteTenantConfig(ctx context.Context, tenantID, environmentID string) error {
	config, err := s.StripeTenantConfigRepo.GetByTenantAndEnvironment(ctx, tenantID, environmentID)
	if err != nil {
		return err
	}

	return s.StripeTenantConfigRepo.Delete(ctx, config)
}

func (s *stripeIntegrationService) TestStripeConnection(ctx context.Context, tenantID, environmentID string) (*StripeConnectionTestResponse, error) {
	config, err := s.StripeTenantConfigRepo.GetByTenantAndEnvironment(ctx, tenantID, environmentID)
	if err != nil {
		return nil, err
	}

	apiKey := s.decryptAPIKey(config.APIKeyEncrypted)
	startTime := time.Now()

	testErr := s.testStripeAPIKey(ctx, apiKey)
	latency := time.Since(startTime)

	return &StripeConnectionTestResponse{
		Success:   testErr == nil,
		Message:   s.getConnectionTestMessage(testErr),
		LatencyMs: latency.Milliseconds(),
		TestedAt:  time.Now().UTC(),
	}, nil
}

// Sync Status Monitoring Implementation

func (s *stripeIntegrationService) GetSyncStatus(ctx context.Context, filter *StripeSyncStatusFilter) (*StripeSyncStatusResponse, error) {
	// Convert filter to repository filter
	repoFilter := s.mapSyncStatusFilterToRepoFilter(filter)

	// Get sync batches
	batches, err := s.StripeSyncBatchRepo.List(ctx, repoFilter)
	if err != nil {
		return nil, err
	}

	// Calculate summary and metrics
	summary := s.calculateSyncStatusSummary(batches)
	metrics := s.calculateSyncMetrics(batches)

	// Get recent batches (limit to 10)
	limit := 10
	offset := 0
	order := "desc"
	recentFilter := &integration.StripeSyncBatchFilter{
		QueryFilter: types.QueryFilter{
			Limit:  &limit,
			Offset: &offset,
			Order:  &order,
		},
	}
	recentBatches, err := s.StripeSyncBatchRepo.List(ctx, recentFilter)
	if err != nil {
		return nil, err
	}

	return &StripeSyncStatusResponse{
		Summary: summary,
		Metrics: metrics,
		Recent:  s.mapSyncBatchesToResponses(recentBatches),
	}, nil
}

func (s *stripeIntegrationService) GetSyncBatches(ctx context.Context, filter *integration.StripeSyncBatchFilter) (*ListStripeSyncBatchesResponse, error) {
	batches, err := s.StripeSyncBatchRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.StripeSyncBatchRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	return &ListStripeSyncBatchesResponse{
		Items:      s.mapSyncBatchesToResponses(batches),
		Pagination: types.NewPaginationResponse(count, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

func (s *stripeIntegrationService) GetSyncBatch(ctx context.Context, id string) (*StripeSyncBatchResponse, error) {
	if id == "" {
		return nil, ierr.NewError("batch ID is required").
			WithHint("Batch ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	batch, err := s.StripeSyncBatchRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return s.mapSyncBatchToResponse(batch), nil
}

// Manual Sync Operations Implementation

func (s *stripeIntegrationService) TriggerManualSync(ctx context.Context, req TriggerManualSyncRequest) (*ManualSyncResponse, error) {
	// Check if Stripe integration is enabled globally
	if !s.Config.Stripe.Enabled {
		return nil, ierr.NewError("Stripe integration is disabled").
			WithHint("Enable Stripe integration in configuration").
			Mark(ierr.ErrValidation)
	}

	if err := s.validateTriggerManualSyncRequest(req); err != nil {
		return nil, err
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	if tenantID == "" || environmentID == "" {
		return nil, ierr.NewError("tenant_id and environment_id are required in context").
			WithHint("Make sure the request carries X-Tenant-Id and X-Environment-Id headers or you are authenticated with a tenant level key").
			Mark(ierr.ErrValidation)
	}

	workflowInput := models.StripeEventSyncWorkflowInput{
		TenantID:                 tenantID,
		EnvironmentID:            environmentID,
		WindowStart:              req.TimeFrom,
		WindowEnd:                req.TimeTo,
		GracePeriod:              s.Config.Stripe.GetSyncGracePeriod(),
		BatchSizeLimit:           s.Config.Stripe.BatchSizeLimit,
		MaxRetries:               s.Config.Stripe.MaxRetries,
		APITimeout:               s.Config.Stripe.GetAPITimeout(),
		AggregationWindowMinutes: s.Config.Stripe.DefaultAggregationWindowMinutes,
		CustomerID:               req.EntityID,
	}

	// Start Temporal workflow
	workflowID := fmt.Sprintf("manual-stripe-sync-%s-%d", req.EntityID, time.Now().Unix())

	// Use the same task queue the worker is polling (from config) so the workflow can actually be executed.
	taskQueue := s.Config.Temporal.TaskQueue
	if taskQueue == "" {
		taskQueue = "billing-task-queue" // sane default to avoid empty queue
	}

	workflowOptions := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: taskQueue,
	}

	// The workflow is registered as ManualStripeEventSyncWorkflow; use that exact name.
	we, err := s.temporalService.ExecuteWorkflow(ctx, workflowOptions, "ManualStripeEventSyncWorkflow", workflowInput)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to trigger manual sync workflow").
			Mark(ierr.ErrInternal)
	}

	estimatedDuration := s.calculateEstimatedSyncDuration(req.TimeFrom, req.TimeTo)

	return &ManualSyncResponse{
		WorkflowID:   workflowID,
		RunID:        we.GetRunID(),
		TriggeredAt:  time.Now().UTC(),
		EstimatedETA: time.Now().Add(estimatedDuration),
	}, nil
}

func (s *stripeIntegrationService) RetryFailedBatches(ctx context.Context, req RetryFailedBatchesRequest) (*RetryFailedBatchesResponse, error) {
	// Build filter for failed batches
	filter := &integration.StripeSyncBatchFilter{
		SyncStatuses: []integration.SyncStatus{integration.SyncStatusFailed},
	}

	if req.EntityID != nil {
		filter.EntityIDs = []string{*req.EntityID}
	}

	if req.MeterID != nil {
		filter.MeterIDs = []string{*req.MeterID}
	}

	if req.MaxRetryAge != nil {
		cutoffTime := time.Now().Add(-*req.MaxRetryAge)
		filter.WindowStartAfter = &cutoffTime
	}

	// Get failed batches
	var batchesToRetry []*integration.StripeSyncBatch

	if len(req.BatchIDs) > 0 {
		// Retry specific batches
		for _, batchID := range req.BatchIDs {
			batch, err := s.StripeSyncBatchRepo.Get(ctx, batchID)
			if err != nil {
				s.Logger.Warn("Failed to get batch for retry", "batch_id", batchID, "error", err)
				continue
			}
			if batch.IsRetryable() {
				batchesToRetry = append(batchesToRetry, batch)
			}
		}
	} else {
		// Retry all retryable failed batches
		retryableBatches, err := s.StripeSyncBatchRepo.ListRetryableBatches(ctx, filter)
		if err != nil {
			return nil, err
		}
		batchesToRetry = retryableBatches
	}

	retriedCount := 0
	skippedCount := 0
	var retriedBatchIDs []string

	for _, batch := range batchesToRetry {
		if !batch.IsRetryable() {
			skippedCount++
			continue
		}

		// Mark as retrying
		if err := batch.MarkAsRetrying(); err != nil {
			s.Logger.Warn("Failed to mark batch as retrying", "batch_id", batch.ID, "error", err)
			skippedCount++
			continue
		}

		if err := s.StripeSyncBatchRepo.Update(ctx, batch); err != nil {
			s.Logger.Error("Failed to update batch status", "batch_id", batch.ID, "error", err)
			skippedCount++
			continue
		}

		retriedCount++
		retriedBatchIDs = append(retriedBatchIDs, batch.ID)
	}

	return &RetryFailedBatchesResponse{
		RetriedCount: retriedCount,
		SkippedCount: skippedCount,
		BatchIDs:     retriedBatchIDs,
	}, nil
}

// Meter Mapping Management Implementation

func (s *stripeIntegrationService) CreateMeterMapping(ctx context.Context, req CreateMeterMappingRequest) (*MeterMappingResponse, error) {
	if err := s.validateCreateMeterMappingRequest(req); err != nil {
		return nil, err
	}

	environmentID := types.GetEnvironmentID(ctx)

	// Check for duplicate mapping
	existing, err := s.MeterProviderMappingRepo.GetByMeterAndProvider(ctx, req.MeterID, req.ProviderType)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}
	if existing != nil {
		return nil, ierr.NewError("meter mapping already exists").
			WithHint("Use update endpoint to modify existing mapping").
			Mark(ierr.ErrValidation)
	}

	mapping := &integration.MeterProviderMapping{
		ID:              types.GenerateUUIDWithPrefix("meter_mapping"),
		MeterID:         req.MeterID,
		ProviderType:    req.ProviderType,
		ProviderMeterID: req.ProviderMeterID,
		EnvironmentID:   environmentID,
		SyncEnabled:     req.SyncEnabled,
		Configuration:   req.Configuration,
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}

	if err := s.MeterProviderMappingRepo.Create(ctx, mapping); err != nil {
		return nil, err
	}

	return s.mapMeterMappingToResponse(mapping), nil
}

func (s *stripeIntegrationService) GetMeterMapping(ctx context.Context, meterID string, providerType integration.ProviderType) (*MeterMappingResponse, error) {
	if meterID == "" {
		return nil, ierr.NewError("meter_id is required").
			WithHint("Meter ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	mapping, err := s.MeterProviderMappingRepo.GetByMeterAndProvider(ctx, meterID, providerType)
	if err != nil {
		return nil, err
	}

	return s.mapMeterMappingToResponse(mapping), nil
}

func (s *stripeIntegrationService) UpdateMeterMapping(ctx context.Context, meterID string, providerType integration.ProviderType, req UpdateMeterMappingRequest) (*MeterMappingResponse, error) {
	mapping, err := s.MeterProviderMappingRepo.GetByMeterAndProvider(ctx, meterID, providerType)
	if err != nil {
		return nil, err
	}

	// Update fields
	if req.ProviderMeterID != nil {
		mapping.ProviderMeterID = *req.ProviderMeterID
	}

	if req.SyncEnabled != nil {
		mapping.SyncEnabled = *req.SyncEnabled
	}

	if req.Configuration != nil {
		mapping.Configuration = req.Configuration
	}

	mapping.UpdatedAt = time.Now().UTC()
	mapping.UpdatedBy = types.GetUserID(ctx)

	if err := s.MeterProviderMappingRepo.Update(ctx, mapping); err != nil {
		return nil, err
	}

	return s.mapMeterMappingToResponse(mapping), nil
}

func (s *stripeIntegrationService) DeleteMeterMapping(ctx context.Context, meterID string, providerType integration.ProviderType) error {
	mapping, err := s.MeterProviderMappingRepo.GetByMeterAndProvider(ctx, meterID, providerType)
	if err != nil {
		return err
	}

	return s.MeterProviderMappingRepo.Delete(ctx, mapping)
}

func (s *stripeIntegrationService) ListMeterMappings(ctx context.Context, filter *integration.MeterProviderMappingFilter) (*ListMeterMappingsResponse, error) {
	mappings, err := s.MeterProviderMappingRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.MeterProviderMappingRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	return &ListMeterMappingsResponse{
		Items:      s.mapMeterMappingsToResponses(mappings),
		Pagination: types.NewPaginationResponse(count, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

// Helper methods

func (s *stripeIntegrationService) validateCreateTenantConfigRequest(req CreateStripeTenantConfigRequest) error {
	if req.APIKey == "" {
		return ierr.NewError("api_key is required").
			WithHint("Stripe API key must be provided").
			Mark(ierr.ErrValidation)
	}

	if req.AggregationWindowMinutes < 5 || req.AggregationWindowMinutes > 60 {
		return ierr.NewError("invalid aggregation_window_minutes").
			WithHint("Aggregation window must be between 5 and 60 minutes").
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (s *stripeIntegrationService) validateUpdateTenantConfigRequest(req UpdateStripeTenantConfigRequest) error {
	if req.AggregationWindowMinutes != nil && (*req.AggregationWindowMinutes < 5 || *req.AggregationWindowMinutes > 60) {
		return ierr.NewError("invalid aggregation_window_minutes").
			WithHint("Aggregation window must be between 5 and 60 minutes").
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (s *stripeIntegrationService) validateTriggerManualSyncRequest(req TriggerManualSyncRequest) error {
	// Ensure entity ID is provided
	if req.EntityID == "" {
		return ierr.NewError("entity_id is required").
			WithHint("Entity ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	// Validate time range ordering
	if req.TimeFrom.After(req.TimeTo) {
		return ierr.NewError("invalid time range").
			WithHint("time_from must be before time_to").
			Mark(ierr.ErrValidation)
	}

	// Disallow future end times (allow a small 5-minute tolerance)
	now := time.Now().UTC()
	if req.TimeTo.After(now.Add(5 * time.Minute)) {
		return ierr.NewError("time_to cannot be in the future").
			WithHint("Specify a time_to that is not more than 5 minutes ahead of the current time").
			Mark(ierr.ErrValidation)
	}

	// Limit maximum range to 7 days
	maxRange := 7 * 24 * time.Hour // 7 days
	if req.TimeTo.Sub(req.TimeFrom) > maxRange {
		return ierr.NewError("time range too large").
			WithHint("Maximum time range is 7 days").
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (s *stripeIntegrationService) validateCreateMeterMappingRequest(req CreateMeterMappingRequest) error {
	if req.MeterID == "" {
		return ierr.NewError("meter_id is required").
			WithHint("Meter ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if req.ProviderMeterID == "" {
		return ierr.NewError("provider_meter_id is required").
			WithHint("Provider meter ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (s *stripeIntegrationService) testStripeAPIKey(ctx context.Context, apiKey string) error {
	// For now, we'll validate the API key format
	// In a full implementation, this would create a temporary client and test it
	if apiKey == "" {
		return ierr.NewError("API key is empty").
			WithHint("Stripe API key must not be empty").
			Mark(ierr.ErrValidation)
	}

	// Basic format validation for Stripe API keys
	if len(apiKey) < 30 || (!strings.HasPrefix(apiKey, "sk_test_") && !strings.HasPrefix(apiKey, "sk_live_")) {
		return ierr.NewError("invalid API key format").
			WithHint("Stripe API key must start with 'sk_test_' or 'sk_live_'").
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (s *stripeIntegrationService) encryptAPIKey(apiKey string) string {
	// TODO: Implement proper encryption using FlexPrice's encryption service
	// For now, return the key as-is (this should be replaced with actual encryption)
	return apiKey
}

func (s *stripeIntegrationService) decryptAPIKey(encryptedKey string) string {
	// TODO: Implement proper decryption using FlexPrice's decryption service
	// For now, return the key as-is (this should be replaced with actual decryption)
	return encryptedKey
}

func (s *stripeIntegrationService) mapWebhookConfig(config *StripeWebhookConfig) map[string]interface{} {
	if config == nil {
		return nil
	}

	return map[string]interface{}{
		"endpoint_url": config.EndpointURL,
		"secret":       config.Secret,
		"enabled":      config.Enabled,
	}
}

func (s *stripeIntegrationService) mapTenantConfigToResponse(config *integration.StripeTenantConfig) *StripeTenantConfigResponse {
	return &StripeTenantConfigResponse{
		TenantID:                 config.TenantID,
		EnvironmentID:            config.EnvironmentID,
		SyncEnabled:              config.SyncEnabled,
		AggregationWindowMinutes: config.AggregationWindowMinutes,
		WebhookConfig:            s.mapWebhookConfigFromMap(config.WebhookConfig),
		Metadata:                 nil, // StripeTenantConfig doesn't have Metadata field
		CreatedAt:                config.CreatedAt,
		UpdatedAt:                config.UpdatedAt,
	}
}

func (s *stripeIntegrationService) mapWebhookConfigFromMap(config map[string]interface{}) *StripeWebhookConfig {
	if config == nil {
		return nil
	}

	return &StripeWebhookConfig{
		EndpointURL: config["endpoint_url"].(string),
		Secret:      config["secret"].(string),
		Enabled:     config["enabled"].(bool),
	}
}

func (s *stripeIntegrationService) getConnectionTestMessage(err error) string {
	if err == nil {
		return "Connection successful"
	}
	return fmt.Sprintf("Connection failed: %s", err.Error())
}

func (s *stripeIntegrationService) mapSyncStatusFilterToRepoFilter(filter *StripeSyncStatusFilter) *integration.StripeSyncBatchFilter {
	if filter == nil {
		return &integration.StripeSyncBatchFilter{}
	}

	return &integration.StripeSyncBatchFilter{
		EntityIDs:         filter.EntityIDs,
		EntityTypes:       filter.EntityTypes,
		MeterIDs:          filter.MeterIDs,
		SyncStatuses:      filter.SyncStatuses,
		WindowStartAfter:  filter.TimeRangeFrom,
		WindowStartBefore: filter.TimeRangeTo,
	}
}

func (s *stripeIntegrationService) calculateSyncStatusSummary(batches []*integration.StripeSyncBatch) SyncStatusSummary {
	summary := SyncStatusSummary{
		StatusBreakdown: make(map[integration.SyncStatus]int),
	}

	for _, batch := range batches {
		summary.TotalBatches++
		summary.StatusBreakdown[batch.SyncStatus]++

		switch batch.SyncStatus {
		case integration.SyncStatusCompleted:
			summary.SuccessfulBatches++
		case integration.SyncStatusFailed:
			summary.FailedBatches++
		case integration.SyncStatusPending, integration.SyncStatusProcessing, integration.SyncStatusRetrying:
			summary.PendingBatches++
		}
	}

	return summary
}

func (s *stripeIntegrationService) calculateSyncMetrics(batches []*integration.StripeSyncBatch) SyncMetrics {
	if len(batches) == 0 {
		return SyncMetrics{}
	}

	successfulBatches := 0
	totalEvents := int64(0)
	var totalLatency time.Duration
	var lastSyncTime *time.Time

	for _, batch := range batches {
		totalEvents += int64(batch.EventCount)

		if batch.SyncStatus == integration.SyncStatusCompleted {
			successfulBatches++
			if batch.SyncedAt != nil {
				latency := batch.SyncedAt.Sub(batch.CreatedAt)
				totalLatency += latency

				if lastSyncTime == nil || batch.SyncedAt.After(*lastSyncTime) {
					lastSyncTime = batch.SyncedAt
				}
			}
		}
	}

	successRate := float64(successfulBatches) / float64(len(batches))
	averageLatency := time.Duration(0)
	if successfulBatches > 0 {
		averageLatency = totalLatency / time.Duration(successfulBatches)
	}

	eventsPerSecond := float64(0)
	if lastSyncTime != nil && len(batches) > 0 {
		timeRange := lastSyncTime.Sub(batches[len(batches)-1].CreatedAt)
		if timeRange > 0 {
			eventsPerSecond = float64(totalEvents) / timeRange.Seconds()
		}
	}

	return SyncMetrics{
		SuccessRate:     successRate,
		AverageLatency:  averageLatency,
		TotalEvents:     totalEvents,
		EventsPerSecond: eventsPerSecond,
		LastSyncTime:    lastSyncTime,
	}
}

func (s *stripeIntegrationService) mapSyncBatchesToResponses(batches []*integration.StripeSyncBatch) []StripeSyncBatchResponse {
	responses := make([]StripeSyncBatchResponse, len(batches))
	for i, batch := range batches {
		responses[i] = *s.mapSyncBatchToResponse(batch)
	}
	return responses
}

func (s *stripeIntegrationService) mapSyncBatchToResponse(batch *integration.StripeSyncBatch) *StripeSyncBatchResponse {
	return &StripeSyncBatchResponse{
		ID:                 batch.ID,
		EntityID:           batch.EntityID,
		EntityType:         batch.EntityType,
		MeterID:            batch.MeterID,
		EventType:          batch.EventType,
		AggregatedQuantity: batch.AggregatedQuantity,
		EventCount:         batch.EventCount,
		StripeEventID:      batch.StripeEventID,
		SyncStatus:         batch.SyncStatus,
		RetryCount:         batch.RetryCount,
		ErrorMessage:       batch.ErrorMessage,
		WindowStart:        batch.WindowStart,
		WindowEnd:          batch.WindowEnd,
		SyncedAt:           batch.SyncedAt,
		CreatedAt:          batch.CreatedAt,
		UpdatedAt:          batch.UpdatedAt,
	}
}

func (s *stripeIntegrationService) calculateEstimatedSyncDuration(timeFrom, timeTo time.Time) time.Duration {
	// Simple estimation based on time range
	timeRange := timeTo.Sub(timeFrom)

	// Estimate ~1 minute per hour of data
	estimatedMinutes := int(timeRange.Hours())
	if estimatedMinutes < 1 {
		estimatedMinutes = 1
	}
	if estimatedMinutes > 30 {
		estimatedMinutes = 30
	}

	return time.Duration(estimatedMinutes) * time.Minute
}

func (s *stripeIntegrationService) mapMeterMappingToResponse(mapping *integration.MeterProviderMapping) *MeterMappingResponse {
	return &MeterMappingResponse{
		ID:              mapping.ID,
		MeterID:         mapping.MeterID,
		ProviderType:    mapping.ProviderType,
		ProviderMeterID: mapping.ProviderMeterID,
		SyncEnabled:     mapping.SyncEnabled,
		Configuration:   mapping.Configuration,
		TenantID:        mapping.TenantID, // This should be accessed from BaseModel
		EnvironmentID:   mapping.EnvironmentID,
		CreatedAt:       mapping.CreatedAt,
		UpdatedAt:       mapping.UpdatedAt,
	}
}

func (s *stripeIntegrationService) mapMeterMappingsToResponses(mappings []*integration.MeterProviderMapping) []MeterMappingResponse {
	responses := make([]MeterMappingResponse, len(mappings))
	for i, mapping := range mappings {
		responses[i] = *s.mapMeterMappingToResponse(mapping)
	}
	return responses
}
