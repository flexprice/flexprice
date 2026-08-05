package payload

import (
	"context"
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/shopspring/decimal"
)

type AlertWebhookPayload struct {
	EventType      types.WebhookEventName `json:"event_type"`
	AlertType      types.AlertType        `json:"alert_type"`
	AlertStatus    types.AlertState       `json:"alert_status"`
	FeatureID      string                 `json:"feature_id,omitempty"`
	WalletID       string                 `json:"wallet_id,omitempty"`
	CustomerID     string                 `json:"customer_id,omitempty"`
	CurrentBalance string                 `json:"current_balance,omitempty"`
	CreditBalance  string                 `json:"credit_balance,omitempty"`
}

func NewAlertWebhookPayload(feature *dto.FeatureResponse, wallet *dto.WalletResponse, customer *dto.CustomerResponse, alertType types.AlertType, alertStatus types.AlertState, eventType types.WebhookEventName) *AlertWebhookPayload {
	payload := &AlertWebhookPayload{
		EventType:   eventType,
		AlertType:   alertType,
		AlertStatus: alertStatus,
	}
	if feature != nil && feature.Feature != nil {
		payload.FeatureID = feature.ID
	}
	if wallet != nil && wallet.Wallet != nil {
		payload.WalletID = wallet.ID
		payload.CurrentBalance = wallet.Balance.String()
		payload.CreditBalance = wallet.CreditBalance.String()
	}
	if customer != nil && customer.Customer != nil {
		payload.CustomerID = customer.ID
	}
	return payload
}

type SpendAlertEvent struct {
	Subscription           *Subscription    `json:"subscription"`
	SubscriptionLineItemID string           `json:"subscription_line_item_id,omitempty"`
	GroupID                string           `json:"group_id,omitempty"`
	AlertType              types.AlertType  `json:"alert_type"`
	AlertStatus            types.AlertState `json:"alert_status"`
	CurrentSpend           string           `json:"current_spend"`
	Threshold              *decimal.Decimal `json:"threshold,omitempty" swaggertype:"string"`
	TriggeredAt            time.Time        `json:"triggered_at"`
}

func thresholdForAlertStatus(settings *types.AlertSettings, status types.AlertState) *decimal.Decimal {
	if settings == nil {
		return nil
	}
	var t *types.AlertThreshold
	switch status {
	case types.AlertStateInAlarm:
		t = settings.Critical
	case types.AlertStateWarning:
		t = settings.Warning
	case types.AlertStateInfo:
		t = settings.Info
	}
	if t == nil {
		return nil
	}
	return &t.Threshold
}

func NewSpendAlertEvent(sub *dto.SubscriptionResponse, lineItemID, groupID string, alertType types.AlertType, alertStatus types.AlertState, currentSpend string, alertSettings *types.AlertSettings, triggeredAt time.Time) *SpendAlertEvent {
	return &SpendAlertEvent{
		Subscription:           NewSubscription(sub),
		SubscriptionLineItemID: lineItemID,
		GroupID:                groupID,
		AlertType:              alertType,
		AlertStatus:            alertStatus,
		CurrentSpend:           currentSpend,
		Threshold:              thresholdForAlertStatus(alertSettings, alertStatus),
		TriggeredAt:            triggeredAt,
	}
}

type EntitlementGrantAlertEvent struct {
	SubscriptionID     string           `json:"subscription_id"`
	CustomerID         string           `json:"customer_id"`
	EntitlementID      string           `json:"entitlement_id"`
	EntitlementGrantID string           `json:"entitlement_grant_id"`
	AlertType          types.AlertType  `json:"alert_type"`
	AlertStatus        types.AlertState `json:"alert_status"`
	UsageRatio         string           `json:"usage_ratio"`
	TriggeredAt        time.Time        `json:"triggered_at"`
}

func NewEntitlementGrantAlertEvent(subscriptionID, customerID, entitlementID, grantID string, alertType types.AlertType, alertStatus types.AlertState, usageRatio string, triggeredAt time.Time) *EntitlementGrantAlertEvent {
	return &EntitlementGrantAlertEvent{
		SubscriptionID:     subscriptionID,
		CustomerID:         customerID,
		EntitlementID:      entitlementID,
		EntitlementGrantID: grantID,
		AlertType:          alertType,
		AlertStatus:        alertStatus,
		UsageRatio:         usageRatio,
		TriggeredAt:        triggeredAt,
	}
}

type AlertPayloadBuilder struct {
	services *Services
}

func NewAlertPayloadBuilder(services *Services) PayloadBuilder {
	return &AlertPayloadBuilder{services: services}
}

// BuildPayload for alert webhooks - fetches entities based on what IDs are provided
func (b *AlertPayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	// Unmarshal the internal alert event containing entity IDs (omitempty fields)
	var internalEvent webhookDto.InternalAlertEvent
	if err := json.Unmarshal(data, &internalEvent); err != nil {
		return nil, err
	}

	if internalEvent.EntityType == types.AlertEntityTypeEntitlementGrant {
		return b.buildEntitlementGrantAlertPayload(ctx, internalEvent)
	}

	// Subscription/line-item/group spend alert (alert_settings table): resolve the owning
	// subscription fresh, so currency and period start reflect its state as of delivery.
	if internalEvent.EntityType != "" {
		return b.buildSpendAlertPayload(ctx, internalEvent)
	}

	// Fetch customer data if customer_id is provided
	var customer *dto.CustomerResponse
	if internalEvent.CustomerID != "" {
		customerData, err := b.services.CustomerService.GetCustomer(ctx, internalEvent.CustomerID)
		if err != nil {
			// Log error but don't fail the webhook if customer fetch fails
			// Customer is optional in the payload
			b.services.Tracing.CaptureException(ctx, err)
			customer = nil
		} else {
			customer = customerData
		}
	}

	// Feature alert: needs both feature and wallet
	if internalEvent.FeatureID != "" && internalEvent.WalletID != "" {
		// Fetch feature
		feature, err := b.services.FeatureService.GetFeature(ctx, internalEvent.FeatureID)
		if err != nil {
			return nil, err
		}

		// Fetch wallet
		wallet, err := b.services.WalletService.GetWalletByID(ctx, internalEvent.WalletID)
		if err != nil {
			return nil, err
		}

		// Build the complete alert webhook payload with both entities and customer
		payload := NewAlertWebhookPayload(
			feature,
			wallet,
			customer,
			internalEvent.AlertType,   // alert_type from internal event
			internalEvent.AlertStatus, // alert_status from internal event
			eventType,
		)

		return json.Marshal(payload)
	}

	// If we get here, no valid combination found - return nil
	return nil, nil
}

// buildEntitlementGrantAlertPayload resolves a grant-exhaustion alert into its webhook payload.
// Only the grant itself is fetched -- subscription/customer/entitlement IDs are already known
// from internalEvent.ParentEntityID and the grant's own CustomerID/EntitlementConfigID fields,
// so under the ID-only payload policy no further fetches are needed.
func (b *AlertPayloadBuilder) buildEntitlementGrantAlertPayload(ctx context.Context, internalEvent webhookDto.InternalAlertEvent) (json.RawMessage, error) {
	if internalEvent.ParentEntityID == "" {
		return nil, ierr.NewError("entitlement grant alert missing subscription id").
			WithReportableDetails(map[string]any{"entitlement_grant_id": internalEvent.EntityID}).
			Mark(ierr.ErrValidation)
	}

	grant, err := b.services.EntitlementGrantSvc.GetGrant(ctx, internalEvent.EntityID)
	if err != nil {
		return nil, err
	}

	payload := NewEntitlementGrantAlertEvent(
		internalEvent.ParentEntityID,
		grant.CustomerID,
		grant.EntitlementConfigID,
		grant.ID,
		internalEvent.AlertType,
		internalEvent.AlertStatus,
		internalEvent.AlertInfo.ValueAtTime.String(),
		internalEvent.AlertInfo.Timestamp,
	)
	return json.Marshal(payload)
}

// buildSpendAlertPayload resolves an InternalAlertEvent carrying a subscription/line-item/group
// spend alert into its final webhook payload. Only the subscription is fetched -- the line item
// or group is represented by its already-known ID (internalEvent.EntityID), not fetched in full.
func (b *AlertPayloadBuilder) buildSpendAlertPayload(ctx context.Context, internalEvent webhookDto.InternalAlertEvent) (json.RawMessage, error) {
	// A line-item or group alert's entity_id is the line item/group itself; the subscription it
	// rolls up to is parent_entity_id. A subscription-level alert has no parent, so entity_id is
	// already the subscription.
	subscriptionID := internalEvent.EntityID
	if internalEvent.ParentEntityID != "" && internalEvent.ParentEntityType == types.AlertEntityTypeSubscription {
		subscriptionID = internalEvent.ParentEntityID
	}

	sub, err := b.services.SubscriptionService.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	var lineItemID, groupID string
	switch internalEvent.EntityType {
	case types.AlertEntityTypeSubscriptionLineItem:
		lineItemID = internalEvent.EntityID
	case types.AlertEntityTypeGroup:
		groupID = internalEvent.EntityID
	}

	payload := NewSpendAlertEvent(
		sub, lineItemID, groupID,
		internalEvent.AlertType, internalEvent.AlertStatus,
		internalEvent.AlertInfo.ValueAtTime.String(), internalEvent.AlertInfo.AlertSettings, internalEvent.AlertInfo.Timestamp,
	)

	return json.Marshal(payload)
}
