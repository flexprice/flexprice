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
