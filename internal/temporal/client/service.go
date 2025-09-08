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

// TemporalService provides a centralized interface for all Temporal operations
type TemporalService struct {
	client        *temporal.TemporalClient
	workerManager *TemporalWorkerManager
	logger        *logger.Logger
	serviceParams service.ServiceParams
	initialized   bool
	initMux       sync.RWMutex
}

var (
	globalService     *TemporalService
	globalServiceOnce sync.Once
	globalServiceMux  sync.RWMutex
)

// GetTemporalService returns the global temporal service instance
func GetTemporalService() *TemporalService {
	globalServiceMux.RLock()
	if globalService != nil {
		defer globalServiceMux.RUnlock()
		return globalService
	}
	globalServiceMux.RUnlock()
	return nil
}

// InitTemporalService initializes the global temporal service
func InitTemporalService(cfg *config.TemporalConfig, log *logger.Logger, params service.ServiceParams) (*TemporalService, error) {
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
		workerManager := NewTemporalWorkerManager(client, log)

		globalService = &TemporalService{
			client:        client,
			workerManager: workerManager,
			logger:        log,
			serviceParams: params,
			initialized:   true,
		}
	})
	return globalService, err
}

// TemporalWorkflowOptions contains options for workflow execution
type TemporalWorkflowOptions struct {
	TaskQueue        string
	ExecutionTimeout time.Duration
	WorkflowID       string
	RetryPolicy      *TemporalRetryPolicy
	CronSchedule     string
}

// ExecuteWorkflow executes a workflow with optional configuration
func (s *TemporalService) ExecuteWorkflow(ctx context.Context, workflowName types.TemporalWorkflowType, input interface{}, opts ...*TemporalWorkflowOptions) (client.WorkflowRun, error) {
	if err := validateService(s.isInitialized()); err != nil {
		return nil, err
	}
	if err := workflowName.Validate(); err != nil {
		return nil, err
	}
	if input == nil {
		return nil, ierr.NewError("workflow input is required").Mark(ierr.ErrValidation)
	}

	// Set up options
	var options *TemporalWorkflowOptions
	if len(opts) > 0 && opts[0] != nil {
		options = opts[0]
	} else {
		options = &TemporalWorkflowOptions{}
	}

	// Set defaults
	if options.TaskQueue == "" {
		options.TaskQueue = workflowName.TaskQueueName()
	}
	if options.ExecutionTimeout <= 0 {
		options.ExecutionTimeout = DefaultExecutionTimeout
	}
	if options.WorkflowID == "" {
		options.WorkflowID = s.generateWorkflowID(workflowName, ctx)
	}

	return s.executeWorkflow(ctx, workflowName, input, options)
}

// executeWorkflow executes a workflow with the given options
func (s *TemporalService) executeWorkflow(ctx context.Context, workflowName types.TemporalWorkflowType, input interface{}, options *TemporalWorkflowOptions) (client.WorkflowRun, error) {
	// Build workflow options
	workflowOptions := client.StartWorkflowOptions{
		ID:                 options.WorkflowID,
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

	// Process input based on workflow type
	processedInput := s.processWorkflowInput(workflowName, input, ctx)

	we, err := s.client.Client.ExecuteWorkflow(ctx, workflowOptions, string(workflowName), processedInput)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to execute temporal workflow - check temporal server connectivity, workflow registration, and task queue configuration").
			WithReportableDetails(map[string]any{
				"workflow_name":     workflowName,
				"workflow_id":       options.WorkflowID,
				"task_queue":        options.TaskQueue,
				"execution_timeout": options.ExecutionTimeout,
			}).
			Mark(ierr.ErrInternal)
	}

	s.logger.Info("Successfully started workflow", "workflowID", options.WorkflowID, "runID", we.GetRunID(), "workflow_name", workflowName)
	return we, nil
}

// processWorkflowInput processes input based on workflow type
func (s *TemporalService) processWorkflowInput(workflowName types.TemporalWorkflowType, input interface{}, ctx context.Context) interface{} {
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
func (s *TemporalService) generateWorkflowID(workflowName types.TemporalWorkflowType, ctx context.Context) string {
	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		tenantID = "unknown"
	}
	return fmt.Sprintf("%s-%s-%d", workflowName, tenantID, time.Now().Unix())
}

// RegisterWorkflow registers a workflow with a specific task queue
func (s *TemporalService) RegisterWorkflow(taskQueue string, workflow interface{}) error {
	if err := validateService(s.isInitialized()); err != nil {
		return err
	}
	if taskQueue == "" {
		return ierr.NewError("task queue is required").Mark(ierr.ErrValidation)
	}
	if workflow == nil {
		return ierr.NewError("workflow function is required").Mark(ierr.ErrValidation)
	}
	return s.workerManager.RegisterWorkflow(taskQueue, workflow)
}

// RegisterActivity registers an activity with a specific task queue
func (s *TemporalService) RegisterActivity(taskQueue string, activity interface{}) error {
	if err := validateService(s.isInitialized()); err != nil {
		return err
	}
	if taskQueue == "" {
		return ierr.NewError("task queue is required").Mark(ierr.ErrValidation)
	}
	if activity == nil {
		return ierr.NewError("activity function is required").Mark(ierr.ErrValidation)
	}
	return s.workerManager.RegisterActivity(taskQueue, activity)
}

// StartWorker starts a worker for the given task queue
func (s *TemporalService) StartWorker(taskQueue string) error {
	if err := validateService(s.isInitialized()); err != nil {
		return err
	}
	if taskQueue == "" {
		return ierr.NewError("task queue is required").Mark(ierr.ErrValidation)
	}
	return s.workerManager.StartWorker(taskQueue)
}

// StopWorker stops a worker for the given task queue
func (s *TemporalService) StopWorker(taskQueue string) error {
	if err := validateService(s.isInitialized()); err != nil {
		return err
	}
	if taskQueue == "" {
		return ierr.NewError("task queue is required").Mark(ierr.ErrValidation)
	}
	return s.workerManager.StopWorker(taskQueue)
}

// StopAllWorkers stops all workers
func (s *TemporalService) StopAllWorkers() error {
	if err := validateService(s.isInitialized()); err != nil {
		return err
	}
	return s.workerManager.StopAllWorkers()
}

// GetWorkerStatus returns the status of all workers
func (s *TemporalService) GetWorkerStatus() map[string]bool {
	if !s.isInitialized() {
		return make(map[string]bool)
	}
	return s.workerManager.GetWorkerStatus()
}

// GetWorkflowResult gets the result of a workflow execution
func (s *TemporalService) GetWorkflowResult(ctx context.Context, workflowID string, result interface{}) error {
	if err := validateService(s.isInitialized()); err != nil {
		return err
	}
	if workflowID == "" {
		return ierr.NewError("workflow ID is required").Mark(ierr.ErrValidation)
	}
	if result == nil {
		return ierr.NewError("result pointer is required").Mark(ierr.ErrValidation)
	}

	we := s.client.Client.GetWorkflow(ctx, workflowID, "")
	err := we.Get(ctx, result)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get workflow result - check if workflow exists and has completed successfully").
			WithReportableDetails(map[string]any{"workflow_id": workflowID}).
			Mark(ierr.ErrInternal)
	}
	return nil
}

// Close closes the temporal service
func (s *TemporalService) Close() error {
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
func (s *TemporalService) isInitialized() bool {
	s.initMux.RLock()
	defer s.initMux.RUnlock()
	return s.initialized
}

// GetWorkerManager returns the worker manager instance
func (s *TemporalService) GetWorkerManager() *TemporalWorkerManager {
	if !s.isInitialized() {
		return nil
	}
	return s.workerManager
}
