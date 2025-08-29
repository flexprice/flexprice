package temporal

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal/models"
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

// StartBillingWorkflow starts a billing workflow
func (s *Service) StartBillingWorkflow(ctx context.Context, input models.BillingWorkflowInput) (*models.BillingWorkflowResult, error) {
	workflowID := fmt.Sprintf("billing-%s-%s", input.CustomerID, input.SubscriptionID)
	workflowOptions := client.StartWorkflowOptions{
		ID:           workflowID,
		TaskQueue:    s.cfg.TaskQueue,
		CronSchedule: "*/5 * * * *", // Runs every 5 minutes
	}

	we, err := s.client.Client.ExecuteWorkflow(ctx, workflowOptions, "CronBillingWorkflow", input)
	if err != nil {
		s.log.Error("Failed to start workflow", "error", err)
		return nil, err
	}

	// For cron workflows, return immediately with scheduled status
	s.log.Info("Successfully scheduled billing workflow",
		"workflowID", workflowID,
		"runID", we.GetRunID())

	return &models.BillingWorkflowResult{
		InvoiceID: workflowID,
		Status:    "scheduled",
	}, nil
}

// StartPlanPriceSync starts a price sync workflow for a plan
func (s *Service) StartPlanPriceSync(ctx context.Context, planID string) (*dto.SyncPlanPricesResponse, error) {
	workflowID := fmt.Sprintf("price-sync-%s-%d", planID, time.Now().Unix())
	workflowOptions := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: s.cfg.TaskQueue,
	}

	planService := service.NewPlanService(s.ServiceParams)
	we, err := s.client.Client.ExecuteWorkflow(ctx, workflowOptions, "PriceSyncWorkflow", models.PriceSyncWorkflowInput{
		PlanID:       planID,
		PriceService: planService,
	})
	if err != nil {
		s.log.Error("Failed to start price sync workflow", "error", err)
		return nil, err
	}

	// Wait for workflow completion since this is a direct API call
	var result dto.SyncPlanPricesResponse
	if err := we.Get(ctx, &result); err != nil {
		s.log.Error("Workflow execution failed", "error", err)
		return nil, err
	}

	s.log.Info("Successfully completed price sync workflow",
		"workflowID", workflowID,
		"runID", we.GetRunID(),
		"updated", result)

	return &result, nil
}

// Close closes the temporal client
func (s *Service) Close() {
	if s.client != nil {
		s.client.Client.Close()
	}
}
