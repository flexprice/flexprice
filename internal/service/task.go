package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/publisher"
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
	taskRepo  task.Repository
	eventRepo events.Repository
	meterRepo meter.Repository
	publisher publisher.EventPublisher
	logger    *logger.Logger
	db        postgres.IClient
	client    httpclient.Client
}

func NewTaskService(
	taskRepo task.Repository,
	eventRepo events.Repository,
	meterRepo meter.Repository,
	publisher publisher.EventPublisher,
	db postgres.IClient,
	logger *logger.Logger,
	client httpclient.Client,
) TaskService {
	return &taskService{
		taskRepo:  taskRepo,
		eventRepo: eventRepo,
		meterRepo: meterRepo,
		publisher: publisher,
		logger:    logger,
		db:        db,
		client:    client,
	}
}

func (s *taskService) CreateTask(ctx context.Context, req dto.CreateTaskRequest) (*dto.TaskResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	t := req.ToTask(ctx)
	if err := t.Validate(); err != nil {
		return nil, fmt.Errorf("invalid task: %w", err)
	}

	if err := s.taskRepo.Create(ctx, t); err != nil {
		s.logger.Error("failed to create task", "error", err)
		return nil, fmt.Errorf("creating task: %w", err)
	}

	// Start processing the task in sync for now
	// TODO: Start processing the task in async using temporal
	if err := s.ProcessTask(ctx, t.ID); err != nil {
		s.logger.Error("failed to process task", "error", err)
		return nil, fmt.Errorf("processing task: %w", err)
	}

	return dto.NewTaskResponse(t), nil
}

func (s *taskService) GetTask(ctx context.Context, id string) (*dto.TaskResponse, error) {
	t, err := s.taskRepo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting task: %w", err)
	}

	return dto.NewTaskResponse(t), nil
}

func (s *taskService) ListTasks(ctx context.Context, filter *types.TaskFilter) (*dto.ListTasksResponse, error) {
	tasks, err := s.taskRepo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}

	count, err := s.taskRepo.Count(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("counting tasks: %w", err)
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
	t, err := s.taskRepo.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("getting task: %w", err)
	}

	// Validate status transition
	if !isValidStatusTransition(t.TaskStatus, status) {
		return fmt.Errorf("invalid status transition from %s to %s", t.TaskStatus, status)
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

	if err := s.taskRepo.Update(ctx, t); err != nil {
		return fmt.Errorf("updating task: %w", err)
	}

	return nil
}

func (s *taskService) ProcessTask(ctx context.Context, id string) error {
	// Update status to processing
	if err := s.UpdateTaskStatus(ctx, id, types.TaskStatusProcessing); err != nil {
		return fmt.Errorf("updating task status: %w", err)
	}

	// Refresh task after status update
	t, err := s.taskRepo.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("getting task: %w", err)
	}

	// Create progress tracker with the task object
	tracker := newProgressTracker(ctx, t, 100, 30*time.Second, s.taskRepo, s.logger)

	// Process based on entity type
	var processErr error
	switch t.EntityType {
	case types.EntityTypeEvents:
		processErr = s.processEvents(ctx, t, tracker)
	default:
		processErr = errors.New(errors.ErrCodeInvalidOperation, fmt.Sprintf("unsupported entity type: %s", t.EntityType))
	}

	// Ensure all progress is updated before updating final status
	tracker.Complete()

	// Update final status based on error
	if processErr != nil {
		// Ensure we mark the task as failed
		if err := s.UpdateTaskStatus(ctx, id, types.TaskStatusFailed); err != nil {
			s.logger.Error("failed to update task status", "error", err)
		}
		return fmt.Errorf("processing task: %w", processErr)
	}

	// Only mark as completed if there was no error
	if err := s.UpdateTaskStatus(ctx, id, types.TaskStatusCompleted); err != nil {
		s.logger.Error("failed to update task status", "error", err)
		return fmt.Errorf("updating task status: %w", err)
	}

	return nil
}

func (s *taskService) processEvents(ctx context.Context, t *task.Task, tracker task.ProgressTracker) error {
	// Get the actual download URL
	downloadURL := t.FileURL
	if strings.Contains(downloadURL, "drive.google.com") {
		// Extract file ID from Google Drive URL
		fileID := extractGoogleDriveFileID(downloadURL)
		if fileID == "" {
			return errors.New(errors.ErrCodeValidation, "invalid Google Drive URL")
		}
		downloadURL = fmt.Sprintf("https://drive.google.com/uc?export=download&id=%s", fileID)
		s.logger.Debug("converted Google Drive URL", "original", t.FileURL, "download_url", downloadURL)
	}

	// Download CSV file
	req := &httpclient.Request{
		Method: "GET",
		URL:    downloadURL,
	}

	resp, err := s.client.Send(ctx, req)
	if err != nil {
		s.logger.Error("failed to download file", "error", err, "url", downloadURL)
		errorSummary := fmt.Sprintf("Failed to download file: %v", err)
		t.ErrorSummary = &errorSummary
		return errors.Wrap(err, errors.ErrCodeHTTPClient, "failed to download file")
	}

	if resp.StatusCode != http.StatusOK {
		s.logger.Error("failed to download file", "status_code", resp.StatusCode, "url", downloadURL)
		errorSummary := fmt.Sprintf("Failed to download file: HTTP %d", resp.StatusCode)
		t.ErrorSummary = &errorSummary
		return errors.New(errors.ErrCodeHTTPClient, fmt.Sprintf("failed to download file: %d", resp.StatusCode))
	}

	// Log the first few bytes of the response for debugging
	previewLen := 200
	if len(resp.Body) < previewLen {
		previewLen = len(resp.Body)
	}
	s.logger.Debug("received CSV content preview",
		"preview", string(resp.Body[:previewLen]),
		"content_type", resp.Headers["Content-Type"],
		"content_length", len(resp.Body))

	// First pass: read headers to identify property columns
	reader := csv.NewReader(bytes.NewReader(resp.Body))

	// Configure CSV reader to handle potential issues
	reader.LazyQuotes = true       // Allow lazy quotes
	reader.FieldsPerRecord = -1    // Allow variable number of fields
	reader.ReuseRecord = true      // Reuse record memory
	reader.TrimLeadingSpace = true // Trim leading space

	headers, err := reader.Read()
	if err != nil {
		s.logger.Error("failed to read CSV headers",
			"error", err,
			"content_preview", string(resp.Body[:previewLen]))
		errorSummary := fmt.Sprintf("Failed to read CSV headers: %v", err)
		t.ErrorSummary = &errorSummary
		return errors.Wrap(err, errors.ErrCodeValidation, "failed to read CSV headers")
	}

	s.logger.Debug("parsed CSV headers", "headers", headers)

	// Map property columns and validate standard headers
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

	// Validate required columns
	if err := validateRequiredColumns(standardHeaders); err != nil {
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
			s.logger.Error("failed to read CSV line", "line", lineNum, "error", err)
			errorLines = append(errorLines, fmt.Sprintf("Line %d: Failed to read - %v", lineNum, err))
			tracker.Increment(false, err)
			failureCount++
			continue
		}

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
			s.logger.Error("failed to parse property fields", "line", lineNum, "error", err)
			errorLines = append(errorLines, fmt.Sprintf("Line %d: Failed to parse properties - %v", lineNum, err))
			tracker.Increment(false, err)
			failureCount++
			continue
		}

		// Validate the event request
		if err := eventReq.Validate(); err != nil {
			s.logger.Error("failed to validate event", "line", lineNum, "error", err)
			errorLines = append(errorLines, fmt.Sprintf("Line %d: Validation failed - %v", lineNum, err))
			tracker.Increment(false, err)
			failureCount++
			continue
		}

		// Parse timestamp
		if err := s.parseTimestamp(eventReq); err != nil {
			s.logger.Error("failed to parse timestamp", "line", lineNum, "error", err)
			errorLines = append(errorLines, fmt.Sprintf("Line %d: Invalid timestamp - %v", lineNum, err))
			tracker.Increment(false, err)
			failureCount++
			continue
		}

		// Process event
		eventSvc := NewEventService(
			s.eventRepo,
			s.meterRepo,
			s.publisher,
			s.logger,
		)
		err = eventSvc.CreateEvent(ctx, eventReq)
		if err != nil {
			s.logger.Error("failed to create event", "line", lineNum, "error", err)
			errorLines = append(errorLines, fmt.Sprintf("Line %d: Failed to create event - %v", lineNum, err))
			tracker.Increment(false, err)
			failureCount++
			continue
		}

		tracker.Increment(true, nil)
	}

	// Ensure all progress is updated
	tracker.Complete()

	// Return error if any events failed
	if failureCount > 0 {
		// Create a summary of errors
		// Keep only the first 10 error lines to avoid too long summaries
		maxErrors := 10
		if len(errorLines) > maxErrors {
			errorSummary := fmt.Sprintf("%d events failed to process. First %d errors:\n%s\n(and %d more errors...)",
				failureCount,
				maxErrors,
				strings.Join(errorLines[:maxErrors], "\n"),
				len(errorLines)-maxErrors)
			t.ErrorSummary = &errorSummary
		} else {
			errorSummary := fmt.Sprintf("%d events failed to process:\n%s",
				failureCount,
				strings.Join(errorLines, "\n"))
			t.ErrorSummary = &errorSummary
		}

		// Update the task one final time to ensure error summary is saved
		if err := s.taskRepo.Update(ctx, t); err != nil {
			s.logger.Error("failed to update task with error summary", "error", err)
		}

		return errors.New(errors.ErrCodeValidation, fmt.Sprintf("%d events failed to process", failureCount))
	}

	return nil
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
			return errors.Wrap(err, errors.ErrCodeValidation, "invalid timestamp format")
		}
		eventReq.Timestamp = timestamp
	} else {
		eventReq.Timestamp = time.Now()
	}
	return nil
}

func validateRequiredColumns(headers []string) error {
	requiredColumns := []string{"event_name", "external_customer_id"}
	headerSet := make(map[string]bool)
	for _, header := range headers {
		headerSet[header] = true
	}

	for _, required := range requiredColumns {
		if !headerSet[required] {
			return errors.New(errors.ErrCodeValidation, fmt.Sprintf("missing required column: %s", required))
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
