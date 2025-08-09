package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type TaxHandler struct {
	service service.TaxService
	logger  *logger.Logger
}

func NewTaxHandler(service service.TaxService, logger *logger.Logger) *TaxHandler {
	return &TaxHandler{
		service: service,
		logger:  logger,
	}
}

// @Summary Create a tax rate
// @Description Create a tax rate
// @Tags Tax Rates
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param tax_rate body dto.CreateTaxRateRequest true "Tax rate to create"
// @Success 201 {object} dto.TaxRateResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxes/rates [post]
func (h *TaxHandler) CreateTaxRate(c *gin.Context) {
	var req dto.CreateTaxRateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateTaxRate(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get a tax rate
// @Description Get a tax rate
// @Tags Tax Rates
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tax rate ID"
// @Success 200 {object} dto.TaxRateResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxes/rates/{id} [get]
func (h *TaxHandler) GetTaxRate(c *gin.Context) {
	taxRates, err := h.service.GetTaxRate(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, taxRates)
}

// @Summary Get tax rates
// @Description Get tax rates
// @Tags Tax Rates
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.TaxRateFilter true "Filter"
// @Success 200 {object} []dto.TaxRateResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxes/rates [get]
func (h *TaxHandler) ListTaxRates(c *gin.Context) {
	var filter types.TaxRateFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	taxRates, err := h.service.ListTaxRates(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, taxRates)
}

// @Summary Update a tax rate
// @Description Update a tax rate
// @Tags Tax Rates
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tax rate ID"
// @Param tax_rate body dto.UpdateTaxRateRequest true "Tax rate to update"
// @Success 200 {object} dto.TaxRateResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxes/rates/{id} [put]
func (h *TaxHandler) UpdateTaxRate(c *gin.Context) {
	var req dto.UpdateTaxRateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	id := c.Param("id")

	resp, err := h.service.UpdateTaxRate(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete a tax rate
// @Description Delete a tax rate
// @Tags Tax Rates
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tax rate ID"
// @Success 204 {object} nil
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxes/rates/{id} [delete]
func (h *TaxHandler) DeleteTaxRate(c *gin.Context) {
	id := c.Param("id")

	err := h.service.DeleteTaxRate(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// @Summary Create Tax Association
// @Description Create a new tax association
// @Tags Tax Associations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param tax_config body dto.CreateTaxAssociationRequest true "Tax Config Request"
// @Success 200 {object} dto.TaxAssociationResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxes/associations [post]
func (h *TaxHandler) CreateTaxAssociation(c *gin.Context) {
	var req dto.CreateTaxAssociationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	taxConfig, err := h.service.CreateTaxAssociation(c.Request.Context(), lo.ToPtr(req))
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, taxConfig)
}

// @Summary Get Tax Association
// @Description Get a tax association by ID
// @Tags Tax Associations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tax Config ID"
// @Success 200 {object} dto.TaxAssociationResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxes/associations/{id} [get]
func (h *TaxHandler) GetTaxAssociation(c *gin.Context) {
	id := c.Param("id")

	taxConfig, err := h.service.GetTaxAssociation(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, taxConfig)
}

// @Summary Update tax association
// @Description Update a tax association by ID
// @Tags Tax Associations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tax Config ID"
// @Param tax_config body dto.TaxAssociationUpdateRequest true "Tax Config Request"
// @Success 200 {object} dto.TaxAssociationResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxes/associations/{id} [put]
func (h *TaxHandler) UpdateTaxAssociation(c *gin.Context) {
	id := c.Param("id")

	var req dto.TaxAssociationUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	taxConfig, err := h.service.UpdateTaxAssociation(c.Request.Context(), id, lo.ToPtr(req))
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, taxConfig)
}

// @Summary Delete tax association
// @Description Delete a tax association by ID
// @Tags Tax Associations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tax Config ID"
// @Success 200 {object} dto.TaxAssociationResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxes/associations/{id} [delete]
func (h *TaxHandler) DeleteTaxAssociation(c *gin.Context) {
	id := c.Param("id")

	err := h.service.DeleteTaxAssociation(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}
}

// @Summary List tax associations
// @Description List tax associations
// @Tags Tax Associations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param tax_association body types.TaxAssociationFilter true "Tax Association Filter"
// @Success 200 {object} dto.ListTaxAssociationsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxes/associations [get]
func (h *TaxHandler) ListTaxAssociations(c *gin.Context) {
	var filter types.TaxAssociationFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	taxConfigs, err := h.service.ListTaxAssociations(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, taxConfigs)
}
