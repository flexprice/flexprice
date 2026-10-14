package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service/selfbilling"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// SelfBillingHandler serves the dashboard's own-billing endpoints for the caller's tenant.
type SelfBillingHandler struct {
	svc selfbilling.Service
}

// NewSelfBillingHandler builds the self-billing handler; every route acts on the tenant in the request context.
func NewSelfBillingHandler(svc selfbilling.Service) *SelfBillingHandler {
	return &SelfBillingHandler{svc: svc}
}

// @Summary Get self-billing status
// @ID getSelfBillingStatus
// @Tags SelfBilling
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} dto.SelfBillingStatusResponse
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @x-scope "read"
// @Router /portal/billing/status [get]
func (h *SelfBillingHandler) Status(c *gin.Context) {
	resp, err := h.svc.Status(c.Request.Context(), types.GetTenantID(c.Request.Context()))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary Start self-billing checkout
// @ID startSelfBillingCheckout
// @Tags SelfBilling
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.SelfBillingCheckoutRequest true "Billing details"
// @Success 200 {object} dto.SelfBillingCheckoutResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /portal/billing/checkout [post]
func (h *SelfBillingHandler) StartCheckout(c *gin.Context) {
	var req dto.SelfBillingCheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request body").Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.svc.StartCheckout(c.Request.Context(), types.GetTenantID(c.Request.Context()), req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary Poll self-billing checkout
// @ID pollSelfBillingCheckout
// @Tags SelfBilling
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Checkout session ID"
// @Success 200 {object} dto.SelfBillingCheckoutResponse
// @Failure 404 {object} ierr.ErrorResponse "Checkout not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @x-scope "write"
// @Router /portal/billing/checkout/{id} [get]
func (h *SelfBillingHandler) PollCheckout(c *gin.Context) {
	resp, err := h.svc.PollCheckout(c.Request.Context(), types.GetTenantID(c.Request.Context()), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary Cancel self-billing checkout
// @ID cancelSelfBillingCheckout
// @Tags SelfBilling
// @Security ApiKeyAuth
// @Param id path string true "Checkout session ID"
// @Success 204 "No content"
// @Failure 404 {object} ierr.ErrorResponse "Checkout not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /portal/billing/checkout/{id}/cancel [post]
func (h *SelfBillingHandler) CancelCheckout(c *gin.Context) {
	if err := h.svc.CancelCheckout(c.Request.Context(), types.GetTenantID(c.Request.Context()), c.Param("id")); err != nil {
		c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

// @Summary Get self-billing summary
// @ID getSelfBillingSummary
// @Tags SelfBilling
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} dto.SelfBillingSummaryResponse
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @x-scope "read"
// @Router /portal/billing/summary [get]
func (h *SelfBillingHandler) Summary(c *gin.Context) {
	resp, err := h.svc.Summary(c.Request.Context(), types.GetTenantID(c.Request.Context()))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary Get tenant billing summary (legacy alias of /portal/billing/summary)
// @ID getTenantBillingSummary
// @Tags SelfBilling
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} dto.SelfBillingSummaryResponse
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @x-scope "read"
// @Router /tenants/billing [get]
func (h *SelfBillingHandler) TenantBilling(c *gin.Context) {
	h.Summary(c)
}

// @Summary Create self-billing portal session
// @ID createSelfBillingPortalSession
// @Tags SelfBilling
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} dto.SelfBillingPortalResponse
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /portal/billing/portal-session [post]
func (h *SelfBillingHandler) PortalSession(c *gin.Context) {
	resp, err := h.svc.PortalSession(c.Request.Context(), types.GetTenantID(c.Request.Context()))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
