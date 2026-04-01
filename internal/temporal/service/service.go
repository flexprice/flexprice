package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/temporal/client"
	temporalInterceptor "github.com/flexprice/flexprice/internal/temporal/interceptor"
	"github.com/flexprice/flexprice/internal/temporal/models"
	eventsModels "github.com/flexprice/flexprice/internal/temporal/models/events"
	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
	"github.com/flexprice/flexprice/internal/temporal/worker"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/interceptor"
)

var (
	globalTemporalService TemporalService
	globalTemporalOnce    sync.Once
)

// TemporalService provides a centralized interface for all Temporal operations
type temporalService struct {
	client        client.TemporalClient
	workerManager worker.TemporalWorkerManager
	logger        *logger.Logger
	sentry        *sentry.Service
	workerConfig  config.TemporalWorkerConfig
}

// NewTemporalService creates a new temporal service instance
func NewTemporalService(client client.TemporalClient, workerManager worker.TemporalWorkerManager, logger *logger.Logger, sentryService *sentry.Service, cfg *config.TemporalConfig) TemporalService {
	return &temporalService{
		client:        client,
		workerManager: workerManager,
		logger:        logger,
		sentry:        sentryService,
		workerConfig:  cfg.Worker,
	}
}

// InitializeGlobalTemporalService initializes the global Temporal service instance
func InitializeGlobalTemporalService(client client.TemporalClient, workerManager worker.TemporalWorkerManager, logger *logger.Logger, sentryService *sentry.Service, cfg *config.TemporalConfig) {
	globalTemporalOnce.Do(func() {
		globalTemporalService = NewTemporalService(client, workerManager, logger, sentryService, cfg)
	})
}

// GetGlobalTemporalService returns the global Temporal service instance
func GetGlobalTemporalService() TemporalService {
	if globalTemporalService == nil {
		// Return a nil service - the ExecuteWorkflow method will handle this gracefully
		return nil
	}
	return globalTemporalService
}

// GetGlobalTemporalClient returns the underlying Temporal client from the global service instance.
// Useful for operations not exposed on the TemporalService interface (e.g. schedules).
func GetGlobalTemporalClient() client.TemporalClient {
	if globalTemporalService == nil {
		return nil
	}

	// The global service is initialized via NewTemporalService, which returns *temporalService.
	if svc, ok := globalTemporalService.(*temporalService); ok {
		return svc.client
	}

	return nil
}

// Start implements TemporalService
func (s *temporalService) Start(ctx context.Context) error {
	// Start client
	if err := s.client.Start(ctx); err != nil {
		return fmt.Errorf("failed to start temporal client: %w", err)
	}

	s.logger.Info("Temporal service started successfully")
	return nil
}

// Stop implements TemporalService
func (s *temporalService) Stop(ctx context.Context) error {
	// Stop all workers first
	if err := s.workerManager.StopAllWorkers(); err != nil {
		s.logger.Error("Failed to stop all workers", "error", err)
	}

	// Stop client
	if err := s.client.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop temporal client: %w", err)
	}

	s.logger.Info("Temporal service stopped successfully")
	return nil
}

// IsHealthy implements TemporalService
func (s *temporalService) IsHealthy(ctx context.Context) bool {
	return s.client.IsHealthy(ctx)
}

// StartWorkflow implements TemporalService
func (s *temporalService) StartWorkflow(ctx context.Context, options models.StartWorkflowOptions, workflow types.TemporalWorkflowType, args ...interface{}) (models.WorkflowRun, error) {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return nil, err
	}
	if err := workflow.Validate(); err != nil {
		return nil, errors.WithError(err).
			WithHint("Invalid workflow type provided").
			Mark(errors.ErrValidation)
	}

	return s.client.StartWorkflow(ctx, options, workflow, args...)
}

// SignalWorkflow implements TemporalService
func (s *temporalService) SignalWorkflow(ctx context.Context, workflowID, runID, signalName string, arg interface{}) error {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return err
	}
	if workflowID == "" {
		return errors.NewError("workflow ID is required").
			WithHint("Workflow ID cannot be empty").
			Mark(errors.ErrValidation)
	}
	if signalName == "" {
		return errors.NewError("signal name is required").
			WithHint("Signal name cannot be empty").
			Mark(errors.ErrValidation)
	}

	return s.client.SignalWorkflow(ctx, workflowID, runID, signalName, arg)
}

// QueryWorkflow implements TemporalService
func (s *temporalService) QueryWorkflow(ctx context.Context, workflowID, runID, queryType string, args ...interface{}) (interface{}, error) {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return nil, err
	}
	if workflowID == "" {
		return nil, errors.NewError("workflow ID is required").
			WithHint("Workflow ID cannot be empty").
			Mark(errors.ErrValidation)
	}
	if queryType == "" {
		return nil, errors.NewError("query type is required").
			WithHint("Query type cannot be empty").
			Mark(errors.ErrValidation)
	}

	return s.client.QueryWorkflow(ctx, workflowID, runID, queryType, args...)
}

// CancelWorkflow implements TemporalService
func (s *temporalService) CancelWorkflow(ctx context.Context, workflowID, runID string) error {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return err
	}
	if workflowID == "" {
		return errors.NewError("workflow ID is required").
			WithHint("Workflow ID cannot be empty").
			Mark(errors.ErrValidation)
	}

	return s.client.CancelWorkflow(ctx, workflowID, runID)
}

// TerminateWorkflow implements TemporalService
func (s *temporalService) TerminateWorkflow(ctx context.Context, workflowID, runID, reason string, details ...interface{}) error {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return err
	}
	if workflowID == "" {
		return errors.NewError("workflow ID is required").
			WithHint("Workflow ID cannot be empty").
			Mark(errors.ErrValidation)
	}

	return s.client.TerminateWorkflow(ctx, workflowID, runID, reason, details...)
}

// CompleteActivity implements TemporalService
func (s *temporalService) CompleteActivity(ctx context.Context, taskToken []byte, result interface{}, err error) error {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return err
	}
	if len(taskToken) == 0 {
		return errors.NewError("task token is required").
			WithHint("Task token cannot be empty").
			Mark(errors.ErrValidation)
	}

	return s.client.CompleteActivity(ctx, taskToken, result, err)
}

// RecordActivityHeartbeat implements TemporalService
func (s *temporalService) RecordActivityHeartbeat(ctx context.Context, taskToken []byte, details ...interface{}) error {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return err
	}
	if len(taskToken) == 0 {
		return errors.NewError("task token is required").
			WithHint("Task token cannot be empty").
			Mark(errors.ErrValidation)
	}

	return s.client.RecordActivityHeartbeat(ctx, taskToken, details...)
}

// RegisterWorkflow implements TemporalService
func (s *temporalService) RegisterWorkflow(taskQueue types.TemporalTaskQueue, workflow interface{}) error {
	if err := taskQueue.Validate(); err != nil {
		return errors.WithError(err).
			WithHint("Invalid task queue provided").
			Mark(errors.ErrValidation)
	}
	if workflow == nil {
		return errors.NewError("workflow is required").
			WithHint("Workflow parameter cannot be nil").
			Mark(errors.ErrValidation)
	}

	options := s.buildWorkerOptions()

	w, err := s.workerManager.GetOrCreateWorker(taskQueue, options)
	if err != nil {
		return errors.WithError(err).
			WithHint("Failed to create or get worker for task queue").
			Mark(errors.ErrInternal)
	}

	return w.RegisterWorkflow(workflow)
}

// RegisterActivity implements TemporalService
func (s *temporalService) RegisterActivity(taskQueue types.TemporalTaskQueue, activity interface{}) error {
	if err := taskQueue.Validate(); err != nil {
		return errors.WithError(err).
			WithHint("Invalid task queue provided").
			Mark(errors.ErrValidation)
	}
	if activity == nil {
		return errors.NewError("activity is required").
			WithHint("Activity parameter cannot be nil").
			Mark(errors.ErrValidation)
	}

	options := s.buildWorkerOptions()

	w, err := s.workerManager.GetOrCreateWorker(taskQueue, options)
	if err != nil {
		return errors.WithError(err).
			WithHint("Failed to create or get worker for task queue").
			Mark(errors.ErrInternal)
	}

	return w.RegisterActivity(activity)
}

// StartWorker implements TemporalService
func (s *temporalService) StartWorker(taskQueue types.TemporalTaskQueue) error {
	if err := taskQueue.Validate(); err != nil {
		return errors.WithError(err).
			WithHint("Invalid task queue provided").
			Mark(errors.ErrValidation)
	}

	return s.workerManager.StartWorker(taskQueue)
}

// StopWorker implements TemporalService
func (s *temporalService) StopWorker(taskQueue types.TemporalTaskQueue) error {
	if err := taskQueue.Validate(); err != nil {
		return errors.WithError(err).
			WithHint("Invalid task queue provided").
			Mark(errors.ErrValidation)
	}

	return s.workerManager.StopWorker(taskQueue)
}

// buildWorkerOptions creates worker options from config with interceptors
func (s *temporalService) buildWorkerOptions() *models.WorkerOptions {
	options := models.DefaultWorkerOptions()

	// Apply config overrides (0 values mean use defaults)
	if s.workerConfig.MaxConcurrentActivityExecutionSize > 0 {
		options.MaxConcurrentActivityExecutionSize = s.workerConfig.MaxConcurrentActivityExecutionSize
	}
	if s.workerConfig.MaxConcurrentWorkflowTaskExecutionSize > 0 {
		options.MaxConcurrentWorkflowTaskExecutionSize = s.workerConfig.MaxConcurrentWorkflowTaskExecutionSize
	}
	if s.workerConfig.WorkerActivitiesPerSecond > 0 {
		options.WorkerActivitiesPerSecond = s.workerConfig.WorkerActivitiesPerSecond
	}
	if s.workerConfig.TaskQueueActivitiesPerSecond > 0 {
		options.TaskQueueActivitiesPerSecond = s.workerConfig.TaskQueueActivitiesPerSecond
	}

	// Add interceptors
	if s.sentry != nil && s.sentry.IsEnabled() {
		options.Interceptors = []interceptor.WorkerInterceptor{
			temporalInterceptor.NewSentryInterceptor(s.sentry),
			temporalInterceptor.NewWorkflowTrackingInterceptor(),
		}
	} else {
		options.Interceptors = []interceptor.WorkerInterceptor{
			temporalInterceptor.NewWorkflowTrackingInterceptor(),
		}
	}

	return options
}

// StopAllWorkers implements TemporalService
func (s *temporalService) StopAllWorkers() error {
	return s.workerManager.StopAllWorkers()
}

// GetWorkflowHistory implements TemporalService
func (s *temporalService) GetWorkflowHistory(ctx context.Context, workflowID, runID string) (interface{}, error) {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return nil, err
	}
	if workflowID == "" {
		return nil, errors.NewError("workflow ID is required").
			WithHint("Workflow ID cannot be empty").
			Mark(errors.ErrValidation)
	}

	return s.client.GetWorkflowHistory(ctx, workflowID, runID)
}

// DescribeWorkflowExecution implements TemporalService
func (s *temporalService) DescribeWorkflowExecution(ctx context.Context, workflowID, runID string) (interface{}, error) {
	// Validate context and inputs
	if err := s.validateTenantContext(ctx); err != nil {
		return nil, err
	}
	if workflowID == "" {
		return nil, errors.NewError("workflow ID is required").
			WithHint("Workflow ID cannot be empty").
			Mark(errors.ErrValidation)
	}

	return s.client.DescribeWorkflowExecution(ctx, workflowID, runID)
}

// ExecuteWorkflow implements the unified workflow execution method
func (s *temporalService) ExecuteWorkflow(ctx context.Context, workflowType types.TemporalWorkflowType, params interface{}) (models.WorkflowRun, error) {
	// Check if service is initialized
	if s == nil {
		return nil, errors.NewError("temporal service not initialized").
			WithHint("Temporal service must be initialized before use").
			Mark(errors.ErrInternal)
	}

	// Build input with context validation
	input, err := s.buildWorkflowInput(ctx, workflowType, params)
	if err != nil {
		return nil, err
	}

	workflowID := s.generateWorkflowID(workflowType, input)
	options := models.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: workflowType.TaskQueueName(),
	}

	// Execute workflow using existing StartWorkflow method
	return s.StartWorkflow(ctx, options, workflowType, input)
}

func (s *temporalService) ExecuteWorkflowWithDelay(ctx context.Context, workflowType types.TemporalWorkflowType, params interface{}, delaySeconds int) (models.WorkflowRun, error) {
	// Check if service is initialized
	if s == nil {
		return nil, errors.NewError("temporal service not initialized").
			WithHint("Temporal service must be initialized before use").
			Mark(errors.ErrInternal)
	}

	// Build input with context validation
	input, err := s.buildWorkflowInput(ctx, workflowType, params)
	if err != nil {
		return nil, err
	}

	workflowID := s.generateWorkflowID(workflowType, input)
	options := models.StartWorkflowOptions{
		ID:         workflowID,
		TaskQueue:  workflowType.TaskQueueName(),
		StartDelay: time.Duration(delaySeconds) * time.Second,
	}

	// Execute workflow using existing StartWorkflow method
	return s.StartWorkflow(ctx, options, workflowType, input)
}

func (s *temporalService) generateWorkflowID(workflowType types.TemporalWorkflowType, params interface{}) string {
	contextID := s.extractWorkflowContextID(workflowType, params)
	if contextID != "" {
		return types.GenerateWorkflowIDWithContext(workflowType.String(), contextID)
	}
	return types.GenerateWorkflowIDForType(workflowType.String())
}

// extractWorkflowContextID extracts the context ID (e.g., subscription_id, invoice_id, event_id) from params
// for deterministic workflow ID generation. Returns empty string if no context ID is applicable.
func (s *temporalService) extractWorkflowContextID(workflowType types.TemporalWorkflowType, params interface{}) string {
	switch workflowType {
	case types.TemporalProcessSubscriptionBillingWorkflow:
		// Extract subscription ID from ProcessSubscriptionBillingWorkflowInput
		if input, ok := params.(subscriptionModels.ProcessSubscriptionBillingWorkflowInput); ok {
			return input.SubscriptionID
		}
	case types.TemporalProcessInvoiceWorkflow, types.TemporalFinalizeDraftInvoiceWorkflow:
		// Extract invoice ID from ProcessInvoiceWorkflowInput
		if input, ok := params.(invoiceModels.ProcessInvoiceWorkflowInput); ok {
			return input.InvoiceID
		}

	// Vendor invoice sync workflows — deterministic IDs prevent duplicate concurrent syncs
	// for the same invoice when the same Kafka message is consumed by multiple consumers.
	case types.TemporalStripeInvoiceSyncWorkflow:
		if input, ok := params.(models.StripeInvoiceSyncWorkflowInput); ok {
			return input.InvoiceID
		}
	case types.TemporalRazorpayInvoiceSyncWorkflow:
		if input, ok := params.(models.RazorpayInvoiceSyncWorkflowInput); ok {
			return input.InvoiceID
		}
	case types.TemporalChargebeeInvoiceSyncWorkflow:
		if input, ok := params.(models.ChargebeeInvoiceSyncWorkflowInput); ok {
			return input.InvoiceID
		}
	case types.TemporalQuickBooksInvoiceSyncWorkflow:
		if input, ok := params.(models.QuickBooksInvoiceSyncWorkflowInput); ok {
			return input.InvoiceID
		}
	case types.TemporalHubSpotInvoiceSyncWorkflow:
		if input, ok := params.(models.HubSpotInvoiceSyncWorkflowInput); ok {
			return input.InvoiceID
		}
	case types.TemporalMoyasarInvoiceSyncWorkflow:
		if input, ok := params.(models.MoyasarInvoiceSyncWorkflowInput); ok {
			return input.InvoiceID
		}
	case types.TemporalNomodInvoiceSyncWorkflow:
		if input, ok := params.(models.NomodInvoiceSyncWorkflowInput); ok {
			return input.InvoiceID
		}
	case types.TemporalPaddleInvoiceSyncWorkflow:
		if input, ok := params.(models.PaddleInvoiceSyncWorkflowInput); ok {
			return input.InvoiceID
		}

	// Vendor customer sync workflows — deterministic IDs prevent duplicate concurrent syncs.
	case types.TemporalStripeCustomerSyncWorkflow:
		if input, ok := params.(models.StripeCustomerSyncWorkflowInput); ok {
			return input.CustomerID
		}
	case types.TemporalRazorpayCustomerSyncWorkflow:
		if input, ok := params.(models.RazorpayCustomerSyncWorkflowInput); ok {
			return input.CustomerID
		}
	case types.TemporalChargebeeCustomerSyncWorkflow:
		if input, ok := params.(models.ChargebeeCustomerSyncWorkflowInput); ok {
			return input.CustomerID
		}
	case types.TemporalQuickBooksCustomerSyncWorkflow:
		if input, ok := params.(models.QuickBooksCustomerSyncWorkflowInput); ok {
			return input.CustomerID
		}
	case types.TemporalNomodCustomerSyncWorkflow:
		if input, ok := params.(models.NomodCustomerSyncWorkflowInput); ok {
			return input.CustomerID
		}
	case types.TemporalPaddleCustomerSyncWorkflow:
		if input, ok := params.(models.PaddleCustomerSyncWorkflowInput); ok {
			return input.CustomerID
		}
	case types.TemporalRecalculateInvoiceWorkflow:
		// Extract invoice ID from RecalculateInvoiceWorkflowInput
		if input, ok := params.(invoiceModels.RecalculateInvoiceWorkflowInput); ok {
			return input.InvoiceID
		}
	case types.TemporalComputeInvoiceWorkflow:
		// Extract invoice ID from ComputeInvoiceWorkflowInput
		if input, ok := params.(invoiceModels.ComputeInvoiceWorkflowInput); ok {
			return input.InvoiceID
		}
	case types.TemporalDraftAndComputeSubscriptionInvoiceWorkflow:
		if input, ok := params.(invoiceModels.DraftAndComputeSubscriptionInvoiceWorkflowInput); ok {
			return input.SubscriptionID
		}
	case types.TemporalPrepareProcessedEventsWorkflow:
		// Extract event ID from PrepareProcessedEventsWorkflowInput
		if input, ok := params.(*models.PrepareProcessedEventsWorkflowInput); ok {
			return input.EventID
		}
		if input, ok := params.(models.PrepareProcessedEventsWorkflowInput); ok {
			return input.EventID
		}
	case types.TemporalReprocessEventsWorkflow:
		// Extract context ID from ReprocessEventsWorkflowInput
		// Format: external_customer_id-event_name (if event_name provided) or just external_customer_id
		if input, ok := params.(eventsModels.ReprocessEventsWorkflowInput); ok {
			if input.ExternalCustomerID != "" {
				if input.EventName != "" {
					return fmt.Sprintf("%s-%s", input.ExternalCustomerID, input.EventName)
				}
				return input.ExternalCustomerID
			}
		}
		// Also handle map input for reprocess events
		if paramsMap, ok := params.(map[string]interface{}); ok {
			externalCustomerID, _ := paramsMap["external_customer_id"].(string)
			eventName, _ := paramsMap["event_name"].(string)
			if externalCustomerID != "" {
				if eventName != "" {
					return fmt.Sprintf("%s-%s", externalCustomerID, eventName)
				}
				return externalCustomerID
			}
		}
	case types.TemporalReprocessRawEventsWorkflow:
		// Extract context ID from ReprocessRawEventsWorkflowInput
		if input, ok := params.(eventsModels.ReprocessRawEventsWorkflowInput); ok {
			if len(input.ExternalCustomerIDs) > 0 {
				if len(input.EventNames) > 0 {
					return fmt.Sprintf("%s-%s", input.ExternalCustomerIDs[0], input.EventNames[0])
				}
				return input.ExternalCustomerIDs[0]
			}
		}
		// Also handle map input for reprocess raw events
		if paramsMap, ok := params.(map[string]interface{}); ok {
			externalCustomerIDs, _ := paramsMap["external_customer_ids"].([]string)
			eventNames, _ := paramsMap["event_names"].([]string)
			if len(externalCustomerIDs) > 0 {
				if len(eventNames) > 0 {
					return fmt.Sprintf("%s-%s", externalCustomerIDs[0], eventNames[0])
				}
				return externalCustomerIDs[0]
			}
		}
	}
	return ""
}

// buildWorkflowInput builds the appropriate input for the workflow type
func (s *temporalService) buildWorkflowInput(ctx context.Context, workflowType types.TemporalWorkflowType, params interface{}) (interface{}, error) {
	// Validate context and workflow type
	if err := s.validateTenantContext(ctx); err != nil {
		return nil, err
	}

	if err := workflowType.Validate(); err != nil {
		return nil, errors.WithError(err).
			WithHint("Invalid workflow type provided").
			Mark(errors.ErrValidation)
	}

	// Extract context values
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	userID := types.GetUserID(ctx)

	// Handle different workflow types
	switch workflowType {
	case types.TemporalPriceSyncWorkflow:
		return s.buildPriceSyncInput(ctx, tenantID, environmentID, userID, params)
	case types.TemporalQuickBooksPriceSyncWorkflow:
		return s.buildQuickBooksPriceSyncInput(ctx, tenantID, environmentID, userID, params)
	case types.TemporalTaskProcessingWorkflow:
		return s.buildTaskProcessingInput(ctx, tenantID, environmentID, userID, params)
	case types.TemporalHubSpotDealSyncWorkflow:
		return s.buildHubSpotDealSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalHubSpotInvoiceSyncWorkflow:
		return s.buildHubSpotInvoiceSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalScheduleSubscriptionBillingWorkflow:
		return s.buildScheduleSubscriptionBillingWorkflowInput(ctx, tenantID, environmentID, userID, params)
	case types.TemporalProcessSubscriptionBillingWorkflow:
		return s.buildProcessSubscriptionBillingWorkflowInput(ctx, tenantID, environmentID, userID, params)
	case types.TemporalHubSpotQuoteSyncWorkflow:
		return s.buildHubSpotQuoteSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalNomodInvoiceSyncWorkflow:
		return s.buildNomodInvoiceSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalMoyasarInvoiceSyncWorkflow:
		return s.buildMoyasarInvoiceSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalPaddleInvoiceSyncWorkflow:
		return s.buildPaddleInvoiceSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalStripeInvoiceSyncWorkflow:
		return s.buildStripeInvoiceSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalRazorpayInvoiceSyncWorkflow:
		return s.buildRazorpayInvoiceSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalChargebeeInvoiceSyncWorkflow:
		return s.buildChargebeeInvoiceSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalQuickBooksInvoiceSyncWorkflow:
		return s.buildQuickBooksInvoiceSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalStripeCustomerSyncWorkflow:
		return s.buildStripeCustomerSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalRazorpayCustomerSyncWorkflow:
		return s.buildRazorpayCustomerSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalChargebeeCustomerSyncWorkflow:
		return s.buildChargebeeCustomerSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalQuickBooksCustomerSyncWorkflow:
		return s.buildQuickBooksCustomerSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalNomodCustomerSyncWorkflow:
		return s.buildNomodCustomerSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalPaddleCustomerSyncWorkflow:
		return s.buildPaddleCustomerSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalCustomerOnboardingWorkflow:
		return s.buildCustomerOnboardingInput(ctx, tenantID, environmentID, userID, params)
	case types.TemporalPrepareProcessedEventsWorkflow:
		return s.buildPrepareProcessedEventsInput(ctx, tenantID, environmentID, userID, params)
	case types.TemporalProcessInvoiceWorkflow, types.TemporalFinalizeDraftInvoiceWorkflow:
		return s.buildProcessInvoiceInput(ctx, tenantID, environmentID, params)
	case types.TemporalRecalculateInvoiceWorkflow:
		return s.buildRecalculateInvoiceInput(ctx, tenantID, environmentID, userID, params)
	case types.TemporalComputeInvoiceWorkflow:
		return s.buildComputeInvoiceInput(ctx, tenantID, environmentID, userID, params)
	case types.TemporalDraftAndComputeSubscriptionInvoiceWorkflow:
		return s.buildDraftAndComputeSubscriptionInvoiceInput(ctx, tenantID, environmentID, userID, params)
	case types.TemporalReprocessEventsWorkflow:
		return s.buildReprocessEventsInput(ctx, tenantID, environmentID, userID, params)
	case types.TemporalReprocessRawEventsWorkflow:
		return s.buildReprocessRawEventsInput(ctx, tenantID, environmentID, userID, params)
	case types.TemporalReprocessEventsForPlanWorkflow:
		return s.buildReprocessEventsForPlanInput(ctx, tenantID, environmentID, userID, params)
	default:
		return nil, errors.NewError("unsupported workflow type").
			WithHintf("Workflow type %s is not supported", workflowType.String()).
			Mark(errors.ErrValidation)
	}
}

// buildPriceSyncInput builds input for price sync workflow
func (s *temporalService) buildPriceSyncInput(_ context.Context, tenantID, environmentID, userID string, params interface{}) (interface{}, error) {
	// If already correct type, just ensure context is set
	if input, ok := params.(models.PriceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		input.UserID = userID
		return input, nil
	}

	// Handle string input (plan ID)
	planID, ok := params.(string)
	if !ok || planID == "" {
		return nil, errors.NewError("plan ID is required").
			WithHint("Provide plan ID as string or PriceSyncWorkflowInput").
			Mark(errors.ErrValidation)
	}

	return models.PriceSyncWorkflowInput{
		PlanID:        planID,
		TenantID:      tenantID,
		EnvironmentID: environmentID,
		UserID:        userID,
	}, nil
}

// buildQuickBooksPriceSyncInput builds input for QuickBooks price sync workflow
func (s *temporalService) buildQuickBooksPriceSyncInput(_ context.Context, tenantID, environmentID, userID string, params interface{}) (interface{}, error) {
	// If already correct type, just ensure context is set
	if input, ok := params.(models.QuickBooksPriceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		input.UserID = userID
		return input, nil
	}

	// Handle map input with price_id and plan_id
	if paramsMap, ok := params.(map[string]interface{}); ok {
		priceID, _ := paramsMap["price_id"].(string)
		planID, _ := paramsMap["plan_id"].(string)

		if priceID == "" || planID == "" {
			return nil, errors.NewError("price ID and plan ID are required").
				WithHint("Provide map with price_id and plan_id").
				Mark(errors.ErrValidation)
		}

		return models.QuickBooksPriceSyncWorkflowInput{
			PriceID:       priceID,
			PlanID:        planID,
			TenantID:      tenantID,
			EnvironmentID: environmentID,
			UserID:        userID,
		}, nil
	}

	return nil, errors.NewError("invalid input for QuickBooks price sync").
		WithHint("Provide QuickBooksPriceSyncWorkflowInput or map with price_id and plan_id").
		Mark(errors.ErrValidation)
}

// buildTaskProcessingInput builds input for task processing workflow
func (s *temporalService) buildTaskProcessingInput(ctx context.Context, tenantID, environmentID, userID string, params interface{}) (interface{}, error) {
	// If already correct type, just ensure context is set
	if input, ok := params.(models.TaskProcessingWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		input.UserID = userID
		return input, nil
	}

	// Handle string input (task ID)
	taskID, ok := params.(string)
	if !ok || taskID == "" {
		return nil, errors.NewError("task ID is required").
			WithHint("Provide task ID as string or TaskProcessingWorkflowInput").
			Mark(errors.ErrValidation)
	}

	return models.TaskProcessingWorkflowInput{
		TaskID:        taskID,
		TenantID:      tenantID,
		EnvironmentID: environmentID,
		UserID:        userID,
	}, nil
}

// buildHubSpotDealSyncInput builds input for HubSpot deal sync workflow
func (s *temporalService) buildHubSpotDealSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	// If already correct type, just ensure context is set
	if input, ok := params.(*models.HubSpotDealSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}

	// Handle value type as well
	if input, ok := params.(models.HubSpotDealSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}

	return nil, errors.NewError("invalid input for HubSpot deal sync workflow").
		WithHint("Provide HubSpotDealSyncWorkflowInput with subscription_id").
		Mark(errors.ErrValidation)
}

// buildHubSpotInvoiceSyncInput builds input for HubSpot invoice sync workflow
func (s *temporalService) buildHubSpotInvoiceSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	// If already correct type, just ensure context is set
	if input, ok := params.(*models.HubSpotInvoiceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}

	// Handle value type as well
	if input, ok := params.(models.HubSpotInvoiceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}

	return nil, errors.NewError("invalid input for HubSpot invoice sync workflow").
		WithHint("Provide HubSpotInvoiceSyncWorkflowInput with invoice_id and customer_id").
		Mark(errors.ErrValidation)
}

// buildHubSpotQuoteSyncInput builds input for HubSpot quote sync workflow
func (s *temporalService) buildHubSpotQuoteSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	// If already correct type, just ensure context is set
	if input, ok := params.(*models.HubSpotQuoteSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}

	// Handle value type as well
	if input, ok := params.(models.HubSpotQuoteSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}

	return nil, errors.NewError("invalid input for HubSpot quote sync workflow").
		WithHint("Provide HubSpotQuoteSyncWorkflowInput with subscription_id").
		Mark(errors.ErrValidation)
}

// buildCustomerOnboardingInput builds input for customer onboarding workflow
func (s *temporalService) buildCustomerOnboardingInput(_ context.Context, tenantID, environmentID, userID string, params interface{}) (interface{}, error) {
	// If already correct type, just ensure context is set
	if input, ok := params.(*models.CustomerOnboardingWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		input.UserID = userID
		return *input, nil
	}

	// Handle value type as well
	if input, ok := params.(models.CustomerOnboardingWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		input.UserID = userID
		return input, nil
	}

	return nil, errors.NewError("invalid input for customer onboarding workflow").
		WithHint("Provide CustomerOnboardingWorkflowInput with customer_id and workflow_config").
		Mark(errors.ErrValidation)
}

func (s *temporalService) buildNomodInvoiceSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	// If already correct type, just ensure context is set
	if input, ok := params.(*models.NomodInvoiceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}

	// Handle value type as well
	if input, ok := params.(models.NomodInvoiceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}

	return nil, errors.NewError("invalid input for Nomod invoice sync workflow").
		WithHint("Provide NomodInvoiceSyncWorkflowInput with invoice_id and customer_id").
		Mark(errors.ErrValidation)
}

func (s *temporalService) buildMoyasarInvoiceSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	// If already correct type, just ensure context is set
	if input, ok := params.(*models.MoyasarInvoiceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}

	// Handle value type as well
	if input, ok := params.(models.MoyasarInvoiceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}

	return nil, errors.NewError("invalid input for Moyasar invoice sync workflow").
		WithHint("Provide MoyasarInvoiceSyncWorkflowInput with invoice_id and customer_id").
		Mark(errors.ErrValidation)
}

func (s *temporalService) buildPaddleInvoiceSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	// If already correct type, just ensure context is set
	if input, ok := params.(*models.PaddleInvoiceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}

	// Handle value type as well
	if input, ok := params.(models.PaddleInvoiceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}

	return nil, errors.NewError("invalid input for Paddle invoice sync workflow").
		WithHint("Provide PaddleInvoiceSyncWorkflowInput with invoice_id and customer_id").
		Mark(errors.ErrValidation)
}

func (s *temporalService) buildStripeInvoiceSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	if input, ok := params.(*models.StripeInvoiceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}

	if input, ok := params.(models.StripeInvoiceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}

	return nil, errors.NewError("invalid input for Stripe invoice sync workflow").
		WithHint("Provide StripeInvoiceSyncWorkflowInput with invoice_id and customer_id").
		Mark(errors.ErrValidation)
}

func (s *temporalService) buildRazorpayInvoiceSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	if input, ok := params.(*models.RazorpayInvoiceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}

	if input, ok := params.(models.RazorpayInvoiceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}

	return nil, errors.NewError("invalid input for Razorpay invoice sync workflow").
		WithHint("Provide RazorpayInvoiceSyncWorkflowInput with invoice_id and customer_id").
		Mark(errors.ErrValidation)
}

func (s *temporalService) buildChargebeeInvoiceSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	if input, ok := params.(*models.ChargebeeInvoiceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}

	if input, ok := params.(models.ChargebeeInvoiceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}

	return nil, errors.NewError("invalid input for Chargebee invoice sync workflow").
		WithHint("Provide ChargebeeInvoiceSyncWorkflowInput with invoice_id and customer_id").
		Mark(errors.ErrValidation)
}

func (s *temporalService) buildQuickBooksInvoiceSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	if input, ok := params.(*models.QuickBooksInvoiceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}

	if input, ok := params.(models.QuickBooksInvoiceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}

	return nil, errors.NewError("invalid input for QuickBooks invoice sync workflow").
		WithHint("Provide QuickBooksInvoiceSyncWorkflowInput with invoice_id and customer_id").
		Mark(errors.ErrValidation)
}

func (s *temporalService) buildStripeCustomerSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	if input, ok := params.(*models.StripeCustomerSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}
	if input, ok := params.(models.StripeCustomerSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}
	return nil, errors.NewError("invalid input for Stripe customer sync workflow").
		WithHint("Provide StripeCustomerSyncWorkflowInput with customer_id").
		Mark(errors.ErrValidation)
}

func (s *temporalService) buildRazorpayCustomerSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	if input, ok := params.(*models.RazorpayCustomerSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}
	if input, ok := params.(models.RazorpayCustomerSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}
	return nil, errors.NewError("invalid input for Razorpay customer sync workflow").
		WithHint("Provide RazorpayCustomerSyncWorkflowInput with customer_id").
		Mark(errors.ErrValidation)
}

func (s *temporalService) buildChargebeeCustomerSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	if input, ok := params.(*models.ChargebeeCustomerSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}
	if input, ok := params.(models.ChargebeeCustomerSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}
	return nil, errors.NewError("invalid input for Chargebee customer sync workflow").
		WithHint("Provide ChargebeeCustomerSyncWorkflowInput with customer_id").
		Mark(errors.ErrValidation)
}

func (s *temporalService) buildQuickBooksCustomerSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	if input, ok := params.(*models.QuickBooksCustomerSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}
	if input, ok := params.(models.QuickBooksCustomerSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}
	return nil, errors.NewError("invalid input for QuickBooks customer sync workflow").
		WithHint("Provide QuickBooksCustomerSyncWorkflowInput with customer_id").
		Mark(errors.ErrValidation)
}

func (s *temporalService) buildNomodCustomerSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	if input, ok := params.(*models.NomodCustomerSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}
	if input, ok := params.(models.NomodCustomerSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}
	return nil, errors.NewError("invalid input for Nomod customer sync workflow").
		WithHint("Provide NomodCustomerSyncWorkflowInput with customer_id").
		Mark(errors.ErrValidation)
}

func (s *temporalService) buildPaddleCustomerSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	if input, ok := params.(*models.PaddleCustomerSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}
	if input, ok := params.(models.PaddleCustomerSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}
	return nil, errors.NewError("invalid input for Paddle customer sync workflow").
		WithHint("Provide PaddleCustomerSyncWorkflowInput with customer_id").
		Mark(errors.ErrValidation)
}

// buildProcessInvoiceInput builds input for process invoice workflow
func (s *temporalService) buildProcessInvoiceInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	// If already correct type, just ensure context is set
	if input, ok := params.(invoiceModels.ProcessInvoiceWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}

	// Handle pointer type as well
	if input, ok := params.(*invoiceModels.ProcessInvoiceWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}

	// Handle string input (invoice ID)
	invoiceID, ok := params.(string)
	if ok && invoiceID != "" {
		return invoiceModels.ProcessInvoiceWorkflowInput{
			InvoiceID:     invoiceID,
			TenantID:      tenantID,
			EnvironmentID: environmentID,
		}, nil
	}

	return nil, errors.NewError("invalid input for process invoice workflow").
		WithHint("Provide ProcessInvoiceWorkflowInput with invoice_id, tenant_id, and environment_id").
		Mark(errors.ErrValidation)
}

// buildRecalculateInvoiceInput builds input for recalculate invoice (voided) workflow
func (s *temporalService) buildRecalculateInvoiceInput(_ context.Context, tenantID, environmentID, userID string, params interface{}) (interface{}, error) {
	if input, ok := params.(invoiceModels.RecalculateInvoiceWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		input.UserID = userID
		return input, nil
	}
	if input, ok := params.(*invoiceModels.RecalculateInvoiceWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		input.UserID = userID
		return *input, nil
	}
	// Handle string input (invoice ID)
	if invoiceID, ok := params.(string); ok && invoiceID != "" {
		return invoiceModels.RecalculateInvoiceWorkflowInput{
			InvoiceID:     invoiceID,
			TenantID:      tenantID,
			EnvironmentID: environmentID,
			UserID:        userID,
		}, nil
	}
	// Handle map input (e.g. from API with invoice_id)
	if paramsMap, ok := params.(map[string]interface{}); ok {
		invoiceID, _ := paramsMap["invoice_id"].(string)
		if invoiceID == "" {
			return nil, errors.NewError("invoice_id is required").
				WithHint("Provide invoice_id in params").
				Mark(errors.ErrValidation)
		}
		return invoiceModels.RecalculateInvoiceWorkflowInput{
			InvoiceID:     invoiceID,
			TenantID:      tenantID,
			EnvironmentID: environmentID,
			UserID:        userID,
		}, nil
	}
	return nil, errors.NewError("invalid input for recalculate invoice workflow").
		WithHint("Provide RecalculateInvoiceWorkflowInput with invoice_id, or invoice_id string, or map with invoice_id").
		Mark(errors.ErrValidation)
}

// buildComputeInvoiceInput builds input for compute invoice workflow
func (s *temporalService) buildComputeInvoiceInput(_ context.Context, tenantID, environmentID, userID string, params interface{}) (interface{}, error) {
	if input, ok := params.(invoiceModels.ComputeInvoiceWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		input.UserID = userID
		return input, nil
	}
	if input, ok := params.(*invoiceModels.ComputeInvoiceWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		input.UserID = userID
		return *input, nil
	}
	// Handle string input (invoice ID)
	if invoiceID, ok := params.(string); ok && invoiceID != "" {
		return invoiceModels.ComputeInvoiceWorkflowInput{
			InvoiceID:     invoiceID,
			TenantID:      tenantID,
			EnvironmentID: environmentID,
			UserID:        userID,
		}, nil
	}
	return nil, errors.NewError("invalid input for compute invoice workflow").
		WithHint("Provide ComputeInvoiceWorkflowInput with invoice_id").
		Mark(errors.ErrValidation)
}

// buildDraftAndComputeSubscriptionInvoiceInput builds input for DraftAndComputeSubscriptionInvoiceWorkflow.
func (s *temporalService) buildDraftAndComputeSubscriptionInvoiceInput(_ context.Context, tenantID, environmentID, userID string, params interface{}) (interface{}, error) {
	if input, ok := params.(invoiceModels.DraftAndComputeSubscriptionInvoiceWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		input.UserID = userID
		if err := input.Validate(); err != nil {
			return nil, err
		}
		return input, nil
	}
	if input, ok := params.(*invoiceModels.DraftAndComputeSubscriptionInvoiceWorkflowInput); ok && input != nil {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		input.UserID = userID
		if err := input.Validate(); err != nil {
			return nil, err
		}
		return *input, nil
	}
	if subscriptionID, ok := params.(string); ok && subscriptionID != "" {
		input := invoiceModels.DraftAndComputeSubscriptionInvoiceWorkflowInput{
			SubscriptionID: subscriptionID,
			TenantID:       tenantID,
			EnvironmentID:  environmentID,
			UserID:         userID,
		}
		if err := input.Validate(); err != nil {
			return nil, err
		}
		return input, nil
	}
	return nil, errors.NewError("invalid input for draft-and-compute subscription invoice workflow").
		WithHint("Provide DraftAndComputeSubscriptionInvoiceWorkflowInput or subscription_id string").
		Mark(errors.ErrValidation)
}

// buildPrepareProcessedEventsInput builds input for prepare processed events workflow
func (s *temporalService) buildPrepareProcessedEventsInput(_ context.Context, tenantID, environmentID, userID string, params interface{}) (interface{}, error) {
	// If already correct type, just ensure context is set
	if input, ok := params.(*models.PrepareProcessedEventsWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}

	if input, ok := params.(models.PrepareProcessedEventsWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}

	return nil, errors.NewError("invalid input for prepare processed events workflow").
		WithHint("Provide PrepareProcessedEventsWorkflowInput with event_name and workflow_config").
		Mark(errors.ErrValidation)
}

// ExecuteWorkflowSync executes a workflow synchronously and waits for completion
func (s *temporalService) ExecuteWorkflowSync(
	ctx context.Context,
	workflowType types.TemporalWorkflowType,
	params interface{},
	timeoutSeconds int,
) (interface{}, error) {
	// Check if service is initialized
	if s == nil {
		return nil, errors.NewError("temporal service not initialized").
			WithHint("Temporal service must be initialized before use").
			Mark(errors.ErrInternal)
	}

	// Create a timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// Start the workflow
	workflowRun, err := s.ExecuteWorkflow(timeoutCtx, workflowType, params)
	if err != nil {
		return nil, errors.WithError(err).
			WithHint("Failed to start workflow for synchronous execution").
			WithReportableDetails(map[string]interface{}{
				"workflow_type": workflowType.String(),
				"timeout":       timeoutSeconds,
			}).
			Mark(errors.ErrInternal)
	}

	// Wait for workflow completion and get result
	switch workflowType {
	case types.TemporalCustomerOnboardingWorkflow:
		var result models.CustomerOnboardingWorkflowResult
		if err := workflowRun.Get(timeoutCtx, &result); err != nil {
			return nil, errors.WithError(err).
				WithHint("Workflow execution failed or timed out").
				WithReportableDetails(map[string]interface{}{
					"workflow_id":   workflowRun.GetID(),
					"run_id":        workflowRun.GetRunID(),
					"workflow_type": workflowType.String(),
					"timeout":       timeoutSeconds,
				}).
				Mark(errors.ErrInternal)
		}
		return &result, nil

	case types.TemporalPrepareProcessedEventsWorkflow:
		var result models.PrepareProcessedEventsWorkflowResult
		if err := workflowRun.Get(timeoutCtx, &result); err != nil {
			return nil, errors.WithError(err).
				WithHint("Workflow execution failed or timed out").
				WithReportableDetails(map[string]interface{}{
					"workflow_id":   workflowRun.GetID(),
					"run_id":        workflowRun.GetRunID(),
					"workflow_type": workflowType.String(),
					"timeout":       timeoutSeconds,
				}).
				Mark(errors.ErrInternal)
		}
		return &result, nil

	case types.TemporalComputeInvoiceWorkflow:
		var result invoiceModels.ComputeInvoiceWorkflowResult
		if err := workflowRun.Get(timeoutCtx, &result); err != nil {
			return nil, errors.WithError(err).
				WithHint("Workflow execution failed or timed out").
				WithReportableDetails(map[string]interface{}{
					"workflow_id":   workflowRun.GetID(),
					"run_id":        workflowRun.GetRunID(),
					"workflow_type": workflowType.String(),
					"timeout":       timeoutSeconds,
				}).
				Mark(errors.ErrInternal)
		}
		return &result, nil

	default:
		return nil, errors.NewError("unsupported workflow type for synchronous execution").
			WithHint("Use an explicitly supported workflow type for ExecuteWorkflowSync").
			WithReportableDetails(map[string]interface{}{
				"workflow_type": workflowType.String(),
			}).
			Mark(errors.ErrValidation)
	}
}

// buildScheduleSubscriptionBillingWorkflowInput builds input for schedule subscription billing workflow
func (s *temporalService) buildScheduleSubscriptionBillingWorkflowInput(_ context.Context, _, _, _ string, params interface{}) (interface{}, error) {
	// If already correct type, return as-is
	if input, ok := params.(subscriptionModels.ScheduleSubscriptionBillingWorkflowInput); ok {
		// Validate the input
		if err := input.Validate(); err != nil {
			return nil, err
		}
		return input, nil
	}

	// Handle map input for batch size
	if paramsMap, ok := params.(map[string]interface{}); ok {
		batchSize := types.DEFAULT_BATCH_SIZE
		if bs, ok := paramsMap["batch_size"].(int); ok && bs > 0 {
			batchSize = bs
		}
		return subscriptionModels.ScheduleSubscriptionBillingWorkflowInput{
			BatchSize: batchSize,
		}, nil
	}

	// Default with validation
	defaultInput := subscriptionModels.ScheduleSubscriptionBillingWorkflowInput{
		BatchSize: types.DEFAULT_BATCH_SIZE,
	}
	if err := defaultInput.Validate(); err != nil {
		return nil, err
	}
	return defaultInput, nil
}

// buildProcessSubscriptionBillingWorkflowInput builds input for process subscription billing workflow
func (s *temporalService) buildProcessSubscriptionBillingWorkflowInput(_ context.Context, tenantID, environmentID, userID string, params interface{}) (interface{}, error) {
	// If already correct type, ensure context is set
	if input, ok := params.(subscriptionModels.ProcessSubscriptionBillingWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		input.UserID = userID
		// Validate the input
		if err := input.Validate(); err != nil {
			return nil, err
		}
		return input, nil
	}

	// Handle map input
	if paramsMap, ok := params.(map[string]interface{}); ok {
		subscriptionID, _ := paramsMap["subscription_id"].(string)
		if subscriptionID == "" {
			return nil, errors.NewError("subscription_id is required").
				WithHint("Provide map with subscription_id").
				Mark(errors.ErrValidation)
		}

		// Extract optional period fields
		var periodStart, periodEnd time.Time
		if ps, ok := paramsMap["period_start"].(time.Time); ok {
			periodStart = ps
		}
		if pe, ok := paramsMap["period_end"].(time.Time); ok {
			periodEnd = pe
		}

		input := subscriptionModels.ProcessSubscriptionBillingWorkflowInput{
			SubscriptionID: subscriptionID,
			TenantID:       tenantID,
			EnvironmentID:  environmentID,
			UserID:         userID,
			PeriodStart:    periodStart,
			PeriodEnd:      periodEnd,
		}

		// Validate the input
		if err := input.Validate(); err != nil {
			return nil, err
		}
		return input, nil
	}

	return nil, errors.NewError("invalid input for process subscription billing workflow").
		WithHint("Provide ProcessSubscriptionBillingWorkflowInput or map with subscription_id").
		Mark(errors.ErrValidation)
}

// buildReprocessEventsInput builds input for reprocess events workflow
func (s *temporalService) buildReprocessEventsInput(_ context.Context, tenantID, environmentID, userID string, params interface{}) (interface{}, error) {
	// If already correct type, just ensure context is set
	if input, ok := params.(eventsModels.ReprocessEventsWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		input.UserID = userID
		// Validate the input
		if err := input.Validate(); err != nil {
			return nil, err
		}
		return input, nil
	}

	// Handle map input
	if paramsMap, ok := params.(map[string]interface{}); ok {
		externalCustomerID, _ := paramsMap["external_customer_id"].(string)
		eventName, _ := paramsMap["event_name"].(string)

		var startDate, endDate time.Time
		if sd, ok := paramsMap["start_date"].(time.Time); ok {
			startDate = sd
		} else if sdStr, ok := paramsMap["start_date"].(string); ok {
			var err error
			startDate, err = time.Parse(time.RFC3339, sdStr)
			if err != nil {
				return nil, errors.NewError("invalid start_date format").
					WithHint("Start date must be in RFC3339 format (e.g., 2006-01-02T15:04:05Z07:00)").
					Mark(errors.ErrValidation)
			}
		}

		if ed, ok := paramsMap["end_date"].(time.Time); ok {
			endDate = ed
		} else if edStr, ok := paramsMap["end_date"].(string); ok {
			var err error
			endDate, err = time.Parse(time.RFC3339, edStr)
			if err != nil {
				return nil, errors.NewError("invalid end_date format").
					WithHint("End date must be in RFC3339 format (e.g., 2006-01-02T15:04:05Z07:00)").
					Mark(errors.ErrValidation)
			}
		}

		// Extract batch size (default to 100 if not provided)
		batchSize := 100
		if bs, ok := paramsMap["batch_size"].(int); ok && bs > 0 {
			batchSize = bs
		} else if bsFloat, ok := paramsMap["batch_size"].(float64); ok && bsFloat > 0 {
			batchSize = int(bsFloat)
		}

		forceReprocess, _ := paramsMap["force_reprocess"].(bool)

		var runStartTime time.Time
		if rst, ok := paramsMap["run_start_time"].(time.Time); ok {
			runStartTime = rst
		} else if rstStr, ok := paramsMap["run_start_time"].(string); ok && rstStr != "" {
			var err error
			runStartTime, err = time.Parse(time.RFC3339, rstStr)
			if err != nil {
				return nil, errors.NewError("invalid run_start_time format").
					WithHint("Run start time must be in RFC3339 format (e.g., 2006-01-02T15:04:05Z07:00)").
					Mark(errors.ErrValidation)
			}
		}

		input := eventsModels.ReprocessEventsWorkflowInput{
			ExternalCustomerID: externalCustomerID,
			EventName:          eventName, // Optional - can be empty
			StartDate:          startDate,
			EndDate:            endDate,
			BatchSize:          batchSize,
			ForceReprocess:     forceReprocess,
			RunStartTime:       runStartTime,
			TenantID:           tenantID,
			EnvironmentID:      environmentID,
			UserID:             userID,
		}

		// Validate the input
		if err := input.Validate(); err != nil {
			return nil, err
		}
		return input, nil
	}

	return nil, errors.NewError("invalid input for reprocess events workflow").
		WithHint("Provide ReprocessEventsWorkflowInput or map with external_customer_id, event_name, start_date, and end_date").
		Mark(errors.ErrValidation)
}

// buildReprocessRawEventsInput builds input for reprocess raw events workflow
func (s *temporalService) buildReprocessRawEventsInput(_ context.Context, tenantID, environmentID, userID string, params interface{}) (interface{}, error) {
	// If already correct type, just ensure context is set
	if input, ok := params.(eventsModels.ReprocessRawEventsWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		input.UserID = userID
		// Validate the input
		if err := input.Validate(); err != nil {
			return nil, err
		}
		return input, nil
	}

	// Handle map input
	if paramsMap, ok := params.(map[string]interface{}); ok {
		var startDate, endDate time.Time
		if sd, ok := paramsMap["start_date"].(time.Time); ok {
			startDate = sd
		} else if sdStr, ok := paramsMap["start_date"].(string); ok {
			var err error
			startDate, err = time.Parse(time.RFC3339, sdStr)
			if err != nil {
				return nil, errors.NewError("invalid start_date format").
					WithHint("Start date must be in RFC3339 format (e.g., 2006-01-02T15:04:05Z07:00)").
					Mark(errors.ErrValidation)
			}
		}

		if ed, ok := paramsMap["end_date"].(time.Time); ok {
			endDate = ed
		} else if edStr, ok := paramsMap["end_date"].(string); ok {
			var err error
			endDate, err = time.Parse(time.RFC3339, edStr)
			if err != nil {
				return nil, errors.NewError("invalid end_date format").
					WithHint("End date must be in RFC3339 format (e.g., 2006-01-02T15:04:05Z07:00)").
					Mark(errors.ErrValidation)
			}
		}

		// Extract batch size (default to 1000 if not provided)
		batchSize := 1000
		if bs, ok := paramsMap["batch_size"].(int); ok && bs > 0 {
			batchSize = bs
		} else if bsFloat, ok := paramsMap["batch_size"].(float64); ok && bsFloat > 0 {
			batchSize = int(bsFloat)
		}

		// Extract optional array filters
		var eventIDs []string
		if ids, ok := paramsMap["event_ids"].([]string); ok {
			eventIDs = ids
		}

		var externalCustomerIDs []string
		if ids, ok := paramsMap["external_customer_ids"].([]string); ok {
			externalCustomerIDs = ids
		}

		var eventNames []string
		if names, ok := paramsMap["event_names"].([]string); ok {
			eventNames = names
		}

		useUnprocessed, _ := paramsMap["use_unprocessed"].(bool)

		input := eventsModels.ReprocessRawEventsWorkflowInput{
			ExternalCustomerIDs: externalCustomerIDs,
			EventNames:          eventNames,
			StartDate:           startDate,
			EndDate:             endDate,
			BatchSize:           batchSize,
			TenantID:            tenantID,
			EnvironmentID:       environmentID,
			UserID:              userID,
			EventIDs:            eventIDs,
			UseUnprocessed:      useUnprocessed,
		}

		// Validate the input
		if err := input.Validate(); err != nil {
			return nil, err
		}
		return input, nil
	}

	return nil, errors.NewError("invalid input for reprocess raw events workflow").
		WithHint("Provide ReprocessRawEventsWorkflowInput or map with start_date and end_date").
		Mark(errors.ErrValidation)
}

// buildReprocessEventsForPlanInput builds input for reprocess events for plan workflow
func (s *temporalService) buildReprocessEventsForPlanInput(_ context.Context, tenantID, environmentID, userID string, params interface{}) (interface{}, error) {
	if input, ok := params.(eventsModels.ReprocessEventsForPlanWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		input.UserID = userID
		if err := input.Validate(); err != nil {
			return nil, err
		}
		return input, nil
	}

	return nil, errors.NewError("invalid input for reprocess events for plan workflow").
		WithHint("Provide ReprocessEventsForPlanWorkflowInput with missing_pairs").
		Mark(errors.ErrValidation)
}

// validateTenantContext validates that the required tenant context fields are present
func (s *temporalService) validateTenantContext(ctx context.Context) error {
	if err := types.ValidateTenantContext(ctx); err != nil {
		return errors.WithError(err).
			WithHint("Ensure the request context contains tenant information").
			Mark(errors.ErrValidation)
	}
	return nil
}
