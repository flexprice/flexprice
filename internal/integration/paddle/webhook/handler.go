package webhook

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	paddlesdk "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/PaddleHQ/paddle-go-sdk/v4/pkg/paddlenotification"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/integration/paddle"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// Handler handles Paddle webhook events
type Handler struct {
	paymentSvc                   *paddle.PaymentService
	customerSvc                  paddle.PaddleCustomerService
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewHandler creates a new Paddle webhook handler
func NewHandler(
	paymentSvc *paddle.PaymentService,
	customerSvc paddle.PaddleCustomerService,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) *Handler {
	return &Handler{
		paymentSvc:                   paymentSvc,
		customerSvc:                  customerSvc,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
	}
}

// ServiceDependencies contains all service dependencies needed by webhook handlers
type ServiceDependencies = interfaces.ServiceDependencies

// HandleWebhookEvent processes a Paddle webhook event.
// This function never returns errors to ensure webhooks always return 200 OK.
// All errors are logged internally to prevent Paddle from retrying.
func (h *Handler) HandleWebhookEvent(ctx context.Context, eventType string, payload []byte, environmentID string, services *ServiceDependencies) error {
	h.logger.Infow("processing Paddle webhook event",
		"event_type", eventType,
		"environment_id", environmentID)

	switch eventType {
	case string(EventTransactionCompleted):
		return h.handleTransactionCompleted(ctx, payload, services)
	case string(EventCustomerCreated):
		return h.handleCustomerCreated(ctx, payload, services)
	case string(EventAddressCreated):
		return h.handleAddressCreated(ctx, payload, services)
	default:
		h.logger.Debugw("ignoring unhandled Paddle event", "type", eventType)
		return nil
	}
}

func (h *Handler) handleTransactionCompleted(ctx context.Context, payload []byte, services *ServiceDependencies) error {
	var event paddlesdk.TransactionCompletedEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		h.logger.Errorw("failed to parse transaction.completed payload",
			"error", err,
			"event_type", EventTransactionCompleted)
		return nil
	}

	txn := &event.Data
	txnID := txn.ID

	filter := &types.EntityIntegrationMappingFilter{
		ProviderTypes:     []string{string(types.SecretProviderPaddle)},
		ProviderEntityIDs: []string{txnID},
		EntityType:        types.IntegrationEntityTypeInvoice,
	}

	mappings, err := h.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		h.logger.Errorw("failed to find mapping for Paddle transaction",
			"error", err,
			"paddle_transaction_id", txnID)
		return nil
	}

	if len(mappings) == 0 {
		h.logger.Warnw("no FlexPrice invoice found for Paddle transaction, skipping",
			"paddle_transaction_id", txnID)
		return nil
	}

	flexpriceInvoiceID := mappings[0].EntityID

	err = h.paymentSvc.ProcessExternalPaddleTransaction(ctx, txn, flexpriceInvoiceID, services.PaymentService, services.InvoiceService)
	if err != nil {
		h.logger.Errorw("failed to process external Paddle transaction",
			"error", err,
			"flexprice_invoice_id", flexpriceInvoiceID,
			"paddle_transaction_id", txnID)
		return nil
	}

	h.logger.Infow("successfully processed transaction.completed",
		"flexprice_invoice_id", flexpriceInvoiceID,
		"paddle_transaction_id", txnID)

	return nil
}

func (h *Handler) handleCustomerCreated(ctx context.Context, payload []byte, services *ServiceDependencies) error {
	if services == nil || services.CustomerService == nil {
		h.logger.Errorw("customer service not available for customer.created webhook")
		return nil
	}
	var event paddlenotification.CustomerCreated
	if err := json.Unmarshal(payload, &event); err != nil {
		h.logger.Errorw("failed to parse customer.created payload",
			"error", err,
			"event_type", EventCustomerCreated)
		return nil
	}

	h.logger.Infow("received customer.created webhook",
		"paddle_customer_id", event.Data.ID,
		"customer_email", event.Data.Email)

	err := h.customerSvc.CreateCustomerFromPaddle(ctx, &event.Data, services.CustomerService)
	if err != nil {
		h.logger.Errorw("failed to create customer from Paddle webhook",
			"error", err,
			"paddle_customer_id", event.Data.ID)
		return nil
	}

	h.logger.Infow("successfully created customer from Paddle webhook",
		"paddle_customer_id", event.Data.ID,
		"customer_email", event.Data.Email)

	return nil
}

func (h *Handler) handleAddressCreated(ctx context.Context, payload []byte, services *ServiceDependencies) error {
	if services == nil || services.CustomerService == nil {
		h.logger.Errorw("customer service not available for address.created webhook")
		return nil
	}
	var event paddlenotification.AddressCreated
	if err := json.Unmarshal(payload, &event); err != nil {
		h.logger.Errorw("failed to parse address.created payload",
			"error", err,
			"event_type", EventAddressCreated)
		return nil
	}

	paddleCustomerID := event.Data.CustomerID

	// Lookup FlexPrice customer by Paddle customer_id
	filter := &types.EntityIntegrationMappingFilter{
		ProviderTypes:     []string{string(types.SecretProviderPaddle)},
		ProviderEntityIDs: []string{paddleCustomerID},
		EntityType:        types.IntegrationEntityTypeCustomer,
	}

	mappings, err := h.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		h.logger.Errorw("failed to find mapping for Paddle customer",
			"error", err,
			"paddle_customer_id", paddleCustomerID)
		return nil
	}

	if len(mappings) == 0 {
		h.logger.Warnw("no FlexPrice customer found for Paddle address, skipping",
			"paddle_customer_id", paddleCustomerID,
			"paddle_address_id", event.Data.ID)
		return nil
	}

	flexpriceCustomerID := mappings[0].EntityID

	// Update customer's embedded address fields (Flexprice has no separate Address entity)
	updateReq := mapToUpdateCustomerAddressRequest(&event.Data)
	_, err = services.CustomerService.UpdateCustomer(ctx, flexpriceCustomerID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update customer address from Paddle webhook",
			"error", err,
			"flexprice_customer_id", flexpriceCustomerID,
			"paddle_address_id", event.Data.ID)
		return nil
	}

	// Store paddle_address_id in the customer's entity integration mapping so invoice sync can find it
	existingMapping := mappings[0]
	if existingMapping.Metadata == nil {
		existingMapping.Metadata = make(map[string]interface{})
	}
	existingMapping.Metadata["paddle_address_id"] = event.Data.ID
	existingMapping.UpdatedAt = time.Now().UTC()
	if err := h.entityIntegrationMappingRepo.Update(ctx, existingMapping); err != nil {
		h.logger.Errorw("failed to update customer mapping with paddle_address_id",
			"error", err,
			"flexprice_customer_id", flexpriceCustomerID,
			"paddle_address_id", event.Data.ID)
		// Don't fail - customer address was updated successfully
	}

	h.logger.Infow("successfully updated customer address from Paddle webhook",
		"flexprice_customer_id", flexpriceCustomerID,
		"paddle_address_id", event.Data.ID)

	return nil
}

// mapToUpdateCustomerAddressRequest maps Paddle AddressNotification to Flexprice UpdateCustomerRequest.
// Flexprice has no separate Address entity; address is embedded on Customer.
func mapToUpdateCustomerAddressRequest(addr *paddlenotification.AddressNotification) dto.UpdateCustomerRequest {
	req := dto.UpdateCustomerRequest{}
	if addr.FirstLine != nil && *addr.FirstLine != "" {
		req.AddressLine1 = addr.FirstLine
	}
	if addr.SecondLine != nil && *addr.SecondLine != "" {
		req.AddressLine2 = addr.SecondLine
	}
	if addr.City != nil && *addr.City != "" {
		req.AddressCity = addr.City
	}
	if addr.Region != nil && *addr.Region != "" {
		req.AddressState = addr.Region
	}
	if addr.PostalCode != nil && *addr.PostalCode != "" {
		req.AddressPostalCode = addr.PostalCode
	}
	if addr.CountryCode != "" {
		req.AddressCountry = lo.ToPtr(strings.ToUpper(string(addr.CountryCode)))
	}
	return req
}
