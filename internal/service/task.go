package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

type TaskService interface {
	CreateTask(ctx context.Context, req dto.CreateTaskRequest) (*dto.TaskResponse, error)
	GetTask(ctx context.Context, id string) (*dto.TaskResponse, error)
	ListTasks(ctx context.Context, filter *types.TaskFilter) (*dto.ListTasksResponse, error)
	UpdateTaskStatus(ctx context.Context, id string, status types.TaskStatus) error
	ProcessTask(ctx context.Context, id string) error
}

type taskService struct {
	ServiceParams
}

func NewTaskService(
	serviceParams ServiceParams,
) TaskService {
	return &taskService{
		ServiceParams: serviceParams,
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

	// Start processing the task in sync for now
	// TODO: Start processing the task in async using temporal
	if err := s.ProcessTask(ctx, t.ID); err != nil {
		s.Logger.Error("failed to process task", "error", err)
		return nil, err
	}

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

func (s *taskService) ProcessTask(ctx context.Context, id string) error {
	// Update status to processing
	if err := s.UpdateTaskStatus(ctx, id, types.TaskStatusProcessing); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update task status").
			Mark(ierr.ErrValidation)
	}

	// Refresh task after status update
	t, err := s.TaskRepo.Get(ctx, id)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get task").
			Mark(ierr.ErrValidation)
	}

	// Create progress tracker with the task object
	tracker := newProgressTracker(ctx, t, 100, 30*time.Second, s.TaskRepo, s.Logger)

	// Process based on entity type
	var processErr error
	switch t.EntityType {
	case types.EntityTypeEvents:
		processErr = s.processEvents(ctx, t, tracker)
	case types.EntityTypeCustomers:
		processErr = s.processCustomers(ctx, t, tracker)
	default:
		processErr = ierr.NewError("unsupported entity type").
			WithHint("Unsupported entity type").
			WithReportableDetails(map[string]interface{}{
				"entity_type": t.EntityType,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// Ensure all progress is updated before updating final status
	tracker.Complete()

	// Update final status based on error
	if processErr != nil {
		// Ensure we mark the task as failed
		if err := s.UpdateTaskStatus(ctx, id, types.TaskStatusFailed); err != nil {
			s.Logger.Error("failed to update task status", "error", err)
		}
		return processErr
	}

	// Only mark as completed if there was no error
	if err := s.UpdateTaskStatus(ctx, id, types.TaskStatusCompleted); err != nil {
		s.Logger.Error("failed to update task status", "error", err)
		return err
	}

	return nil
}

// downloadFile downloads a file from the given URL and returns the response body
func (s *taskService) downloadFile(ctx context.Context, t *task.Task) ([]byte, error) {
	// Get the actual download URL
	downloadURL := t.FileURL
	if strings.Contains(downloadURL, "drive.google.com") {
		// Extract file ID from Google Drive URL
		fileID := extractGoogleDriveFileID(downloadURL)
		if fileID == "" {
			return nil, ierr.NewError("invalid Google Drive URL").
				WithHint("Invalid Google Drive URL").
				WithReportableDetails(map[string]interface{}{
					"url": downloadURL,
				}).
				Mark(ierr.ErrValidation)
		}
		downloadURL = fmt.Sprintf("https://drive.google.com/uc?export=download&id=%s", fileID)
		s.Logger.Debugw("converted Google Drive URL", "original", t.FileURL, "download_url", downloadURL)
	}

	// Download file
	req := &httpclient.Request{
		Method: "GET",
		URL:    downloadURL,
	}

	resp, err := s.Client.Send(ctx, req)
	if err != nil {
		s.Logger.Error("failed to download file", "error", err, "url", downloadURL)
		errorSummary := fmt.Sprintf("Failed to download file: %v", err)
		t.ErrorSummary = &errorSummary
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		s.Logger.Error("failed to download file", "status_code", resp.StatusCode, "url", downloadURL)
		errorSummary := fmt.Sprintf("Failed to download file: HTTP %d", resp.StatusCode)
		t.ErrorSummary = &errorSummary
		return nil, ierr.NewError(fmt.Sprintf("failed to download file: %d", resp.StatusCode)).
			WithHint("Failed to download file").
			WithReportableDetails(map[string]interface{}{
				"status_code": resp.StatusCode,
				"url":         downloadURL,
			}).
			Mark(ierr.ErrHTTPClient)
	}

	// Log the first few bytes of the response for debugging
	previewLen := 200
	if len(resp.Body) < previewLen {
		previewLen = len(resp.Body)
	}
	s.Logger.Debugw("received file content preview",
		"preview", string(resp.Body[:previewLen]),
		"content_type", resp.Headers["Content-Type"],
		"content_length", len(resp.Body))

	return resp.Body, nil
}

// prepareCSVReader creates a configured CSV reader from the file content
func (s *taskService) prepareCSVReader(fileContent []byte) (*csv.Reader, error) {
	// Check for and remove BOM if present
	if len(fileContent) >= 3 && fileContent[0] == 0xEF && fileContent[1] == 0xBB && fileContent[2] == 0xBF {
		// BOM detected, remove it
		fileContent = fileContent[3:]
		s.Logger.Debug("DEBUG: BOM detected and removed from file content")
	}

	reader := csv.NewReader(bytes.NewReader(fileContent))

	// Configure CSV reader to handle potential issues
	reader.LazyQuotes = true       // Allow lazy quotes
	reader.FieldsPerRecord = -1    // Allow variable number of fields
	reader.ReuseRecord = true      // Reuse record memory
	reader.TrimLeadingSpace = true // Trim leading space

	return reader, nil
}

// processCSVFile is a generic function to process a CSV file with a custom row processor
func (s *taskService) processCSVFile(
	ctx context.Context,
	t *task.Task,
	tracker task.ProgressTracker,
	validateHeaders func([]string) error,
	processHeadersFunc func([]string) ([]string, map[int]string, error),
	processRowFunc func(lineNum int, record []string, standardHeaders []string, specialColumns map[int]string) (bool, error),
) error {
	// Download the file
	fileContent, err := s.downloadFile(ctx, t)
	if err != nil {
		return err
	}

	// Prepare CSV reader
	reader, err := s.prepareCSVReader(fileContent)
	if err != nil {
		return err
	}

	// Read headers
	headers, err := reader.Read()
	if err != nil {
		previewLen := 200
		if len(fileContent) < previewLen {
			previewLen = len(fileContent)
		}
		s.Logger.Error("failed to read CSV headers",
			"error", err,
			"content_preview", string(fileContent[:previewLen]))
		errorSummary := fmt.Sprintf("Failed to read CSV headers: %v", err)
		t.ErrorSummary = &errorSummary
		return ierr.NewError("failed to read CSV headers").
			WithHint("Failed to read CSV headers").
			WithReportableDetails(map[string]interface{}{
				"error": err,
			}).
			Mark(ierr.ErrValidation)
	}

	s.Logger.Debugw("parsed CSV headers", "headers", headers)

	// Process headers with the provided function
	standardHeaders, specialColumns, err := processHeadersFunc(headers)
	if err != nil {
		errorSummary := fmt.Sprintf("Failed to process headers: %v", err)
		t.ErrorSummary = &errorSummary
		return err
	}

	// Debug: Print processed headers
	s.Logger.Debug("DEBUG: Processed standard headers: %#v\n", standardHeaders)
	s.Logger.Debug("DEBUG: Special columns: %#v\n", specialColumns)

	// Validate required columns
	if err := validateHeaders(standardHeaders); err != nil {
		errorSummary := fmt.Sprintf("Invalid CSV format: %v", err)
		t.ErrorSummary = &errorSummary
		return err
	}

	// Process rows
	lineNum := 1 // Start after headers
	var failureCount int
	var errorLines []string // Track line numbers with errors

	for {
		lineNum++
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			s.Logger.Error("failed to read CSV line", "line", lineNum, "error", err)
			errorLines = append(errorLines, fmt.Sprintf("Line %d: Failed to read - %v", lineNum, err))
			tracker.Increment(false, err)
			failureCount++
			continue
		}

		// Process the row with the provided function
		success, err := processRowFunc(lineNum, record, standardHeaders, specialColumns)
		if !success {
			errorLines = append(errorLines, fmt.Sprintf("Line %d: %v", lineNum, err))
			tracker.Increment(false, err)
			failureCount++
			continue
		}

		tracker.Increment(true, nil)
	}

	// Return error if any rows failed
	if failureCount > 0 {
		// Create a summary of errors
		// Keep only the first 10 error lines to avoid too long summaries
		maxErrors := 10
		if len(errorLines) > maxErrors {
			errorSummary := fmt.Sprintf("%d records failed to process. First %d errors:\n%s\n(and %d more errors...)",
				failureCount,
				maxErrors,
				strings.Join(errorLines[:maxErrors], "\n"),
				len(errorLines)-maxErrors)
			t.ErrorSummary = &errorSummary
		} else {
			errorSummary := fmt.Sprintf("%d records failed to process:\n%s",
				failureCount,
				strings.Join(errorLines, "\n"))
			t.ErrorSummary = &errorSummary
		}

		// Update the task one final time to ensure error summary is saved
		if err := s.TaskRepo.Update(ctx, t); err != nil {
			s.Logger.Error("failed to update task with error summary", "error", err)
		}

		entityName := strings.ToLower(string(t.EntityType))
		return ierr.NewError(fmt.Sprintf("%d %s failed to process", failureCount, entityName)).
			WithHint("Failed to process").
			WithReportableDetails(map[string]interface{}{
				"entity_name":   entityName,
				"failure_count": failureCount,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (s *taskService) processEvents(ctx context.Context, t *task.Task, tracker task.ProgressTracker) error {
	// Define header processor for events
	processEventHeaders := func(headers []string) ([]string, map[int]string, error) {
		propertyColumns := make(map[int]string) // index -> property name
		standardHeaders := make([]string, 0)
		for i, header := range headers {
			if strings.HasPrefix(header, "properties.") {
				propertyName := strings.TrimPrefix(header, "properties.")
				propertyColumns[i] = propertyName
			} else {
				standardHeaders = append(standardHeaders, header)
			}
		}
		return standardHeaders, propertyColumns, nil
	}

	// Define row processor for events
	processEventRow := func(lineNum int, record []string, standardHeaders []string, propertyColumns map[int]string) (bool, error) {
		// Create event request with standard fields
		eventReq := &dto.IngestEventRequest{
			Properties: make(map[string]interface{}),
		}

		// Map standard fields
		for i, header := range standardHeaders {
			if i >= len(record) {
				continue
			}
			value := record[i]
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

		// Parse property fields
		if err := s.parsePropertyFields(record, propertyColumns, eventReq); err != nil {
			s.Logger.Error("failed to parse property fields", "line", lineNum, "error", err)
			return false, err
		}

		// Validate the event request
		if err := eventReq.Validate(); err != nil {
			s.Logger.Error("failed to validate event", "line", lineNum, "error", err)
			return false, err
		}

		// Parse timestamp
		if err := s.parseTimestamp(eventReq); err != nil {
			s.Logger.Error("failed to parse timestamp", "line", lineNum, "error", err)
			return false, err
		}

		// Process event
		eventSvc := NewEventService(
			s.EventRepo,
			s.MeterRepo,
			s.EventPublisher,
			s.Logger,
		)
		err := eventSvc.CreateEvent(ctx, eventReq)
		if err != nil {
			s.Logger.Error("failed to create event", "line", lineNum, "error", err)
			return false, err
		}

		return true, nil
	}

	// Process the CSV file with the event-specific processors
	return s.processCSVFile(
		ctx,
		t,
		tracker,
		validateEventsRequiredColumns,
		processEventHeaders,
		processEventRow,
	)
}

func (s *taskService) processCustomers(ctx context.Context, t *task.Task, tracker task.ProgressTracker) error {
	// Define header processor for customers
	processCustomerHeaders := func(headers []string) ([]string, map[int]string, error) {
		s.Logger.Debug("DEBUG: Processing customer headers: %v\n", headers)
		metadataColumns := make(map[int]string) // index -> metadata key
		standardHeaders := make([]string, 0)
		for i, header := range headers {
			// Trim whitespace from header
			header = strings.TrimSpace(header)

			// Remove BOM character if present (only needed for the first header)
			if i == 0 {
				header = strings.TrimPrefix(header, "\ufeff")
			}

			if strings.HasPrefix(header, "metadata.") {
				metadataKey := strings.TrimPrefix(header, "metadata.")
				metadataColumns[i] = metadataKey
				s.Logger.Debug("DEBUG: Found metadata column at index %d: %s -> %s\n", i, header, metadataKey)
			} else {
				standardHeaders = append(standardHeaders, header)
				s.Logger.Debug("DEBUG: Found standard column at index %d: %s\n", i, header)
			}
		}
		s.Logger.Debug("DEBUG: Final standard headers: %v\n", standardHeaders)
		s.Logger.Debug("DEBUG: Final metadata columns: %v\n", metadataColumns)
		return standardHeaders, metadataColumns, nil
	}

	// Define row processor for customers
	processCustomerRow := func(lineNum int, record []string, standardHeaders []string, metadataColumns map[int]string) (bool, error) {
		// Create customer request with standard fields
		customerReq := &dto.CreateCustomerRequest{
			Metadata: make(map[string]string),
		}

		customerID := ""

		// Process customer
		customerSvc := NewCustomerService(s.ServiceParams)

		// Map standard fields
		for i, header := range standardHeaders {
			if i >= len(record) {
				continue
			}
			value := record[i]
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
			case "id":
				customerID = value
			}
		}

		// Parse metadata fields
		if err := s.parseCustomerMetadataFields(record, metadataColumns, customerReq); err != nil {
			s.Logger.Error("failed to parse metadata fields", "line", lineNum, "error", err)
			return false, err
		}

		s.Logger.Debugw("parsed customer request", "customer_req", customerReq)

		// Check if customer with this external ID already exists
		if customerID != "" {
			customer, err := customerSvc.GetCustomer(ctx, customerID)
			if err != nil {
				s.Logger.Error("failed to search for existing customer", "line", lineNum, "external_id", customerReq.ExternalID, "error", err)
				return false, err
			}

			// If customer exists, update it
			if customer != nil && customer.Status == types.StatusPublished {
				existingCustomer := customer

				// Create update request from create request
				updateReq := dto.UpdateCustomerRequest{}

				// Only update external ID if it's different
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

				mergedMetadata := make(map[string]string)
				maps.Copy(mergedMetadata, existingCustomer.Metadata)
				maps.Copy(mergedMetadata, customerReq.Metadata)
				updateReq.Metadata = mergedMetadata

				// Update the customer
				_, err := customerSvc.UpdateCustomer(ctx, existingCustomer.ID, updateReq)
				if err != nil {
					s.Logger.Error("failed to update customer", "line", lineNum, "customer_id", existingCustomer.ID, "error", err)
					return false, err
				}

				s.Logger.Info("updated existing customer", "line", lineNum, "customer_id", existingCustomer.ID, "external_id", customerReq.ExternalID)
				return true, nil
			}
		}

		// If no existing customer found, create a new one
		_, err := customerSvc.CreateCustomer(ctx, *customerReq)
		if err != nil {
			s.Logger.Error("failed to create customer", "line", lineNum, "error", err)
			return false, err
		}

		s.Logger.Info("created new customer", "line", lineNum, "external_id", customerReq.ExternalID)
		return true, nil
	}

	// Process the CSV file with the customer-specific processors
	return s.processCSVFile(
		ctx,
		t,
		tracker,
		validateCustomerRequiredColumns,
		processCustomerHeaders,
		processCustomerRow,
	)
}

func (s *taskService) parsePropertyFields(record []string, propertyColumns map[int]string, eventReq *dto.IngestEventRequest) error {
	for idx, propName := range propertyColumns {
		if idx < len(record) {
			value := record[idx]
			// Try to parse as JSON if it looks like a JSON value
			if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
				var jsonValue interface{}
				if err := json.Unmarshal([]byte(value), &jsonValue); err == nil {
					eventReq.Properties[propName] = jsonValue
					continue
				}
			}
			eventReq.Properties[propName] = value
		}
	}
	return nil
}

func (s *taskService) parseTimestamp(eventReq *dto.IngestEventRequest) error {
	if eventReq.TimestampStr != "" {
		timestamp, err := time.Parse(time.RFC3339, eventReq.TimestampStr)
		if err != nil {
			return ierr.NewError("invalid timestamp format").
				WithHint("Invalid timestamp format").
				WithReportableDetails(map[string]interface{}{
					"error": err,
				}).
				Mark(ierr.ErrValidation)
		}
		eventReq.Timestamp = timestamp
	} else {
		eventReq.Timestamp = time.Now()
	}
	return nil
}

func validateEventsRequiredColumns(headers []string) error {
	requiredColumns := []string{"event_name", "external_customer_id"}
	headerSet := make(map[string]bool)
	for _, header := range headers {
		headerSet[header] = true
	}

	for _, required := range requiredColumns {
		if !headerSet[required] {
			return ierr.NewError(fmt.Sprintf("missing required column: %s", required)).
				WithHint("Missing required column").
				WithReportableDetails(map[string]interface{}{
					"column": required,
				}).
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

func (s *taskService) parseCustomerMetadataFields(record []string, metadataColumns map[int]string, customerReq *dto.CreateCustomerRequest) error {
	for idx, metadataKey := range metadataColumns {
		if idx < len(record) {
			value := record[idx]
			customerReq.Metadata[metadataKey] = value
		}
	}
	return nil
}

func validateCustomerRequiredColumns(headers []string) error {
	requiredColumns := []string{"external_id"}
	headerSet := make(map[string]bool)
	for _, header := range headers {
		// Trim whitespace and convert to lowercase for case-insensitive comparison
		trimmedHeader := strings.TrimSpace(strings.ToLower(header))

		// Remove BOM character if present
		trimmedHeader = strings.TrimPrefix(trimmedHeader, "\ufeff")

		headerSet[trimmedHeader] = true
	}

	for _, required := range requiredColumns {
		required = strings.ToLower(required)
		if !headerSet[required] {
			return ierr.NewError(fmt.Sprintf("missing required column: %s", required)).
				WithHint("Missing required column").
				WithReportableDetails(map[string]interface{}{
					"column": required,
				}).
				Mark(ierr.ErrValidation)
		}
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

// ProgressTracker is a simple implementation of the ProgressTracker interface
type progressTracker struct {
	ctx            context.Context
	task           *task.Task
	lastUpdateTime time.Time
	batchSize      int
	updateInterval time.Duration
	repo           task.Repository
	logger         *logger.Logger
	errors         []string
}

// NewProgressTracker creates a new progress tracker
func newProgressTracker(
	ctx context.Context,
	task *task.Task,
	batchSize int,
	updateInterval time.Duration,
	repo task.Repository,
	logger *logger.Logger,
) task.ProgressTracker {
	return &progressTracker{
		ctx:            ctx,
		task:           task,
		batchSize:      batchSize,
		updateInterval: updateInterval,
		lastUpdateTime: time.Now(),
		repo:           repo,
		logger:         logger,
	}
}

// Increment updates the progress counters
func (t *progressTracker) Increment(success bool, err error) {
	t.task.ProcessedRecords++
	if success {
		t.task.SuccessfulRecords++
	} else {
		t.task.FailedRecords++
		if err != nil && len(t.errors) < 10 {
			t.errors = append(t.errors, err.Error())
		}
	}

	if t.shouldUpdate() {
		t.flush()
	}
}

// Complete ensures any remaining updates are flushed
func (t *progressTracker) Complete() {
	if t.task.ProcessedRecords > 0 {
		t.flush()
	}
}

func (t *progressTracker) shouldUpdate() bool {
	return t.task.ProcessedRecords%t.batchSize == 0 ||
		time.Since(t.lastUpdateTime) >= t.updateInterval
}

func (t *progressTracker) flush() {
	if len(t.errors) > 0 {
		errorSummary := "Last errors: " + t.errors[len(t.errors)-1]
		t.task.ErrorSummary = &errorSummary
	}

	if err := t.repo.Update(t.ctx, t.task); err != nil {
		// Log error but continue processing
		t.logger.Error("failed to update progress", "error", err)
	}

	t.lastUpdateTime = time.Now()
	t.errors = t.errors[:0] // Clear errors after update
}

func extractGoogleDriveFileID(url string) string {
	// Handle different Google Drive URL formats
	patterns := []string{
		`/file/d/([^/]+)`,   // Format: /file/d/{fileId}/
		`id=([^&]+)`,        // Format: ?id={fileId}
		`/d/([^/]+)`,        // Format: /d/{fileId}/
		`/open\?id=([^&]+)`, // Format: /open?id={fileId}
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(url)
		if len(matches) > 1 {
			return matches[1]
		}
	}
	return ""
}
