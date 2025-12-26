package v1

import (
	"errors"
	"io"
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
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

// CreateOneOffInvoice godoc
// @Summary Create a new one off invoice
// @Description Create a new one off invoice with the provided details
// @Tags Invoices
// @Accept json
// @Security ApiKeyAuth
// @Produce json
// @Param invoice body dto.CreateInvoiceRequest true "Invoice details"
// @Success 201 {object} dto.InvoiceResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /invoices [post]
func (h *InvoiceHandler) CreateOneOffInvoice(c *gin.Context) {
	var req dto.CreateInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorw("failed to bind request", "error", err)
		c.Error(ierr.WithError(err).WithHint("invalid request").Mark(ierr.ErrValidation))
		return
	}

	invoice, err := h.invoiceService.CreateOneOffInvoice(c.Request.Context(), req)
	if err != nil {
		h.logger.Errorw("failed to create invoice", "error", err)
		c.Error(err)
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
// @Security ApiKeyAuth
// @Param id path string true "Invoice ID"
// @Param expand_by_source query bool false "Include source-level price breakdown for usage line items (legacy)"
// @Param group_by query []string false "Group usage breakdown by specified fields (e.g., source, feature_id, properties.org_id)"
// @Success 200 {object} dto.InvoiceResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /invoices/{id} [get]
func (h *InvoiceHandler) GetInvoice(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("invalid invoice id").Mark(ierr.ErrValidation))
		return
	}

	groupByParams := c.QueryArray("group_by")

	// Use the new service method that handles breakdown logic internally
	req := dto.GetInvoiceWithBreakdownRequest{
		ID:      id,
		GroupBy: groupByParams,
	}

	invoice, err := h.invoiceService.GetInvoiceWithBreakdown(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
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
// @Security ApiKeyAuth
// @Param filter query types.InvoiceFilter false "Filter"
// @Success 200 {object} dto.ListInvoicesResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /invoices [get]
func (h *InvoiceHandler) ListInvoices(c *gin.Context) {
	var filter types.InvoiceFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		h.logger.Error("Failed to bind query parameters", "error", err)
		c.Error(ierr.WithError(err).WithHint("invalid query parameters").Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	// Validate filter
	if err := filter.Validate(); err != nil {
		h.logger.Error("Invalid filter parameters", "error", err)
		c.Error(ierr.WithError(err).WithHint("invalid filter parameters").Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.invoiceService.ListInvoices(c.Request.Context(), &filter)
	if err != nil {
		h.logger.Error("Failed to list invoices", "error", err)
		c.Error(err)
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
// @Security ApiKeyAuth
// @Param id path string true "Invoice ID"
// @Success 200 {object} dto.SuccessResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /invoices/{id}/finalize [post]
func (h *InvoiceHandler) FinalizeInvoice(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("invalid invoice id").Mark(ierr.ErrValidation))
		return
	}

	if err := h.invoiceService.FinalizeInvoice(c.Request.Context(), id); err != nil {
		h.logger.Errorw("failed to finalize invoice", "error", err, "invoice_id", id)
		c.Error(err)
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
// @Security ApiKeyAuth
// @Param id path string true "Invoice ID"
// @Success 200 {object} dto.SuccessResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /invoices/{id}/void [post]
func (h *InvoiceHandler) VoidInvoice(c *gin.Context) {
	id := c.Param("id")
	var req dto.InvoiceVoidRequest

	if id == "" {
		c.Error(ierr.NewError("invalid invoice id").Mark(ierr.ErrValidation))
		return
	}

	// This will handle empty body gracefully and only bind if there's valid JSON
	if err := c.ShouldBindJSON(&req); err != nil {
		// Check if it's actually an EOF error (empty body)
		if err == io.EOF {
			// Empty body is fine, use zero value
			req = dto.InvoiceVoidRequest{}
		} else {
			h.logger.Error("Failed to parse request body", "error", err)
			c.Error(ierr.WithError(err).WithHint("failed to parse request body").Mark(ierr.ErrValidation))
			return
		}
	}

	if err := h.invoiceService.VoidInvoice(c.Request.Context(), id, req); err != nil {
		h.logger.Errorw("failed to void invoice", "error", err, "invoice_id", id)
		c.Error(err)
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
// @Param request body dto.UpdatePaymentStatusRequest true "Payment Status Update Request"
// @Success 200 {object} dto.InvoiceResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /invoices/{id}/payment [put]
func (h *InvoiceHandler) UpdatePaymentStatus(c *gin.Context) {
	id := c.Param("id")
	var req dto.UpdatePaymentStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Failed to bind request body", "error", err)
		c.Error(ierr.WithError(err).WithHint("failed to bind request body").Mark(ierr.ErrValidation))
		return
	}

	if err := h.invoiceService.UpdatePaymentStatus(c.Request.Context(), id, req.PaymentStatus, req.Amount); err != nil {
		if errors.Is(err, ierr.ErrNotFound) {
			c.Error(ierr.WithError(err).WithHint("invoice not found").Mark(ierr.ErrNotFound))
			return
		}
		if errors.Is(err, ierr.ErrValidation) {
			c.Error(ierr.WithError(err).WithHint("invalid request").Mark(ierr.ErrValidation))
			return
		}
		h.logger.Error("Failed to update invoice payment status",
			"invoice_id", id,
			"payment_status", req.PaymentStatus,
			"error", err,
		)
		c.Error(err)
		return
	}

	// Get updated invoice
	resp, err := h.invoiceService.GetInvoice(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("Failed to get updated invoice",
			"invoice_id", id,
			"error", err,
		)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetPreviewInvoice godoc
// @Summary Get a preview invoice
// @Description Get a preview invoice
// @Tags Invoices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetPreviewInvoiceRequest true "Preview Invoice Request"
// @Success 200 {object} dto.InvoiceResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /invoices/preview [post]
func (h *InvoiceHandler) GetPreviewInvoice(c *gin.Context) {
	var req dto.GetPreviewInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Failed to bind request body", "error", err)
		c.Error(ierr.WithError(err).WithHint("failed to bind request body").Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.invoiceService.GetPreviewInvoice(c.Request.Context(), req)
	if err != nil {
		h.logger.Error("Failed to get preview invoice", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetCustomerInvoiceSummary godoc
// @Summary Get a customer invoice summary
// @Description Get a customer invoice summary
// @Tags Invoices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Customer ID"
// @Success 200 {object} dto.CustomerMultiCurrencyInvoiceSummary
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customers/{id}/invoices/summary [get]
func (h *InvoiceHandler) GetCustomerInvoiceSummary(c *gin.Context) {
	id := c.Param("id")

	resp, err := h.invoiceService.GetCustomerMultiCurrencyInvoiceSummary(c.Request.Context(), id)
	if err != nil {
		h.logger.Errorw("failed to get customer invoice summary", "error", err, "customer_id", id)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// AttemptPayment godoc
// @Summary Attempt payment for an invoice
// @Description Attempt to pay an invoice using customer's available wallets
// @Tags Invoices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Invoice ID"
// @Success 200 {object} dto.SuccessResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /invoices/{id}/payment/attempt [post]
func (h *InvoiceHandler) AttemptPayment(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("invalid invoice id").
			WithHint("Invalid invoice id").
			Mark(ierr.ErrValidation),
		)
		return
	}

	if err := h.invoiceService.AttemptPayment(c.Request.Context(), id); err != nil {
		h.logger.Errorw("failed to attempt payment for invoice", "error", err, "invoice_id", id)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "payment processed successfully"})
}

// GetInvoicePDF godoc
// @Summary Get PDF for an invoice
// @Description Retrieve the PDF document for a specific invoice by its ID
// @Tags Invoices
// @Security ApiKeyAuth
// @Param id path string true "Invoice ID"
// @Param url query bool false "Return presigned URL from s3 instead of PDF"
// @Success 200 {file} application/pdf
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /invoices/{id}/pdf [get]
func (h *InvoiceHandler) GetInvoicePDF(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("invalid invoice id").WithHint("invalid invoice id").Mark(ierr.ErrValidation))
		return
	}

	if c.Query("url") == "true" {
		url, err := h.invoiceService.GetInvoicePDFUrl(c.Request.Context(), id)
		if err != nil {
			h.logger.Errorw("failed to get invoice pdf url", "error", err, "invoice_id", id)
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"presigned_url": url})
		return
	}

	pdf, err := h.invoiceService.GetInvoicePDF(c.Request.Context(), id)
	if err != nil {
		h.logger.Errorw("failed to generate invoice pdf", "error", err, "invoice_id", id)
		c.Error(err)
		return
	}

	c.Data(http.StatusOK, "application/pdf", pdf)
}

// RecalculateInvoice godoc
// @Summary Recalculate invoice totals and line items
// @Description Recalculate totals and line items for a draft invoice, useful when subscription line items or usage data has changed
// @Tags Invoices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Invoice ID"
// @Param finalize query bool false "Whether to finalize the invoice after recalculation (default: true)"
// @Success 200 {object} dto.InvoiceResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /invoices/{id}/recalculate [post]
func (h *InvoiceHandler) RecalculateInvoice(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("invalid invoice id").Mark(ierr.ErrValidation))
		return
	}

	// Parse finalize query parameter (default: true)
	finalizeParam := c.DefaultQuery("finalize", "true")
	finalize := finalizeParam == "true"

	invoice, err := h.invoiceService.RecalculateInvoice(c.Request.Context(), id, finalize)
	if err != nil {
		h.logger.Errorw("failed to recalculate invoice", "error", err, "invoice_id", id)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, invoice)
}

// UpdateInvoice godoc
// @Summary Update an invoice
// @Description Update invoice details like PDF URL and due date.
// Works for draft, finalized, and paid invoices. Only safe fields like PDF URL and due date can be updated for paid invoices.
// @Tags Invoices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Invoice ID"
// @Param request body dto.UpdateInvoiceRequest true "Invoice Update Request"
// @Success 200 {object} dto.InvoiceResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /invoices/{id} [put]
func (h *InvoiceHandler) UpdateInvoice(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("invalid invoice id").Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdateInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorw("failed to bind request", "error", err)
		c.Error(ierr.WithError(err).WithHint("invalid request").Mark(ierr.ErrValidation))
		return
	}

	invoice, err := h.invoiceService.UpdateInvoice(c.Request.Context(), id, req)
	if err != nil {
		h.logger.Errorw("failed to update invoice", "error", err, "invoice_id", id)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, invoice)
}

// ListInvoicesByFilter godoc
// @Summary List invoices by filter
// @Description List invoices by filter
// @Tags Invoices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.InvoiceFilter true "Filter"
// @Success 200 {object} dto.ListInvoicesResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /invoices/search [post]
func (h *InvoiceHandler) ListInvoicesByFilter(c *gin.Context) {
	var filter types.InvoiceFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		h.logger.Error("Failed to bind request body", "error", err)
		c.Error(ierr.WithError(err).WithHint("invalid request body").Mark(ierr.ErrValidation))
		return
	}

	if err := filter.Validate(); err != nil {
		h.logger.Error("Invalid filter parameters", "error", err)
		c.Error(ierr.WithError(err).WithHint("invalid filter parameters").Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.invoiceService.ListInvoices(c.Request.Context(), &filter)
	if err != nil {
		h.logger.Error("Failed to list invoices", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// TriggerCommunication godoc
// @Summary Trigger communication webhook for an invoice
// @Description Triggers a communication webhook event containing all information about the invoice
// @Tags Invoices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Invoice ID"
// @Success 200 {object} dto.SuccessResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /invoices/{id}/comms/trigger [post]
func (h *InvoiceHandler) TriggerCommunication(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("invalid invoice id").Mark(ierr.ErrValidation))
		return
	}

	if err := h.invoiceService.TriggerCommunication(c.Request.Context(), id); err != nil {
		h.logger.Errorw("failed to trigger communication", "error", err, "invoice_id", id)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "communication triggered successfully"})
}
