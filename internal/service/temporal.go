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
	return &TemporalService{
		client: c,
		log:    log,
		cfg:    cfg,
	}, nil
}

// StartBillingWorkflow starts a billing workflow.
func (s *TemporalService) StartBillingWorkflow(ctx context.Context, input temporal.BillingWorkflowInput) (*temporal.BillingWorkflowResult, error) {
	s.log.Info("Starting billing workflow",
		"customerID", input.CustomerID,
		"subscriptionID", input.SubscriptionID,
	)

	workflowID := fmt.Sprintf("billing-%s-%s", input.CustomerID, input.SubscriptionID)
	workflowOptions := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: s.cfg.TaskQueue,
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
