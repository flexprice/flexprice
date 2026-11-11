package v1

import (
	"net/http"

	journalsvc "github.com/flexprice/flexprice/internal/ee/service/journal"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	journalRepo "github.com/flexprice/flexprice/internal/repository/journal"
	"github.com/gin-gonic/gin"
)

type JournalEntryHandler struct {
	service journalsvc.JournalService
	log     *logger.Logger
}

func NewJournalEntryHandler(service journalsvc.JournalService, log *logger.Logger) *JournalEntryHandler {
	return &JournalEntryHandler{service: service, log: log}
}

// @Summary Get journal entry
// @ID getJournalEntry
// @Description Use when you need one revenue recognition journal entry with its debit and credit lines. The journal is derived from billing and recognition and is read-only.
// @Tags Journal Entries
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Journal entry ID"
// @Success 200 {object} dto.JournalEntryResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /journal_entries/{id} [get]
func (h *JournalEntryHandler) GetJournalEntry(c *gin.Context) {
	resp, err := h.service.GetJournalEntry(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Query journal entries
// @ID queryJournalEntry
// @Description Use when listing journal entries by period, event type, account, customer, contract, performance obligation or source. Returns a paginated list with lines.
// @Tags Journal Entries
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param filter body journalRepo.Filter true "Filter"
// @Success 200 {object} dto.ListJournalEntriesResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /journal_entries/search [post]
func (h *JournalEntryHandler) QueryJournalEntries(c *gin.Context) {
	var filter journalRepo.Filter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetJournalEntries(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
