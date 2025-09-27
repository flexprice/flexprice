package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/alertlogs"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// AlertLogsService defines the interface for alert logs operations
type AlertLogsService interface {
	// LogAlert creates a new alert log entry and triggers webhook if status changes
	// This is the main method used by cron jobs or other internal processes
	LogAlert(ctx context.Context, req *LogAlertRequest) error

	// GetLatestAlertForEntityAndType retrieves the latest alert log for a specific entity and alert type
	GetLatestAlertForEntityAndType(ctx context.Context, entityType types.AlertEntityType, entityID string, alertType types.AlertType) (*alertlogs.AlertLog, error)

	// GetLatestAlertForEntity retrieves the latest alert log for a specific entity (any alert type)
	GetLatestAlertForEntity(ctx context.Context, entityType types.AlertEntityType, entityID string) (*alertlogs.AlertLog, error)

	// ListAlertsByEntity retrieves alert logs for a specific entity
	ListAlertsByEntity(ctx context.Context, entityType types.AlertEntityType, entityID string, limit int) ([]*alertlogs.AlertLog, error)
}

// LogAlertRequest represents the request to log an alert
type LogAlertRequest struct {
	EntityType  types.AlertEntityType `json:"entity_type" validate:"required"`
	EntityID    string                `json:"entity_id" validate:"required"`
	AlertType   types.AlertType       `json:"alert_type" validate:"required"`
	AlertStatus types.AlertState      `json:"alert_status" validate:"required"`
	AlertInfo   types.AlertInfo       `json:"alert_info" validate:"required"`
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

	s.Logger.Infow("logging alert",
		"entity_type", req.EntityType,
		"entity_id", req.EntityID,
		"alert_type", req.AlertType,
		"alert_status", req.AlertStatus,
	)

	// Check for existing alert log for this specific entity and alert type combination
	existingAlert, err := s.AlertLogsRepo.GetLatestByEntityAndAlertType(ctx, req.EntityType, req.EntityID, req.AlertType)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to check existing alert status").
			Mark(ierr.ErrDatabase)
	}

	// Business logic based on your specifications:
	shouldCreateLog := false
	var webhookEventName string

	if req.AlertStatus == types.AlertStateInAlarm {
		// For ALERT TRIGGERED condition:
		// Publish if threshold breached AND:
		// 1. No latest alert exists for entity.id x alert_type
		// 2. Latest alert exists but status is "ok"
		if existingAlert == nil {
			// Case 1: No previous alert exists
			shouldCreateLog = true
			webhookEventName = s.getWebhookEventName(req.AlertType, req.AlertStatus)
			s.Logger.Infow("creating new alert - no previous alert exists",
				"entity_type", req.EntityType,
				"entity_id", req.EntityID,
				"alert_type", req.AlertType,
				"alert_status", req.AlertStatus,
				"webhook_event", webhookEventName,
			)
		} else if existingAlert.AlertStatus == types.AlertStateOk {
			// Case 2: Previous alert exists but is in "ok" state
			shouldCreateLog = true
			webhookEventName = s.getWebhookEventName(req.AlertType, req.AlertStatus)
			s.Logger.Infow("creating alert - transitioning from ok to in_alarm",
				"entity_type", req.EntityType,
				"entity_id", req.EntityID,
				"alert_type", req.AlertType,
				"previous_status", existingAlert.AlertStatus,
				"new_status", req.AlertStatus,
				"webhook_event", webhookEventName,
			)
		} else {
			// Latest alert exists and is already "in_alarm" - don't log/publish
			s.Logger.Debugw("skipping alert - already in alarm state",
				"entity_type", req.EntityType,
				"entity_id", req.EntityID,
				"alert_type", req.AlertType,
				"current_status", existingAlert.AlertStatus,
			)
		}
	} else if req.AlertStatus == types.AlertStateOk {
		// For ALERT RECOVERED condition:
		// Publish if threshold not breached AND:
		// 1. Latest alert exists for entity.id x alert_type AND status is "in_alarm"
		if existingAlert != nil && existingAlert.AlertStatus == types.AlertStateInAlarm {
			// Only case: Previous alert exists and is in "in_alarm" state
			shouldCreateLog = true
			webhookEventName = s.getWebhookEventName(req.AlertType, req.AlertStatus)
			s.Logger.Infow("creating recovery alert - transitioning from in_alarm to ok",
				"entity_type", req.EntityType,
				"entity_id", req.EntityID,
				"alert_type", req.AlertType,
				"previous_status", existingAlert.AlertStatus,
				"new_status", req.AlertStatus,
				"webhook_event", webhookEventName,
			)
		} else {
			// No previous alert OR previous alert is already "ok" - don't log/publish
			var reason string
			if existingAlert == nil {
				reason = "no previous alert exists"
			} else {
				reason = "previous alert already in ok state"
			}
			s.Logger.Debugw("skipping recovery alert",
				"entity_type", req.EntityType,
				"entity_id", req.EntityID,
				"alert_type", req.AlertType,
				"reason", reason,
				"existing_status", func() string {
					if existingAlert != nil {
						return string(existingAlert.AlertStatus)
					}
					return "none"
				}(),
			)
		}
	}

	if !shouldCreateLog {
		return nil
	}

	// Create new alert log entry
	alertLog := &alertlogs.AlertLog{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ALERT_LOG),
		EntityType:    req.EntityType,
		EntityID:      req.EntityID,
		AlertType:     req.AlertType,
		AlertStatus:   req.AlertStatus,
		AlertInfo:     req.AlertInfo,
		EnvironmentID: types.GetEnvironmentID(ctx),
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

	s.Logger.Infow("alert logged successfully",
		"alert_log_id", alertLog.ID,
		"entity_type", req.EntityType,
		"entity_id", req.EntityID,
		"alert_type", req.AlertType,
		"alert_status", req.AlertStatus,
		"webhook_event", webhookEventName,
	)

	switch req.EntityType {
	case types.AlertEntityTypeWallet:
		// Get wallet domain object directly from repository
		wallet, err := s.WalletRepo.GetWalletByID(ctx, alertLog.EntityID)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to get wallet").
				Mark(ierr.ErrDatabase)
		}

		// Update wallet alert state to match the current alert status
		wallet.AlertState = string(req.AlertStatus)

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
	default:
		s.Logger.Warnw("webhook event not published for alert log:",
			"entity_type", req.EntityType,
			"alert_log_id", alertLog.ID,
		)
		return nil
	}

	return nil
}

func (s *alertLogsService) GetLatestAlertForEntityAndType(ctx context.Context, entityType types.AlertEntityType, entityID string, alertType types.AlertType) (*alertlogs.AlertLog, error) {
	return s.AlertLogsRepo.GetLatestByEntityAndAlertType(ctx, entityType, entityID, alertType)
}

func (s *alertLogsService) GetLatestAlertForEntity(ctx context.Context, entityType types.AlertEntityType, entityID string) (*alertlogs.AlertLog, error) {
	return s.AlertLogsRepo.GetLatestByEntity(ctx, entityType, entityID)
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
