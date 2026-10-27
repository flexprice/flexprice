package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service/selfbilling"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// SelfBillingHandler serves the dashboard's own-billing endpoints; every route acts on the caller's tenant.
type SelfBillingHandler struct {
	svc selfbilling.Service
}

func NewSelfBillingHandler(svc selfbilling.Service) *SelfBillingHandler {
	return &SelfBillingHandler{svc: svc}
}

// @Summary Get self-billing status
// @ID getSelfBillingStatus
// @Description Reports whether self-billing is enabled and the caller tenant's billing status.
// @Tags SelfBilling
// @x-scope "read"
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} dto.SelfBillingStatusResponse
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /portal/billing/status [get]
func (h *SelfBillingHandler) Status(c *gin.Context) {
	ctx := c.Request.Context()
	resp, err := h.svc.Status(ctx, types.GetTenantID(ctx))
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Start self-billing checkout
// @ID startSelfBillingCheckout
// @Description Saves the billing details and opens the checkout that ends the caller tenant's trial.
// @Tags SelfBilling
// @x-scope "write"
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.SelfBillingCheckoutRequest true "Billing details"
// @Success 200 {object} dto.SelfBillingCheckoutResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Self-billing disabled or subscription not found"
// @Failure 409 {object} ierr.ErrorResponse "A checkout is already in progress"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /portal/billing/checkout [post]
func (h *SelfBillingHandler) StartCheckout(c *gin.Context) {
	var req dto.SelfBillingCheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request body").Mark(ierr.ErrValidation))
		return
	}

	ctx := c.Request.Context()
	resp, err := h.svc.StartCheckout(ctx, types.GetTenantID(ctx), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Poll self-billing checkout
// @ID pollSelfBillingCheckout
// @Description Reports the caller tenant's checkout and activates the tenant once it is paid.
// @Tags SelfBilling
// @x-scope "write"
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Checkout session ID"
// @Success 200 {object} dto.SelfBillingCheckoutResponse
// @Failure 404 {object} ierr.ErrorResponse "Checkout not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /portal/billing/checkout/{id} [get]
func (h *SelfBillingHandler) PollCheckout(c *gin.Context) {
	ctx := c.Request.Context()
	resp, err := h.svc.PollCheckout(ctx, types.GetTenantID(ctx), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Cancel self-billing checkout
// @ID cancelSelfBillingCheckout
// @Description Cancels the caller tenant's open checkout.
// @Tags SelfBilling
// @x-scope "write"
// @Security ApiKeyAuth
// @Param id path string true "Checkout session ID"
// @Success 204 "No content"
// @Failure 404 {object} ierr.ErrorResponse "Checkout not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /portal/billing/checkout/{id}/cancel [post]
func (h *SelfBillingHandler) CancelCheckout(c *gin.Context) {
	ctx := c.Request.Context()
	if err := h.svc.CancelCheckout(ctx, types.GetTenantID(ctx), c.Param("id")); err != nil {
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// @Summary Get self-billing summary
// @ID getSelfBillingSummary
// @Description Returns the caller tenant's billing status, subscription, recent invoices and usage.
// @Tags SelfBilling
// @x-scope "read"
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} dto.SelfBillingSummaryResponse
// @Failure 404 {object} ierr.ErrorResponse "Self-billing disabled"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /portal/billing/summary [get]
func (h *SelfBillingHandler) Summary(c *gin.Context) {
	ctx := c.Request.Context()
	resp, err := h.svc.Summary(ctx, types.GetTenantID(ctx))
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get tenant billing summary
// @ID getTenantBillingSummary
// @Description Returns the caller tenant's billing summary; same as /portal/billing/summary.
// @Tags SelfBilling
// @x-scope "read"
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} dto.SelfBillingSummaryResponse
// @Failure 404 {object} ierr.ErrorResponse "Self-billing disabled"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /tenants/billing [get]
func (h *SelfBillingHandler) TenantBilling(c *gin.Context) {
	h.Summary(c)
}

// @Summary Create self-billing portal session
// @ID createSelfBillingPortalSession
// @Description Opens a short-lived customer portal session for the caller tenant.
// @Tags SelfBilling
// @x-scope "write"
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} dto.SelfBillingPortalResponse
// @Failure 404 {object} ierr.ErrorResponse "Self-billing disabled"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /portal/billing/portal-session [post]
func (h *SelfBillingHandler) PortalSession(c *gin.Context) {
	ctx := c.Request.Context()
	resp, err := h.svc.PortalSession(ctx, types.GetTenantID(ctx))
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
