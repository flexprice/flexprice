package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/client"
)

// TemporalService handles Temporal workflows and worker management.
type TemporalService struct {
	client client.Client
}

// NewTemporalService initializes a Temporal service with the specified configuration.
func NewTemporalService(cfg *config.TemporalConfig) (*TemporalService, error) {
	c, err := client.NewClient(client.Options{
		HostPort: cfg.Address,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create temporal client: %w", err)
	}
	return &TemporalService{
		client: c,
	}, nil
}

// StartBillingWorkflow starts a billing workflow.
func (s *TemporalService) StartBillingWorkflow(ctx context.Context, input models.BillingWorkflowInput) (*models.BillingWorkflowResult, error) {
	workflowID := fmt.Sprintf("billing-%s-%s-%d", input.CustomerID, input.SubscriptionID, time.Now().UnixNano())
	workflowOptions := client.StartWorkflowOptions{
		ID:           workflowID,
		TaskQueue:    "billing-task-queue",
		CronSchedule: "*/5 * * * *", // Example: Runs every 5 minutes.
	}

	_, err := s.client.ExecuteWorkflow(ctx, workflowOptions, "CronBillingWorkflow", input)
	if err != nil {
		return nil, fmt.Errorf("failed to start workflow: %w", err)
	}

	return &models.BillingWorkflowResult{
		InvoiceID: workflowID,
		Status:    "scheduled",
	}, nil
}
