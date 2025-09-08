package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/temporal/worker"
)

// temporalServiceImpl implements TemporalService
type temporalServiceImpl struct {
	client        client.TemporalClient
	workerManager worker.TemporalWorkerManager
	logger        *logger.Logger
}

// NewTemporalService creates a new temporal service instance
func NewTemporalService(client client.TemporalClient, workerManager worker.TemporalWorkerManager, logger *logger.Logger) TemporalService {
	return &temporalServiceImpl{
		client:        client,
		workerManager: workerManager,
		logger:        logger,
	}
}

// Start implements TemporalService
func (s *temporalServiceImpl) Start(ctx context.Context) error {
	// Validate context
	if err := s.validateTenantContext(ctx); err != nil {
		return err
	}

	// Start client
	if err := s.client.Start(ctx); err != nil {
		return fmt.Errorf("failed to start temporal client: %w", err)
	}

	s.logger.Info("Temporal service started successfully")
	return nil
}

// Stop implements TemporalService
func (s *temporalServiceImpl) Stop(ctx context.Context) error {
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
func (s *temporalServiceImpl) IsHealthy(ctx context.Context) bool {
	return s.client.IsHealthy(ctx)
}

// StartWorkflow implements TemporalService
func (s *temporalServiceImpl) StartWorkflow(ctx context.Context, options models.StartWorkflowOptions, workflow interface{}, args ...interface{}) (models.WorkflowRun, error) {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return nil, err
	}
	if workflow == nil {
		return nil, fmt.Errorf("workflow is required")
	}

	return s.client.StartWorkflow(ctx, options, workflow, args...)
}

// SignalWorkflow implements TemporalService
func (s *temporalServiceImpl) SignalWorkflow(ctx context.Context, workflowID, runID, signalName string, arg interface{}) error {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return err
	}
	if workflowID == "" {
		return fmt.Errorf("workflow ID is required")
	}
	if signalName == "" {
		return fmt.Errorf("signal name is required")
	}

	return s.client.SignalWorkflow(ctx, workflowID, runID, signalName, arg)
}

// QueryWorkflow implements TemporalService
func (s *temporalServiceImpl) QueryWorkflow(ctx context.Context, workflowID, runID, queryType string, args ...interface{}) (interface{}, error) {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return nil, err
	}
	if workflowID == "" {
		return nil, fmt.Errorf("workflow ID is required")
	}
	if queryType == "" {
		return nil, fmt.Errorf("query type is required")
	}

	return s.client.QueryWorkflow(ctx, workflowID, runID, queryType, args...)
}

// CancelWorkflow implements TemporalService
func (s *temporalServiceImpl) CancelWorkflow(ctx context.Context, workflowID, runID string) error {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return err
	}
	if workflowID == "" {
		return fmt.Errorf("workflow ID is required")
	}

	return s.client.CancelWorkflow(ctx, workflowID, runID)
}

// TerminateWorkflow implements TemporalService
func (s *temporalServiceImpl) TerminateWorkflow(ctx context.Context, workflowID, runID, reason string, details ...interface{}) error {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return err
	}
	if workflowID == "" {
		return fmt.Errorf("workflow ID is required")
	}

	return s.client.TerminateWorkflow(ctx, workflowID, runID, reason, details...)
}

// CompleteActivity implements TemporalService
func (s *temporalServiceImpl) CompleteActivity(ctx context.Context, taskToken []byte, result interface{}, err error) error {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return err
	}
	if len(taskToken) == 0 {
		return fmt.Errorf("task token is required")
	}

	return s.client.CompleteActivity(ctx, taskToken, result, err)
}

// RecordActivityHeartbeat implements TemporalService
func (s *temporalServiceImpl) RecordActivityHeartbeat(ctx context.Context, taskToken []byte, details ...interface{}) error {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return err
	}
	if len(taskToken) == 0 {
		return fmt.Errorf("task token is required")
	}

	return s.client.RecordActivityHeartbeat(ctx, taskToken, details...)
}

// RegisterWorkflow implements TemporalService
func (s *temporalServiceImpl) RegisterWorkflow(taskQueue string, workflow interface{}) error {
	if taskQueue == "" {
		return fmt.Errorf("task queue is required")
	}
	if workflow == nil {
		return fmt.Errorf("workflow is required")
	}

	w, err := s.workerManager.GetOrCreateWorker(taskQueue, models.DefaultWorkerOptions())
	if err != nil {
		return err
	}

	return w.RegisterWorkflow(workflow)
}

// RegisterActivity implements TemporalService
func (s *temporalServiceImpl) RegisterActivity(taskQueue string, activity interface{}) error {
	if taskQueue == "" {
		return fmt.Errorf("task queue is required")
	}
	if activity == nil {
		return fmt.Errorf("activity is required")
	}

	w, err := s.workerManager.GetOrCreateWorker(taskQueue, models.DefaultWorkerOptions())
	if err != nil {
		return err
	}

	return w.RegisterActivity(activity)
}

// StartWorker implements TemporalService
func (s *temporalServiceImpl) StartWorker(taskQueue string) error {
	if taskQueue == "" {
		return fmt.Errorf("task queue is required")
	}

	return s.workerManager.StartWorker(taskQueue)
}

// StopWorker implements TemporalService
func (s *temporalServiceImpl) StopWorker(taskQueue string) error {
	if taskQueue == "" {
		return fmt.Errorf("task queue is required")
	}

	return s.workerManager.StopWorker(taskQueue)
}

// StopAllWorkers implements TemporalService
func (s *temporalServiceImpl) StopAllWorkers() error {
	return s.workerManager.StopAllWorkers()
}

// GetWorkflowHistory implements TemporalService
func (s *temporalServiceImpl) GetWorkflowHistory(ctx context.Context, workflowID, runID string) (interface{}, error) {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return nil, err
	}
	if workflowID == "" {
		return nil, fmt.Errorf("workflow ID is required")
	}

	return s.client.GetWorkflowHistory(ctx, workflowID, runID)
}

// DescribeWorkflowExecution implements TemporalService
func (s *temporalServiceImpl) DescribeWorkflowExecution(ctx context.Context, workflowID, runID string) (interface{}, error) {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return nil, err
	}
	if workflowID == "" {
		return nil, fmt.Errorf("workflow ID is required")
	}

	return s.client.DescribeWorkflowExecution(ctx, workflowID, runID)
}

// validateTenantContext validates that the required tenant context fields are present
func (s *temporalServiceImpl) validateTenantContext(ctx context.Context) error {
	tc, err := models.FromContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tenant context: %w", err)
	}

	if tc.TenantID == "" {
		return models.ErrInvalidTenantContext
	}

	return nil
}
