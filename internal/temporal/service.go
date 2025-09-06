package temporal

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/client"
)

// Service handles Temporal workflow operations
type Service struct {
	client *TemporalClient
	log    *logger.Logger
	cfg    *config.TemporalConfig
	service.ServiceParams
}

// NewService creates a new Temporal service
func NewService(client *TemporalClient, cfg *config.TemporalConfig, log *logger.Logger, params service.ServiceParams) (*Service, error) {
	return &Service{
		client:        client,
		log:           log,
		cfg:           cfg,
		ServiceParams: params,
	}, nil
}

// StartPlanPriceSync starts a price sync workflow for a plan
func (s *Service) StartPlanPriceSync(ctx context.Context, planID string) (*models.TemporalWorkflowResult, error) {
	// Extract tenant and environment from context using proper type assertion
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	workflowID := fmt.Sprintf("price-sync-%s-%d", planID, time.Now().Unix())

	workflowOptions := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: s.cfg.TaskQueue,
	}

	we, err := s.client.Client.ExecuteWorkflow(ctx, workflowOptions, string(types.TemporalPriceSyncWorkflow), models.PriceSyncWorkflowInput{
		PlanID:        planID,
		TenantID:      tenantID,
		EnvironmentID: environmentID,
	})
	if err != nil {
		return nil, err
	}

	return &models.TemporalWorkflowResult{
		WorkflowID: we.GetID(),
		RunID:      we.GetRunID(),
	}, nil
}

// StartTaskProcessingWorkflow starts a task processing workflow
func (s *Service) StartTaskProcessingWorkflow(ctx context.Context, taskID string) (*models.TaskProcessingWorkflowResult, error) {
	// Extract tenant and environment from context
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	workflowID := fmt.Sprintf("task-processing-%s-%d", taskID, time.Now().Unix())

	workflowOptions := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: s.cfg.TaskQueue,
	}

	// Create the workflow input with extracted context values
	input := models.TaskProcessingWorkflowInput{
		TaskID:        taskID,
		TenantID:      tenantID,
		EnvironmentID: environmentID,
	}

	we, err := s.client.Client.ExecuteWorkflow(ctx, workflowOptions, "TaskProcessingWorkflow", input)
	if err != nil {
		s.log.Error("Failed to start task processing workflow", "error", err, "task_id", taskID)
		return nil, err
	}

	s.log.Info("Successfully started task processing workflow",
		"workflowID", workflowID,
		"runID", we.GetRunID(),
		"task_id", taskID)

	return &models.TaskProcessingWorkflowResult{
		TaskID: taskID,
		Status: "started",
		Metadata: map[string]interface{}{
			"workflow_id": we.GetID(),
			"run_id":      we.GetRunID(),
		},
	}, nil
}

// GetTaskProcessingWorkflowResult gets the result of a task processing workflow
func (s *Service) GetTaskProcessingWorkflowResult(ctx context.Context, workflowID string) (*models.TaskProcessingWorkflowResult, error) {
	// Get workflow execution
	we := s.client.Client.GetWorkflow(ctx, workflowID, "")

	var result models.TaskProcessingWorkflowResult
	err := we.Get(ctx, &result)
	if err != nil {
		s.log.Error("Failed to get task processing workflow result", "error", err, "workflow_id", workflowID)
		return nil, err
	}

	return &result, nil
}

// Close closes the temporal client
func (s *Service) Close() {
	if s.client != nil {
		s.client.Client.Close()
	}
}
