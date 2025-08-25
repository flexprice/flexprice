package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/alert"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
)

// AlertService defines the interface for alert operations
type AlertService interface {
	// CheckAlerts checks alerts for the given entities
	CheckAlerts(ctx context.Context, req *dto.CheckAlertsRequest) error
}

type alertService struct {
	ServiceParams
}

// NewAlertService creates a new alert service
func NewAlertService(params ServiceParams) AlertService {
	return &alertService{
		ServiceParams: params,
	}
}

// CheckAlerts checks alerts for the given entities
func (s *alertService) CheckAlerts(ctx context.Context, req *dto.CheckAlertsRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}

	// Process each tenant×environment pair
	for _, tenantID := range req.TenantIDs {
		for _, envID := range req.EnvIDs {
			// Get all entities for this tenant×environment pair
			entities, err := s.getEntitiesByType(ctx, tenantID, envID, req.EntityType, req.EntityIDs)
			if err != nil {
				s.Logger.Error("failed to get entities", "error", err, "tenant_id", tenantID, "env_id", envID)
				continue // Skip this pair but continue with others
			}

			// Process each entity
			for _, entity := range entities {
				if !entity.AlertEnabled {
					continue
				}

				// Get threshold - either from request or entity's own config
				threshold := req.Threshold
				if threshold == nil {
					// Get entity's threshold from its table
					entityThreshold, err := s.getEntityThreshold(ctx, entity)
					if err != nil {
						s.Logger.Error("failed to get entity threshold", "error", err, "entity_id", entity.ID)
						continue
					}
					if entityThreshold == nil {
						s.Logger.Error("threshold configuration missing for entity with alerts enabled",
							"error", "no threshold found in request or entity config",
							"entity_type", entity.Type,
							"entity_id", entity.ID,
							"tenant_id", tenantID,
							"environment_id", envID)
						continue
					}
					threshold = entityThreshold
				}

				// Check alert metric against threshold
				currentValue, err := s.getMetricValue(ctx, entity, req.AlertMetric)
				if err != nil {
					s.Logger.Error("failed to get metric value", "error", err, "entity_id", entity.ID)
					continue
				}

				// Determine alert state
				alertState := s.determineAlertState(currentValue, threshold)
				s.Logger.Infow("determined alert state",
					"entity_id", entity.ID,
					"alert_metric", req.AlertMetric,
					"current_value", currentValue,
					"threshold", threshold,
					"alert_state", alertState)

				// Get latest alert for this entity
				latestAlert, err := s.AlertRepo.GetLatestByEntity(ctx, tenantID, envID, req.EntityType, entity.ID, req.AlertMetric)
				if err != nil {
					s.Logger.Error("failed to get latest alert", "error", err, "entity_id", entity.ID)
					continue
				}

				// Handle alert state changes
				if err := s.handleAlertStateChange(ctx, entity, latestAlert, alertState, currentValue, req.Threshold, req.AlertMetric); err != nil {
					s.Logger.Error("failed to handle alert state change", "error", err, "entity_id", entity.ID)
					continue
				}
			}
		}
	}

	return nil
}

// publishWebhookEvent publishes a webhook event for an alert
func (s *alertService) publishWebhookEvent(ctx context.Context, eventName string, a *alert.Alert) error {
	if s.WebhookPublisher == nil {
		s.Logger.Warn("webhook publisher not initialized", "event", eventName)
		return nil
	}

	// Create internal event
	internalEvent := &webhookDto.InternalAlertEvent{
		EventType:    eventName,
		AlertID:      a.ID,
		TenantID:     a.TenantID,
		EntityType:   a.EntityType,
		EntityID:     a.EntityID,
		AlertMetric:  a.AlertMetric,
		AlertState:   string(a.AlertState),
		AlertEnabled: a.AlertEnabled,
		AlertData:    a.AlertData,
	}

	// Convert to JSON
	eventJSON, err := json.Marshal(internalEvent)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to marshal webhook event").
			Mark(ierr.ErrInternal)
	}

	// Create webhook event
	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WEBHOOK_EVENT),
		EventName:     eventName,
		TenantID:      a.TenantID,
		EnvironmentID: a.EnvironmentID,
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(eventJSON),
	}

	return s.WebhookPublisher.PublishWebhook(ctx, webhookEvent)
}

// getEntitiesByType retrieves entities of a specific type
func (s *alertService) getEntitiesByType(ctx context.Context, tenantID, envID, entityType string, entityIDs []string) ([]*types.Entity, error) {
	switch entityType {
	case "wallet":
		// Initialize wallet service with required dependencies
		walletService := NewWalletService(s.ServiceParams)

		// Create base query filter
		queryFilter := types.NewDefaultQueryFilter()
		queryFilter.Status = lo.ToPtr(types.Status(types.WalletStatusActive))

		// Create wallet filter
		filter := &types.WalletFilter{
			QueryFilter: queryFilter,
			Status:      lo.ToPtr(types.WalletStatusActive),
		}

		// Add specific wallet IDs if provided
		if len(entityIDs) > 0 {
			filter.WalletIDs = entityIDs
		}

		// Get wallets
		response, err := walletService.GetWallets(ctx, filter)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to get wallets").
				WithReportableDetails(map[string]interface{}{
					"tenant_id":      tenantID,
					"environment_id": envID,
				}).
				Mark(ierr.ErrDatabase)
		}

		if len(response.Items) == 0 {
			s.Logger.Infow("no wallets found for monitoring",
				"tenant_id", tenantID,
				"environment_id", envID,
				"filter_ids", entityIDs)
			return nil, nil
		}

		// Convert eligible wallets to generic entities
		var entities []*types.Entity
		for _, w := range response.Items {
			// Only include wallets that have alerts enabled
			if !w.AlertEnabled {
				continue
			}

			// Only include alert config if alerts are enabled
			if w.AlertConfig == nil {
				s.Logger.Warnw("wallet has alerts enabled but no alert config",
					"wallet_id", w.ID,
					"customer_id", w.CustomerID)
				continue
			}

			entities = append(entities, &types.Entity{
				ID:           w.ID,
				Type:         "wallet",
				AlertEnabled: true,
				AlertConfig:  w.AlertConfig,
			})
		}

		s.Logger.Infow("found wallets for monitoring",
			"tenant_id", tenantID,
			"environment_id", envID,
			"count", len(entities))

		return entities, nil

	// Add cases for other entity types here
	default:
		return nil, ierr.NewError("unsupported entity type").
			WithHint(fmt.Sprintf("Entity type '%s' is not supported", entityType)).
			WithReportableDetails(map[string]interface{}{
				"entity_type":    entityType,
				"tenant_id":      tenantID,
				"environment_id": envID,
			}).
			Mark(ierr.ErrValidation)
	}
}

// getEntityThreshold gets the threshold configuration from the entity's alert config
func (s *alertService) getEntityThreshold(ctx context.Context, entity *types.Entity) (*types.AlertThreshold, error) {
	// We already validated that AlertConfig exists when getting entities
	return entity.AlertConfig.Threshold, nil
}

// getMetricValue retrieves the current value for a metric
func (s *alertService) getMetricValue(ctx context.Context, entity *types.Entity, metric string) (float64, error) {
	switch entity.Type {
	case "wallet":
		// Initialize wallet service with required dependencies
		walletService := NewWalletService(s.ServiceParams)

		// Get wallet details
		wallet, err := s.WalletRepo.GetWalletByID(ctx, entity.ID)
		if err != nil {
			return 0, ierr.WithError(err).
				WithHint("Failed to get wallet").
				WithReportableDetails(map[string]interface{}{
					"wallet_id": entity.ID,
				}).
				Mark(ierr.ErrDatabase)
		}

		switch metric {
		case "credit_balance":
			// For credit balance, we can directly use the wallet's credit balance
			if wallet.CreditBalance.IsZero() {
				s.Logger.Warnw("wallet has zero credit balance",
					"wallet_id", wallet.ID,
					"customer_id", wallet.CustomerID)
			}
			return wallet.CreditBalance.InexactFloat64(), nil

		case "ongoing_balance":
			// For ongoing balance, we need to calculate real-time balance including:
			// - Current wallet balance
			// - Unpaid invoices
			// - Current period charges
			balance, err := walletService.GetWalletBalance(ctx, wallet.ID)
			if err != nil {
				return 0, ierr.WithError(err).
					WithHint("Failed to get wallet balance").
					WithReportableDetails(map[string]interface{}{
						"wallet_id": wallet.ID,
					}).
					Mark(ierr.ErrInternal)
			}

			if balance.RealTimeBalance == nil {
				return 0, ierr.NewError("real time balance not available").
					WithHint("Failed to calculate real-time balance").
					WithReportableDetails(map[string]interface{}{
						"wallet_id": wallet.ID,
					}).
					Mark(ierr.ErrInternal)
			}

			s.Logger.Infow("got wallet balance details",
				"wallet_id", wallet.ID,
				"real_time_balance", balance.RealTimeBalance,
				"unpaid_invoices", balance.UnpaidInvoiceAmount,
				"current_period_usage", balance.CurrentPeriodUsage)

			return balance.RealTimeBalance.InexactFloat64(), nil

		default:
			return 0, ierr.NewError("unsupported metric for wallet").
				WithHint(fmt.Sprintf("Metric '%s' is not supported for wallet", metric)).
				WithReportableDetails(map[string]interface{}{
					"entity_type": entity.Type,
					"entity_id":   entity.ID,
					"metric":      metric,
				}).
				Mark(ierr.ErrValidation)
		}

	// Add cases for other entity types here
	default:
		return 0, ierr.NewError("unsupported entity type").
			WithHint(fmt.Sprintf("Entity type '%s' is not supported", entity.Type)).
			WithReportableDetails(map[string]interface{}{
				"entity_type": entity.Type,
				"entity_id":   entity.ID,
			}).
			Mark(ierr.ErrValidation)
	}
}

// determineAlertState determines the alert state based on the current value and threshold
func (s *alertService) determineAlertState(currentValue float64, threshold *types.AlertThreshold) types.AlertState {
	if threshold == nil {
		return types.AlertStateOk
	}

	switch threshold.Type {
	case "amount":
		// For amount type, alert if value is less than or equal to threshold
		if currentValue <= threshold.Value.InexactFloat64() {
			return types.AlertStateInAlarm
		}
	// Add other threshold types here (percentage, ratio, etc.)
	default:
		s.Logger.Errorw("unsupported threshold type",
			"type", threshold.Type,
			"value", threshold.Value,
			"current_value", currentValue)
	}

	return types.AlertStateOk
}

// handleAlertStateChange handles changes in alert state
func (s *alertService) handleAlertStateChange(ctx context.Context, entity *types.Entity, latestAlert *alert.Alert, newState types.AlertState, currentValue float64, threshold *types.AlertThreshold, alertMetric string) error {

	// If no previous alert exists or state has changed
	s.Logger.Infow("checking if new alert needed",
		"entity_id", entity.ID,
		"alert_metric", alertMetric,
		"new_state", newState,
		"latest_alert", latestAlert)

	// Always create a new alert entry when state changes
	if latestAlert == nil || latestAlert.AlertState != newState {
		// Create new alert
		newAlert := &alert.Alert{
			ID:           types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ALERT),
			EntityType:   entity.Type,
			EntityID:     entity.ID,
			AlertMetric:  alertMetric,
			AlertState:   newState,
			AlertEnabled: true,
			AlertData: map[string]interface{}{
				"threshold":     threshold,
				"current_value": currentValue,
				"timestamp":     time.Now().UTC(),
			},
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

		if err := s.AlertRepo.Create(ctx, newAlert); err != nil {
			return err
		}

		// Determine webhook event type
		eventType := types.WebhookEventAlertTriggered
		if newState == types.AlertStateOk && latestAlert != nil && latestAlert.AlertState == types.AlertStateInAlarm {
			eventType = types.WebhookEventAlertRecovered
			s.Logger.Infow("sending recovery webhook",
				"entity_id", entity.ID,
				"alert_metric", alertMetric,
				"old_state", latestAlert.AlertState,
				"new_state", newState)
		}

		// Publish webhook event
		if err := s.publishWebhookEvent(ctx, eventType, newAlert); err != nil {
			s.Logger.Error("failed to publish webhook event", "error", err)
			// Don't return error as the alert was created successfully
		}
	}

	return nil
}
