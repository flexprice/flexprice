package service

import (
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
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type TaskService interface {
	CreateTask(ctx context.Context, req dto.CreateTaskRequest) (*dto.TaskResponse, error)
	GetTask(ctx context.Context, id string) (*dto.TaskResponse, error)
	ListTasks(ctx context.Context, filter *types.TaskFilter) (*dto.ListTasksResponse, error)
	UpdateTaskStatus(ctx context.Context, id string, status types.TaskStatus) error
	ProcessTaskWithStreaming(ctx context.Context, id string) error
}

type taskService struct {
	ServiceParams
	fileProcessor *FileProcessor
}

func NewTaskService(
	serviceParams ServiceParams,
) TaskService {
	return &taskService{
		ServiceParams: serviceParams,
		fileProcessor: NewFileProcessor(serviceParams.Client, serviceParams.Logger),
	}
}

func (s *taskService) CreateTask(ctx context.Context, req dto.CreateTaskRequest) (*dto.TaskResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	t := req.ToTask(ctx)
	if err := t.Validate(); err != nil {
		return nil, err
	}

	if err := s.TaskRepo.Create(ctx, t); err != nil {
		s.Logger.Error("failed to create task", "error", err)
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

// ProcessTaskWithStreaming processes a task using streaming for large files
func (s *taskService) ProcessTaskWithStreaming(ctx context.Context, id string) error {
	// Get task first to check current status
	t, err := s.TaskRepo.Get(ctx, id)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get task").
			Mark(ierr.ErrValidation)
	}

	// Only update status to processing if not already processing
	// This makes the method idempotent for Temporal retries
	if t.TaskStatus != types.TaskStatusProcessing {
		if err := s.UpdateTaskStatus(ctx, id, types.TaskStatusProcessing); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to update task status").
				Mark(ierr.ErrValidation)
		}
	}

	// Create a context with extended timeout for streaming file processing
	// This ensures we have enough time for large file downloads and processing
	processingCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	// Use the file processor for streaming
	streamingProcessor := s.fileProcessor.StreamingProcessor

	// Process based on entity type
	var processor ChunkProcessor
	switch t.EntityType {
	case types.EntityTypeEvents:
		// Create event service for chunk processor
		eventSvc := NewEventService(
			s.EventRepo,
			s.MeterRepo,
			s.EventPublisher,
			s.Logger,
			s.Config,
		)
		processor = &EventsChunkProcessor{
			eventService: eventSvc,
			logger:       s.Logger,
		}
	case types.EntityTypeCustomers:
		// Create customer service for chunk processor
		customerSvc := NewCustomerService(s.ServiceParams)
		processor = &CustomersChunkProcessor{
			customerService: customerSvc,
			logger:          s.Logger,
		}
	case types.EntityTypePrices:
		// Create price service for chunk processor
		priceSvc := NewPriceService(s.ServiceParams)
		processor = &PricesChunkProcessor{
			priceService: priceSvc,
			logger:       s.Logger,
		}
	default:
		return ierr.NewError("unsupported entity type").
			WithHint("Unsupported entity type").
			WithReportableDetails(map[string]interface{}{
				"entity_type": t.EntityType,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// Process file with streaming using the extended context
	config := DefaultStreamingConfig()
	err = streamingProcessor.ProcessFileStream(processingCtx, t, processor, config)
	if err != nil {
		// Update task status to failed
		if updateErr := s.UpdateTaskStatus(ctx, id, types.TaskStatusFailed); updateErr != nil {
			s.Logger.Error("failed to update task status", "error", updateErr)
		}
		return err
	}

	// Update task with final processing results before marking as completed
	if err := s.updateTaskWithResults(ctx, id, t); err != nil {
		s.Logger.Error("failed to update task with results", "error", err)
		return err
	}

	// Update task status to completed
	if err := s.UpdateTaskStatus(ctx, id, types.TaskStatusCompleted); err != nil {
		s.Logger.Error("failed to update task status", "error", err)
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

	s.Logger.Infow("updated task with processing results",
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
			p.logger.Error("batch event creation failed", "error", err)
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
		p.logger.Debugw("processed event batch",
			"batch_start", i,
			"batch_end", end,
			"batch_size", len(batch),
			"success_count", successCount,
			"failed_count", failedCount,
			"total_processed", end,
			"total_remaining", len(events)-end)
	}

	p.logger.Infow("completed batch event creation",
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
		p.logger.Errorw("bulk event creation failed",
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

	p.logger.Debugw("processing customer chunk",
		"chunk_index", chunkIndex,
		"chunk_size", len(chunk),
		"headers", headers)

	// Process each record in the chunk
	for i, record := range chunk {
		processedRecords++

		p.logger.Debugw("processing customer record",
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
		p.logger.Debugw("looking up existing customer",
			"external_id", customerReq.ExternalID,
			"tenant_id", tenantID,
			"environment_id", environmentID)

		customer, err := p.customerService.GetCustomerByLookupKey(ctx, customerReq.ExternalID)
		if err != nil {
			// Only treat non-"not found" errors as fatal
			if !ierr.IsNotFound(err) {
				p.logger.Error("failed to search for existing customer", "external_id", customerReq.ExternalID, "error", err)
				return fmt.Errorf("failed to search for existing customer: %w", err)
			}
			// Customer not found - this is expected for new customers
			p.logger.Debugw("customer not found in current environment, will create new",
				"external_id", customerReq.ExternalID,
				"tenant_id", tenantID,
				"environment_id", environmentID)
			customer = nil
		} else {
			p.logger.Debugw("found existing customer",
				"external_id", customerReq.ExternalID,
				"customer_id", customer.ID,
				"tenant_id", tenantID,
				"environment_id", environmentID)
		}

		// Additional debugging: Check if customer exists in other environments
		if customer == nil {
			p.logger.Debugw("checking if customer exists in other environments",
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
				p.logger.Error("failed to update customer", "customer_id", existingCustomer.ID, "error", err)
				return fmt.Errorf("failed to update customer: %w", err)
			}

			p.logger.Info("updated existing customer", "customer_id", existingCustomer.ID, "external_id", customerReq.ExternalID)
			return nil
		}
	}

	// If no existing customer found, create a new one
	_, err := p.customerService.CreateCustomer(ctx, *customerReq)
	if err != nil {
		p.logger.Error("failed to create customer", "error", err)
		return fmt.Errorf("failed to create customer: %w", err)
	}

	p.logger.Info("created new customer", "external_id", customerReq.ExternalID)
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

	p.logger.Debugw("processing price chunk",
		"chunk_index", chunkIndex,
		"chunk_size", len(chunk),
		"headers", headers)

	// Process each record in the chunk
	for i, record := range chunk {
		processedRecords++

		p.logger.Debugw("processing price record",
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
			case "billing_cadence":
				priceReq.BillingCadence = types.BillingCadence(value)
			case "meter_id":
				priceReq.MeterID = value
			case "lookup_key":
				priceReq.LookupKey = value
			case "invoice_cadence":
				priceReq.InvoiceCadence = types.InvoiceCadence(value)
			case "trial_period":
				trial, _ := strconv.Atoi(value)
				priceReq.TrialPeriod = trial
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
		p.logger.Error("failed to create price", "error", err)
		return fmt.Errorf("failed to create price: %w", err)
	}

	p.logger.Info("created new price", "entity_type", priceReq.EntityType, "entity_id", priceReq.EntityID, "lookup_key", priceReq.LookupKey)
	return nil
}
