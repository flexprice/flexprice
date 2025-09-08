package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/client"
	temporalsdk "go.temporal.io/sdk/temporal"
)

const (
	DefaultExecutionTimeout = time.Hour
	DefaultRetryDelay       = time.Second * 5
	MaxRetryAttempts        = 10
)

// Service provides a centralized interface for all Temporal operations
type Service struct {
	client        *temporal.TemporalClient
	workerManager *WorkerManager
	logger        *logger.Logger
	serviceParams service.ServiceParams
	initialized   bool
	initMux       sync.RWMutex
}

var (
	globalService     *Service
	globalServiceOnce sync.Once
	globalServiceMux  sync.RWMutex
)

// GetService returns the global temporal service instance
func GetService() *Service {
	globalServiceMux.RLock()
	if globalService != nil {
		defer globalServiceMux.RUnlock()
		return globalService
	}
	globalServiceMux.RUnlock()
	return nil
}

// InitService initializes the global temporal service
func InitService(cfg *config.TemporalConfig, log *logger.Logger, params service.ServiceParams) (*Service, error) {
	var err error
	globalServiceOnce.Do(func() {
		// Get or create client
		client, clientErr := GetTemporalClient(cfg, log)
		if clientErr != nil {
			err = ierr.WithError(clientErr).
				WithHint("Failed to initialize temporal client").
				WithReportableDetails(map[string]any{"client_error": clientErr}).
				Mark(ierr.ErrInternal)
			return
		}

		// Initialize worker manager
		workerManager := InitWorkerManager(client, log)

		globalService = &Service{
			client:        client,
			workerManager: workerManager,
			logger:        log,
			serviceParams: params,
			initialized:   true,
		}
	})
	return globalService, err
}

// WorkflowExecutionOptions contains all possible options for workflow execution
type WorkflowExecutionOptions struct {
	// Basic options
	TaskQueue        string
	ExecutionTimeout time.Duration
	WorkflowID       string

	// Retry options
	MaxRetries int
	RetryDelay time.Duration

	// Advanced options
	RetryPolicy  *RetryPolicy
	CronSchedule string
}

// DefaultWorkflowExecutionOptions returns default options for workflow execution
func DefaultWorkflowExecutionOptions() *WorkflowExecutionOptions {
	return &WorkflowExecutionOptions{
		ExecutionTimeout: DefaultExecutionTimeout,
		MaxRetries:       0, // No retries by default
		RetryDelay:       DefaultRetryDelay,
		RetryPolicy:      DefaultRetryPolicy(),
	}
}

// ExecuteWorkflow executes a workflow with comprehensive options
func (s *Service) ExecuteWorkflow(ctx context.Context, workflowName types.TemporalWorkflowType, input interface{}, opts ...*WorkflowExecutionOptions) (client.WorkflowRun, error) {
	if err := s.ValidateServiceState(); err != nil {
		return nil, err
	}

	// Validate workflow name using the type's built-in validation
	if err := workflowName.Validate(); err != nil {
		return nil, err
	}

	// Basic input validation
	if input == nil {
		return nil, ierr.NewError("input is required").
			WithHint("Provide valid input parameters").
			Mark(ierr.ErrValidation)
	}

	// Merge options with defaults
	options := DefaultWorkflowExecutionOptions()
	if len(opts) > 0 && opts[0] != nil {
		options = opts[0]
	}

	// Set default task queue if not provided
	if options.TaskQueue == "" {
		options.TaskQueue = workflowName.TaskQueueName()
	}

	// Validate options
	if options.TaskQueue == "" {
		return nil, ierr.NewError("task queue is required").
			WithHint("Provide a valid task queue name").
			Mark(ierr.ErrValidation)
	}

	if options.ExecutionTimeout <= 0 {
		options.ExecutionTimeout = DefaultExecutionTimeout
	}

	// Handle retry logic if maxRetries > 0
	if options.MaxRetries > 0 {
		return s.executeWorkflowWithRetry(ctx, workflowName, input, options)
	}

	// Execute workflow with options
	return s.executeWorkflowOnce(ctx, workflowName, input, options)
}

// executeWorkflowOnce executes a workflow once without retry logic
func (s *Service) executeWorkflowOnce(ctx context.Context, workflowName types.TemporalWorkflowType, input interface{}, options *WorkflowExecutionOptions) (client.WorkflowRun, error) {
	// Generate workflow ID if not provided
	workflowID := options.WorkflowID
	if workflowID == "" {
		workflowID = s.generateWorkflowID(workflowName, ctx)
	}

	// Build workflow options
	workflowOptions := client.StartWorkflowOptions{
		ID:                 workflowID,
		TaskQueue:          options.TaskQueue,
		WorkflowRunTimeout: options.ExecutionTimeout,
	}

	// Add retry policy if provided
	if options.RetryPolicy != nil {
		workflowOptions.RetryPolicy = &temporalsdk.RetryPolicy{
			InitialInterval:        options.RetryPolicy.InitialInterval,
			BackoffCoefficient:     options.RetryPolicy.BackoffCoefficient,
			MaximumInterval:        options.RetryPolicy.MaximumInterval,
			MaximumAttempts:        options.RetryPolicy.MaximumAttempts,
			NonRetryableErrorTypes: options.RetryPolicy.NonRetryableErrorTypes,
		}
	}

	// Add cron schedule if provided
	if options.CronSchedule != "" {
		workflowOptions.CronSchedule = options.CronSchedule
	}

	// Handle different workflow types internally for special input processing
	processedInput := s.processWorkflowInput(workflowName, input, ctx)

	we, err := s.client.Client.ExecuteWorkflow(ctx, workflowOptions, string(workflowName), processedInput)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to execute workflow").
			WithReportableDetails(map[string]any{"workflow_name": workflowName, "input": input, "options": options}).
			Mark(ierr.ErrInternal)
	}

	s.logger.Info("Successfully started workflow",
		"workflowID", workflowID,
		"runID", we.GetRunID(),
		"workflow_name", workflowName)

	return we, nil
}

// executeWorkflowWithRetry executes a workflow with retry logic
func (s *Service) executeWorkflowWithRetry(ctx context.Context, workflowName types.TemporalWorkflowType, input interface{}, options *WorkflowExecutionOptions) (client.WorkflowRun, error) {
	// Validate retry attempts
	if options.MaxRetries < 0 || options.MaxRetries > MaxRetryAttempts {
		return nil, ierr.NewError("invalid retry attempts").
			WithHint("Retry attempts must be between 0 and 10").
			Mark(ierr.ErrValidation)
	}

	var lastErr error
	for i := 0; i < options.MaxRetries; i++ {
		// Create a copy of options for this attempt
		retryOptions := *options
		retryOptions.WorkflowID = fmt.Sprintf("%s-retry-%d-%d", workflowName, i, time.Now().Unix())

		run, err := s.executeWorkflowOnce(ctx, workflowName, input, &retryOptions)
		if err == nil {
			return run, nil
		}

		lastErr = err
		if i < options.MaxRetries-1 { // Don't sleep after the last attempt
			time.Sleep(time.Duration(i+1) * options.RetryDelay) // Exponential backoff
		}
	}

	return nil, ierr.WithError(lastErr).
		WithHint("Workflow execution failed after retries").
		WithReportableDetails(map[string]any{"workflow_name": workflowName, "input": input, "max_retries": options.MaxRetries}).
		Mark(ierr.ErrInternal)
}

// processWorkflowInput processes input based on workflow type
func (s *Service) processWorkflowInput(workflowName types.TemporalWorkflowType, input interface{}, ctx context.Context) interface{} {
	// Extract tenant and environment from context
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	switch workflowName {
	case types.TemporalTaskProcessingWorkflow:
		// Parse input to get task ID
		if taskID, ok := input.(string); ok {
			return models.TaskProcessingWorkflowInput{
				TaskID:        taskID,
				TenantID:      tenantID,
				EnvironmentID: environmentID,
			}
		}
	case types.TemporalPriceSyncWorkflow:
		// Parse input to get plan ID
		if planID, ok := input.(string); ok {
			return models.PriceSyncWorkflowInput{
				PlanID:        planID,
				TenantID:      tenantID,
				EnvironmentID: environmentID,
			}
		}
	}

	// For other workflow types, return input as-is
	return input
}

// generateWorkflowID generates a unique workflow ID
func (s *Service) generateWorkflowID(workflowName types.TemporalWorkflowType, ctx context.Context) string {
	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		tenantID = "unknown"
	}
	return fmt.Sprintf("%s-%s-%d", workflowName, tenantID, time.Now().Unix())
}

// RegisterWorkflow registers a workflow with a specific task queue
func (s *Service) RegisterWorkflow(taskQueue string, workflow interface{}) error {
	if err := s.ValidateServiceState(); err != nil {
		return err
	}

	if taskQueue == "" {
		return ierr.NewError("task queue is required").
			WithHint("Provide a valid task queue name").
			Mark(ierr.ErrValidation)
	}

	if workflow == nil {
		return ierr.NewError("workflow is required").
			WithHint("Provide a valid workflow function").
			Mark(ierr.ErrValidation)
	}

	err := s.workerManager.RegisterWorkflow(taskQueue, workflow)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to register workflow").
			WithReportableDetails(map[string]any{"task_queue": taskQueue, "workflow": workflow}).
			Mark(ierr.ErrInternal)
	}

	return nil
}

// RegisterActivity registers an activity with a specific task queue
func (s *Service) RegisterActivity(taskQueue string, activity interface{}) error {
	if err := s.ValidateServiceState(); err != nil {
		return err
	}

	if taskQueue == "" {
		return ierr.NewError("task queue is required").
			WithHint("Provide a valid task queue name").
			Mark(ierr.ErrValidation)
	}

	if activity == nil {
		return ierr.NewError("activity is required").
			WithHint("Provide a valid activity function").
			Mark(ierr.ErrValidation)
	}

	err := s.workerManager.RegisterActivity(taskQueue, activity)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to register activity").
			WithReportableDetails(map[string]any{"task_queue": taskQueue, "activity": activity}).
			Mark(ierr.ErrInternal)
	}

	return nil
}

// StartWorker starts a worker for the given task queue
func (s *Service) StartWorker(taskQueue string) error {
	if err := s.ValidateServiceState(); err != nil {
		return err
	}

	if taskQueue == "" {
		return ierr.NewError("task queue is required").
			WithHint("Provide a valid task queue name").
			Mark(ierr.ErrValidation)
	}

	err := s.workerManager.StartWorker(taskQueue)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to start worker").
			WithReportableDetails(map[string]any{"task_queue": taskQueue}).
			Mark(ierr.ErrInternal)
	}

	return nil
}

// StopWorker stops a worker for the given task queue
func (s *Service) StopWorker(taskQueue string) error {
	if err := s.ValidateServiceState(); err != nil {
		return err
	}

	if taskQueue == "" {
		return ierr.NewError("task queue is required").
			WithHint("Provide a valid task queue name").
			Mark(ierr.ErrValidation)
	}

	err := s.workerManager.StopWorker(taskQueue)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to stop worker").
			WithReportableDetails(map[string]any{"task_queue": taskQueue}).
			Mark(ierr.ErrInternal)
	}

	return nil
}

// StopAllWorkers stops all workers
func (s *Service) StopAllWorkers() error {
	if err := s.ValidateServiceState(); err != nil {
		return err
	}

	err := s.workerManager.StopAllWorkers()
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to stop all workers").
			WithReportableDetails(map[string]any{"task_queues": s.workerManager.GetWorkerStatus()}).
			Mark(ierr.ErrInternal)
	}

	return nil
}

// GetWorkerStatus returns the status of all workers
func (s *Service) GetWorkerStatus() map[string]bool {
	if !s.isInitialized() {
		return make(map[string]bool)
	}
	return s.workerManager.GetWorkerStatus()
}

// HealthCheck performs a health check on the temporal system
func (s *Service) HealthCheck() error {
	if err := s.ValidateServiceState(); err != nil {
		return err
	}

	status := s.GetWorkerStatus()
	var unhealthyWorkers []string
	for taskQueue, isStarted := range status {
		if !isStarted {
			unhealthyWorkers = append(unhealthyWorkers, taskQueue)
		}
	}

	if len(unhealthyWorkers) > 0 {
		return ierr.NewError("unhealthy workers detected").
			WithHint("Check worker status and restart if necessary").
			Mark(ierr.ErrSystem)
	}

	return nil
}

// GetWorkflowResult gets the result of a workflow execution
func (s *Service) GetWorkflowResult(ctx context.Context, workflowID string, result interface{}) error {
	if err := s.ValidateServiceState(); err != nil {
		return err
	}

	if workflowID == "" {
		return ierr.NewError("workflow ID is required").
			WithHint("Provide a valid workflow ID").
			Mark(ierr.ErrValidation)
	}

	if result == nil {
		return ierr.NewError("result parameter is required").
			WithHint("Provide a valid result pointer").
			Mark(ierr.ErrValidation)
	}

	we := s.client.Client.GetWorkflow(ctx, workflowID, "")
	err := we.Get(ctx, result)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get workflow result").
			WithReportableDetails(map[string]any{"workflow_id": workflowID, "result": result}).
			Mark(ierr.ErrInternal)
	}

	return nil
}

// GetWorkflowResultWithTimeout gets the result of a workflow execution with timeout
func (s *Service) GetWorkflowResultWithTimeout(ctx context.Context, workflowID string, result interface{}, timeout time.Duration) error {
	if timeout <= 0 {
		return ierr.NewError("timeout must be positive").
			WithHint("Provide a valid timeout duration").
			Mark(ierr.ErrValidation)
	}

	// Create a context with timeout and delegate to the main method
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return s.GetWorkflowResult(timeoutCtx, workflowID, result)
}

// Close closes the temporal service
func (s *Service) Close() error {
	if !s.isInitialized() {
		return nil
	}

	// Stop all workers first
	if err := s.StopAllWorkers(); err != nil {
		s.logger.Error("Failed to stop workers during close", "error", err)
	}

	// Close the client
	if s.client != nil {
		s.client.Client.Close()
	}

	s.initialized = false
	return nil
}

// isInitialized checks if the service is initialized
func (s *Service) isInitialized() bool {
	s.initMux.RLock()
	defer s.initMux.RUnlock()
	return s.initialized
}

// ValidateServiceState validates that the service is properly initialized
func (s *Service) ValidateServiceState() error {
	if !s.isInitialized() {
		return ierr.NewError("temporal service not initialized").
			WithHint("Service must be initialized before use").
			WithReportableDetails(map[string]any{"initialized": s.initialized}).
			Mark(ierr.ErrInternal)
	}
	return nil
}
