package webhook

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	whop "github.com/flexprice/flexprice/internal/integration/whop"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// WhopWebhookEvent is the top-level Whop webhook payload.
type WhopWebhookEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	CompanyID string          `json:"company_id"`
	Data      json.RawMessage `json:"data"`
}

// WhopInvoiceData is the data object for invoice.paid events
type WhopInvoiceData struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// paymentSucceededPlanRef is the plan reference inside a payment.succeeded event payload
type paymentSucceededPlanRef struct {
	ID string `json:"id"`
}

// paymentSucceededMemberRef is the member reference inside a payment.succeeded event payload
type paymentSucceededMemberRef struct {
	ID string `json:"id"`
}

// paymentSucceededEventData is the data object of a payment.succeeded webhook event.
// Only the fields used for customer→member mapping are mapped; the rest are ignored.
type paymentSucceededEventData struct {
	ID     string                    `json:"id"`
	Plan   paymentSucceededPlanRef   `json:"plan"`
	Member paymentSucceededMemberRef `json:"member"`
}

// ServiceDependencies mirrors interfaces.ServiceDependencies for webhook handlers
type ServiceDependencies = interfaces.ServiceDependencies

// Handler handles Whop webhook events
type Handler struct {
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	invoiceSyncService           *whop.InvoiceSyncService
	client                       whop.WhopClient
	logger                       *logger.Logger
}

func NewHandler(
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	invoiceSyncService *whop.InvoiceSyncService,
	client whop.WhopClient,
	logger *logger.Logger,
) *Handler {
	return &Handler{
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		invoiceSyncService:           invoiceSyncService,
		client:                       client,
		logger:                       logger,
	}
}

// HandleWebhookEvent processes an inbound Whop webhook event.
// Always returns nil so Whop receives 200 OK.
func (h *Handler) HandleWebhookEvent(ctx context.Context, event *WhopWebhookEvent, services *ServiceDependencies) error {
	h.logger.Infow("processing Whop webhook event",
		"type", event.Type,
		"msg_id", event.ID,
		"company_id", event.CompanyID,
	)

	switch event.Type {
	case whop.WhopEventInvoicePaid:
		var data WhopInvoiceData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			h.logger.Errorw("failed to parse invoice.paid data", "error", err)
			return nil
		}
		return h.handleInvoicePaid(ctx, &data, services)

	case whop.WhopEventPaymentSucceeded:
		var data paymentSucceededEventData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			h.logger.Errorw("failed to parse payment.succeeded data", "error", err)
			return nil
		}
		return h.handlePaymentSucceeded(ctx, &data, services)

	default:
		h.logger.Infow("unhandled Whop webhook type, skipping", "type", event.Type)
	}
	return nil
}

// handleInvoicePaid marks the corresponding Flexprice invoice as paid.
func (h *Handler) handleInvoicePaid(ctx context.Context, data *WhopInvoiceData, services *ServiceDependencies) error {
	h.logger.Infow("handling Whop invoice.paid", "whop_invoice_id", data.ID)

	// Look up Flexprice invoice via entity_integration_mapping
	filter := &types.EntityIntegrationMappingFilter{
		ProviderTypes:     []string{string(types.SecretProviderWhop)},
		ProviderEntityIDs: []string{data.ID},
		EntityType:        types.IntegrationEntityTypeInvoice,
	}
	mappings, err := h.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		h.logger.Errorw("failed to look up entity mapping for Whop invoice",
			"error", err, "whop_invoice_id", data.ID)
		return nil
	}
	if len(mappings) == 0 {
		h.logger.Warnw("no Flexprice invoice found for Whop invoice, skipping",
			"whop_invoice_id", data.ID)
		return nil
	}
	flexpriceInvoiceID := mappings[0].EntityID

	// Idempotency: skip if already paid
	inv, err := services.InvoiceService.GetInvoice(ctx, flexpriceInvoiceID)
	if err != nil {
		h.logger.Errorw("failed to get Flexprice invoice",
			"error", err, "invoice_id", flexpriceInvoiceID)
		return nil
	}
	if inv.PaymentStatus == types.PaymentStatusSucceeded {
		h.logger.Infow("Flexprice invoice already paid, skipping",
			"invoice_id", flexpriceInvoiceID, "whop_invoice_id", data.ID)
		return nil
	}

	// Mark invoice as paid
	amount := inv.AmountDue
	if err := services.InvoiceService.ReconcilePaymentStatus(ctx, flexpriceInvoiceID, types.PaymentStatusSucceeded, &amount); err != nil {
		h.logger.Errorw("failed to reconcile invoice payment status",
			"error", err, "invoice_id", flexpriceInvoiceID, "whop_invoice_id", data.ID)
		return nil
	}

	h.logger.Infow("successfully marked Flexprice invoice as paid from Whop",
		"invoice_id", flexpriceInvoiceID, "whop_invoice_id", data.ID)
	return nil
}

// handlePaymentSucceeded creates a customer→Whop member mapping so that future invoices
// for this customer are charged automatically instead of sent as hosted-checkout links.
//
// Flow:
//  1. Read plan.id and member.id from the webhook payload
//  2. GET /v1/plans/:plan_id → internal_notes holds the Flexprice customer_id
//  3. Idempotency check — skip if mapping already exists
//  4. Create entity mapping: customer_id → member_id
func (h *Handler) handlePaymentSucceeded(ctx context.Context, data *paymentSucceededEventData, _ *ServiceDependencies) error {
	h.logger.Infow("handling Whop payment.succeeded",
		"payment_id", data.ID,
		"plan_id", data.Plan.ID,
		"member_id", data.Member.ID)

	if data.Plan.ID == "" {
		h.logger.Warnw("payment.succeeded has no plan id, cannot resolve customer", "payment_id", data.ID)
		return nil
	}
	if data.Member.ID == "" {
		h.logger.Warnw("payment.succeeded has no member id, cannot create customer mapping", "payment_id", data.ID)
		return nil
	}

	// Fetch the plan — internal_notes holds the Flexprice customer_id
	plan, err := h.client.GetPlan(ctx, data.Plan.ID)
	if err != nil {
		h.logger.Errorw("failed to fetch Whop plan for customer_id resolution",
			"error", err, "plan_id", data.Plan.ID)
		return nil
	}
	customerID := plan.InternalNotes
	if customerID == "" {
		h.logger.Warnw("Whop plan has no internal_notes (customer_id), skipping mapping",
			"plan_id", data.Plan.ID)
		return nil
	}

	// Idempotency: skip if mapping already exists
	existingFilter := &types.EntityIntegrationMappingFilter{
		QueryFilter: &types.QueryFilter{
			Limit:  lo.ToPtr(1),
			Status: lo.ToPtr(types.StatusPublished),
		},
		EntityIDs:     []string{customerID},
		EntityType:    types.IntegrationEntityTypeCustomer,
		ProviderTypes: []string{string(types.SecretProviderWhop)},
	}
	existing, err := h.entityIntegrationMappingRepo.List(ctx, existingFilter)
	if err != nil {
		h.logger.Errorw("failed to check existing customer mapping", "error", err, "customer_id", customerID)
		return nil
	}
	if len(existing) > 0 {
		h.logger.Infow("customer→Whop member mapping already exists, skipping", "customer_id", customerID)
		return nil
	}

	// Create mapping: entity_type=customer, provider_entity_id=member_id (used to fetch payment methods)
	if err := h.invoiceSyncService.CreateCustomerMapping(ctx, customerID, data.Member.ID); err != nil {
		h.logger.Errorw("failed to create customer→Whop mapping",
			"error", err, "customer_id", customerID, "member_id", data.Member.ID)
		return nil
	}

	h.logger.Infow("created customer→Whop member mapping from payment.succeeded",
		"customer_id", customerID, "member_id", data.Member.ID)
	return nil
}
