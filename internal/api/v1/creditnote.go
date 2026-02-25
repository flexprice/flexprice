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

type CreditNoteHandler struct {
	creditNoteService service.CreditNoteService
	logger            *logger.Logger
}

func NewCreditNoteHandler(creditNoteService service.CreditNoteService, logger *logger.Logger) *CreditNoteHandler {
	return &CreditNoteHandler{
		creditNoteService: creditNoteService,
	}
}

// @Summary Create credit note
// @ID createCreditNote
// @Description Use when issuing a refund or adjustment (e.g. customer dispute or proration). Links to an invoice; create as draft then finalize.
// @Tags Credit Notes
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param credit_note body dto.CreateCreditNoteRequest true "Credit note request"
// @Success 201 {object} dto.CreditNoteResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 403 {object} ierr.ErrorResponse "Forbidden"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /creditnotes [post]
// @Security ApiKeyAuth
func (h *CreditNoteHandler) CreateCreditNote(c *gin.Context) {
	var req dto.CreateCreditNoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	response, err := h.creditNoteService.CreateCreditNote(c.Request.Context(), &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, response)
}

// @Summary Get credit note
// @ID getCreditNote
// @Description Use when you need to load a single credit note (e.g. for display or reconciliation).
// @Tags Credit Notes
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Credit note ID"
// @Success 200 {object} dto.CreditNoteResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error" "Server error"
// @Router /creditnotes/{id} [get]
func (h *CreditNoteHandler) GetCreditNote(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("credit note ID is required").
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	response, err := h.creditNoteService.GetCreditNote(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *CreditNoteHandler) ListCreditNotes(c *gin.Context) {
	var filter types.CreditNoteFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	response, err := h.creditNoteService.ListCreditNotes(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// @Summary Void credit note
// @ID voidCreditNote
// @Description Use when cancelling a draft credit note (e.g. created by mistake). Only draft credit notes can be voided.
// @Tags Credit Notes
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Credit note ID"
// @Success 200 {object} dto.CreditNoteResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 403 {object} ierr.ErrorResponse "Forbidden"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /creditnotes/{id}/void [post]
// @Security ApiKeyAuth
func (h *CreditNoteHandler) VoidCreditNote(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("credit note ID is required").
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	err := h.creditNoteService.VoidCreditNote(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Credit note voided successfully"})
}

// @Summary Finalize credit note
// @ID processCreditNote
// @Description Use when locking a draft credit note and applying the credit (e.g. after approval). Once finalized, applied per billing provider.
// @Tags Credit Notes
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Credit note ID"
// @Success 200 {object} dto.CreditNoteResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 403 {object} ierr.ErrorResponse "Forbidden"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /creditnotes/{id}/finalize [post]
// @Security ApiKeyAuth
func (h *CreditNoteHandler) FinalizeCreditNote(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("credit note ID is required").
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	err := h.creditNoteService.FinalizeCreditNote(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Adjustment credit note processed successfully"})
}
