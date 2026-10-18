package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

// HostedInvoiceHandler handles the public hosted invoice page's requests
type HostedInvoiceHandler struct {
	hostedInvoiceService service.HostedInvoiceService
	log                  *logger.Logger
}

func NewHostedInvoiceHandler(hostedInvoiceService service.HostedInvoiceService, log *logger.Logger) *HostedInvoiceHandler {
	return &HostedInvoiceHandler{hostedInvoiceService: hostedInvoiceService, log: log}
}

func (h *HostedInvoiceHandler) GetInvoice(c *gin.Context) {
	resp, err := h.hostedInvoiceService.GetInvoice(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *HostedInvoiceHandler) GetInvoicePDF(c *gin.Context) {
	url, err := h.hostedInvoiceService.GetInvoicePDFUrl(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"presigned_url": url})
}

func (h *HostedInvoiceHandler) PayInvoice(c *gin.Context) {
	var req dto.HostedInvoicePayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Something went wrong, please try again.").Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.hostedInvoiceService.PayInvoice(c.Request.Context(), &req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
