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

func bindReminderBody(c *gin.Context, req any) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request format").Mark(ierr.ErrValidation))
		return false
	}
	return true
}

func (h *ReminderHandler) respond(c *gin.Context, resp any, err error) {
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ListReminders godoc
// @Summary List reminder drafts
// @ID listReminderDrafts
// @Description Use when you need the payment reminder drafts for overdue invoices: one draft per customer and currency, with tone, text, covered invoices and the recorded decision. Drafts are rendered when read and only decisions and edits are stored; the list is paged and each draft shows at most 25 invoices (the single draft shows all). Nothing is sent.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param status query string false "pending, approved, held or skipped"
// @Param tone query string false "friendly, firm or final"
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
	h.respond(c, resp, err)
}

// GetReminder godoc
// @Summary Get reminder draft
// @ID getReminderDraft
// @Description Use when you need one reminder draft with its text, invoices and decision.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Reminder draft ID"
// @Success 200 {object} dto.ReminderDraftResponse
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/reminders/{id} [get]
func (h *ReminderHandler) GetReminder(c *gin.Context) {
	resp, err := h.service.Get(c.Request.Context(), c.Param("id"))
	h.respond(c, resp, err)
}

// UpdateReminder godoc
// @Summary Edit reminder draft
// @ID updateReminderDraft
// @Description Use when you need to change the subject, body or tone of a pending or held reminder draft. A tone change rewrites subject and body from that tone's template.
// @Tags Collections
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Reminder draft ID"
// @Param request body dto.UpdateReminderRequest true "Changes"
// @Success 200 {object} dto.ReminderDraftResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 409 {object} ierr.ErrorResponse "Draft is not pending or held"
// @Router /collections/reminders/{id} [put]
func (h *ReminderHandler) UpdateReminder(c *gin.Context) {
	var req dto.UpdateReminderRequest
	if !bindReminderBody(c, &req) {
		return
	}
	resp, err := h.service.Update(c.Request.Context(), c.Param("id"), &req)
	h.respond(c, resp, err)
}

// ApproveReminder godoc
// @Summary Approve reminder draft
// @ID approveReminderDraft
// @Description Use when you want to record approval of a pending or held reminder draft. Nothing is sent.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Reminder draft ID"
// @Success 200 {object} dto.ReminderDraftResponse
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 409 {object} ierr.ErrorResponse "Draft cannot be approved in its status"
// @Router /collections/reminders/{id}/approve [post]
func (h *ReminderHandler) ApproveReminder(c *gin.Context) {
	resp, err := h.service.Approve(c.Request.Context(), c.Param("id"))
	h.respond(c, resp, err)
}

// HoldReminder godoc
// @Summary Hold reminder draft
// @ID holdReminderDraft
// @Description Use when a pending reminder should wait, for example after a promise to pay. A reason is required.
// @Tags Collections
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Reminder draft ID"
// @Param request body dto.HoldReminderRequest true "Reason"
// @Success 200 {object} dto.ReminderDraftResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 409 {object} ierr.ErrorResponse "Draft is not pending"
// @Router /collections/reminders/{id}/hold [post]
func (h *ReminderHandler) HoldReminder(c *gin.Context) {
	var req dto.HoldReminderRequest
	if !bindReminderBody(c, &req) {
		return
	}
	resp, err := h.service.Hold(c.Request.Context(), c.Param("id"), &req)
	h.respond(c, resp, err)
}

// SkipReminder godoc
// @Summary Skip reminder draft
// @ID skipReminderDraft
// @Description Use when no reminder should go out for this customer now.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Reminder draft ID"
// @Success 200 {object} dto.ReminderDraftResponse
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 409 {object} ierr.ErrorResponse "Draft cannot be skipped in its status"
// @Router /collections/reminders/{id}/skip [post]
func (h *ReminderHandler) SkipReminder(c *gin.Context) {
	resp, err := h.service.Skip(c.Request.Context(), c.Param("id"))
	h.respond(c, resp, err)
}

// ReopenReminder godoc
// @Summary Reopen reminder draft
// @ID reopenReminderDraft
// @Description Use when you want to undo a decision and put a reminder draft back to pending.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Reminder draft ID"
// @Success 200 {object} dto.ReminderDraftResponse
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 409 {object} ierr.ErrorResponse "Draft is already pending"
// @Router /collections/reminders/{id}/reopen [post]
func (h *ReminderHandler) ReopenReminder(c *gin.Context) {
	resp, err := h.service.Reopen(c.Request.Context(), c.Param("id"))
	h.respond(c, resp, err)
}

// BulkApproveReminders godoc
// @Summary Approve reminder drafts in bulk
// @ID bulkApproveReminderDrafts
// @Description Use when you want to approve up to 200 pending reminder drafts at once. Drafts that are not pending are left as they are and left out of the response. Nothing is sent.
// @Tags Collections
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param request body dto.BulkApproveRemindersRequest true "Draft IDs"
// @Success 200 {object} dto.BulkApproveRemindersResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "A draft was not found"
// @Router /collections/reminders/approve [post]
func (h *ReminderHandler) BulkApproveReminders(c *gin.Context) {
	var req dto.BulkApproveRemindersRequest
	if !bindReminderBody(c, &req) {
		return
	}
	resp, err := h.service.BulkApprove(c.Request.Context(), &req)
	h.respond(c, resp, err)
}
