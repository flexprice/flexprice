package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/temporal"
	"github.com/flexprice/flexprice/internal/logger"
	"go.temporal.io/sdk/client"
)

// TemporalService implements temporal.Service interface
type TemporalService struct {
	client client.Client
	log    *logger.Logger
	cfg    *config.TemporalConfig
}

// Ensure TemporalService implements the interface
var _ temporal.Service = (*TemporalService)(nil)

// NewTemporalService initializes a Temporal service with the specified configuration.
func NewTemporalService(cfg *config.TemporalConfig, log *logger.Logger) (*TemporalService, error) {
	c, err := client.NewClient(client.Options{
		HostPort: cfg.Address,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create temporal client: %w", err)
	}

	service := &TemporalService{
		client: c,
		log:    log,
		cfg:    cfg,
	}

	// Initialize cron workflows
	if err := service.initializeCronWorkflows(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize cron workflows: %w", err)
	}

	return service, nil
}

// initializeCronWorkflows starts all the necessary cron workflows
func (s *TemporalService) initializeCronWorkflows(ctx context.Context) error {
	// Initialize with default values
	input := temporal.BillingWorkflowInput{
		CustomerID:     "system",
		SubscriptionID: "cron-billing",
	}

	workflowID := fmt.Sprintf("billing-%s-%s", input.CustomerID, input.SubscriptionID)

	// Check if workflow already exists
	_, err := s.client.DescribeWorkflowExecution(ctx, workflowID, "")
	if err == nil {
		s.log.Info("Cron workflow already running", "workflowID", workflowID)
		return nil
	}

	// Start the cron workflow if it doesn't exist
	_, err = s.StartBillingWorkflow(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to start billing cron workflow: %w", err)
	}

	s.log.Info("Successfully initialized cron workflow", "workflowID", workflowID)
	return nil
}

// StartBillingWorkflow starts a billing workflow.
func (s *TemporalService) StartBillingWorkflow(ctx context.Context, input temporal.BillingWorkflowInput) (*temporal.BillingWorkflowResult, error) {
	s.log.Info("Starting billing workflow",
		"customerID", input.CustomerID,
		"subscriptionID", input.SubscriptionID,
	)

	workflowID := fmt.Sprintf("billing-%s-%s", input.CustomerID, input.SubscriptionID)
	workflowOptions := client.StartWorkflowOptions{
		ID:           workflowID,
		TaskQueue:    s.cfg.TaskQueue,
		CronSchedule: "*/5 * * * *", // Runs every 5 minutes
	}

	_, err := s.client.ExecuteWorkflow(ctx, workflowOptions, "CronBillingWorkflow", input)
	if err != nil {
		s.log.Error("Failed to start workflow", "error", err)
		return nil, err
	}

	s.log.Info("Successfully started billing workflow", "workflowID", workflowID)
	return &temporal.BillingWorkflowResult{
		InvoiceID: workflowID,
		Status:    "scheduled",
	}, nil
}
