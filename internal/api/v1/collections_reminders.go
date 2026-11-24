package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

type ReminderHandler struct {
	service service.ReminderService
	log     *logger.Logger
}

func NewReminderHandler(service service.ReminderService, log *logger.Logger) *ReminderHandler {
	return &ReminderHandler{service: service, log: log}
}

// ListReminders godoc
// @Summary List reminder drafts
// @ID listReminderDrafts
// @Description Use when you need the payment reminder of each overdue invoice with its tone, text and status. A reminder nobody acted on is rendered when read and stored nowhere; only edits and decisions are stored. The id of a reminder is its invoice id. The list is paged and nothing is sent.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param status query string false "pending, approved, held or skipped"
// @Param q query string false "Match customer name, email or invoice number"
// @Param customer_id query string false "Customer ID"
// @Param currency query string false "Currency code"
// @Param limit query int false "Page size (1-100, default 15)"
// @Param page query int false "Page number, from 1 (default 1); page times limit may not exceed 5000"
// @Param as_of query string false "as_of (RFC3339) of an earlier response; within the last 24 hours and not in the future"
// @Success 200 {object} dto.ListRemindersResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/reminders [get]
func (h *ReminderHandler) ListReminders(c *gin.Context) {
	var req dto.ListRemindersRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid query parameters").Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.List(c.Request.Context(), &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetReminder godoc
// @Summary Get reminder draft
// @ID getReminderDraft
// @Description Use when you need the reminder of one overdue invoice. The id is the invoice id; 404 when the invoice is not open and overdue.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Invoice ID"
// @Success 200 {object} dto.InvoiceReminderResponse
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/reminders/{id} [get]
func (h *ReminderHandler) GetReminder(c *gin.Context) {
	resp, err := h.service.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateReminder godoc
// @Summary Update reminder
// @ID updateReminderDraft
// @Description Use when you need to approve, hold, skip or reopen (status pending) a reminder, edit its tone, subject or body, or both at once. The request decides: status is pending, approved, held or skipped (omit it to keep the current one); status_reason is required for held and optional for skipped. A tone change rewrites subject and body from that tone's template, then a subject or body in the same request is applied on top. Allowed moves: pending to approved, held or skipped; held to pending, approved or skipped; approved or skipped to pending. Mail can only be edited while pending or held. Nothing is sent.
// @Tags Collections
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Invoice ID"
// @Param request body dto.UpdateReminderRequest true "Target status and/or mail changes"
// @Success 200 {object} dto.InvoiceReminderResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Invoice is not open and overdue"
// @Failure 409 {object} ierr.ErrorResponse "Not allowed in the reminder's status, or changed by someone else"
// @Router /collections/reminders/update/{id} [put]
func (h *ReminderHandler) UpdateReminder(c *gin.Context) {
	var req dto.UpdateReminderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request format").Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateReminder(c.Request.Context(), c.Param("id"), &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// BulkUpdateReminders godoc
// @Summary Update reminders in bulk
// @ID bulkUpdateReminderDrafts
// @Description Use when you want to apply one status (and optionally tone) change to up to 200 invoices at once, with the same rules as updating one reminder. Subject and body are not allowed. Unknown or not overdue invoices fail the request with 404 before anything is written; each invoice is applied on its own, so a failure partway keeps the invoices already applied; invoices the change is not legal for, or that someone changed meanwhile, are left as they are and left out of the response. Approving queues each email for the dispatcher; nothing is sent in the request.
// @Tags Collections
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param request body dto.BulkUpdateRemindersRequest true "Invoice IDs and the change"
// @Success 200 {object} dto.BulkUpdateRemindersResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "An invoice was not found"
// @Router /collections/reminders/bulk-update [put]
func (h *ReminderHandler) BulkUpdateReminders(c *gin.Context) {
	var req dto.BulkUpdateRemindersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request format").Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.BulkUpdateReminders(c.Request.Context(), &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
