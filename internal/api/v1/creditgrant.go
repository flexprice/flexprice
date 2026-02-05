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

// @Summary Create a new credit grant
// @Description Create a new credit grant with the specified configuration
// @Tags CreditGrants
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param credit_grant body dto.CreateCreditGrantRequest true "Credit Grant configuration"
// @Success 201 {object} dto.CreditGrantResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
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

// @Summary Get a credit grant by ID
// @Description Get a credit grant by ID
// @Tags CreditGrants
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Credit Grant ID"
// @Success 200 {object} dto.CreditGrantResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
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

// @Summary Get credit grants
// @Description Get credit grants with the specified filter
// @Tags CreditGrants
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.CreditGrantFilter true "Filter"
// @Success 200 {object} dto.ListCreditGrantsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /creditgrants [get]
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

// @Summary Update a credit grant
// @Description Update a credit grant with the specified configuration
// @Tags CreditGrants
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Credit Grant ID"
// @Param credit_grant body dto.UpdateCreditGrantRequest true "Credit Grant configuration"
// @Success 200 {object} dto.CreditGrantResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
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

// @Summary Delete a credit grant
// @Description Delete a credit grant
// @Tags CreditGrants
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Credit Grant ID"
// @Success 200 {object} dto.SuccessResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /creditgrants/{id} [delete]
func (h *CreditGrantHandler) DeleteCreditGrant(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Credit Grant ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	if err := h.service.DeleteCreditGrant(c.Request.Context(), id); err != nil {
		h.log.Error("Failed to delete credit grant", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "credit grant deleted successfully"})
}

// @Summary Cancel future subscription credit grants
// @Description Cancel future credit grants for a subscription by setting their end date and archiving them. If credit_grant_ids is provided, only those specific grants are cancelled. If not provided or empty, all grants for the subscription are cancelled.
// @Tags CreditGrants
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.CancelFutureSubscriptionGrantsRequest true "Cancel future grants request"
// @Success 200 {object} dto.SuccessResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /creditgrants/cancel-future [post]
func (h *CreditGrantHandler) CancelFutureSubscriptionGrants(c *gin.Context) {
	var req dto.CancelFutureSubscriptionGrantsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	if err := h.service.CancelFutureSubscriptionGrants(c.Request.Context(), req); err != nil {
		h.log.Error("Failed to cancel future subscription grants", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "future subscription credit grants cancelled successfully"})
}
