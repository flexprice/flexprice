package v1

import (
	"errors"
	"io"
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type CommitmentHandler struct {
	service service.CommitmentService
	logger  *logger.Logger
}

func NewCommitmentHandler(service service.CommitmentService, logger *logger.Logger) *CommitmentHandler {
	return &CommitmentHandler{service: service, logger: logger}
}

// @Summary Preview commitment
// @ID previewCommitment
// @Description Validates a commitment and returns its computed value without creating it.
// @Tags Commitments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param commitment body dto.CreateCommitmentRequest true "Commitment"
// @Success 200 {object} dto.CommitmentResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /commitments/preview [post]
func (h *CommitmentHandler) PreviewCommitment(c *gin.Context) {
	var req dto.CreateCommitmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.Preview(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Create commitment
// @ID createCommitment
// @Description Creates a commitment on a subscription: a minimum spend or quantity over a term, with optional committed and overage rates.
// @Tags Commitments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param commitment body dto.CreateCommitmentRequest true "Commitment"
// @Success 201 {object} dto.CommitmentResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /commitments [post]
func (h *CommitmentHandler) CreateCommitment(c *gin.Context) {
	var req dto.CreateCommitmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get commitment
// @ID getCommitment
// @Description Loads a single commitment by ID.
// @Tags Commitments
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Commitment ID"
// @Success 200 {object} dto.CommitmentResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /commitments/{id} [get]
func (h *CommitmentHandler) GetCommitment(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("commitment ID is required").
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Query commitments
// @ID queryCommitment
// @Description Lists commitments with filtering and sorting. Returns a paginated list.
// @Tags Commitments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param filter body commitment.Filter true "Filter"
// @Success 200 {object} dto.ListCommitmentsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /commitments/search [post]
func (h *CommitmentHandler) QueryCommitments(c *gin.Context) {
	var filter commitment.Filter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	resp, err := h.service.List(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update commitment
// @ID updateCommitment
// @Description Updates the descriptive fields of a commitment. Economic terms are immutable.
// @Tags Commitments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Commitment ID"
// @Param commitment body dto.UpdateCommitmentRequest true "Commitment update"
// @Success 200 {object} dto.CommitmentResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /commitments/{id} [put]
func (h *CommitmentHandler) UpdateCommitment(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("commitment ID is required").
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdateCommitmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.Update(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Terminate commitment
// @ID terminateCommitment
// @Description Ends a commitment early, effective at the given time or now.
// @Tags Commitments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "delete"
// @Param id path string true "Commitment ID"
// @Param request body dto.TerminateCommitmentRequest false "Termination request"
// @Success 200 {object} dto.CommitmentResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /commitments/{id}/terminate [post]
func (h *CommitmentHandler) TerminateCommitment(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("commitment ID is required").
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	// An empty body, even one of unknown length, means terminate now.
	var req dto.TerminateCommitmentRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			c.Error(ierr.WithError(err).
				WithHint("Invalid request format").
				Mark(ierr.ErrValidation))
			return
		}
	}

	resp, err := h.service.Terminate(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
