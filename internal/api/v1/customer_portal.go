package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

// CustomerPortalHandler handles customer portal API requests
type CustomerPortalHandler struct {
	portalService service.CustomerPortalService
	log           *logger.Logger
}

// NewCustomerPortalHandler creates a new customer portal handler
func NewCustomerPortalHandler(
	portalService service.CustomerPortalService,
	log *logger.Logger,
) *CustomerPortalHandler {
	return &CustomerPortalHandler{
		portalService: portalService,
		log:           log,
	}
}

// CreateSession creates a dashboard session for a customer
// @Summary Create a customer portal session
// @ID createCustomerPortalSession
// @Description Generate a dashboard URL/token for a customer to access their billing information
// @Tags CustomerPortal
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param external_id path string true "Customer External ID"
// @Success 200 {object} dto.PortalSessionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /portal/{external_id} [get]
func (h *CustomerPortalHandler) CreateSession(c *gin.Context) {
	externalID := c.Param("external_id")
	if externalID == "" {
		c.Error(ierr.NewError("external_id is required").Mark(ierr.ErrValidation))
		return
	}
	response, err := h.portalService.CreatePortalSession(c.Request.Context(), externalID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *CustomerPortalHandler) GetCustomer(c *gin.Context) {
	response, err := h.portalService.GetCustomer(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *CustomerPortalHandler) UpdateCustomer(c *gin.Context) {
	var req dto.UpdateCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).Mark(ierr.ErrValidation))
		return
	}

	response, err := h.portalService.UpdateCustomer(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *CustomerPortalHandler) GetUsageSummary(c *gin.Context) {
	var req dto.GetCustomerUsageSummaryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.Error(ierr.WithError(err).Mark(ierr.ErrValidation))
		return
	}

	response, err := h.portalService.GetUsageSummary(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *CustomerPortalHandler) GetSubscriptions(c *gin.Context) {
	var req dto.PortalPaginatedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).Mark(ierr.ErrValidation))
		return
	}

	response, err := h.portalService.GetSubscriptions(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *CustomerPortalHandler) GetInvoices(c *gin.Context) {
	var req dto.PortalPaginatedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).Mark(ierr.ErrValidation))
		return
	}

	response, err := h.portalService.GetInvoices(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *CustomerPortalHandler) GetInvoice(c *gin.Context) {
	invoiceID := c.Param("id")
	if invoiceID == "" {
		c.Error(ierr.NewError("invoice_id is required").Mark(ierr.ErrValidation))
		return
	}

	response, err := h.portalService.GetInvoice(c.Request.Context(), invoiceID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *CustomerPortalHandler) GetSubscription(c *gin.Context) {
	subscriptionID := c.Param("id")
	if subscriptionID == "" {
		c.Error(ierr.NewError("subscription_id is required").Mark(ierr.ErrValidation))
		return
	}

	response, err := h.portalService.GetSubscription(c.Request.Context(), subscriptionID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *CustomerPortalHandler) GetWallets(c *gin.Context) {
	response, err := h.portalService.GetWallets(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *CustomerPortalHandler) GetWallet(c *gin.Context) {
	walletID := c.Param("id")
	if walletID == "" {
		c.Error(ierr.NewError("wallet_id is required").Mark(ierr.ErrValidation))
		return
	}

	response, err := h.portalService.GetWallet(c.Request.Context(), walletID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *CustomerPortalHandler) GetAnalytics(c *gin.Context) {
	var req dto.PortalAnalyticsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).Mark(ierr.ErrValidation))
		return
	}

	response, err := h.portalService.GetAnalytics(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *CustomerPortalHandler) GetCostAnalytics(c *gin.Context) {
	var req dto.PortalCostAnalyticsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).Mark(ierr.ErrValidation))
		return
	}

	response, err := h.portalService.GetCostAnalytics(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *CustomerPortalHandler) GetInvoicePDF(c *gin.Context) {
	invoiceID := c.Param("id")
	if invoiceID == "" {
		c.Error(ierr.NewError("invoice_id is required").Mark(ierr.ErrValidation))
		return
	}

	url, err := h.portalService.GetInvoicePDFUrl(c.Request.Context(), invoiceID)
	if err != nil {
		h.log.Errorw("failed to get invoice pdf url", "error", err, "invoice_id", invoiceID)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"presigned_url": url})
}

func (h *CustomerPortalHandler) GetWalletTransactions(c *gin.Context) {
	walletID := c.Param("id")
	if walletID == "" {
		c.Error(ierr.NewError("wallet_id is required").Mark(ierr.ErrValidation))
		return
	}

	var filter types.WalletTransactionFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		h.log.Errorw("failed to bind query", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	response, err := h.portalService.GetWalletTransactions(c.Request.Context(), walletID, &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}
