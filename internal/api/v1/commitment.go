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
	commitmentService service.CommitmentService
	logger            *logger.Logger
}

func NewCommitmentHandler(commitmentService service.CommitmentService, logger *logger.Logger) *CommitmentHandler {
	return &CommitmentHandler{commitmentService: commitmentService, logger: logger}
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

	resp, err := h.commitmentService.Preview(c.Request.Context(), req)
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

	resp, err := h.commitmentService.Create(c.Request.Context(), req)
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
// @x-scope "read"
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

	resp, err := h.commitmentService.Get(c.Request.Context(), id)
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

	resp, err := h.commitmentService.List(c.Request.Context(), &filter)
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

	resp, err := h.commitmentService.Update(c.Request.Context(), id, req)
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

	resp, err := h.commitmentService.Terminate(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get billing terms
// @ID getCommitmentBillingTerms
// @Description Returns the commitment's current billing terms and the instalments they produce. Cancelled instalments are listed by the instalments endpoint.
// @Tags Commitments
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Commitment ID"
// @Success 200 {object} dto.BillingTermsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /commitments/{id}/billing-terms [get]
func (h *CommitmentHandler) GetBillingTerms(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("commitment ID is required").
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.commitmentService.GetBillingTerms(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Edit billing terms
// @ID replaceCommitmentBillingTerms
// @Description Replaces the whole set of billing terms of an active commitment. Every current instalment is cancelled, including invoiced ones, and the new terms generate fresh instalments against the unchanged total. Invoices are not touched; invoices_to_review lists the finalized invoices that carry a replaced instalment.
// @Tags Commitments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Commitment ID"
// @Param billing_terms body dto.ReplaceBillingTermsRequest true "Billing terms"
// @Success 200 {object} dto.BillingTermsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /commitments/{id}/billing-terms [put]
func (h *CommitmentHandler) ReplaceBillingTerms(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("commitment ID is required").
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.ReplaceBillingTermsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.commitmentService.ReplaceBillingTerms(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Preview billing terms edit
// @ID previewCommitmentBillingTerms
// @Description Returns what editing the billing terms would produce, without saving anything: the resolved terms, the new instalments and the invoices to review.
// @Tags Commitments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Commitment ID"
// @Param billing_terms body dto.ReplaceBillingTermsRequest true "Billing terms"
// @Success 200 {object} dto.BillingTermsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /commitments/{id}/billing-terms/preview [post]
func (h *CommitmentHandler) PreviewBillingTerms(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("commitment ID is required").
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.ReplaceBillingTermsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.commitmentService.PreviewBillingTerms(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary List instalments
// @ID listCommitmentInstalments
// @Description Lists the instalments of the commitment in billing order. Without a status filter every instalment is listed, cancelled ones included.
// @Tags Commitments
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Commitment ID"
// @Param status query []string false "Only instalments in these statuses (scheduled, invoiced, cancelled)" collectionFormat(multi)
// @Success 200 {object} dto.ListInstalmentsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /commitments/{id}/instalments [get]
func (h *CommitmentHandler) ListInstalments(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("commitment ID is required").
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	statuses := lo.Map(c.QueryArray("status"), func(status string, _ int) types.InstalmentStatus {
		return types.InstalmentStatus(status)
	})
	for _, status := range statuses {
		if status == "" {
			c.Error(ierr.NewError("status must not be empty").
				WithHint("status must be one of scheduled, invoiced, cancelled").
				Mark(ierr.ErrValidation))
			return
		}

		if err := status.Validate(); err != nil {
			c.Error(err)
			return
		}
	}

	resp, err := h.commitmentService.ListInstalments(c.Request.Context(), id, statuses)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
