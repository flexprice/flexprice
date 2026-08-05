package payload

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

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
		payload := webhookDto.NewAlertWebhookPayload(
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

	payload := webhookDto.NewEntitlementGrantAlertEvent(
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

	payload := webhookDto.NewSpendAlertEvent(
		sub, lineItemID, groupID,
		internalEvent.AlertType, internalEvent.AlertStatus,
		internalEvent.AlertInfo.ValueAtTime.String(), internalEvent.AlertInfo.AlertSettings, internalEvent.AlertInfo.Timestamp,
	)

	return json.Marshal(payload)
}
