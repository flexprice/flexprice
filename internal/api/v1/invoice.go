package v1

import (
	"errors"
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type InvoiceHandler struct {
	invoiceService  service.InvoiceService
	temporalService *temporal.Service
	logger          *logger.Logger
}

func NewInvoiceHandler(invoiceService service.InvoiceService, temporalService *temporal.Service, logger *logger.Logger) *InvoiceHandler {
	return &InvoiceHandler{
		invoiceService:  invoiceService,
		temporalService: temporalService,
		logger:          logger,
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
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /invoices [post]
func (h *InvoiceHandler) CreateInvoice(c *gin.Context) {
	var req dto.CreateInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorw("failed to bind request", "error", err)
		c.Error(ierr.Wrap(err, ierr.ErrCodeValidation, "invalid request"))
		return
	}

	invoice, err := h.invoiceService.CreateInvoice(c.Request.Context(), req)
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
// @Param id path string true "Invoice ID"
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

	invoice, err := h.invoiceService.GetInvoice(c.Request.Context(), id)
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
// @Param id path string true "Invoice ID"
// @Success 200 {object} gin.H
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
// @Param id path string true "Invoice ID"
// @Success 200 {object} gin.H
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /invoices/{id}/void [post]
func (h *InvoiceHandler) VoidInvoice(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("invalid invoice id").Mark(ierr.ErrValidation))
		return
	}

	if err := h.invoiceService.VoidInvoice(c.Request.Context(), id); err != nil {
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
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
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

// GenerateInvoice handles manual invoice generation requests
func (h *InvoiceHandler) GenerateInvoice(c *gin.Context) {
	ctx := c.Request.Context()

	var req models.BillingWorkflowInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("failed to bind request body").Mark(ierr.ErrValidation))
		return
	}

	result, err := h.temporalService.StartBillingWorkflow(ctx, req)
	if err != nil {
		h.logger.Errorw("failed to start billing workflow",
			"error", err,
			"customer_id", req.CustomerID,
			"subscription_id", req.SubscriptionID)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, result)
}
