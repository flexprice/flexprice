package webhook

import (
	"context"
	"encoding/json"

	paddlesdk "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/PaddleHQ/paddle-go-sdk/v4/pkg/paddlenotification"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/integration/paddle"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
)

// Handler handles Paddle webhook events
type Handler struct {
	paymentSvc       *paddle.PaymentService
	syncSvc          *paddle.PaddleSyncService
	subscriptionRepo subscription.Repository
	logger           *logger.Logger
}

// NewHandler creates a new Paddle webhook handler
func NewHandler(
	paymentSvc *paddle.PaymentService,
	syncSvc *paddle.PaddleSyncService,
	subscriptionRepo subscription.Repository,
	logger *logger.Logger,
) *Handler {
	return &Handler{
		paymentSvc:       paymentSvc,
		syncSvc:          syncSvc,
		subscriptionRepo: subscriptionRepo,
		logger:           logger,
	}
}

// ServiceDependencies contains all service dependencies needed by webhook handlers
type ServiceDependencies = interfaces.ServiceDependencies

// HandleWebhookEvent processes a Paddle webhook event.
// This function never returns errors to ensure webhooks always return 200 OK.
// All errors are logged internally to prevent Paddle from retrying.
func (h *Handler) HandleWebhookEvent(ctx context.Context, eventType string, payload []byte, environmentID string, services *ServiceDependencies) error {
	h.logger.Info(ctx, "processing Paddle webhook event",
		"event_type", eventType,
		"environment_id", environmentID)

	switch eventType {
	case string(EventTransactionCompleted):
		return h.handleTransactionCompleted(ctx, payload, services)
	case string(EventCustomerCreated):
		return h.handleCustomerCreated(ctx, payload, services)
	case string(EventAddressCreated):
		return h.handleAddressCreated(ctx, payload, services)
	case string(EventSubscriptionActivated):
		return h.handleSubscriptionActivated(ctx, payload, services)
	default:
		h.logger.Debug(ctx, "ignoring unhandled Paddle event", "type", eventType)
		return nil
	}
}

func (h *Handler) handleTransactionCompleted(ctx context.Context, payload []byte, services *ServiceDependencies) error {
	var event paddlesdk.TransactionCompletedEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		h.logger.Error(ctx, "failed to parse transaction.completed payload",
			"error", err, "event_type", EventTransactionCompleted)
		return nil
	}
	// temp fix
	// todo: handle using payment
	// if txn is the bootstrap txn of an internal subscription, meaning a 0$ txn
	// no invoice linked to this payment
	// skip this webhook( subscription gets activated using subscription.activated webhook event)
	if h.isSubscriptionBootstrapTransaction(ctx, event.Data.CustomData, event.Data.ID) {
		h.logger.Info(ctx, "skipping transaction.completed for subscription bootstrap transaction",
			"paddle_transaction_id", event.Data.ID)
		return nil
	}
	err := h.syncSvc.ProcessTransactionCompletedWebhook(ctx, event.Data.ID, services.PaymentService, services.InvoiceService)
	if err != nil {
		h.logger.Error(ctx, "failed to process transaction.completed webhook",
			"error", err, "paddle_transaction_id", event.Data.ID)
		return err
	}
	return nil
}

// isSubscriptionBootstrapTransaction reports whether the completed transaction is the
// bootstrap transaction stored on the FlexPrice subscription referenced in custom_data.
// Such transactions have no linked invoice — the subscription is activated by the
// subscription.activated webhook instead.
func (h *Handler) isSubscriptionBootstrapTransaction(ctx context.Context, customData map[string]any, paddleTransactionID string) bool {
	if customData == nil || h.subscriptionRepo == nil {
		return false
	}

	subID := h.syncSvc.ExtractFlexSubIDFromCustomData(customData)
	if subID == "" {
		return false
	}

	sub, err := h.subscriptionRepo.Get(ctx, subID)
	if err != nil {
		h.logger.Error(ctx, "failed to lookup subscription for transaction.completed webhook",
			"error", err, "subscription_id", subID, "paddle_transaction_id", paddleTransactionID)
		return false
	}
	if sub == nil || sub.Metadata == nil {
		return false
	}

	return sub.Metadata[paddle.MetaKeyPaddleTransactionID] == paddleTransactionID
}

func (h *Handler) handleCustomerCreated(ctx context.Context, payload []byte, services *ServiceDependencies) error {
	if services == nil || services.CustomerService == nil {
		h.logger.Info(ctx, "customer service not available for customer.created webhook")
		return nil
	}
	var event paddlenotification.CustomerCreated
	if err := json.Unmarshal(payload, &event); err != nil {
		h.logger.Error(ctx, "failed to parse customer.created payload",
			"error", err, "event_type", EventCustomerCreated)
		return nil
	}
	err := h.syncSvc.ProcessCustomerCreatedWebhook(ctx, &event.Data, services.CustomerService)
	if err != nil {
		h.logger.Error(ctx, "failed to process customer.created webhook",
			"error", err, "paddle_customer_id", event.Data.ID)
	}
	return nil
}

func (h *Handler) handleSubscriptionActivated(ctx context.Context, payload []byte, services *ServiceDependencies) error {
	if services == nil || services.SubscriptionService == nil {
		h.logger.Info(ctx, "subscription service not available for subscription.activated webhook")
		return nil
	}
	var event paddlenotification.SubscriptionActivated
	if err := json.Unmarshal(payload, &event); err != nil {
		h.logger.Error(ctx, "failed to parse subscription.activated payload",
			"error", err, "event_type", EventSubscriptionActivated)
		return nil
	}
	err := h.syncSvc.ProcessSubscriptionActivatedWebhook(ctx, &event.Data, services.SubscriptionService)
	if err != nil {
		h.logger.Error(ctx, "failed to process subscription.activated webhook",
			"error", err, "paddle_sub_id", event.Data.ID)
	}
	return nil
}

func (h *Handler) handleAddressCreated(ctx context.Context, payload []byte, services *ServiceDependencies) error {
	if services == nil || services.CustomerService == nil {
		h.logger.Info(ctx, "customer service not available for address.created webhook")
		return nil
	}
	var event paddlenotification.AddressCreated
	if err := json.Unmarshal(payload, &event); err != nil {
		h.logger.Error(ctx, "failed to parse address.created payload",
			"error", err, "event_type", EventAddressCreated)
		return nil
	}
	err := h.syncSvc.ProcessAddressCreatedWebhook(ctx, event.Data.CustomerID, &event.Data, services.CustomerService)
	if err != nil {
		h.logger.Error(ctx, "failed to process address.created webhook",
			"error", err, "paddle_customer_id", event.Data.CustomerID)
	}
	return nil
}
