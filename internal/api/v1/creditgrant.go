package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type CreditGrantHandler struct {
	service service.CreditGrantService
	log     *logger.Logger
}

func NewCreditGrantHandler(service service.CreditGrantService, log *logger.Logger) *CreditGrantHandler {
	return &CreditGrantHandler{service: service, log: log}
}

// @Summary Create credit grant
// @ID createCreditGrant
// @Description Use when giving a customer or plan credits (e.g. prepaid balance or promotional credits). Scope can be plan or subscription; supports start/end dates.
// @Tags Credit Grants
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param credit_grant body dto.CreateCreditGrantRequest true "Credit Grant configuration"
// @Success 201 {object} dto.CreditGrantResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /creditgrants [post]
func (h *CreditGrantHandler) CreateCreditGrant(c *gin.Context) {
	var req dto.CreateCreditGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateCreditGrant(c.Request.Context(), req)
	if err != nil {
		h.log.Error("Failed to create credit grant", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get credit grant
// @ID getCreditGrant
// @Description Use when you need to load a single credit grant (e.g. for display or to check balance).
// @Tags Credit Grants
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Credit Grant ID"
// @Success 200 {object} dto.CreditGrantResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /creditgrants/{id} [get]
func (h *CreditGrantHandler) GetCreditGrant(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Credit Grant ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetCreditGrant(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to get credit grant", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *CreditGrantHandler) ListCreditGrants(c *gin.Context) {
	var filter types.CreditGrantFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		h.log.Error("Failed to bind query", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	// Set default filter if not provided
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	resp, err := h.service.ListCreditGrants(c.Request.Context(), &filter)
	if err != nil {
		h.log.Error("Failed to list credit grants", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update credit grant
// @ID updateCreditGrant
// @Description Use when changing a credit grant (e.g. amount or end date). Request body contains the fields to update.
// @Tags Credit Grants
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Credit Grant ID"
// @Param credit_grant body dto.UpdateCreditGrantRequest true "Credit Grant configuration"
// @Success 200 {object} dto.CreditGrantResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /creditgrants/{id} [put]
func (h *CreditGrantHandler) UpdateCreditGrant(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Credit Grant ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdateCreditGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateCreditGrant(c.Request.Context(), id, req)
	if err != nil {
		h.log.Error("Failed to update credit grant", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete credit grant
// @ID deleteCreditGrant
// @Description Use when removing or ending a credit grant (e.g. revoke promo or close prepaid). Plan-scoped grants are archived; subscription-scoped supports optional effective_date in body.
// @Tags Credit Grants
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Credit Grant ID"
// @Param body body dto.DeleteCreditGrantRequest false "Optional: effective_date for subscription-scoped grants"
// @Success 200 {object} dto.SuccessResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /creditgrants/{id} [delete]
func (h *CreditGrantHandler) DeleteCreditGrant(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Credit Grant ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	req := dto.DeleteCreditGrantRequest{CreditGrantID: id}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			h.log.Error("Failed to bind JSON", "error", err)
			c.Error(ierr.WithError(err).
				WithHint("Invalid request format").
				Mark(ierr.ErrValidation))
			return
		}
		req.CreditGrantID = id // always from path
	}

	if err := h.service.DeleteCreditGrant(c.Request.Context(), req); err != nil {
		h.log.Error("Failed to delete credit grant", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "credit grant deleted successfully"})
}
