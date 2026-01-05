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

// CustomerDashboardHandler handles customer dashboard API requests
type CustomerDashboardHandler struct {
	dashboardService service.CustomerDashboardService
	log              *logger.Logger
}

// NewCustomerDashboardHandler creates a new customer dashboard handler
func NewCustomerDashboardHandler(
	dashboardService service.CustomerDashboardService,
	log *logger.Logger,
) *CustomerDashboardHandler {
	return &CustomerDashboardHandler{
		dashboardService: dashboardService,
		log:              log,
	}
}

// CreateSession creates a dashboard session for a customer
// @Summary Create a customer dashboard session
// @Description Generate a dashboard URL/token for a customer to access their billing information
// @Tags CustomerDashboard
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} dto.DashboardSessionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customer-dashboard/:external_id [get]
func (h *CustomerDashboardHandler) CreateSession(c *gin.Context) {
	externalID := c.Param("external_id")
	if externalID == "" {
		c.Error(ierr.NewError("external_id is required").Mark(ierr.ErrValidation))
		return
	}
	response, err := h.dashboardService.CreateDashboardSession(c.Request.Context(), externalID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetCustomer returns the current customer's information
// @Summary Get customer information
// @Description Get the authenticated customer's information
// @Tags CustomerDashboard
// @Produce json
// @Security BearerAuth
// @Success 200 {object} dto.CustomerResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customer-dashboard/info [get]
func (h *CustomerDashboardHandler) GetCustomer(c *gin.Context) {
	response, err := h.dashboardService.GetCustomer(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// UpdateCustomer updates the current customer's information
// @Summary Update customer information
// @Description Update the authenticated customer's profile information
// @Tags CustomerDashboard
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.UpdateCustomerRequest true "Update customer request"
// @Success 200 {object} dto.CustomerResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customer-dashboard/info [put]
func (h *CustomerDashboardHandler) UpdateCustomer(c *gin.Context) {
	var req dto.UpdateCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).Mark(ierr.ErrValidation))
		return
	}

	response, err := h.dashboardService.UpdateCustomer(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetUsageSummary returns usage summary for the customer
// @Summary Get customer usage summary
// @Description Get usage summary for the authenticated customer's metered features
// @Tags CustomerDashboard
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param filter query dto.GetCustomerUsageSummaryRequest false "Filter"
// @Success 200 {object} dto.CustomerUsageSummaryResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customer-dashboard/usage [get]
func (h *CustomerDashboardHandler) GetUsageSummary(c *gin.Context) {
	var req dto.GetCustomerUsageSummaryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.Error(ierr.WithError(err).Mark(ierr.ErrValidation))
		return
	}

	response, err := h.dashboardService.GetUsageSummary(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetSubscriptions returns the customer's subscriptions
// @Summary Get customer subscriptions
// @Description Get all subscriptions for the authenticated customer with pagination
// @Tags CustomerDashboard
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.DashboardPaginatedRequest true "Pagination request"
// @Success 200 {object} dto.ListSubscriptionsResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customer-dashboard/subscriptions [post]
func (h *CustomerDashboardHandler) GetSubscriptions(c *gin.Context) {
	var req dto.DashboardPaginatedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).Mark(ierr.ErrValidation))
		return
	}

	response, err := h.dashboardService.GetSubscriptions(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetInvoices returns the customer's invoices
// @Summary Get customer invoices
// @Description Get all invoices for the authenticated customer with pagination
// @Tags CustomerDashboard
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.DashboardPaginatedRequest true "Pagination request"
// @Success 200 {object} dto.ListInvoicesResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customer-dashboard/invoices [post]
func (h *CustomerDashboardHandler) GetInvoices(c *gin.Context) {
	var req dto.DashboardPaginatedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).Mark(ierr.ErrValidation))
		return
	}

	response, err := h.dashboardService.GetInvoices(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetInvoice returns a specific invoice
// @Summary Get invoice by ID
// @Description Get a specific invoice for the authenticated customer
// @Tags CustomerDashboard
// @Produce json
// @Security BearerAuth
// @Param id path string true "Invoice ID"
// @Success 200 {object} dto.InvoiceResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customer-dashboard/invoices/{id} [get]
func (h *CustomerDashboardHandler) GetInvoice(c *gin.Context) {
	invoiceID := c.Param("id")
	if invoiceID == "" {
		c.Error(ierr.NewError("invoice_id is required").Mark(ierr.ErrValidation))
		return
	}

	response, err := h.dashboardService.GetInvoice(c.Request.Context(), invoiceID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetSubscription returns a specific subscription
// @Summary Get subscription by ID
// @Description Get a specific subscription for the authenticated customer
// @Tags CustomerDashboard
// @Produce json
// @Security BearerAuth
// @Param id path string true "Subscription ID"
// @Success 200 {object} dto.SubscriptionResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customer-dashboard/subscriptions/{id} [get]
func (h *CustomerDashboardHandler) GetSubscription(c *gin.Context) {
	subscriptionID := c.Param("id")
	if subscriptionID == "" {
		c.Error(ierr.NewError("subscription_id is required").Mark(ierr.ErrValidation))
		return
	}

	response, err := h.dashboardService.GetSubscription(c.Request.Context(), subscriptionID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetWallets returns the customer's wallet balances
// @Summary Get customer wallets
// @Description Get all wallet balances for the authenticated customer
// @Tags CustomerDashboard
// @Produce json
// @Security BearerAuth
// @Success 200 {array} dto.WalletBalanceResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customer-dashboard/wallets [post]
func (h *CustomerDashboardHandler) GetWallets(c *gin.Context) {
	response, err := h.dashboardService.GetWallets(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetWallet returns a specific wallet
// @Summary Get wallet by ID
// @Description Get a specific wallet for the authenticated customer
// @Tags CustomerDashboard
// @Produce json
// @Security BearerAuth
// @Param id path string true "Wallet ID"
// @Success 200 {object} dto.WalletBalanceResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customer-dashboard/wallets/{id} [get]
func (h *CustomerDashboardHandler) GetWallet(c *gin.Context) {
	walletID := c.Param("id")
	if walletID == "" {
		c.Error(ierr.NewError("wallet_id is required").Mark(ierr.ErrValidation))
		return
	}

	response, err := h.dashboardService.GetWallet(c.Request.Context(), walletID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetAnalytics returns usage analytics for the customer
// @Summary Get customer analytics
// @Description Get usage analytics for the authenticated customer
// @Tags CustomerDashboard
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.DashboardAnalyticsRequest true "Analytics request"
// @Success 200 {object} dto.GetUsageAnalyticsResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customer-dashboard/analytics [post]
func (h *CustomerDashboardHandler) GetAnalytics(c *gin.Context) {
	var req dto.DashboardAnalyticsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).Mark(ierr.ErrValidation))
		return
	}

	response, err := h.dashboardService.GetAnalytics(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetCostAnalytics returns cost analytics for the customer
// @Summary Get customer cost analytics
// @Description Get cost analytics for the authenticated customer
// @Tags CustomerDashboard
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.DashboardCostAnalyticsRequest true "Cost analytics request"
// @Success 200 {object} dto.GetDetailedCostAnalyticsResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customer-dashboard/cost-analytics [post]
func (h *CustomerDashboardHandler) GetCostAnalytics(c *gin.Context) {
	var req dto.DashboardCostAnalyticsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).Mark(ierr.ErrValidation))
		return
	}

	response, err := h.dashboardService.GetCostAnalytics(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetInvoicePDF returns a presigned URL for an invoice PDF
// @Summary Get invoice PDF URL
// @Description Get a presigned URL for downloading an invoice PDF for the authenticated customer
// @Tags CustomerDashboard
// @Produce json
// @Security BearerAuth
// @Param id path string true "Invoice ID"
// @Param url query bool false "Return presigned URL from s3 instead of PDF" default(true)
// @Success 200 {object} map[string]string "Response with presigned_url"
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customer-dashboard/invoices/{id}/pdf [get]
func (h *CustomerDashboardHandler) GetInvoicePDF(c *gin.Context) {
	invoiceID := c.Param("id")
	if invoiceID == "" {
		c.Error(ierr.NewError("invoice_id is required").Mark(ierr.ErrValidation))
		return
	}

	url, err := h.dashboardService.GetInvoicePDFUrl(c.Request.Context(), invoiceID)
	if err != nil {
		h.log.Errorw("failed to get invoice pdf url", "error", err, "invoice_id", invoiceID)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"presigned_url": url})
}

// GetWalletBalance returns the real-time balance for a wallet
// @Summary Get wallet balance
// @Description Get real-time balance for a wallet belonging to the authenticated customer
// @Tags CustomerDashboard
// @Produce json
// @Security BearerAuth
// @Param id path string true "Wallet ID"
// @Success 200 {object} dto.WalletBalanceResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customer-dashboard/wallets/{id}/balance [get]
func (h *CustomerDashboardHandler) GetWalletBalance(c *gin.Context) {
	walletID := c.Param("id")
	if walletID == "" {
		c.Error(ierr.NewError("wallet_id is required").Mark(ierr.ErrValidation))
		return
	}

	response, err := h.dashboardService.GetWalletBalance(c.Request.Context(), walletID)
	if err != nil {
		h.log.Errorw("failed to get wallet balance", "error", err, "wallet_id", walletID)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetWalletTransactions returns transactions for a wallet
// @Summary Get wallet transactions
// @Description Get transactions for a wallet belonging to the authenticated customer with pagination
// @Tags CustomerDashboard
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Wallet ID"
// @Param limit query int false "Limit" default(10)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} dto.ListWalletTransactionsResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customer-dashboard/wallets/{id}/transactions [get]
func (h *CustomerDashboardHandler) GetWalletTransactions(c *gin.Context) {
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

	response, err := h.dashboardService.GetWalletTransactions(c.Request.Context(), walletID, &filter)
	if err != nil {
		h.log.Errorw("failed to get wallet transactions", "error", err, "wallet_id", walletID)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}
