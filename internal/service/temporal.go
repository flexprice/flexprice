package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/flexprice/flexprice/internal/domain/temporal"
	"github.com/flexprice/flexprice/internal/temporal/activities"
	"github.com/flexprice/flexprice/internal/temporal/workflows"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

type TemporalService struct {
	client client.Client
	worker worker.Worker
}

func NewTemporalService(address string) (*TemporalService, error) {
	c, err := client.NewClient(client.Options{
		HostPort: address,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create temporal client: %w", err)
	}

	// Create worker
	w := worker.New(c, "billing-task-queue", worker.Options{})

	// Register workflows and activities
	w.RegisterWorkflow(workflows.CronBillingWorkflow)
	w.RegisterWorkflow(workflows.CalculateChargesWorkflow)
	w.RegisterActivity(&activities.BillingActivities{})

	ts := &TemporalService{
		client: c,
		worker: w,
	}

	// Start the worker
	if err := w.Start(); err != nil {
		c.Close()
		return nil, fmt.Errorf("failed to start worker: %w", err)
	}

	log.Printf("Temporal worker started successfully on queue: billing-task-queue")
	return ts, nil
}

func (s *TemporalService) StartBillingWorkflow(ctx context.Context, customerID, subscriptionID string, periodStart, periodEnd time.Time) (*temporal.BillingWorkflowResult, error) {
	workflowID := fmt.Sprintf("billing-%s-%s-%d", customerID, subscriptionID, time.Now().UnixNano())
	workflowOptions := client.StartWorkflowOptions{
		ID:           workflowID,
		TaskQueue:    "billing-task-queue",
		CronSchedule: "*/5 * * * *",
	}

	input := workflows.BillingWorkflowInput{
		CustomerID:     customerID,
		SubscriptionID: subscriptionID,
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
	}

	// Execute the workflow but don't store the workflow execution handle since we don't use it
	_, err := s.client.ExecuteWorkflow(ctx, workflowOptions, workflows.CronBillingWorkflow, input)
	if err != nil {
		return nil, fmt.Errorf("failed to start workflow: %w", err)
	}

	// For cron workflows, we don't want to wait for the result
	return &temporal.BillingWorkflowResult{
		InvoiceID: workflowID,
		Status:    "scheduled",
	}, nil
}

func (s *TemporalService) Close() {
	if s.worker != nil {
		s.worker.Stop()
	}
	if s.client != nil {
		s.client.Close()
	}
}
