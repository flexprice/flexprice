package v1

import (
	"net/http"

	contractsvc "github.com/flexprice/flexprice/internal/ee/service/contract"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	"github.com/gin-gonic/gin"
)

type ContractHandler struct {
	service contractsvc.ContractService
	log     *logger.Logger
}

func NewContractHandler(service contractsvc.ContractService, log *logger.Logger) *ContractHandler {
	return &ContractHandler{service: service, log: log}
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
