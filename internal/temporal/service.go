package temporal

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/client"
)

// Service handles Temporal workflow operations
type Service struct {
	client client.Client
	log    *logger.Logger
	cfg    *config.TemporalConfig
}

// NewService creates a new Temporal service
func NewService(cfg *config.TemporalConfig, log *logger.Logger) (*Service, error) {
	c, err := client.NewClient(cfg.GetClientOptions())
	if err != nil {
		return nil, fmt.Errorf("failed to create temporal client: %w", err)
	}

	return &Service{
		client: c,
		log:    log,
		cfg:    cfg,
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

	_, err := s.client.ExecuteWorkflow(ctx, workflowOptions, "CronBillingWorkflow", input)
	if err != nil {
		s.log.Error("Failed to start workflow", "error", err)
		return nil, err
	}

	s.log.Info("Successfully started billing workflow", "workflowID", workflowID)
	return &models.BillingWorkflowResult{
		InvoiceID: workflowID,
		Status:    "scheduled",
	}, nil
}

// Close closes the temporal client
func (s *Service) Close() {
	if s.client != nil {
		s.client.Close()
	}
}
