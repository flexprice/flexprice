package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type InvoiceHandler struct {
	invoiceService service.InvoiceService
	logger         *logger.Logger
}

func NewInvoiceHandler(invoiceService service.InvoiceService, logger *logger.Logger) *InvoiceHandler {
	return &InvoiceHandler{
		invoiceService: invoiceService,
		logger:         logger,
	}
}

// CreateInvoice godoc
// @Summary Create a new invoice
// @Description Create a new invoice with the provided details
// @Tags Invoices
// @Accept json
// @Produce json
// @Param invoice body dto.CreateInvoiceRequest true "Invoice details"
// @Success 201 {object} dto.InvoiceResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /invoices [post]
func (h *InvoiceHandler) CreateInvoice(c *gin.Context) {
	var req dto.CreateInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorw("failed to bind request", "error", err)
		NewErrorResponse(c, http.StatusBadRequest, "invalid request", err)
		return
	}

	invoice, err := h.invoiceService.CreateInvoice(c.Request.Context(), req)
	if err != nil {
		h.logger.Errorw("failed to create invoice", "error", err)
		NewErrorResponse(c, http.StatusInternalServerError, "failed to create invoice", err)
		return
	}

	c.JSON(http.StatusCreated, invoice)
}

// GetInvoice godoc
// @Summary Get an invoice by ID
// @Description Get detailed information about an invoice
// @Tags Invoices
// @Accept json
// @Produce json
// @Param id path string true "Invoice ID"
// @Success 200 {object} dto.InvoiceResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /invoices/{id} [get]
func (h *InvoiceHandler) GetInvoice(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		NewErrorResponse(c, http.StatusBadRequest, "invalid invoice id", nil)
		return
	}

	invoice, err := h.invoiceService.GetInvoice(c.Request.Context(), id)
	if err != nil {
		h.logger.Errorw("failed to get invoice", "error", err, "invoice_id", id)
		NewErrorResponse(c, http.StatusInternalServerError, "failed to get invoice", err)
		return
	}

	c.JSON(http.StatusOK, invoice)
}

// ListInvoices godoc
// @Summary List invoices
// @Description List invoices with optional filtering
// @Tags Invoices
// @Accept json
// @Produce json
// @Param customer_id query string false "Customer ID"
// @Param subscription_id query string false "Subscription ID"
// @Param wallet_id query string false "Wallet ID"
// @Param status query []string false "Invoice statuses"
// @Param start_time query string false "Start time (RFC3339)"
// @Param end_time query string false "End time (RFC3339)"
// @Param limit query int false "Limit"
// @Param offset query int false "Offset"
// @Success 200 {object} dto.ListInvoicesResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /invoices [get]
func (h *InvoiceHandler) ListInvoices(c *gin.Context) {
	var filter types.InvoiceFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		h.logger.Error("Failed to bind query parameters", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Set default values if not provided
	if filter.Limit == 0 {
		filter.Limit = types.FILTER_DEFAULT_LIMIT
	}
	if filter.Sort == "" {
		filter.Sort = types.FILTER_DEFAULT_SORT
	}
	if filter.Order == "" {
		filter.Order = types.FILTER_DEFAULT_ORDER
	}

	resp, err := h.invoiceService.ListInvoices(c.Request.Context(), &filter)
	if err != nil {
		h.logger.Error("Failed to list invoices", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// FinalizeInvoice godoc
// @Summary Finalize an invoice
// @Description Finalize a draft invoice
// @Tags Invoices
// @Accept json
// @Produce json
// @Param id path string true "Invoice ID"
// @Success 200 {object} gin.H
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /invoices/{id}/finalize [post]
func (h *InvoiceHandler) FinalizeInvoice(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		NewErrorResponse(c, http.StatusBadRequest, "invalid invoice id", nil)
		return
	}

	if err := h.invoiceService.FinalizeInvoice(c.Request.Context(), id); err != nil {
		h.logger.Errorw("failed to finalize invoice", "error", err, "invoice_id", id)
		NewErrorResponse(c, http.StatusInternalServerError, "failed to finalize invoice", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "invoice finalized successfully"})
}

// VoidInvoice godoc
// @Summary Void an invoice
// @Description Void an invoice that hasn't been paid
// @Tags Invoices
// @Accept json
// @Produce json
// @Param id path string true "Invoice ID"
// @Success 200 {object} gin.H
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /invoices/{id}/void [post]
func (h *InvoiceHandler) VoidInvoice(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		NewErrorResponse(c, http.StatusBadRequest, "invalid invoice id", nil)
		return
	}

	if err := h.invoiceService.VoidInvoice(c.Request.Context(), id); err != nil {
		h.logger.Errorw("failed to void invoice", "error", err, "invoice_id", id)
		NewErrorResponse(c, http.StatusInternalServerError, "failed to void invoice", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "invoice voided successfully"})
}

// UpdatePaymentStatus godoc
// @Summary Update invoice payment status
// @Description Update the payment status of an invoice
// @Tags Invoices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Invoice ID"
// @Param request body dto.UpdateInvoicePaymentStatusRequest true "Payment Status Update Request"
// @Success 200 {object} dto.InvoiceResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /invoices/{id}/payment [put]
func (h *InvoiceHandler) UpdatePaymentStatus(c *gin.Context) {
	id := c.Param("id")
	var req dto.UpdateInvoicePaymentStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Failed to bind request body", "error", err)
		NewErrorResponse(c, http.StatusBadRequest, "failed to bind request body", err)
		return
	}

	if err := h.invoiceService.UpdatePaymentStatus(c.Request.Context(), id, req.PaymentStatus, req.Amount); err != nil {
		if invoice.IsNotFoundError(err) {
			NewErrorResponse(c, http.StatusNotFound, "invoice not found", err)
			return
		}
		if invoice.IsValidationError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		h.logger.Error("Failed to update invoice payment status",
			"invoice_id", id,
			"payment_status", req.PaymentStatus,
			"error", err,
		)
		NewErrorResponse(c, http.StatusInternalServerError, "failed to update invoice payment status", err)
		return
	}

	// Get updated invoice
	resp, err := h.invoiceService.GetInvoice(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("Failed to get updated invoice",
			"invoice_id", id,
			"error", err,
		)
		NewErrorResponse(c, http.StatusInternalServerError, "failed to get updated invoice", err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
