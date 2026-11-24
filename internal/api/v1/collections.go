package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

type CollectionsHandler struct {
	service service.CollectionsService
	log     *logger.Logger
}

func NewCollectionsHandler(service service.CollectionsService, log *logger.Logger) *CollectionsHandler {
	return &CollectionsHandler{service: service, log: log}
}

// GetBoard godoc
// @Summary Get collections board
// @ID getCollectionsBoard
// @Description Use when you need every open invoice by where it stands: sent but not due, overdue (one card per invoice, split by reminder status), and paid in the last 7 days. Counts and per-currency totals cover whole columns and follow the q and currency filters; by default no cards are returned (limit 0): each column carries count and totals, and has_more is true when the column has cards to fetch with GET /collections/board/columns/{key}. With limit above 0 the first page of cards is returned too, and has_more says more cards follow. available_currencies lists the currencies present before filtering.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param limit query int false "Cards per column in this response (0-200, default 0 = counts and totals only)"
// @Param q query string false "Keep receivables whose customer name, customer email or invoice number contains this text (case-insensitive, max 100 characters)"
// @Param currency query string false "Keep receivables in this currency (case-insensitive)"
// @Success 200 {object} dto.CollectionsBoardResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/board [get]
func (h *CollectionsHandler) GetBoard(c *gin.Context) {
	var req dto.CollectionsBoardRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid query parameters").Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.service.GetBoard(c.Request.Context(), &req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetBoardColumn godoc
// @Summary Get a page of a collections board column
// @ID getCollectionsBoardColumn
// @Description Use when you need the cards of one board column, one page at a time, after the board call. Pages are numbered from 1 and ordered stably; send the as_of of the board response with every page so the overdue boundary and the 7-day window do not move while you scroll. A row that changes between page requests can repeat or be skipped, so reset pages when the board changes. Page times limit may not exceed 5000.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param key path string true "Column key: not_due, overdue, chasing, held or paid_this_week"
// @Param page query int false "Page number, from 1 (default 1)"
// @Param as_of query string false "as_of (RFC3339) of the board response; within the last 24 hours and not in the future"
// @Param limit query int false "Cards per page (1-200, default 15)"
// @Param q query string false "Keep receivables whose customer name, customer email or invoice number contains this text (case-insensitive, max 100 characters)"
// @Param currency query string false "Keep receivables in this currency (case-insensitive)"
// @Success 200 {object} dto.CollectionsColumnPageResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request, as_of or page too deep"
// @Failure 404 {object} ierr.ErrorResponse "Unknown column"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/board/columns/{key} [get]
func (h *CollectionsHandler) GetBoardColumn(c *gin.Context) {
	var req dto.CollectionsBoardRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid query parameters").Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.service.GetColumn(c.Request.Context(), c.Param("key"), &req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
