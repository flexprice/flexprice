package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/flexprice/flexprice/internal/domain/temporal"
	"github.com/flexprice/flexprice/internal/temporal/workflows"
	"go.temporal.io/sdk/client"
)

type TemporalService struct {
	client client.Client
}

func NewTemporalService(hostPort string) (*TemporalService, error) {
	c, err := client.NewClient(client.Options{
		HostPort: hostPort,
	})
	if err != nil {
		log.Printf("Failed to create Temporal client: %v", err)
		return nil, err
	}

	return &TemporalService{
		client: c,
	}, nil
}

func (s *TemporalService) Close() {
	if s.client != nil {
		s.client.Close()
	}
}

func (s *TemporalService) StartBillingWorkflow(ctx context.Context, customerID, subscriptionID string, periodStart, periodEnd time.Time) (*temporal.BillingWorkflowResult, error) {
	workflowOptions := client.StartWorkflowOptions{
		ID:           fmt.Sprintf("billing-%s-%s-%d", customerID, subscriptionID, time.Now().Unix()),
		TaskQueue:    "default",
		CronSchedule: "*/5 * * * *", // Run every 5 minutes
	}

	input := workflows.BillingWorkflowInput{
		CustomerID:     customerID,
		SubscriptionID: subscriptionID,
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
	}

	we, err := s.client.ExecuteWorkflow(ctx, workflowOptions, workflows.CronBillingWorkflow, input)
	if err != nil {
		log.Printf("Failed to start workflow: %v", err)
		return nil, fmt.Errorf("failed to start workflow: %w", err)
	}

	var result workflows.BillingWorkflowResult
	if err := we.Get(ctx, &result); err != nil {
		log.Printf("Failed to get workflow result: %v", err)
		return nil, fmt.Errorf("failed to get workflow result: %w", err)
	}

	// Convert the result to the domain type
	return &temporal.BillingWorkflowResult{
		InvoiceID: result.InvoiceID,
		Status:    result.Status,
	}, nil
}
