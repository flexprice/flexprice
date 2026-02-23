package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type SubscriptionHandler struct {
	service service.SubscriptionService
	log     *logger.Logger
}

func NewSubscriptionHandler(service service.SubscriptionService, log *logger.Logger) *SubscriptionHandler {
	return &SubscriptionHandler{
		service: service,
		log:     log,
	}
}

// @Summary Create subscription
// @ID createSubscription
// @Description Use when onboarding a customer to a plan or starting a new subscription. Ideal for draft subscriptions (activate later) or active from start.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param subscription body dto.CreateSubscriptionRequest true "Subscription Request"
// @Success 201 {object} dto.SubscriptionResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions [post]
func (h *SubscriptionHandler) CreateSubscription(c *gin.Context) {
	var req dto.CreateSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateSubscription(c.Request.Context(), req)
	if err != nil {
		h.log.Error("Failed to create subscription", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get subscription
// @ID getSubscription
// @Description Use when you need to load a single subscription (e.g. for a billing portal or to check status).
// @Tags Subscriptions
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Success 200 {object} dto.SubscriptionResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id} [get]
func (h *SubscriptionHandler) GetSubscription(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetSubscription(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to get subscription", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update subscription
// @ID updateSubscription
// @Description Use when changing subscription details (e.g. quantity, billing anchor, or parent). Supports partial update; send "" to clear parent_subscription_id.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Param request body dto.UpdateSubscriptionRequest true "Update Subscription Request"
// @Success 200 {object} dto.SubscriptionResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id} [put]
func (h *SubscriptionHandler) UpdateSubscription(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdateSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateSubscription(c.Request.Context(), id, req)
	if err != nil {
		h.log.Error("Failed to update subscription", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get subscription (V2)
// @ID getSubscriptionV2
// @Description Use when you need a subscription with related data (line items, prices, plan). Supports expand for detailed payloads without extra round-trips.
// @Tags Subscriptions
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Param expand query string false "Comma-separated list of fields to expand (e.g., 'subscription_line_items,prices,plan')"
// @Success 200 {object} dto.SubscriptionResponseV2
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/v2 [get]
func (h *SubscriptionHandler) GetSubscriptionV2(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	// Parse expand query parameter
	expandStr := c.Query("expand")
	expand := types.NewExpand(expandStr)

	resp, err := h.service.GetSubscriptionV2(c.Request.Context(), id, expand)
	if err != nil {
		h.log.Error("Failed to get subscription v2", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *SubscriptionHandler) ListSubscriptions(c *gin.Context) {
	var filter types.SubscriptionFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		h.log.Error("Failed to bind query", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.ListSubscriptions(c.Request.Context(), &filter)
	if err != nil {
		h.log.Error("Failed to list subscriptions", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Cancel subscription
// @ID cancelSubscription
// @Description Use when a customer churns or downgrades. Supports immediate or end-of-period cancellation and proration. Ideal for self-serve or support-driven cancellations.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Param request body dto.CancelSubscriptionRequest true "Cancel Subscription Request"
// @Success 200 {object} dto.CancelSubscriptionResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/cancel [post]
func (h *SubscriptionHandler) CancelSubscription(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.CancelSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	// Always use the enhanced cancellation method with proration support
	response, err := h.service.CancelSubscription(c.Request.Context(), id, &req)
	if err != nil {
		h.log.Error("Failed to cancel subscription", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// @Summary Activate draft subscription
// @ID activateSubscription
// @Description Use when turning a draft subscription live (e.g. after collecting payment or completing setup). Once activated, billing and entitlements apply.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Param request body dto.ActivateDraftSubscriptionRequest true "Activate Draft Subscription Request"
// @Success 200 {object} dto.SubscriptionResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/activate [post]
func (h *SubscriptionHandler) ActivateDraftSubscription(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.ActivateDraftSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.ActivateDraftSubscription(c.Request.Context(), id, req)
	if err != nil {
		h.log.Error("Failed to activate draft subscription", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get usage by subscription
// @ID getSubscriptionUsage
// @Description Use when showing usage for a subscription (e.g. in a portal or for overage checks). Supports time range and filters.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetUsageBySubscriptionRequest true "Usage request"
// @Success 200 {object} dto.GetUsageBySubscriptionResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/usage [post]
func (h *SubscriptionHandler) GetUsageBySubscription(c *gin.Context) {
	var req dto.GetUsageBySubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetFeatureUsageBySubscription(c.Request.Context(), &req)
	if err != nil {
		h.log.Error("Failed to get usage", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Query subscriptions
// @ID querySubscription
// @Description Use when listing or searching subscriptions (e.g. admin view or customer subscription list). Returns a paginated list; supports filtering by customer, plan, status.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.SubscriptionFilter true "Filter"
// @Success 200 {object} dto.ListSubscriptionsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/search [post]
func (h *SubscriptionHandler) QuerySubscriptions(c *gin.Context) {
	var filter types.SubscriptionFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if err := filter.Validate(); err != nil {
		h.log.Error("Invalid filter parameters", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Please provide valid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.ListSubscriptions(c.Request.Context(), &filter)
	if err != nil {
		h.log.Error("Failed to list subscriptions", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Add addon to subscription
// @ID addSubscriptionAddon
// @Description Use when adding an optional product or add-on to an existing subscription (e.g. extra storage or support tier).
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.AddAddonRequest true "Add Addon Request"
// @Success 200 {object} dto.AddonAssociationResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/addon [post]
func (h *SubscriptionHandler) AddAddonToSubscription(c *gin.Context) {
	var req dto.AddAddonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.AddAddonToSubscription(c.Request.Context(), req.SubscriptionID, &req.AddAddonToSubscriptionRequest)
	if err != nil {
		h.log.Error("Failed to add addon to subscription", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Remove addon from subscription
// @ID removeSubscriptionAddon
// @Description Use when removing an add-on from a subscription (e.g. downgrade or opt-out).
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.RemoveAddonRequest true "Remove Addon Request"
// @Success 200 {object} dto.SuccessResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/addon [delete]
func (h *SubscriptionHandler) RemoveAddonToSubscription(c *gin.Context) {
	var req dto.RemoveAddonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	if err := h.service.RemoveAddonFromSubscription(c.Request.Context(), &req); err != nil {
		h.log.Error("Failed to remove addon from subscription", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "addon removed from subscription successfully"})
}

// @Summary Get subscription entitlements
// @ID getSubscriptionEntitlements
// @Description Use when checking what features or limits a subscription has (e.g. entitlement checks or feature gating). Optional feature_ids to filter.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Param feature_ids query []string false "Feature IDs to filter by"
// @Success 200 {object} dto.SubscriptionEntitlementsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/entitlements [get]
func (h *SubscriptionHandler) GetSubscriptionEntitlements(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	// Call the service method with structured response
	var req dto.GetSubscriptionEntitlementsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		h.log.Error("Failed to bind query", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}
	response, err := h.service.GetAggregatedSubscriptionEntitlements(c.Request.Context(), id, &req)
	if err != nil {
		h.log.Error("Failed to get subscription entitlements", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// @Summary Create subscription line item
// @ID createSubscriptionLineItem
// @Description Use when adding a new charge or seat to a subscription (e.g. extra seat or one-time add). Supports price_id or inline price.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Param request body dto.CreateSubscriptionLineItemRequest true "Create Line Item Request"
// @Success 201 {object} dto.SubscriptionLineItemResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/lineitems [post]
func (h *SubscriptionHandler) AddSubscriptionLineItem(c *gin.Context) {
	subscriptionID := c.Param("id")
	if subscriptionID == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.CreateSubscriptionLineItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.AddSubscriptionLineItem(c.Request.Context(), subscriptionID, req)
	if err != nil {
		h.log.Error("Failed to add subscription line item", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Update subscription line item
// @ID updateSubscriptionLineItem
// @Description Use when changing a subscription line item (e.g. quantity or price). Implemented by ending the current line and creating a new one for clean billing.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Line Item ID"
// @Param request body dto.UpdateSubscriptionLineItemRequest true "Update Line Item Request"
// @Success 200 {object} dto.SubscriptionLineItemResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/lineitems/{id} [put]
func (h *SubscriptionHandler) UpdateSubscriptionLineItem(c *gin.Context) {
	lineItemID := c.Param("id")
	if lineItemID == "" {
		c.Error(ierr.NewError("line item ID is required").
			WithHint("Please provide a valid line item ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdateSubscriptionLineItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateSubscriptionLineItem(c.Request.Context(), lineItemID, req)
	if err != nil {
		h.log.Error("Failed to update subscription line item", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete subscription line item
// @ID deleteSubscriptionLineItem
// @Description Use when removing a charge or seat from a subscription (e.g. downgrade). Line item ends; retained for history but no longer billed.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Line Item ID"
// @Param request body dto.DeleteSubscriptionLineItemRequest true "Delete Line Item Request"
// @Success 200 {object} dto.SubscriptionLineItemResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/lineitems/{id} [delete]
func (h *SubscriptionHandler) DeleteSubscriptionLineItem(c *gin.Context) {
	lineItemID := c.Param("id")
	if lineItemID == "" {
		c.Error(ierr.NewError("line item ID is required").
			WithHint("Please provide a valid line item ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.DeleteSubscriptionLineItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.DeleteSubscriptionLineItem(c.Request.Context(), lineItemID, req)
	if err != nil {
		h.log.Error("Failed to delete subscription line item", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get upcoming credit grant applications
// @ID getSubscriptionUpcomingGrants
// @Description Use when showing upcoming or pending credits for a subscription (e.g. in a portal or for forecasting).
// @Tags Subscriptions
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Success 200 {object} dto.ListCreditGrantApplicationsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/grants/upcoming [get]
func (h *SubscriptionHandler) GetUpcomingCreditGrantApplications(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	// Create request DTO with the subscription ID from path parameter
	req := &dto.GetUpcomingCreditGrantApplicationsRequest{
		SubscriptionIDs: []string{id},
	}

	resp, err := h.service.GetUpcomingCreditGrantApplications(c.Request.Context(), req)
	if err != nil {
		h.log.Error("Failed to get upcoming credit grant applications", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get active addon associations
// @ID getSubscriptionAddonAssociations
// @Description Use when listing which add-ons are currently attached to a subscription (e.g. for display or editing).
// @Tags Subscriptions
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Success 200 {array} dto.AddonAssociationResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/addons/associations [get]
func (h *SubscriptionHandler) GetActiveAddonAssociations(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetActiveAddonAssociations(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to get active addon associations", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *SubscriptionHandler) TriggerSubscriptionWorkflow(c *gin.Context) {
	subscriptionID := c.Param("subscription_id")
	if subscriptionID == "" {
		c.Error(ierr.NewError("subscription_id is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	// Call the service method to trigger the workflow
	response, err := h.service.TriggerSubscriptionWorkflow(c.Request.Context(), subscriptionID)
	if err != nil {
		h.log.Error("Failed to trigger subscription workflow", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}
