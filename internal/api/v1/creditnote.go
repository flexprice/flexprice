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

// @Summary Create a new credit note
// @Description Creates a new credit note
// @Tags Credit Notes
// @Accept json
// @Produce json
// @Param credit_note body dto.CreateCreditNoteRequest true "Credit note request"
// @Success 201 {object} dto.CreditNoteResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 403 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
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

// @Summary Get a credit note by ID
// @Description Retrieves a credit note by ID
// @Tags Credit Notes
// @Accept json
// @Produce json
// @Param id path string true "Credit note ID"
// @Success 200 {object} dto.CreditNoteResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 403 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /creditnotes/{id} [get]
// @Security ApiKeyAuth
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

// @Summary List credit notes with filtering
// @Description Lists credit notes with filtering
// @Tags Credit Notes
// @Accept json
// @Produce json
// @Param filter query types.CreditNoteFilter true "Filter options"
// @Success 200 {object} dto.ListCreditNotesResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 403 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /creditnotes [get]
// @Security ApiKeyAuth
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

// @Summary Void a credit note
// @Description Voids a credit note
// @Tags Credit Notes
// @Accept json
// @Produce json
// @Param id path string true "Credit note ID"
// @Success 200 {object} dto.CreditNoteResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 403 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
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

// @Summary Process a draft credit note
// @Description Processes a draft credit note
// @Tags Credit Notes
// @Accept json
// @Produce json
// @Param id path string true "Credit note ID"
// @Success 200 {object} dto.CreditNoteResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 403 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /creditnotes/{id}/process [post]
// @Security ApiKeyAuth
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
