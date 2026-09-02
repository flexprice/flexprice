package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

// InvoiceModificationExecutor is the router-facing surface of the invoice
// modification handler; the concrete type stays private per repo convention.
type InvoiceModificationExecutor interface {
	Execute(c *gin.Context)
}

type invoiceModificationHandler struct {
	invoiceService service.InvoiceService
	log            *logger.Logger
}

func NewInvoiceModificationHandler(
	invoiceService service.InvoiceService,
	log *logger.Logger,
) InvoiceModificationExecutor {
	return &invoiceModificationHandler{
		invoiceService: invoiceService,
		log:            log,
	}
}

// @Summary Execute invoice modification
// @ID executeInvoiceModify
// @Description Execute a modification on a draft invoice. Currently supports line item changes: add (bulk), update (one line item per call; the edit is versioned, so the line item id changes), and remove (bulk, soft delete). Totals are recalculated from the remaining line items; a manual edit marks the invoice as manually edited, which disables recompute.
// @Tags Invoices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Invoice ID"
// @Param request body dto.ExecuteInvoiceModifyRequest true "Modification request"
// @Success 200 {object} dto.InvoiceModifyResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /invoices/{id}/modify/execute [post]
func (h *invoiceModificationHandler) Execute(c *gin.Context) {
	invoiceID := c.Param("id")
	if invoiceID == "" {
		c.Error(ierr.NewError("invoice ID is required").
			WithHint("Please provide a valid invoice ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.ExecuteInvoiceModifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error(c.Request.Context(), "failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.invoiceService.ModifyInvoice(c.Request.Context(), invoiceID, req)
	if err != nil {
		h.log.Error(c.Request.Context(), "failed to execute invoice modification", "error", err, "invoice_id", invoiceID)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
