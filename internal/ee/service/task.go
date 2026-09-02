package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type TaskService interface {
	CreateTask(ctx context.Context, req dto.CreateTaskRequest) (*dto.TaskResponse, error)
	GetTask(ctx context.Context, id string) (*dto.TaskResponse, error)
	ListTasks(ctx context.Context, filter *types.TaskFilter) (*dto.ListTasksResponse, error)
	UpdateTaskStatus(ctx context.Context, id string, status types.TaskStatus) error
	ProcessTaskWithStreaming(ctx context.Context, id string) error
	GenerateDownloadURL(ctx context.Context, id string) (string, error)
}

type taskService struct {
	ServiceParams
	streaming *StreamingProcessor
}

func NewTaskService(
	serviceParams ServiceParams,
) TaskService {
	return &taskService{
		ServiceParams: serviceParams,
		streaming:     NewStreamingProcessor(serviceParams.Logger),
	}
}

// importObjectKey composes the S3 key CSV Box wrote to. The convention is
// <prefix>/<upload_id>_<customer_id>.csv where we set CSV Box's customer_id
// to <tenant_id>__<environment_id>. That means the key is fully determined
// by (config prefix, request upload_id, ctx tenant, ctx environment); a
// caller cannot address any other tenant's uploads because the tenant/env
// come from the authenticated ctx, not the request body.
func importObjectKey(prefix, uploadID, tenantID, environmentID string) string {
	trimmed := strings.Trim(prefix, "/")
	filename := fmt.Sprintf("%s_%s__%s.csv", uploadID, tenantID, environmentID)
	if trimmed == "" {
		return filename
	}
	return trimmed + "/" + filename
}

// openImportReader downloads the task's CSV from the imports bucket and
// returns a reader over the bytes. It re-derives the key from ctx +
// upload_id, never from anything on the task row that a caller could have
// influenced — the persisted metadata is treated as an audit log, not a
// source of truth.
func (s *taskService) openImportReader(ctx context.Context, t *task.Task) (*bytes.Reader, error) {
	uploadID, _ := t.Metadata["upload_id"].(string)
	if uploadID == "" {
		return nil, ierr.NewError("task has no upload_id in metadata").
			WithHint("Cannot process an import task that was not created via the CSV Box path").
			Mark(ierr.ErrInvalidOperation)
	}

	store, err := s.StorageResolver.ForPlatform(ctx, storage.PurposeImport)
	if err != nil {
		return nil, err
	}

	key := importObjectKey(
		s.Config.FlexpriceS3Imports.KeyPrefix,
		uploadID,
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
	)
	s.Logger.Info(ctx, "downloading import object",
		"task_id", t.ID,
		"bucket", s.Config.FlexpriceS3Imports.Bucket,
		"key", key)

	data, err := store.Download(ctx, key)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to download import object; verify the upload_id and that CSV Box has finished uploading").
			WithReportableDetails(map[string]interface{}{
				"task_id":   t.ID,
				"upload_id": uploadID,
			}).
			Mark(ierr.ErrHTTPClient)
	}

	return bytes.NewReader(data), nil
}

// chunkProcessorFor returns the entity-specific ChunkProcessor. Today the
// DTO only accepts events, so this is effectively a one-branch switch — the
// other cases stay wired so re-enabling an entity type is a DTO change, not
// a service change.
func (s *taskService) chunkProcessorFor(entityType types.EntityType) (ChunkProcessor, error) {
	switch entityType {
	case types.EntityTypeEvents:
		eventSvc := NewEventService(
			s.EventRepo,
			s.MeterRepo,
			s.EventPublisher,
			s.Logger,
			s.Config,
			s.TracingSvc,
		)
		return &EventsChunkProcessor{eventService: eventSvc, logger: s.Logger}, nil
	case types.EntityTypeCustomers:
		return &CustomersChunkProcessor{
			customerService: NewCustomerService(s.ServiceParams),
			logger:          s.Logger,
		}, nil
	case types.EntityTypePrices:
		return &PricesChunkProcessor{
			priceService: NewPriceService(s.ServiceParams),
			logger:       s.Logger,
		}, nil
	case types.EntityTypeFeatures:
		return &FeaturesChunkProcessor{
			featureService: NewFeatureService(s.ServiceParams),
			logger:         s.Logger,
		}, nil
	default:
		return nil, ierr.NewError("unsupported entity type").
			WithHint("Unsupported entity type").
			WithReportableDetails(map[string]interface{}{
				"entity_type": entityType,
			}).
			Mark(ierr.ErrInvalidOperation)
	}
}

func (s *taskService) CreateTask(ctx context.Context, req dto.CreateTaskRequest) (*dto.TaskResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Imports must be explicitly enabled on this deployment. Fail fast here
	// so the caller sees a clear error instead of a download failure later
	// when the workflow tries to hit an unconfigured bucket.
	if !s.Config.FlexpriceS3Imports.Enabled || s.Config.FlexpriceS3Imports.Bucket == "" {
		return nil, ierr.NewError("csv imports are not configured on this deployment").
			WithHint("Set flexprice_s3_imports.enabled=true and provide bucket/credentials").
			Mark(ierr.ErrInvalidOperation)
	}

	t := req.ToTask(ctx)
	if err := t.Validate(); err != nil {
		return nil, err
	}

	if err := s.TaskRepo.Create(ctx, t); err != nil {
		s.Logger.Error(ctx, "failed to create task", "error", err)
		return nil, err
	}

	// Task is created and ready for processing
	// The API layer will handle starting the temporal workflow

	return dto.NewTaskResponse(t), nil
}

func (s *taskService) GetTask(ctx context.Context, id string) (*dto.TaskResponse, error) {
	t, err := s.TaskRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return dto.NewTaskResponse(t), nil
}

func (s *taskService) ListTasks(ctx context.Context, filter *types.TaskFilter) (*dto.ListTasksResponse, error) {
	tasks, err := s.TaskRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.TaskRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	items := make([]*dto.TaskResponse, len(tasks))
	for i, t := range tasks {
		items[i] = dto.NewTaskResponse(t)
	}

	return &dto.ListTasksResponse{
		Items: items,
		Pagination: types.PaginationResponse{
			Total:  count,
			Limit:  filter.GetLimit(),
			Offset: filter.GetOffset(),
		},
	}, nil
}

func (s *taskService) UpdateTaskStatus(ctx context.Context, id string, status types.TaskStatus) error {
	t, err := s.TaskRepo.Get(ctx, id)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get task").
			Mark(ierr.ErrValidation)
	}

	// Validate status transition
	if !isValidStatusTransition(t.TaskStatus, status) {
		return ierr.NewError(fmt.Sprintf("invalid status transition from %s to %s", t.TaskStatus, status)).
			WithHint("Invalid status transition").
			WithReportableDetails(map[string]interface{}{
				"from": t.TaskStatus,
				"to":   status,
			}).
			Mark(ierr.ErrValidation)
	}

	now := time.Now().UTC()
	t.TaskStatus = status
	t.UpdatedAt = now
	t.UpdatedBy = types.GetUserID(ctx)

	switch status {
	case types.TaskStatusProcessing:
		t.StartedAt = &now
	case types.TaskStatusCompleted:
		t.CompletedAt = &now
	case types.TaskStatusFailed:
		t.FailedAt = &now
	}

	if err := s.TaskRepo.Update(ctx, t); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update task").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Helper functions

func isValidStatusTransition(from, to types.TaskStatus) bool {
	allowedTransitions := map[types.TaskStatus][]types.TaskStatus{
		types.TaskStatusPending: {
			types.TaskStatusProcessing,
			types.TaskStatusFailed,
		},
		types.TaskStatusProcessing: {
			types.TaskStatusCompleted,
			types.TaskStatusFailed,
		},
		types.TaskStatusFailed: {
			types.TaskStatusProcessing,
		},
	}

	allowed, ok := allowedTransitions[from]
	if !ok {
		return false
	}

	for _, status := range allowed {
		if status == to {
			return true
		}
	}

	return false
}

// ProcessTaskWithStreaming downloads the CSV from the imports bucket and
// streams it into the entity-specific chunk processor. Idempotent for
// Temporal retries.
func (s *taskService) ProcessTaskWithStreaming(ctx context.Context, id string) error {
	t, err := s.TaskRepo.Get(ctx, id)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get task").
			Mark(ierr.ErrValidation)
	}

	// Idempotent status flip: a Temporal retry lands here with the task
	// already PROCESSING; that must not be treated as an invalid transition.
	if t.TaskStatus != types.TaskStatusProcessing {
		if err := s.UpdateTaskStatus(ctx, id, types.TaskStatusProcessing); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to update task status").
				Mark(ierr.ErrValidation)
		}
	}

	processingCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	processor, err := s.chunkProcessorFor(t.EntityType)
	if err != nil {
		if updateErr := s.UpdateTaskStatus(ctx, id, types.TaskStatusFailed); updateErr != nil {
			s.Logger.Error(ctx, "failed to update task status", "error", updateErr)
		}
		return err
	}

	reader, err := s.openImportReader(processingCtx, t)
	if err != nil {
		if updateErr := s.UpdateTaskStatus(ctx, id, types.TaskStatusFailed); updateErr != nil {
			s.Logger.Error(ctx, "failed to update task status", "error", updateErr)
		}
		return err
	}

	err = s.streaming.ProcessFileStream(processingCtx, t, reader, processor, DefaultStreamingConfig())
	if err != nil {
		if updateErr := s.UpdateTaskStatus(ctx, id, types.TaskStatusFailed); updateErr != nil {
			s.Logger.Error(ctx, "failed to update task status", "error", updateErr)
		}
		return err
	}

	// Update task with final processing results before marking as completed
	if err := s.updateTaskWithResults(ctx, id, t); err != nil {
		s.Logger.Error(ctx, "failed to update task with results", "error", err)
		return err
	}

	// Update task status to completed
	if err := s.UpdateTaskStatus(ctx, id, types.TaskStatusCompleted); err != nil {
		s.Logger.Error(ctx, "failed to update task status", "error", err)
		return err
	}

	return nil
}

// updateTaskWithResults updates the task with processing results
func (s *taskService) updateTaskWithResults(ctx context.Context, id string, t *task.Task) error {
	// Get the current task to preserve other fields
	currentTask, err := s.TaskRepo.Get(ctx, id)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get task for results update").
			Mark(ierr.ErrValidation)
	}

	// Update only the processing result fields
	currentTask.ProcessedRecords = t.ProcessedRecords
	currentTask.SuccessfulRecords = t.SuccessfulRecords
	currentTask.FailedRecords = t.FailedRecords
	currentTask.ErrorSummary = t.ErrorSummary

	// Update the task in the database
	if err := s.TaskRepo.Update(ctx, currentTask); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update task with results").
			Mark(ierr.ErrValidation)
	}

	s.Logger.Info(ctx, "updated task with processing results",
		"task_id", id,
		"processed_records", currentTask.ProcessedRecords,
		"successful_records", currentTask.SuccessfulRecords,
		"failed_records", currentTask.FailedRecords)

	return nil
}

// EventsChunkProcessor processes chunks of event data
type EventsChunkProcessor struct {
	eventService EventService
	logger       *logger.Logger
}

// ProcessChunk processes a chunk of event records
func (p *EventsChunkProcessor) ProcessChunk(ctx context.Context, chunk [][]string, headers []string, chunkIndex int) (*ChunkResult, error) {
	processedRecords := 0
	successfulRecords := 0
	failedRecords := 0
	var errors []string

	// Batch process events for better performance
	var eventRequests []*dto.IngestEventRequest

	// Parse all records in the chunk
	for i, record := range chunk {
		processedRecords++

		// Create event request
		eventReq := &dto.IngestEventRequest{
			Properties: make(map[string]interface{}),
		}

		// Flag to track if this record should be skipped due to errors
		skipRecord := false

		// Map standard fields
		for j, header := range headers {
			if j >= len(record) {
				continue
			}
			value := record[j]
			switch header {
			case "event_id":
				eventReq.EventID = value
			case "event_name":
				eventReq.EventName = value
			case "external_customer_id":
				eventReq.ExternalCustomerID = value
			case "customer_id":
				eventReq.CustomerID = value
			case "timestamp":
				eventReq.TimestampStr = value
			case "source":
				eventReq.Source = value
			}
		}

		// Parse property fields (headers starting with "properties.")
		for j, header := range headers {
			if j >= len(record) {
				continue
			}
			if strings.HasPrefix(header, "properties.") {
				propertyName := strings.TrimPrefix(header, "properties.")
				eventReq.Properties[propertyName] = record[j]
			}
		}

		// Handle single "properties" column with JSON content
		for j, header := range headers {
			if j >= len(record) {
				continue
			}
			if header == "properties" && record[j] != "" {
				// Parse JSON properties
				var properties map[string]interface{}
				if err := json.Unmarshal([]byte(record[j]), &properties); err != nil {
					errors = append(errors, fmt.Sprintf("Record %d: invalid JSON in properties column: %v", i, err))
					failedRecords++
					skipRecord = true
					break // Break out of the inner loop to skip this record
				}
				// Merge JSON properties into event properties
				for key, value := range properties {
					eventReq.Properties[key] = value
				}
			}
		}

		// Skip this record if JSON parsing failed
		if skipRecord {
			continue
		}

		// Validate the event request
		if err := eventReq.Validate(); err != nil {
			errors = append(errors, fmt.Sprintf("Record %d: %v", i, err))
			failedRecords++
			continue
		}

		// Parse timestamp
		if err := p.parseTimestamp(eventReq); err != nil {
			errors = append(errors, fmt.Sprintf("Record %d: timestamp error: %v", i, err))
			failedRecords++
			continue
		}

		eventRequests = append(eventRequests, eventReq)
	}

	// Batch create events
	if len(eventRequests) > 0 {
		successCount, err := p.batchCreateEvents(ctx, eventRequests)
		if err != nil {
			// If batch creation fails, mark all as failed
			p.logger.Error(ctx, "batch event creation failed", "error", err)
			failedRecords += len(eventRequests)
			errors = append(errors, fmt.Sprintf("Batch event creation failed: %v", err))
		} else {
			successfulRecords += successCount
			failedRecords += len(eventRequests) - successCount
		}
	}

	// Create error summary if there are errors
	var errorSummary *string
	if len(errors) > 0 {
		summary := strings.Join(errors, "; ")
		errorSummary = &summary
	}

	return &ChunkResult{
		ProcessedRecords:  processedRecords,
		SuccessfulRecords: successfulRecords,
		FailedRecords:     failedRecords,
		ErrorSummary:      errorSummary,
	}, nil
}

// parseTimestamp parses the timestamp string
func (p *EventsChunkProcessor) parseTimestamp(eventReq *dto.IngestEventRequest) error {
	if eventReq.TimestampStr != "" {
		timestamp, err := time.Parse(time.RFC3339, eventReq.TimestampStr)
		if err != nil {
			return fmt.Errorf("invalid timestamp format: %w", err)
		}
		eventReq.Timestamp = timestamp
	} else {
		eventReq.Timestamp = time.Now()
	}
	return nil
}

// batchCreateEvents creates multiple events in a batch using the bulk API
// It processes events in batches of BATCH_SIZE to optimize performance
func (p *EventsChunkProcessor) batchCreateEvents(ctx context.Context, events []*dto.IngestEventRequest) (int, error) {
	const BATCH_SIZE = 100 // Process 100 events per batch for optimal performance

	if len(events) == 0 {
		return 0, nil
	}

	totalSuccessCount := 0
	totalFailedCount := 0

	// Process events in batches
	for i := 0; i < len(events); i += BATCH_SIZE {
		end := i + BATCH_SIZE
		if end > len(events) {
			end = len(events)
		}

		batch := events[i:end]
		successCount, failedCount := p.processBatch(ctx, batch)
		totalSuccessCount += successCount
		totalFailedCount += failedCount

		// Log batch progress
		p.logger.Debug(ctx, "processed event batch",
			"batch_start", i,
			"batch_end", end,
			"batch_size", len(batch),
			"success_count", successCount,
			"failed_count", failedCount,
			"total_processed", end,
			"total_remaining", len(events)-end)
	}

	p.logger.Info(ctx, "completed batch event creation",
		"total_events", len(events),
		"successful_events", totalSuccessCount,
		"failed_events", totalFailedCount)

	return totalSuccessCount, nil
}

// processBatch processes a single batch of events using the bulk API
func (p *EventsChunkProcessor) processBatch(ctx context.Context, batch []*dto.IngestEventRequest) (int, int) {
	// Create bulk request
	bulkRequest := &dto.BulkIngestEventRequest{
		Events: batch,
	}

	// Use bulk API for better performance
	if err := p.eventService.BulkCreateEvents(ctx, bulkRequest); err != nil {
		p.logger.Error(ctx, "bulk event creation failed",
			"batch_size", len(batch),
			"error", err)
		return 0, len(batch) // All events in batch failed
	}

	// All events in batch were successful
	return len(batch), 0
}

// CustomersChunkProcessor processes chunks of customer data
type CustomersChunkProcessor struct {
	customerService CustomerService
	logger          *logger.Logger
}

// ProcessChunk processes a chunk of customer records
func (p *CustomersChunkProcessor) ProcessChunk(ctx context.Context, chunk [][]string, headers []string, chunkIndex int) (*ChunkResult, error) {
	processedRecords := 0
	successfulRecords := 0
	failedRecords := 0
	var errors []string

	p.logger.Debug(ctx, "processing customer chunk",
		"chunk_index", chunkIndex,
		"chunk_size", len(chunk),
		"headers", headers)

	// Process each record in the chunk
	for i, record := range chunk {
		processedRecords++

		p.logger.Debug(ctx, "processing customer record",
			"record_index", i,
			"record", record,
			"chunk_index", chunkIndex)

		// Create customer request
		customerReq := &dto.CreateCustomerRequest{
			Metadata: make(map[string]string),
		}

		// Map standard fields
		for j, header := range headers {
			if j >= len(record) {
				continue
			}
			value := record[j]
			switch header {
			case "external_id":
				customerReq.ExternalID = value
			case "name":
				customerReq.Name = value
			case "email":
				customerReq.Email = value
			case "address_line1":
				customerReq.AddressLine1 = value
			case "address_line2":
				customerReq.AddressLine2 = value
			case "address_city":
				customerReq.AddressCity = value
			case "address_state":
				customerReq.AddressState = value
			case "address_postal_code":
				customerReq.AddressPostalCode = value
			case "address_country":
				customerReq.AddressCountry = value
			}
		}

		// Parse metadata fields (headers starting with "metadata.")
		for j, header := range headers {
			if j >= len(record) {
				continue
			}
			if strings.HasPrefix(header, "metadata.") {
				metadataKey := strings.TrimPrefix(header, "metadata.")
				customerReq.Metadata[metadataKey] = record[j]
			}
		}

		// Validate the customer request
		if err := customerReq.Validate(); err != nil {
			errors = append(errors, fmt.Sprintf("Record %d: %v", i, err))
			failedRecords++
			continue
		}

		// Process the customer (create or update)
		if err := p.processCustomer(ctx, customerReq); err != nil {
			errors = append(errors, fmt.Sprintf("Record %d: %v", i, err))
			failedRecords++
			continue
		}

		successfulRecords++
	}

	// Create error summary if there are errors
	var errorSummary *string
	if len(errors) > 0 {
		summary := strings.Join(errors, "; ")
		errorSummary = &summary
	}

	return &ChunkResult{
		ProcessedRecords:  processedRecords,
		SuccessfulRecords: successfulRecords,
		FailedRecords:     failedRecords,
		ErrorSummary:      errorSummary,
	}, nil
}

// processCustomer processes a single customer (create or update)
func (p *CustomersChunkProcessor) processCustomer(ctx context.Context, customerReq *dto.CreateCustomerRequest) error {
	// Check if customer with this external ID already exists
	if customerReq.ExternalID != "" {
		// Log context information for debugging
		tenantID := types.GetTenantID(ctx)
		environmentID := types.GetEnvironmentID(ctx)
		p.logger.Debug(ctx, "looking up existing customer",
			"external_id", customerReq.ExternalID,
			"tenant_id", tenantID,
			"environment_id", environmentID)

		customer, err := p.customerService.GetCustomerByLookupKey(ctx, customerReq.ExternalID)
		if err != nil {
			// Only treat non-"not found" errors as fatal
			if !ierr.IsNotFound(err) {
				p.logger.Error(ctx, "failed to search for existing customer", "external_id", customerReq.ExternalID, "error", err)
				return fmt.Errorf("failed to search for existing customer: %w", err)
			}
			// Customer not found - this is expected for new customers
			p.logger.Debug(ctx, "customer not found in current environment, will create new",
				"external_id", customerReq.ExternalID,
				"tenant_id", tenantID,
				"environment_id", environmentID)
			customer = nil
		} else {
			p.logger.Debug(ctx, "found existing customer",
				"external_id", customerReq.ExternalID,
				"customer_id", customer.ID,
				"tenant_id", tenantID,
				"environment_id", environmentID)
		}

		// Additional debugging: Check if customer exists in other environments
		if customer == nil {
			p.logger.Debug(ctx, "checking if customer exists in other environments",
				"external_id", customerReq.ExternalID,
				"current_tenant_id", tenantID,
				"current_environment_id", environmentID)
		}

		// If customer exists, update it
		if customer != nil && customer.Status == types.StatusPublished {
			existingCustomer := customer

			// Create update request from create request
			updateReq := dto.UpdateCustomerRequest{}

			// Only update fields that are different
			if existingCustomer.ExternalID != customerReq.ExternalID {
				updateReq.ExternalID = &customerReq.ExternalID
			}
			if existingCustomer.Email != customerReq.Email {
				updateReq.Email = &customerReq.Email
			}
			if existingCustomer.Name != customerReq.Name {
				updateReq.Name = &customerReq.Name
			}
			if existingCustomer.AddressLine1 != customerReq.AddressLine1 {
				updateReq.AddressLine1 = &customerReq.AddressLine1
			}
			if existingCustomer.AddressLine2 != customerReq.AddressLine2 {
				updateReq.AddressLine2 = &customerReq.AddressLine2
			}
			if existingCustomer.AddressCity != customerReq.AddressCity {
				updateReq.AddressCity = &customerReq.AddressCity
			}
			if existingCustomer.AddressState != customerReq.AddressState {
				updateReq.AddressState = &customerReq.AddressState
			}
			if existingCustomer.AddressPostalCode != customerReq.AddressPostalCode {
				updateReq.AddressPostalCode = &customerReq.AddressPostalCode
			}
			if existingCustomer.AddressCountry != customerReq.AddressCountry {
				updateReq.AddressCountry = &customerReq.AddressCountry
			}

			// Merge metadata
			mergedMetadata := make(map[string]string)
			maps.Copy(mergedMetadata, existingCustomer.Metadata)
			maps.Copy(mergedMetadata, customerReq.Metadata)
			updateReq.Metadata = mergedMetadata

			// Update the customer
			_, err := p.customerService.UpdateCustomer(ctx, existingCustomer.ID, updateReq)
			if err != nil {
				p.logger.Error(ctx, "failed to update customer", "customer_id", existingCustomer.ID, "error", err)
				return fmt.Errorf("failed to update customer: %w", err)
			}

			p.logger.Info(ctx, "updated existing customer", "customer_id", existingCustomer.ID, "external_id", customerReq.ExternalID)
			return nil
		}
	}

	// If no existing customer found, create a new one
	_, err := p.customerService.CreateCustomer(ctx, *customerReq)
	if err != nil {
		p.logger.Error(ctx, "failed to create customer", "error", err)
		return fmt.Errorf("failed to create customer: %w", err)
	}

	p.logger.Info(ctx, "created new customer", "external_id", customerReq.ExternalID)
	return nil
}

// PricesChunkProcessor processes chunks of price data
type PricesChunkProcessor struct {
	priceService PriceService
	logger       *logger.Logger
}

// ProcessChunk processes a chunk of price records
func (p *PricesChunkProcessor) ProcessChunk(ctx context.Context, chunk [][]string, headers []string, chunkIndex int) (*ChunkResult, error) {
	processedRecords := 0
	successfulRecords := 0
	failedRecords := 0
	var errors []string

	p.logger.Debug(ctx, "processing price chunk",
		"chunk_index", chunkIndex,
		"chunk_size", len(chunk),
		"headers", headers)

	// Process each record in the chunk
	for i, record := range chunk {
		processedRecords++

		p.logger.Debug(ctx, "processing price record",
			"record_index", i,
			"record", record,
			"chunk_index", chunkIndex)

		// Create price request
		priceReq := &dto.CreatePriceRequest{
			Metadata: make(map[string]string),
		}

		hasParsingError := false

		// Map standard fields
		for j, header := range headers {
			if j >= len(record) {
				continue
			}
			value := record[j]
			if value == "" {
				continue
			}
			switch header {
			case "amount":
				amount, _ := decimal.NewFromString(value)
				priceReq.Amount = &amount
			case "currency":
				priceReq.Currency = value
			case "entity_type":
				priceReq.EntityType = types.PriceEntityType(value)
			case "entity_id":
				priceReq.EntityID = value
			case "type":
				priceReq.Type = types.PriceType(value)
			case "price_unit_type":
				priceReq.PriceUnitType = types.PriceUnitType(value)
			case "billing_period":
				priceReq.BillingPeriod = types.BillingPeriod(value)
			case "billing_period_count":
				count, _ := strconv.Atoi(value)
				priceReq.BillingPeriodCount = count
			case "billing_model":
				priceReq.BillingModel = types.BillingModel(value)
			case "meter_id":
				priceReq.MeterID = value
			case "lookup_key":
				priceReq.LookupKey = value
			case "invoice_cadence":
				priceReq.InvoiceCadence = types.InvoiceCadence(value)
			case "trial_period_days", "trial_period":
				trial, _ := strconv.Atoi(value)
				priceReq.TrialPeriodDays = trial
			case "description":
				priceReq.Description = value
			case "tier_mode":
				priceReq.TierMode = types.BillingTier(value)
			case "start_date":
				startDate, _ := time.Parse(time.RFC3339, value)
				priceReq.StartDate = &startDate
			case "end_date":
				endDate, _ := time.Parse(time.RFC3339, value)
				priceReq.EndDate = &endDate
			case "display_name":
				priceReq.DisplayName = value
			case "min_quantity":
				minQty, _ := strconv.ParseInt(value, 10, 64)
				priceReq.MinQuantity = &minQty
			case "group_id":
				priceReq.GroupID = value
			case "transform_quantity_divide_by":
				divideBy, _ := strconv.Atoi(value)
				if priceReq.TransformQuantity == nil {
					priceReq.TransformQuantity = &price.TransformQuantity{}
				}
				priceReq.TransformQuantity.DivideBy = divideBy
			case "transform_quantity_round":
				if priceReq.TransformQuantity == nil {
					priceReq.TransformQuantity = &price.TransformQuantity{}
				}
				priceReq.TransformQuantity.Round = types.RoundType(value)
			case "price_unit_config_price_unit":
				if priceReq.PriceUnitConfig == nil {
					priceReq.PriceUnitConfig = &dto.PriceUnitConfig{}
				}
				priceReq.PriceUnitConfig.PriceUnit = value
			case "price_unit_config_amount":
				amount, _ := decimal.NewFromString(value)
				if priceReq.PriceUnitConfig == nil {
					priceReq.PriceUnitConfig = &dto.PriceUnitConfig{}
				}
				priceReq.PriceUnitConfig.Amount = &amount
			case "tiers":
				// Parse JSON array of tiers
				var tiers []dto.CreatePriceTier
				if err := json.Unmarshal([]byte(value), &tiers); err != nil {
					errors = append(errors, fmt.Sprintf("Record %d: failed to parse tiers JSON: %v", i, err))
					failedRecords++
					hasParsingError = true
					break
				}
				priceReq.Tiers = tiers
			case "price_unit_config_tiers":
				// Parse JSON array of price unit config tiers
				if priceReq.PriceUnitConfig == nil {
					priceReq.PriceUnitConfig = &dto.PriceUnitConfig{}
				}
				var tiers []dto.CreatePriceTier
				if err := json.Unmarshal([]byte(value), &tiers); err != nil {
					errors = append(errors, fmt.Sprintf("Record %d: failed to parse price_unit_config_tiers JSON: %v", i, err))
					failedRecords++
					hasParsingError = true
					break
				}
				priceReq.PriceUnitConfig.PriceUnitTiers = tiers
			}
		}

		// Skip this record if there was a JSON parsing error
		if hasParsingError {
			continue
		}

		// Parse metadata fields (headers starting with "metadata.")
		for j, header := range headers {
			if j >= len(record) {
				continue
			}
			if strings.HasPrefix(header, "metadata.") {
				value := record[j]
				if value != "" {
					metadataKey := strings.TrimPrefix(header, "metadata.")
					priceReq.Metadata[metadataKey] = value
				}
			}
		}

		// Validate the price request
		if err := priceReq.Validate(); err != nil {
			errors = append(errors, fmt.Sprintf("Record %d: %v", i, err))
			failedRecords++
			continue
		}

		// Process the price (create)
		if err := p.processPrice(ctx, priceReq); err != nil {
			errors = append(errors, fmt.Sprintf("Record %d: %v", i, err))
			failedRecords++
			continue
		}

		successfulRecords++
	}

	// Create error summary if there are errors
	var errorSummary *string
	if len(errors) > 0 {
		summary := strings.Join(errors, "; ")
		errorSummary = &summary
	}

	return &ChunkResult{
		ProcessedRecords:  processedRecords,
		SuccessfulRecords: successfulRecords,
		FailedRecords:     failedRecords,
		ErrorSummary:      errorSummary,
	}, nil
}

// processPrice processes a single price (create)
func (p *PricesChunkProcessor) processPrice(ctx context.Context, priceReq *dto.CreatePriceRequest) error {
	// Create the price
	_, err := p.priceService.CreatePrice(ctx, *priceReq)
	if err != nil {
		p.logger.Error(ctx, "failed to create price", "error", err)
		return fmt.Errorf("failed to create price: %w", err)
	}

	p.logger.Info(ctx, "created new price", "entity_type", priceReq.EntityType, "entity_id", priceReq.EntityID, "lookup_key", priceReq.LookupKey)
	return nil
}

// FeaturesChunkProcessor processes chunks of feature data
type FeaturesChunkProcessor struct {
	featureService FeatureService
	logger         *logger.Logger
}

// ProcessChunk processes a chunk of feature records
func (f *FeaturesChunkProcessor) ProcessChunk(ctx context.Context, chunk [][]string, headers []string, chunkIndex int) (*ChunkResult, error) {
	processedRecords := 0
	successfulRecords := 0
	failedRecords := 0
	var errors []string

	f.logger.Debug(ctx, "processing feature chunk",
		"chunk_index", chunkIndex,
		"chunk_size", len(chunk),
		"headers", headers)

	// Process each record in the chunk
	for i, record := range chunk {
		processedRecords++
		f.logger.Debug(ctx, "processing feature record",
			"record_index", i,
			"record", record,
			"chunk_index", chunkIndex)
		// Create feature request
		featureReq := &dto.CreateFeatureRequest{
			Metadata: make(map[string]string),
		}

		// Helper to initialize meter lazily
		initMeterIfNeeded := func() {
			if featureReq.Meter == nil {
				featureReq.Meter = &dto.CreateMeterRequest{}
			}
		}

		// Map standard fields
		for j, header := range headers {
			if j >= len(record) {
				continue
			}
			value := record[j]
			// Skip empty values to avoid overwriting with empty strings
			if value == "" {
				continue
			}
			switch header {
			case "name":
				featureReq.Name = value
			case "type":
				featureReq.Type = types.FeatureType(value)
			case "lookup_key":
				featureReq.LookupKey = value
			case "unit_singular":
				featureReq.UnitSingular = value
			case "unit_plural":
				featureReq.UnitPlural = value
			case "meter_name":
				initMeterIfNeeded()
				featureReq.Meter.Name = value
			case "event_name":
				initMeterIfNeeded()
				featureReq.Meter.EventName = value
			case "reset_usage":
				initMeterIfNeeded()
				featureReq.Meter.ResetUsage = types.ResetUsage(value)
			case "aggregation_type":
				initMeterIfNeeded()
				featureReq.Meter.Aggregation.Type = types.AggregationType(value)
			case "aggregation_field":
				initMeterIfNeeded()
				featureReq.Meter.Aggregation.Field = value
			case "aggregation_multiplier":
				initMeterIfNeeded()
				multiplier, err := decimal.NewFromString(value)
				if err == nil {
					featureReq.Meter.Aggregation.Multiplier = lo.ToPtr(multiplier)
				}
			case "aggregation_bucket_size":
				initMeterIfNeeded()
				featureReq.Meter.Aggregation.BucketSize = types.WindowSize(value)
			}
		}

		// Parse metadata fields (headers starting with "metadata.")
		for j, header := range headers {
			if j >= len(record) {
				continue
			}
			if strings.HasPrefix(header, "metadata.") {
				metadataKey := strings.TrimPrefix(header, "metadata.")
				featureReq.Metadata[metadataKey] = record[j]
			}
		}

		// Set default reset_usage if meter exists but reset_usage not provided
		if featureReq.Meter != nil && featureReq.Meter.ResetUsage == "" {
			featureReq.Meter.ResetUsage = types.ResetUsageBillingPeriod
		}
		// Log parsed feature request for debugging
		f.logger.Debug(ctx, "parsed feature request",
			"record_index", i,
			"name", featureReq.Name,
			"type", featureReq.Type,
			"lookup_key", featureReq.LookupKey,
			"has_meter", featureReq.Meter != nil)
		// Validate the feature request
		if err := featureReq.Validate(); err != nil {
			f.logger.Error(ctx, "feature validation failed",
				"record_index", i,
				"error", err,
				"feature_req", featureReq)
			errors = append(errors, fmt.Sprintf("Record %d: %v", i, err))
			failedRecords++
			continue
		}

		// Process the feature (create or update)
		if err := f.processFeature(ctx, featureReq); err != nil {
			errors = append(errors, fmt.Sprintf("Record %d: %v", i, err))
			failedRecords++
			continue
		}

		successfulRecords++
	}

	// Create error summary if there are errors
	var errorSummary *string
	if len(errors) > 0 {
		summary := strings.Join(errors, "; ")
		errorSummary = &summary
	}

	return &ChunkResult{
		ProcessedRecords:  processedRecords,
		SuccessfulRecords: successfulRecords,
		FailedRecords:     failedRecords,
		ErrorSummary:      errorSummary,
	}, nil
}

// processFeature processes a single feature (create or update)
func (f *FeaturesChunkProcessor) processFeature(ctx context.Context, featureReq *dto.CreateFeatureRequest) error {
	// Check if feature with this lookup key already exists
	if featureReq.LookupKey != "" {
		// Log context information for debugging
		tenantID := types.GetTenantID(ctx)
		environmentID := types.GetEnvironmentID(ctx)
		f.logger.Debug(ctx, "looking up existing feature",
			"lookup_key", featureReq.LookupKey,
			"tenant_id", tenantID,
			"environment_id", environmentID)

		features, err := f.featureService.GetFeatures(ctx, &types.FeatureFilter{
			LookupKey: featureReq.LookupKey,
		})
		if err != nil {
			// Only treat non-"not found" errors as fatal
			if !ierr.IsNotFound(err) {
				f.logger.Error(ctx, "failed to search for existing feature", "lookup_key", featureReq.LookupKey, "error", err)
				return fmt.Errorf("failed to search for existing feature: %w", err)
			}
			// Feature not found - this is expected for new features
			f.logger.Debug(ctx, "feature not found in current environment, will create new",
				"lookup_key", featureReq.LookupKey,
				"tenant_id", tenantID,
				"environment_id", environmentID)
			features = nil
		} else {
			if len(features.Items) > 0 {
				f.logger.Debug(ctx, "found existing feature",
					"lookup_key", featureReq.LookupKey,
					"feature_id", features.Items[0].ID,
					"tenant_id", tenantID,
					"environment_id", environmentID)
			}
		}

		// If feature exists, update it
		if features != nil && len(features.Items) > 0 && features.Items[0].Status == types.StatusPublished {
			existingFeature := features.Items[0]

			// Create update request from create request
			updateReq := dto.UpdateFeatureRequest{}

			// Only update fields that are different
			if existingFeature.Name != featureReq.Name {
				updateReq.Name = &featureReq.Name
			}
			if existingFeature.UnitSingular != featureReq.UnitSingular {
				updateReq.UnitSingular = &featureReq.UnitSingular
			}
			if existingFeature.UnitPlural != featureReq.UnitPlural {
				updateReq.UnitPlural = &featureReq.UnitPlural
			}

			// Merge metadata
			mergedMetadata := make(map[string]string)
			maps.Copy(mergedMetadata, existingFeature.Metadata)
			maps.Copy(mergedMetadata, featureReq.Metadata)
			updateReq.Metadata = lo.ToPtr(types.Metadata(mergedMetadata))

			// Update the feature
			_, err := f.featureService.UpdateFeature(ctx, existingFeature.ID, updateReq)
			if err != nil {
				f.logger.Error(ctx, "failed to update feature", "feature_id", existingFeature.ID, "error", err)
				return fmt.Errorf("failed to update feature: %w", err)
			}

			f.logger.Info(ctx, "updated existing feature", "feature_id", existingFeature.ID, "lookup_key", featureReq.LookupKey)
			return nil
		}
	}

	// If no existing feature found, create a new one
	_, err := f.featureService.CreateFeature(ctx, *featureReq)
	if err != nil {
		f.logger.Error(ctx, "failed to create feature", "error", err)
		return fmt.Errorf("failed to create feature: %w", err)
	}

	f.logger.Info(ctx, "created new feature", "lookup_key", featureReq.LookupKey)
	return nil
}

// GenerateDownloadURL generates a presigned URL for downloading an exported file
func (s *taskService) GenerateDownloadURL(ctx context.Context, id string) (string, error) {
	// Get the task
	t, err := s.TaskRepo.Get(ctx, id)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to get task").
			Mark(ierr.ErrNotFound)
	}

	// Check if task has a file
	if t.FileURL == "" {
		return "", ierr.NewError("task has no file").
			WithHint("Task export has not completed yet or failed").
			Mark(ierr.ErrNotFound)
	}

	provider, bucket, key, err := storage.ParseFileURL(t.FileURL)
	if err != nil {
		return "", err
	}

	s.Logger.Info(ctx, "generating presigned URL for task file",
		"task_id", id,
		"bucket", bucket,
		"key", key)

	// Verify task has a scheduled task (export tasks must have connection credentials)
	if t.ScheduledTaskID == "" {
		return "", ierr.NewError("task has no scheduled_task_id").
			WithHint("Cannot generate download URL for task without scheduled task").
			WithReportableDetails(map[string]interface{}{
				"task_id": id,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Get the scheduled task to find the connection
	scheduledTask, err := s.ScheduledTaskRepo.Get(ctx, t.ScheduledTaskID)
	if err != nil {
		s.Logger.Error(ctx, "failed to get scheduled task", "error", err, "scheduled_task_id", t.ScheduledTaskID)
		return "", ierr.WithError(err).
			WithHint("Failed to get scheduled task").
			Mark(ierr.ErrNotFound)
	}

	if scheduledTask.ConnectionID == "" {
		return "", ierr.NewError("scheduled task has no connection_id").
			WithHint("Cannot generate download URL without connection").
			WithReportableDetails(map[string]interface{}{
				"task_id":           id,
				"scheduled_task_id": t.ScheduledTaskID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Get the connection to determine if it's Flexprice-managed
	conn, err := s.ConnectionRepo.Get(ctx, scheduledTask.ConnectionID)
	if err != nil {
		s.Logger.Error(ctx, "failed to get connection", "error", err, "connection_id", scheduledTask.ConnectionID)
		return "", ierr.WithError(err).
			WithHint("Failed to get connection").
			Mark(ierr.ErrNotFound)
	}

	// Check if connection is Flexprice-managed
	isFlexpriceManaged := conn.SyncConfig != nil && conn.SyncConfig.Storage != nil && conn.SyncConfig.Storage.IsFlexpriceManaged

	// Recorded bucket survives platform rotation.
	if isFlexpriceManaged {
		expectedBucket := conn.SyncConfig.Storage.Bucket
		if expectedBucket == "" {
			expectedBucket = s.Config.FlexpriceS3Exports.Bucket
			if conn.ProviderType == types.SecretProviderGCS {
				expectedBucket = s.Config.FlexpriceGCSExports.Bucket
			}
		}
		if bucket != expectedBucket {
			s.Logger.Info(ctx, "bucket mismatch for Flexprice-managed export",
				"expected_bucket", expectedBucket,
				"actual_bucket", bucket,
				"task_id", id)
			return "", ierr.NewError("bucket mismatch").
				WithHint("File URL bucket does not match Flexprice-managed configuration").
				WithReportableDetails(map[string]interface{}{
					"task_id":         id,
					"expected_bucket": expectedBucket,
					"actual_bucket":   bucket,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	s.Logger.Debug(ctx, "generating presigned URL",
		"connection_id", scheduledTask.ConnectionID,
		"is_flexprice_managed", isFlexpriceManaged)

	store, err := s.StorageResolver.ForConnection(ctx, scheduledTask.ConnectionID)
	if err != nil {
		s.Logger.Error(ctx, "failed to get storage provider", "error", err)
		return "", ierr.WithError(err).
			WithHint("Failed to initialize storage provider").
			Mark(ierr.ErrInternal)
	}

	// Reject URL scheme mismatched to backend.
	if store.Provider() != provider {
		return "", ierr.NewErrorf("file URL provider '%s' does not match connection's configured storage provider '%s'", provider, store.Provider()).
			WithHint("File URL provider does not match connection's configured storage provider").
			WithReportableDetails(map[string]interface{}{
				"task_id":             id,
				"connection_id":       scheduledTask.ConnectionID,
				"file_url_provider":   provider,
				"connection_provider": store.Provider(),
			}).
			Mark(ierr.ErrValidation)
	}

	url, err := store.PresignGet(ctx, key, 30*time.Minute)
	if err != nil {
		s.Logger.Error(ctx, "failed to generate presigned URL", "error", err)
		return "", ierr.WithError(err).
			WithHint("Failed to generate presigned URL").
			Mark(ierr.ErrInternal)
	}

	s.Logger.Info(ctx, "successfully generated presigned URL",
		"task_id", id,
		"connection_id", scheduledTask.ConnectionID,
		"is_flexprice_managed", isFlexpriceManaged)

	return url, nil
}
