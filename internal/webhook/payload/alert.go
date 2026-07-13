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

// buildSpendAlertPayload resolves an InternalAlertEvent carrying a subscription/line-item/group
// spend alert into its final webhook payload.
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
	// Same bloat workaround SubscriptionPayloadBuilder already applies to this same type.
	sub.Plan = nil

	payload := &webhookDto.SpendAlertEvent{
		Subscription:  sub,
		AlertType:     internalEvent.AlertType,
		AlertStatus:   internalEvent.AlertStatus,
		AlertSettings: internalEvent.AlertInfo.AlertSettings,
		CurrentSpend:  internalEvent.AlertInfo.ValueAtTime.String(),
		TriggeredAt:   internalEvent.AlertInfo.Timestamp,
	}

	switch internalEvent.EntityType {
	case types.AlertEntityTypeSubscriptionLineItem:
		lineItems, err := b.services.SubscriptionService.ListSubscriptionLineItems(ctx, &types.SubscriptionLineItemFilter{
			QueryFilter:             types.NewNoLimitQueryFilter(),
			SubscriptionLineItemIDs: []string{internalEvent.EntityID},
		})
		if err != nil {
			return nil, err
		}
		if len(lineItems.Items) == 0 {
			return nil, ierr.NewError("subscription line item not found").
				WithHint("Please provide a valid subscription line item").
				WithReportableDetails(map[string]any{"subscription_line_item_id": internalEvent.EntityID}).
				Mark(ierr.ErrNotFound)
		}
		payload.SubscriptionLineItem = lineItems.Items[0].SubscriptionLineItem
	case types.AlertEntityTypeGroup:
		grp, err := b.services.GroupService.GetGroup(ctx, internalEvent.EntityID)
		if err != nil {
			return nil, err
		}
		payload.Group = grp
	}

	return json.Marshal(payload)
}
