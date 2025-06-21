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

// CreateCreditNote creates a new credit note
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

// GetCreditNote retrieves a credit note by ID
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

// ListCreditNotes lists credit notes with filtering
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

// VoidCreditNote voids a credit note
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

// ProcessDraftCreditNote processes a draft credit note
func (h *CreditNoteHandler) ProcessDraftCreditNote(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("credit note ID is required").
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	err := h.creditNoteService.ProcessDraftCreditNote(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Adjustment credit note processed successfully"})
}
