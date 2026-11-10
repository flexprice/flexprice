package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

// EmailNotificationHandler handles emails sent to customers.
type EmailNotificationHandler struct {
	emailNotificationService service.EmailNotificationService
	logger                   *logger.Logger
}

func NewEmailNotificationHandler(emailNotificationService service.EmailNotificationService, logger *logger.Logger) *EmailNotificationHandler {
	return &EmailNotificationHandler{emailNotificationService: emailNotificationService, logger: logger}
}

// SendInvoiceEmail godoc
// @Summary Email invoice to customer
// @ID sendInvoiceEmail
// @Description Use when you want Flexprice to email a finalized invoice to the customer, with a link to the hosted invoice and the PDF attached. Sends to the customer's email unless `to` is given. Returns the queued email; delivery happens in the background.
// @Tags Invoices
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Invoice ID"
// @Param request body dto.SendInvoiceEmailRequest false "Recipients overriding the customer's email"
// @Success 200 {object} dto.EmailLogResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /invoices/{id}/email [post]
func (h *EmailNotificationHandler) SendInvoiceEmail(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("invalid invoice id").Mark(ierr.ErrValidation))
		return
	}

	var req dto.SendInvoiceEmailRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.Error(ierr.WithError(err).WithHint("Invalid request format").Mark(ierr.ErrValidation))
			return
		}
	}

	resp, err := h.emailNotificationService.SendInvoiceEmail(c.Request.Context(), id, &req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ListInvoiceEmails godoc
// @Summary List invoice emails
// @ID listInvoiceEmails
// @Description Use when showing which emails were sent for an invoice and whether each was delivered. Newest first.
// @Tags Invoices
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Invoice ID"
// @Success 200 {object} dto.ListEmailLogsResponse
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /invoices/{id}/emails [get]
func (h *EmailNotificationHandler) ListInvoiceEmails(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("invalid invoice id").Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.emailNotificationService.ListInvoiceEmails(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
