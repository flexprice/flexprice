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
// @Description Ends a commitment immediately. effective_at defaults to now and cannot be in the future. Under billing terms, its scheduled instalments are cancelled and any amount it earned beyond what is already invoiced is billed as a settlement. The settlement and a fixed cancellation fee, when given, are billed together on one one-off invoice.
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
// @Description Returns the commitment's current billing terms with its scheduled and invoiced instalments in billing order. Invoiced instalments may come from terms an edit replaced. Cancelled instalments are listed by the instalments endpoint.
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
// @Description Replaces the whole set of billing terms of an active commitment. Its scheduled instalments are cancelled and the new terms generate instalments for what the invoiced ones leave of the commitment total; invoiced instalments and their invoices are kept. A term without bill_on or start_date starts at the billing period holding now, or at the commitment start once its term has ended. Returns the new terms with the scheduled and invoiced instalments in billing order.
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
// @Description Returns what editing the billing terms would produce, without saving anything: the new terms with the invoiced and new instalments in billing order.
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

// @Summary Trigger instalment
// @ID triggerCommitmentInstalment
// @Description Bills a manual instalment of an active commitment on bill_on, now by default, which must fall within the commitment term. The instalment goes on the first subscription invoice at or after that date, or on its own invoice once due when its instalment_invoicing, the term's or else the commitment's, is separate_invoice. Either way it waits while the subscription is trialing or paused. Until it is triggered, a manual instalment waits at the end of the term, and bill_on can only move earlier; edit the billing terms to bill it later.
// @Tags Commitments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Commitment ID"
// @Param instalment_id path string true "Instalment ID"
// @Param trigger body dto.TriggerInstalmentRequest false "Trigger request"
// @Success 200 {object} billingterm.Instalment
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /commitments/{id}/instalments/{instalment_id}/trigger [post]
func (h *CommitmentHandler) TriggerInstalment(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("commitment ID is required").
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	instalmentID := c.Param("instalment_id")
	if instalmentID == "" {
		c.Error(ierr.NewError("instalment ID is required").
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	// An empty body, even one of unknown length, means trigger now.
	var req dto.TriggerInstalmentRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			c.Error(ierr.WithError(err).
				WithHint("Invalid request format").
				Mark(ierr.ErrValidation))
			return
		}
	}

	resp, err := h.commitmentService.TriggerInstalment(c.Request.Context(), id, instalmentID, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
