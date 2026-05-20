package v1

import (
	"net/http"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/integration/paddle"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

// PaddleHandler exposes manual trigger endpoints for Paddle integration.
type PaddleHandler struct {
	factory *integration.Factory
	log     *logger.Logger
}

// NewPaddleHandler creates a new PaddleHandler.
func NewPaddleHandler(factory *integration.Factory, log *logger.Logger) *PaddleHandler {
	return &PaddleHandler{factory: factory, log: log}
}

// SyncInvoice triggers a full Paddle invoice sync for the given invoice ID.
// POST /v1/integrations/paddle/invoices/:invoice_id/sync
func (h *PaddleHandler) SyncInvoice(c *gin.Context) {
	invoiceID := c.Param("invoice_id")
	if invoiceID == "" {
		c.Error(ierr.NewError("invoice_id is required").Mark(ierr.ErrValidation))
		return
	}

	ctx := c.Request.Context()
	paddleIntegration, err := h.factory.GetPaddleIntegration(ctx)
	if err != nil {
		c.Error(err)
		return
	}

	resp, err := paddleIntegration.SyncSvc.SyncInvoice(ctx, paddle.SyncInvoiceRequest{InvoiceID: invoiceID})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
