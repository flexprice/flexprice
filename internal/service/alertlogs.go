package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/domain/alertlogs"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
)

// AlertLogsService defines the interface for alert logs operations
type AlertLogsService interface {
	// LogAlert creates a new alert log entry and triggers webhook if status changes
	// This is the main method used by cron jobs or other internal processes
	LogAlert(ctx context.Context, req *LogAlertRequest) error

	// GetLatestAlert retrieves the latest alert log based on provided filters
	GetLatestAlert(ctx context.Context, entityType types.AlertEntityType, entityID string, alertType *types.AlertType, parentEntityType *string, parentEntityID *string) (*alertlogs.AlertLog, error)

	// ListAlertsByEntity retrieves alert logs for a specific entity
	ListAlertsByEntity(ctx context.Context, entityType types.AlertEntityType, entityID string, limit int) ([]*alertlogs.AlertLog, error)

	// ListAlertLogsByFilter retrieves alert logs by filter
	ListAlertLogsByFilter(ctx context.Context, filter *types.AlertLogFilter) (*types.ListResponse[*alertlogs.AlertLog], error)
}

// LogAlertRequest represents the request to log an alert
type LogAlertRequest struct {
	EntityType       types.AlertEntityType `json:"entity_type" validate:"required"`
	EntityID         string                `json:"entity_id" validate:"required"`
	ParentEntityType *string               `json:"parent_entity_type,omitempty"` // Optional parent entity type (e.g., "wallet")
	ParentEntityID   *string               `json:"parent_entity_id,omitempty"`   // Optional parent entity ID (e.g., wallet_id)
	CustomerID       *string               `json:"customer_id,omitempty"`        // Optional customer ID for whom alert has been raised
	AlertType        types.AlertType       `json:"alert_type" validate:"required"`
	AlertStatus      types.AlertState      `json:"alert_status" validate:"required"`
	AlertInfo        types.AlertInfo       `json:"alert_info" validate:"required"`
}

// Validate validates the log alert request
func (r *LogAlertRequest) Validate() error {
	if r.EntityType == "" {
		return ierr.NewError("entity_type is required").
			WithHint("Please provide an entity type").
			Mark(ierr.ErrValidation)
	}
	if err := r.EntityType.Validate(); err != nil {
		return err
	}
	if r.EntityID == "" {
		return ierr.NewError("entity_id is required").
			WithHint("Please provide an entity ID").
			Mark(ierr.ErrValidation)
	}
	if err := r.AlertType.Validate(); err != nil {
		return err
	}
	return nil
}

type alertLogsService struct {
	ServiceParams
}

func NewAlertLogsService(params ServiceParams) AlertLogsService {
	return &alertLogsService{
		ServiceParams: params,
	}
}

func (s *alertLogsService) LogAlert(ctx context.Context, req *LogAlertRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}

	s.Logger.Debugw("evaluating alert status",
		"entity_type", req.EntityType,
		"entity_id", req.EntityID,
		"parent_entity_type", req.ParentEntityType,
		"parent_entity_id", req.ParentEntityID,
		"alert_type", req.AlertType,
		"alert_status", req.AlertStatus,
	)

	// Check for existing alert log using the unified GetLatestAlert method
	// This method handles all alert types - with or without parent entities
	existingAlert, err := s.AlertLogsRepo.GetLatestAlert(
		ctx,
		req.EntityType,
		req.EntityID,
		&req.AlertType,
		req.ParentEntityType,
		req.ParentEntityID,
	)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to check existing alert status").
			Mark(ierr.ErrDatabase)
	}

	// Debug log to verify what we fetched from DB (NO CACHE!)
	if existingAlert != nil {
		s.Logger.Infow("fetched existing alert from database",
			"entity_type", req.EntityType,
			"entity_id", req.EntityID,
			"alert_type", req.AlertType,
			"existing_alert_id", existingAlert.ID,
			"existing_alert_status", existingAlert.AlertStatus,
			"existing_alert_created_at", existingAlert.CreatedAt,
			"requested_status", req.AlertStatus,
		)
	} else {
		s.Logger.Infow("no existing alert found in database",
			"entity_type", req.EntityType,
			"entity_id", req.EntityID,
			"alert_type", req.AlertType,
			"requested_status", req.AlertStatus,
		)
	}

	// Business logic: Log alerts ONLY when state changes or when problems are detected
	// Works for all alert types (wallet, feature, etc.) and all states (ok, info, warning, in_alarm)
	shouldCreateLog := false
	var webhookEventName string

	// State transition rules:
	// 1. No previous alert + OK state → Skip (system is healthy from start, no alert needed)
	// 2. No previous alert + INFO/WARNING/IN_ALARM → Create (problem detected for first time)
	// 3. Previous alert exists + status changed → Create (state transition)
	// 4. Previous alert exists + status unchanged → Skip (no change)

	if existingAlert == nil {
		// No previous alert exists
		if req.AlertStatus == types.AlertStateOk {
			// System is healthy from the start - no need to log
			s.Logger.Debugw("skipping alert - no previous alert and status is ok (system healthy)",
				"entity_type", req.EntityType,
				"entity_id", req.EntityID,
				"alert_type", req.AlertType,
				"alert_status", req.AlertStatus,
			)
		} else {
			// Problem state detected for first time (INFO, WARNING or IN_ALARM) - create alert
			shouldCreateLog = true
			webhookEventName = s.getWebhookEventName(req.AlertType, req.AlertStatus)
			s.Logger.Infow("creating alert - problem detected for first time",
				"entity_type", req.EntityType,
				"entity_id", req.EntityID,
				"alert_type", req.AlertType,
				"alert_status", req.AlertStatus,
				"webhook_event", webhookEventName,
			)
		}
	} else if existingAlert.AlertStatus != req.AlertStatus {
		// Previous alert exists BUT status is different - state changed, create log
		shouldCreateLog = true
		webhookEventName = s.getWebhookEventName(req.AlertType, req.AlertStatus)
		s.Logger.Infow("creating alert - state changed",
			"entity_type", req.EntityType,
			"entity_id", req.EntityID,
			"alert_type", req.AlertType,
			"previous_status", existingAlert.AlertStatus,
			"new_status", req.AlertStatus,
			"webhook_event", webhookEventName,
		)
	} else {
		// Previous alert exists AND status is the same - no change, skip
		s.Logger.Debugw("skipping alert - status unchanged",
			"entity_type", req.EntityType,
			"entity_id", req.EntityID,
			"alert_type", req.AlertType,
			"current_status", existingAlert.AlertStatus,
			"requested_status", req.AlertStatus,
		)
	}

	if !shouldCreateLog {
		return nil
	}

	// Create new alert log entry
	alertLog := &alertlogs.AlertLog{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ALERT_LOG),
		EntityType:       req.EntityType,
		EntityID:         req.EntityID,
		ParentEntityType: req.ParentEntityType,
		ParentEntityID:   req.ParentEntityID,
		CustomerID:       req.CustomerID,
		AlertType:        req.AlertType,
		AlertStatus:      req.AlertStatus,
		AlertInfo:        req.AlertInfo,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel: types.BaseModel{
			TenantID:  types.GetTenantID(ctx),
			Status:    types.StatusPublished,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			CreatedBy: types.GetUserID(ctx),
			UpdatedBy: types.GetUserID(ctx),
		},
	}

	if err := s.AlertLogsRepo.Create(ctx, alertLog); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create alert log").
			Mark(ierr.ErrDatabase)
	}

	s.Logger.Infow("alert log created successfully",
		"alert_log_id", alertLog.ID,
		"entity_type", req.EntityType,
		"entity_id", req.EntityID,
		"alert_type", req.AlertType,
		"alert_status", req.AlertStatus,
		"webhook_event", webhookEventName,
	)

	switch req.AlertType {
	case types.AlertTypeLowOngoingBalance, types.AlertTypeLowCreditBalance:
		// Get wallet domain object directly from repository
		wallet, err := s.WalletRepo.GetWalletByID(ctx, alertLog.EntityID)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to get wallet").
				Mark(ierr.ErrDatabase)
		}

		// Update wallet alert state to match the current alert status
		wallet.AlertState = req.AlertStatus

		// Publish webhook event using existing wallet event infrastructure
		if webhookEventName != "" {
			walletService := NewWalletService(s.ServiceParams)
			if err := walletService.PublishEvent(ctx, webhookEventName, wallet); err != nil {
				s.Logger.Errorw("failed to publish webhook event",
					"error", err,
					"alert_log_id", alertLog.ID,
					"entity_type", req.EntityType,
					"entity_id", req.EntityID,
					"alert_type", req.AlertType,
					"alert_status", req.AlertStatus,
					"webhook_event", webhookEventName,
				)
			}
			s.Logger.Infow("webhook event published successfully",
				"alert_log_id", alertLog.ID,
				"entity_type", req.EntityType,
				"entity_id", req.EntityID,
				"alert_type", req.AlertType,
				"alert_status", req.AlertStatus,
				"webhook_event", webhookEventName,
			)
		}
	case types.AlertTypeFeatureWalletBalance:
		// Publish webhook event using the publishWebhookEvent helper
		// This will pass the alert log with parent entity fields (feature_id, wallet_id) to AlertPayloadBuilder
		if webhookEventName != "" {
			if err := s.publishWebhookEvent(ctx, webhookEventName, alertLog, req.AlertType); err != nil {
				s.Logger.Errorw("failed to publish webhook event",
					"error", err,
					"alert_log_id", alertLog.ID,
					"entity_type", req.EntityType,
					"entity_id", req.EntityID,
					"alert_type", req.AlertType,
					"alert_status", req.AlertStatus,
					"webhook_event", webhookEventName,
				)
			} else {
				s.Logger.Infow("webhook event published successfully",
					"alert_log_id", alertLog.ID,
					"entity_type", req.EntityType,
					"entity_id", req.EntityID,
					"alert_type", req.AlertType,
					"alert_status", req.AlertStatus,
					"webhook_event", webhookEventName,
				)
			}
		}
	default:
		s.Logger.Warnw("webhook event not published for alert log:",
			"entity_type", req.EntityType,
			"alert_log_id", alertLog.ID,
		)
		return nil
	}

	return nil
}

// GetLatestAlert retrieves the latest alert log based on provided filters
// All parameters except entityType and entityID are optional
func (s *alertLogsService) GetLatestAlert(ctx context.Context, entityType types.AlertEntityType, entityID string, alertType *types.AlertType, parentEntityType *string, parentEntityID *string) (*alertlogs.AlertLog, error) {
	return s.AlertLogsRepo.GetLatestAlert(ctx, entityType, entityID, alertType, parentEntityType, parentEntityID)
}

func (s *alertLogsService) ListAlertsByEntity(ctx context.Context, entityType types.AlertEntityType, entityID string, limit int) ([]*alertlogs.AlertLog, error) {
	return s.AlertLogsRepo.ListByEntity(ctx, entityType, entityID, limit)
}

// WebhookEventMapping represents the mapping configuration for alert types and statuses to webhook events
type WebhookEventMapping struct {
	WebhookEvent string `json:"webhook_event"`
}

// alertWebhookMapping defines the mapping from alert types and statuses to specific webhook events
// This mapping allows us to send specific webhook events that clients are already listening to,
// rather than generic "alert.triggered" or "alert.recovered" events.
//
// Structure: map[AlertType][AlertState] = WebhookEventMapping
// Example: map[low_wallet_balance][in_alarm] = "wallet.credit_balance.dropped"
var alertWebhookMapping = map[types.AlertType]map[types.AlertState]WebhookEventMapping{
	types.AlertTypeLowOngoingBalance: {
		types.AlertStateInAlarm: {
			WebhookEvent: types.WebhookEventWalletOngoingBalanceDropped, // "wallet.ongoing_balance.dropped"
		},
		types.AlertStateOk: {
			WebhookEvent: types.WebhookEventWalletOngoingBalanceRecovered, // "wallet.ongoing_balance.recovered"
		},
	},
	types.AlertTypeLowCreditBalance: {
		types.AlertStateInAlarm: {
			WebhookEvent: types.WebhookEventWalletCreditBalanceDropped, // "wallet.ongoing_balance.dropped"
		},
		types.AlertStateOk: {
			WebhookEvent: types.WebhookEventWalletCreditBalanceRecovered, // "wallet.credit_balance.recovered"
		},
	},
	types.AlertTypeFeatureWalletBalance: {
		types.AlertStateInAlarm: {
			WebhookEvent: types.WebhookEventFeatureWalletBalanceAlert, // "feature.balance.threshold.alert"
		},
		types.AlertStateWarning: {
			WebhookEvent: types.WebhookEventFeatureWalletBalanceAlert, // "feature.balance.threshold.alert"
		},
		types.AlertStateInfo: {
			WebhookEvent: types.WebhookEventFeatureWalletBalanceAlert, // "feature.balance.threshold.alert"
		},
		types.AlertStateOk: {
			WebhookEvent: types.WebhookEventFeatureWalletBalanceAlert, // "feature.balance.threshold.alert"
		},
	},
}

// getWebhookEventName determines the appropriate webhook event name based on alert type and status
func (s *alertLogsService) getWebhookEventName(alertType types.AlertType, alertStatus types.AlertState) string {
	// Check if we have a mapping for this alert type
	if alertTypeMapping, exists := alertWebhookMapping[alertType]; exists {
		// Check if we have a mapping for this alert status
		if statusMapping, exists := alertTypeMapping[alertStatus]; exists {
			return statusMapping.WebhookEvent
		}
	}

	// Return empty string if no mapping found
	return ""
}

func (s *alertLogsService) publishWebhookEvent(ctx context.Context, eventName string, alertLog *alertlogs.AlertLog, alertType types.AlertType) error {
	var webhookPayload []byte
	var err error

	switch alertType {
	case types.AlertTypeFeatureWalletBalance:
		// For feature alerts, pass both feature_id and wallet_id
		walletID := ""
		if alertLog.ParentEntityID != nil {
			walletID = lo.FromPtr(alertLog.ParentEntityID)
		}

		// Get customer_id
		customerID := ""
		if alertLog.CustomerID != nil {
			customerID = lo.FromPtr(alertLog.CustomerID)
		}

		webhookPayload, err = json.Marshal(webhookDto.InternalAlertEvent{
			FeatureID:   alertLog.EntityID,            // Feature ID
			WalletID:    walletID,                     // Wallet ID from parent entity ID
			CustomerID:  customerID,                   // Customer ID
			AlertType:   string(alertLog.AlertType),   // Alert type
			AlertStatus: string(alertLog.AlertStatus), // Alert status
		})
		if err != nil {
			s.Logger.Errorw("failed to marshal webhook payload", "error", err)
			return err
		}
	default:
		return ierr.NewError("invalid alert type").
			WithHint("Invalid alert type").
			Mark(ierr.ErrValidation)
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WEBHOOK_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
	}

	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Errorf("failed to publish %s event: %v", webhookEvent.EventName, err)
		return err
	}

	return nil
}

func (s *alertLogsService) ListAlertLogsByFilter(ctx context.Context, filter *types.AlertLogFilter) (*types.ListResponse[*alertlogs.AlertLog], error) {
	if filter == nil {
		filter = types.NewDefaultAlertLogFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	// Set default sort order if not specified
	if filter.QueryFilter.Sort == nil {
		filter.QueryFilter.Sort = lo.ToPtr("created_at")
		filter.QueryFilter.Order = lo.ToPtr("desc")
	}

	// Validate expand fields
	if filter.Expand != nil && *filter.Expand != "" {
		expand := filter.GetExpand()
		if err := expand.Validate(types.AlertLogExpandConfig); err != nil {
			return nil, err
		}
	}

	// validate filters
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	alertLogs, err := s.AlertLogsRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	alertLogCount, err := s.AlertLogsRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	response := &types.ListResponse[*alertlogs.AlertLog]{
		Items: alertLogs,
	}

	response.Pagination = types.NewPaginationResponse(
		alertLogCount,
		filter.GetLimit(),
		filter.GetOffset(),
	)

	return response, nil
}
