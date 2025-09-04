package temporal

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
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

	// Extract tenant and environment from context using proper type assertion
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Validate that tenant ID is present (environment ID can be empty for some cases)
	if tenantID == "" || environmentID == "" {
		return nil, ierr.NewError("tenant ID and environment ID are required but not found in context").
			WithHint("Tenant ID and environment ID are required but not found in context").
			Mark(ierr.ErrValidation)
	}

	workflowID := fmt.Sprintf("price-sync-%s-%d", planID, time.Now().Unix())

	workflowOptions := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: s.cfg.TaskQueue,
	}

	we, err := s.client.Client.ExecuteWorkflow(ctx, workflowOptions, string(types.PriceSyncWorkflow), models.PriceSyncWorkflowInput{
		PlanID:        planID,
		TenantID:      tenantID,
		EnvironmentID: environmentID,
	})
	if err != nil {
		return nil, err
	}

	// Wait for workflow completion since this is a direct API call
	var result dto.SyncPlanPricesResponse
	if err := we.Get(ctx, &result); err != nil {
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
