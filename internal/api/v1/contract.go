package v1

import (
	"io"
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	contractsvc "github.com/flexprice/flexprice/internal/ee/service/contract"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	"github.com/gin-gonic/gin"
)

const maxContractUploadBodyBytes = 21 << 20

type ContractHandler struct {
	service contractsvc.ContractService
	ingest  contractsvc.ContractIngestService
	log     *logger.Logger
}

func NewContractHandler(service contractsvc.ContractService, ingest contractsvc.ContractIngestService, log *logger.Logger) *ContractHandler {
	return &ContractHandler{service: service, ingest: ingest, log: log}
}

// @Summary Upload contract
// @ID uploadContract
// @Description Use when you have a signed contract file to read. Stores the file, creates a draft contract with extraction_status pending and queues AI extraction. Poll GET /contracts/{id} until extraction_status is completed or failed.
// @Tags Contracts
// @Accept multipart/form-data
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param file formData file true "Contract file (PDF, PNG or JPG, up to 20 MB)"
// @Success 202 {object} dto.ContractResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Failure 503 {object} ierr.ErrorResponse "Contract processing unavailable"
// @Router /contracts/upload [post]
func (h *ContractHandler) UploadContract(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxContractUploadBodyBytes)

	header, err := c.FormFile("file")
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Attach the contract as the multipart field \"file\", up to 20 MB").
			Mark(ierr.ErrValidation))
		return
	}
	file, err := header.Open()
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Could not read the uploaded file").
			Mark(ierr.ErrValidation))
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Could not read the uploaded file").
			Mark(ierr.ErrValidation))
		return
	}

	created, err := h.ingest.Upload(c.Request.Context(), &contractsvc.UploadRequest{
		FileName: header.Filename,
		Data:     data,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusAccepted, dto.NewContractResponse(created))
}

// @Summary Get contract
// @ID getContract
// @Description Use when you need to load a single revenue recognition contract with the subscriptions it covers. Contracts are derived from subscriptions and are read-only.
// @Tags Contracts
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Contract ID"
// @Success 200 {object} dto.ContractResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /contracts/{id} [get]
func (h *ContractHandler) GetContract(c *gin.Context) {
	resp, err := h.service.GetContract(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Query contracts
// @ID queryContract
// @Description Use when searching revenue recognition contracts by customer, subscription or contract status. Returns a paginated list.
// @Tags Contracts
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param filter body contractRepo.Filter true "Filter"
// @Success 200 {object} dto.ListContractsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /contracts/search [post]
func (h *ContractHandler) QueryContracts(c *gin.Context) {
	var filter contractRepo.Filter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetContracts(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Recompute derived contract terms
// @ID deriveContractTerms
// @Description Use while reviewing an uploaded contract to see the end date, notice deadline and invoice schedules that follow from edited terms. Omitted fields keep the extracted value; charges line up with extraction.result.charges by position. Nothing is saved.
// @Tags Contracts
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Contract ID"
// @Param request body dto.DeriveContractTermsRequest true "Edited terms"
// @Success 200 {object} dto.DeriveContractTermsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /contracts/{id}/derive [post]
func (h *ContractHandler) DeriveContractTerms(c *gin.Context) {
	var req dto.DeriveContractTermsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request body").
			Mark(ierr.ErrValidation))
		return
	}

	derived, err := h.ingest.DeriveTerms(c.Request.Context(), c.Param("id"), &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.DeriveContractTermsResponse{Derived: derived})
}

// @Summary Get contract review
// @ID getContractReview
// @Description Use to load the reviewer's working copy of an uploaded contract's terms, with derived values and what is left to check. Before the first save it is the extraction, at version 0.
// @Tags Contracts
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Contract ID"
// @Success 200 {object} dto.ContractReviewResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /contracts/{id}/review [get]
func (h *ContractHandler) GetContractReview(c *gin.Context) {
	resp, err := h.ingest.GetReview(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Save contract review step
// @ID saveContractReviewStep
// @Description Use to save one review step (customer, charges or billing) of an uploaded contract: edited term values, confirmed terms and, on the customer step, the customer decision. Send the version you loaded; a 409 means the review changed since.
// @Tags Contracts
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Contract ID"
// @Param step path string true "Review step" Enums(customer, charges, billing)
// @Param request body dto.SaveContractReviewRequest true "Step edits"
// @Success 200 {object} dto.ContractReviewResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 409 {object} ierr.ErrorResponse "Review changed since it was loaded"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /contracts/{id}/review/{step} [put]
func (h *ContractHandler) SaveContractReviewStep(c *gin.Context) {
	var req dto.SaveContractReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request body").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.ingest.SaveReviewStep(c.Request.Context(), c.Param("id"), c.Param("step"), &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Complete draft contract
// @ID completeContractDraft
// @Description Use after reviewing a draft contract. Creates its subscription as a draft and the given performance obligations as drafts. With activate true, also activates the subscription; the contract and its performance obligations follow it.
// @Tags Contracts
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Contract ID"
// @Param request body dto.CompleteDraftRequest true "Subscription, performance obligations and activation"
// @Success 200 {object} dto.ContractResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /contracts/{id}/complete-draft [post]
func (h *ContractHandler) CompleteDraft(c *gin.Context) {
	var req dto.CompleteDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request body").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.ingest.CompleteDraft(c.Request.Context(), c.Param("id"), &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Activate contract
// @ID activateContract
// @Description Use when a completed draft contract starts. Activates its draft subscriptions from term_start; the contract and its performance obligations become active with them.
// @Tags Contracts
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Contract ID"
// @Param request body dto.ActivateContractRequest true "Term start"
// @Success 200 {object} dto.ContractResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /contracts/{id}/activate [post]
func (h *ContractHandler) Activate(c *gin.Context) {
	var req dto.ActivateContractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request body").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.ingest.Activate(c.Request.Context(), c.Param("id"), &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
