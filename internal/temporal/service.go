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
	client *TemporalClient // Changed to use TemporalClient
	log    *logger.Logger
	cfg    *config.TemporalConfig
}

// NewService creates a new Temporal service
func NewService(client *TemporalClient, cfg *config.TemporalConfig, log *logger.Logger) (*Service, error) {
	return &Service{
		client: client,
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

// Close closes the temporal client
func (s *Service) Close() {
	if s.client != nil {
		s.client.Client.Close()
	}
}
