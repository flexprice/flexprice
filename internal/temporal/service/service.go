package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/temporal/worker"
	"github.com/flexprice/flexprice/internal/types"
)

// temporalService implements TemporalService
type temporalService struct {
	client        client.TemporalClient
	workerManager worker.TemporalWorkerManager
	logger        *logger.Logger
}

// NewTemporalService creates a new temporal service instance
func NewTemporalService(client client.TemporalClient, workerManager worker.TemporalWorkerManager, logger *logger.Logger) TemporalService {
	return &temporalService{
		client:        client,
		workerManager: workerManager,
		logger:        logger,
	}
}

// Start implements TemporalService
func (s *temporalService) Start(ctx context.Context) error {
	// Start client
	if err := s.client.Start(ctx); err != nil {
		return fmt.Errorf("failed to start temporal client: %w", err)
	}

	s.logger.Info("Temporal service started successfully")
	return nil
}

// Stop implements TemporalService
func (s *temporalService) Stop(ctx context.Context) error {
	// Stop all workers first
	if err := s.workerManager.StopAllWorkers(); err != nil {
		s.logger.Error("Failed to stop all workers", "error", err)
	}

	// Stop client
	if err := s.client.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop temporal client: %w", err)
	}

	s.logger.Info("Temporal service stopped successfully")
	return nil
}

// IsHealthy implements TemporalService
func (s *temporalService) IsHealthy(ctx context.Context) bool {
	return s.client.IsHealthy(ctx)
}

// StartWorkflow implements TemporalService
func (s *temporalService) StartWorkflow(ctx context.Context, options models.StartWorkflowOptions, workflow types.TemporalWorkflowType, args ...interface{}) (models.WorkflowRun, error) {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return nil, err
	}
	if err := workflow.Validate(); err != nil {
		return nil, errors.WithError(err).
			WithHint("Invalid workflow type provided").
			Mark(errors.ErrValidation)
	}

	return s.client.StartWorkflow(ctx, options, workflow, args...)
}

// SignalWorkflow implements TemporalService
func (s *temporalService) SignalWorkflow(ctx context.Context, workflowID, runID, signalName string, arg interface{}) error {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return err
	}
	if workflowID == "" {
		return errors.NewError("workflow ID is required").
			WithHint("Workflow ID cannot be empty").
			Mark(errors.ErrValidation)
	}
	if signalName == "" {
		return errors.NewError("signal name is required").
			WithHint("Signal name cannot be empty").
			Mark(errors.ErrValidation)
	}

	return s.client.SignalWorkflow(ctx, workflowID, runID, signalName, arg)
}

// QueryWorkflow implements TemporalService
func (s *temporalService) QueryWorkflow(ctx context.Context, workflowID, runID, queryType string, args ...interface{}) (interface{}, error) {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return nil, err
	}
	if workflowID == "" {
		return nil, errors.NewError("workflow ID is required").
			WithHint("Workflow ID cannot be empty").
			Mark(errors.ErrValidation)
	}
	if queryType == "" {
		return nil, errors.NewError("query type is required").
			WithHint("Query type cannot be empty").
			Mark(errors.ErrValidation)
	}

	return s.client.QueryWorkflow(ctx, workflowID, runID, queryType, args...)
}

// CancelWorkflow implements TemporalService
func (s *temporalService) CancelWorkflow(ctx context.Context, workflowID, runID string) error {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return err
	}
	if workflowID == "" {
		return errors.NewError("workflow ID is required").
			WithHint("Workflow ID cannot be empty").
			Mark(errors.ErrValidation)
	}

	return s.client.CancelWorkflow(ctx, workflowID, runID)
}

// TerminateWorkflow implements TemporalService
func (s *temporalService) TerminateWorkflow(ctx context.Context, workflowID, runID, reason string, details ...interface{}) error {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return err
	}
	if workflowID == "" {
		return errors.NewError("workflow ID is required").
			WithHint("Workflow ID cannot be empty").
			Mark(errors.ErrValidation)
	}

	return s.client.TerminateWorkflow(ctx, workflowID, runID, reason, details...)
}

// CompleteActivity implements TemporalService
func (s *temporalService) CompleteActivity(ctx context.Context, taskToken []byte, result interface{}, err error) error {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return err
	}
	if len(taskToken) == 0 {
		return errors.NewError("task token is required").
			WithHint("Task token cannot be empty").
			Mark(errors.ErrValidation)
	}

	return s.client.CompleteActivity(ctx, taskToken, result, err)
}

// RecordActivityHeartbeat implements TemporalService
func (s *temporalService) RecordActivityHeartbeat(ctx context.Context, taskToken []byte, details ...interface{}) error {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return err
	}
	if len(taskToken) == 0 {
		return errors.NewError("task token is required").
			WithHint("Task token cannot be empty").
			Mark(errors.ErrValidation)
	}

	return s.client.RecordActivityHeartbeat(ctx, taskToken, details...)
}

// RegisterWorkflow implements TemporalService
func (s *temporalService) RegisterWorkflow(taskQueue types.TemporalTaskQueue, workflow interface{}) error {
	if err := taskQueue.Validate(); err != nil {
		return errors.WithError(err).
			WithHint("Invalid task queue provided").
			Mark(errors.ErrValidation)
	}
	if workflow == nil {
		return errors.NewError("workflow is required").
			WithHint("Workflow parameter cannot be nil").
			Mark(errors.ErrValidation)
	}

	w, err := s.workerManager.GetOrCreateWorker(taskQueue, models.DefaultWorkerOptions())
	if err != nil {
		return errors.WithError(err).
			WithHint("Failed to create or get worker for task queue").
			Mark(errors.ErrInternal)
	}

	return w.RegisterWorkflow(workflow)
}

// RegisterActivity implements TemporalService
func (s *temporalService) RegisterActivity(taskQueue types.TemporalTaskQueue, activity interface{}) error {
	if err := taskQueue.Validate(); err != nil {
		return errors.WithError(err).
			WithHint("Invalid task queue provided").
			Mark(errors.ErrValidation)
	}
	if activity == nil {
		return errors.NewError("activity is required").
			WithHint("Activity parameter cannot be nil").
			Mark(errors.ErrValidation)
	}

	w, err := s.workerManager.GetOrCreateWorker(taskQueue, models.DefaultWorkerOptions())
	if err != nil {
		return errors.WithError(err).
			WithHint("Failed to create or get worker for task queue").
			Mark(errors.ErrInternal)
	}

	return w.RegisterActivity(activity)
}

// StartWorker implements TemporalService
func (s *temporalService) StartWorker(taskQueue types.TemporalTaskQueue) error {
	if err := taskQueue.Validate(); err != nil {
		return errors.WithError(err).
			WithHint("Invalid task queue provided").
			Mark(errors.ErrValidation)
	}

	return s.workerManager.StartWorker(taskQueue)
}

// StopWorker implements TemporalService
func (s *temporalService) StopWorker(taskQueue types.TemporalTaskQueue) error {
	if err := taskQueue.Validate(); err != nil {
		return errors.WithError(err).
			WithHint("Invalid task queue provided").
			Mark(errors.ErrValidation)
	}

	return s.workerManager.StopWorker(taskQueue)
}

// StopAllWorkers implements TemporalService
func (s *temporalService) StopAllWorkers() error {
	return s.workerManager.StopAllWorkers()
}

// GetWorkflowHistory implements TemporalService
func (s *temporalService) GetWorkflowHistory(ctx context.Context, workflowID, runID string) (interface{}, error) {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return nil, err
	}
	if workflowID == "" {
		return nil, errors.NewError("workflow ID is required").
			WithHint("Workflow ID cannot be empty").
			Mark(errors.ErrValidation)
	}

	return s.client.GetWorkflowHistory(ctx, workflowID, runID)
}

// DescribeWorkflowExecution implements TemporalService
func (s *temporalService) DescribeWorkflowExecution(ctx context.Context, workflowID, runID string) (interface{}, error) {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return nil, err
	}
	if workflowID == "" {
		return nil, errors.NewError("workflow ID is required").
			WithHint("Workflow ID cannot be empty").
			Mark(errors.ErrValidation)
	}

	return s.client.DescribeWorkflowExecution(ctx, workflowID, runID)
}

// validateTenantContext validates that the required tenant context fields are present
func (s *temporalService) validateTenantContext(ctx context.Context) error {
	tc, err := models.FromContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tenant context: %w", err)
	}

	if tc.TenantID == "" {
		return models.ErrInvalidTenantContext
	}

	return nil
}
