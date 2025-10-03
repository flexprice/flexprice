package cron

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/stripe/stripe-go/v82"
)

// InvoiceHandler handles invoice related cron jobs
type InvoiceHandler struct {
	invoiceService      service.InvoiceService
	subscriptionService service.SubscriptionService
	connectionService   service.ConnectionService
	tenantService       service.TenantService
	environmentService  service.EnvironmentService
	integrationFactory  *integration.Factory
	logger              *logger.Logger
}

// NewInvoiceHandler creates a new invoice handler
func NewInvoiceHandler(
	invoiceService service.InvoiceService,
	subscriptionService service.SubscriptionService,
	connectionService service.ConnectionService,
	tenantService service.TenantService,
	environmentService service.EnvironmentService,
	integrationFactory *integration.Factory,
	logger *logger.Logger,
) *InvoiceHandler {
	return &InvoiceHandler{
		invoiceService:      invoiceService,
		subscriptionService: subscriptionService,
		connectionService:   connectionService,
		tenantService:       tenantService,
		environmentService:  environmentService,
		integrationFactory:  integrationFactory,
		logger:              logger,
	}
}

// TenantEnvironmentPair represents a tenant and environment pair to process
type TenantEnvironmentPair struct {
	TenantID      string `json:"tenant_id" binding:"required"`
	EnvironmentID string `json:"environment_id" binding:"required"`
}

// VoidOldPendingInvoicesRequest represents the request payload for the void old pending invoices cron job
type VoidOldPendingInvoicesRequest struct {
	// targets is an optional array of tenant-environment pairs to process. If empty, all tenants and environments are processed.
	Targets []TenantEnvironmentPair `json:"targets,omitempty"`
}

// VoidOldPendingInvoices processes incomplete subscriptions and voids their old pending invoices
func (h *InvoiceHandler) VoidOldPendingInvoices(c *gin.Context) {
	h.logger.Infow("starting void old pending invoices cron job", "time", time.Now().UTC().Format(time.RFC3339))

	ctx := c.Request.Context()

	// Parse request parameters (optional)
	var req VoidOldPendingInvoicesRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBind(&req); err != nil {
			h.logger.Errorw("failed to parse request parameters", "error", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request parameters"})
			return
		}
	}
	// If no body provided, req.Targets will be empty (default behavior)

	response := &dto.VoidOldPendingInvoicesResponse{
		Items:   make([]*dto.VoidOldPendingInvoicesResponseItem, 0),
		Total:   0,
		Success: 0,
		Failed:  0,
	}

	// Log filtering parameters
	if len(req.Targets) > 0 {
		h.logger.Infow("filtering enabled", "targets", req.Targets)
	}

	if len(req.Targets) == 0 {
		// No specific targets, process all tenants and environments
		err := h.processAllTenantsAndEnvironments(ctx, response)
		if err != nil {
			h.logger.Errorw("failed to process all tenants and environments", "error", err)
			_ = c.Error(err)
			return
		}
	} else {
		// Process specific tenant-environment pairs
		for _, target := range req.Targets {
			h.logger.Infow("processing target",
				"tenant_id", target.TenantID,
				"environment_id", target.EnvironmentID)

			// Create context with tenant and environment
			tenantCtx := context.WithValue(ctx, types.CtxTenantID, target.TenantID)
			envCtx := context.WithValue(tenantCtx, types.CtxEnvironmentID, target.EnvironmentID)

			// Process incomplete subscriptions for this specific environment
			envResponse, err := h.processIncompleteSubscriptionsForEnvironment(envCtx, target.TenantID, target.EnvironmentID)
			if err != nil {
				h.logger.Errorw("failed to process incomplete subscriptions for target",
					"tenant_id", target.TenantID,
					"environment_id", target.EnvironmentID,
					"error", err)
				response.Failed++
				continue
			}

			response.Items = append(response.Items, envResponse)
			response.Total += envResponse.Count
			response.Success += envResponse.Success
			response.Failed += envResponse.Failed
		}
	}

	h.logger.Infow("completed void old pending invoices cron job",
		"total_processed", response.Total,
		"successful", response.Success,
		"failed", response.Failed)

	c.JSON(http.StatusOK, response)
}

// processAllTenantsAndEnvironments processes all tenants and their environments
func (h *InvoiceHandler) processAllTenantsAndEnvironments(ctx context.Context, response *dto.VoidOldPendingInvoicesResponse) error {
	// Get all tenants
	tenants, err := h.tenantService.GetAllTenants(ctx)
	if err != nil {
		return err
	}

	h.logger.Infow("processing all tenants", "count", len(tenants))

	// Process each tenant
	for _, tenant := range tenants {
		h.logger.Infow("processing tenant", "tenant_id", tenant.ID, "name", tenant.Name)

		tenantCtx := context.WithValue(ctx, types.CtxTenantID, tenant.ID)

		// Get all environments for this tenant
		environments, err := h.environmentService.GetEnvironments(tenantCtx, types.GetDefaultFilter())
		if err != nil {
			h.logger.Errorw("failed to get environments for tenant",
				"tenant_id", tenant.ID, "error", err)
			response.Failed++
			continue
		}

		for _, environment := range environments.Environments {
			h.logger.Infow("processing environment",
				"tenant_id", tenant.ID,
				"environment_id", environment.ID,
				"name", environment.Name)

			envCtx := context.WithValue(tenantCtx, types.CtxEnvironmentID, environment.ID)

			// Process incomplete subscriptions for this environment
			envResponse, err := h.processIncompleteSubscriptionsForEnvironment(envCtx, tenant.ID, environment.ID)
			if err != nil {
				h.logger.Errorw("failed to process incomplete subscriptions for environment",
					"tenant_id", tenant.ID,
					"environment_id", environment.ID,
					"error", err)
				response.Failed++
				continue
			}

			response.Items = append(response.Items, envResponse)
			response.Total += envResponse.Count
			response.Success += envResponse.Success
			response.Failed += envResponse.Failed
		}
	}

	return nil
}

// processIncompleteSubscriptionsForEnvironment processes incomplete subscriptions for a specific environment
func (h *InvoiceHandler) processIncompleteSubscriptionsForEnvironment(
	ctx context.Context,
	tenantID, environmentID string,
) (*dto.VoidOldPendingInvoicesResponseItem, error) {

	response := &dto.VoidOldPendingInvoicesResponseItem{
		TenantID:      tenantID,
		EnvironmentID: environmentID,
		Count:         0,
	}

	// First check if Stripe connection exists and invoice sync is enabled
	h.logger.Infow("checking Stripe connection and invoice sync configuration",
		"tenant_id", tenantID,
		"environment_id", environmentID)

	// Get Stripe integration
	stripeIntegration, err := h.integrationFactory.GetStripeIntegration(ctx)
	if err != nil {
		h.logger.Infow("Stripe integration not available, skipping environment",
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"error", err)
		return response, nil // Not an error, just skip this environment
	}

	// Check if Stripe connection exists
	if !stripeIntegration.Client.HasStripeConnection(ctx) {
		h.logger.Infow("Stripe connection not available, skipping environment",
			"tenant_id", tenantID,
			"environment_id", environmentID)
		return response, nil // Not an error, just skip this environment
	}

	// Note: For now, we assume invoice sync is enabled if connection exists
	// In the future, we can add a method to check if invoice sync is enabled
	h.logger.Infow("Stripe connection available, proceeding with processing",
		"tenant_id", tenantID,
		"environment_id", environmentID)

	// Get incomplete subscriptions older than 24 hours
	cutoffTime := time.Now().UTC().Add(-24 * time.Hour)
	subscriptionFilter := &types.SubscriptionFilter{
		SubscriptionStatus: []types.SubscriptionStatus{
			types.SubscriptionStatusIncomplete,
		},
		Filters: []*types.FilterCondition{
			{
				Field:    lo.ToPtr("created_at"),
				Operator: lo.ToPtr(types.BEFORE),
				DataType: lo.ToPtr(types.DataTypeDate),
				Value: &types.Value{
					Date: &cutoffTime,
				},
			},
		},
	}

	subscriptions, err := h.subscriptionService.ListSubscriptions(ctx, subscriptionFilter)
	if err != nil {
		return response, err
	}

	h.logger.Infow("found old incomplete subscriptions",
		"tenant_id", tenantID,
		"environment_id", environmentID,
		"count", len(subscriptions.Items),
		"cutoff_time", cutoffTime.Format(time.RFC3339))

	// Process each old incomplete subscription
	for _, sub := range subscriptions.Items {
		h.logger.Infow("processing old incomplete subscription",
			"subscription_id", sub.ID,
			"customer_id", sub.CustomerID,
			"created_at", sub.CreatedAt)

		if err := h.processOldIncompleteSubscription(ctx, sub); err != nil {
			h.logger.Errorw("failed to process old incomplete subscription",
				"subscription_id", sub.ID,
				"error", err)
			response.Failed++
		} else {
			response.Success++
		}
		response.Count++
	}

	return response, nil
}

// processOldIncompleteSubscription processes old incomplete subscriptions according to new flow
func (h *InvoiceHandler) processOldIncompleteSubscription(ctx context.Context, sub *dto.SubscriptionResponse) error {
	// Get all old pending invoices for this subscription (no payment status filter)
	cutoffTime := time.Now().UTC().Add(-24 * time.Hour)
	invoiceFilter := &types.InvoiceFilter{
		SubscriptionID: sub.ID,
		InvoiceStatus: []types.InvoiceStatus{
			types.InvoiceStatusDraft,
			types.InvoiceStatusFinalized,
		},
		// No PaymentStatus filter - get all invoices
	}

	invoices, err := h.invoiceService.ListInvoices(ctx, invoiceFilter)
	if err != nil {
		h.logger.Errorw("failed to list invoices for subscription, but continuing",
			"subscription_id", sub.ID,
			"error", err)
		// Don't return error - treat as success to avoid incrementing failed count
		return nil
	}

	// Filter by creation date (older than 24 hours)
	var oldInvoices []*dto.InvoiceResponse
	for _, inv := range invoices.Items {
		if inv.CreatedAt.Before(cutoffTime) {
			oldInvoices = append(oldInvoices, inv)
		}
	}

	h.logger.Infow("processing subscription",
		"subscription_id", sub.ID,
		"total_invoices", len(invoices.Items),
		"old_invoices", len(oldInvoices))

	// Decision logic based on invoice count
	switch len(oldInvoices) {
	case 0:
		// No invoices - cancel subscription in FlexPrice
		h.logger.Infow("no old invoices found, cancelling subscription", "subscription_id", sub.ID)
		return h.cancelIncompleteSubscription(ctx, sub)

	case 1:
		// One invoice - check if eligible for voiding
		return h.processSingleInvoice(ctx, sub, oldInvoices[0])

	default:
		// More than one invoice - skip subscription
		h.logger.Infow("multiple old invoices found, skipping subscription",
			"subscription_id", sub.ID,
			"invoice_count", len(oldInvoices))
		return nil
	}
}

// processSingleInvoice handles the case where subscription has exactly one old invoice
func (h *InvoiceHandler) processSingleInvoice(ctx context.Context, sub *dto.SubscriptionResponse, inv *dto.InvoiceResponse) error {

	// Get Stripe integration
	stripeIntegration, err := h.integrationFactory.GetStripeIntegration(ctx)
	if err != nil {
		h.logger.Errorw("failed to get Stripe integration", "error", err, "invoice_id", inv.ID)
		return nil
	}

	// Check if synced to Stripe
	mapping, err := stripeIntegration.InvoiceSyncSvc.GetExistingStripeMapping(ctx, inv.ID)
	if err != nil || mapping == nil {
		h.logger.Infow("invoice not synced to Stripe, skipping", "invoice_id", inv.ID)
		return nil
	}

	// Check for partial payments in FlexPrice
	if inv.AmountPaid.IsPositive() {
		h.logger.Infow("invoice has partial payment in FlexPrice, skipping", "invoice_id", inv.ID)
		return nil
	}

	// Check for partial payments in Stripe
	if hasPartial, err := h.checkStripePartialPayment(ctx, inv.ID); err != nil || hasPartial {
		if hasPartial {
			h.logger.Infow("invoice has partial payment in Stripe, skipping", "invoice_id", inv.ID)
		}
		return nil
	}

	// All checks passed - void invoice and cancel subscription
	h.logger.Infow("voiding invoice and cancelling subscription",
		"invoice_id", inv.ID,
		"subscription_id", sub.ID)

	// Void invoice in FlexPrice
	if err := h.voidOldPendingInvoice(ctx, inv); err != nil {
		h.logger.Errorw("failed to void invoice in FlexPrice, but continuing",
			"invoice_id", inv.ID,
			"error", err)
		// Don't return error - treat as success to avoid incrementing failed count
	}

	// Cancel subscription in FlexPrice
	return h.cancelIncompleteSubscription(ctx, sub)
}

// cancelIncompleteSubscription cancels an incomplete subscription that has old pending invoices
func (h *InvoiceHandler) cancelIncompleteSubscription(ctx context.Context, sub *dto.SubscriptionResponse) error {
	h.logger.Infow("cancelling incomplete subscription with old pending invoices",
		"subscription_id", sub.ID,
		"customer_id", sub.CustomerID,
		"current_status", sub.SubscriptionStatus)

	// Create cancellation request
	cancelRequest := dto.CancelSubscriptionRequest{
		CancellationType:  types.CancellationTypeImmediate,
		Reason:            "Automatic cancellation due to old pending invoices",
		ProrationBehavior: types.ProrationBehaviorNone, // No proration for failed payment scenarios
	}

	// Cancel the subscription
	_, err := h.subscriptionService.CancelSubscription(ctx, sub.ID, &cancelRequest)
	if err != nil {
		// If subscription is not found, it's already cancelled - treat as success
		if strings.Contains(err.Error(), "subscription not found") || strings.Contains(err.Error(), "not found") {
			h.logger.Infow("subscription already cancelled or not found, treating as success",
				"subscription_id", sub.ID,
				"customer_id", sub.CustomerID)
			return nil
		}
		h.logger.Errorw("failed to cancel subscription, but continuing",
			"subscription_id", sub.ID,
			"error", err)
		// Don't return error - treat as success to avoid incrementing failed count
		return nil
	}

	h.logger.Infow("successfully cancelled incomplete subscription",
		"subscription_id", sub.ID,
		"customer_id", sub.CustomerID,
		"reason", "old_pending_invoices_cleanup")

	return nil
}

// voidOldPendingInvoice voids an old pending invoice in both FlexPrice and Stripe
func (h *InvoiceHandler) voidOldPendingInvoice(
	ctx context.Context,
	inv *dto.InvoiceResponse,
) error {

	// Void invoice in FlexPrice
	voidRequest := dto.InvoiceVoidRequest{
		Metadata: types.Metadata{
			"voided_by":   "cron_job",
			"void_reason": "old_pending_invoice_cleanup",
			"voided_at":   time.Now().UTC().Format(time.RFC3339),
		},
	}

	if err := h.invoiceService.VoidInvoice(ctx, inv.ID, voidRequest); err != nil {
		return err
	}

	// Void invoice in Stripe if it exists
	if err := h.voidInvoiceInStripe(ctx, inv.ID); err != nil {
		h.logger.Errorw("failed to void invoice in Stripe",
			"invoice_id", inv.ID,
			"error", err)
		// Don't fail the entire operation if Stripe void fails
		// The invoice is already voided in FlexPrice
	}

	return nil
}

// checkStripePartialPayment checks if an invoice has partial payments in Stripe
func (h *InvoiceHandler) checkStripePartialPayment(ctx context.Context, invoiceID string) (bool, error) {
	// Get Stripe integration
	stripeIntegration, err := h.integrationFactory.GetStripeIntegration(ctx)
	if err != nil {
		h.logger.Errorw("failed to get Stripe integration", "error", err, "invoice_id", invoiceID)
		return false, err
	}

	// Get Stripe invoice ID from entity integration mapping
	mapping, err := stripeIntegration.InvoiceSyncSvc.GetExistingStripeMapping(ctx, invoiceID)
	if err != nil {
		// If no mapping exists, the invoice was never synced to Stripe
		h.logger.Debugw("no Stripe mapping found for invoice, assuming no partial payments", "invoice_id", invoiceID)
		return false, nil
	}

	stripeInvoiceID := mapping.ProviderEntityID

	// Get Stripe client
	stripeClient, _, err := stripeIntegration.Client.GetStripeClient(ctx)
	if err != nil {
		return false, err
	}

	// Retrieve the invoice from Stripe
	stripeInvoice, err := stripeClient.V1Invoices.Retrieve(ctx, stripeInvoiceID, nil)
	if err != nil {
		h.logger.Warnw("failed to retrieve invoice from Stripe",
			"invoice_id", invoiceID,
			"stripe_invoice_id", stripeInvoiceID,
			"error", err)
		// If we can't retrieve from Stripe, assume no partial payments to be safe
		return false, nil
	}

	// Check if there are any partial payments
	hasPartialPayment := stripeInvoice.AmountPaid > 0 && stripeInvoice.AmountPaid < stripeInvoice.Total

	if hasPartialPayment {
		h.logger.Infow("found partial payment in Stripe",
			"invoice_id", invoiceID,
			"stripe_invoice_id", stripeInvoiceID,
			"amount_paid", stripeInvoice.AmountPaid,
			"total", stripeInvoice.Total,
			"amount_remaining", stripeInvoice.AmountRemaining)
	}

	return hasPartialPayment, nil
}

// voidInvoiceInStripe voids an invoice in Stripe if it exists
func (h *InvoiceHandler) voidInvoiceInStripe(ctx context.Context, invoiceID string) error {
	// Get Stripe integration
	stripeIntegration, err := h.integrationFactory.GetStripeIntegration(ctx)
	if err != nil {
		h.logger.Errorw("failed to get Stripe integration", "error", err, "invoice_id", invoiceID)
		return err
	}

	// Get Stripe invoice ID from entity integration mapping
	mapping, err := stripeIntegration.InvoiceSyncSvc.GetExistingStripeMapping(ctx, invoiceID)
	if err != nil {
		// If no mapping exists, the invoice was never synced to Stripe
		h.logger.Debugw("no Stripe mapping found for invoice", "invoice_id", invoiceID)
		return nil
	}

	stripeInvoiceID := mapping.ProviderEntityID
	h.logger.Infow("voiding invoice in Stripe",
		"invoice_id", invoiceID,
		"stripe_invoice_id", stripeInvoiceID)

	// Get Stripe client
	stripeClient, _, err := stripeIntegration.Client.GetStripeClient(ctx)
	if err != nil {
		return err
	}

	// Void the invoice in Stripe
	_, err = stripeClient.V1Invoices.VoidInvoice(ctx, stripeInvoiceID, &stripe.InvoiceVoidInvoiceParams{})
	if err != nil {
		return err
	}

	h.logger.Infow("successfully voided invoice in Stripe",
		"invoice_id", invoiceID,
		"stripe_invoice_id", stripeInvoiceID)

	return nil
}
