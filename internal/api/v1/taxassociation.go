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

type TaxConfigHandler struct {
	s      service.TaxConfigService
	logger *logger.Logger
}

func NewTaxConfigHandler(s service.TaxConfigService, logger *logger.Logger) *TaxConfigHandler {
	return &TaxConfigHandler{
		s:      s,
		logger: logger,
	}
}

// @Summary CreateTaxAssociation tax config
// @Description CreateTaxAssociation a new tax config
// @Tags Tax Configs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param tax_config body dto.CreateTaxAssociationRequest true "Tax Config Request"
// @Success 200 {object} dto.TaxAssociationResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxassociations [post]
func (h *TaxConfigHandler) CreateTaxAssociation(c *gin.Context) {
	var req dto.CreateTaxAssociationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	taxConfig, err := h.s.Create(c.Request.Context(), lo.ToPtr(req))
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, taxConfig)
}

// @Summary GetTaxAssociation tax config
// @Description GetTaxAssociation a tax config by ID
// @Tags Tax Configs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tax Config ID"
// @Success 200 {object} dto.TaxAssociationResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxassociations/{id} [get]
func (h *TaxConfigHandler) GetTaxAssociation(c *gin.Context) {
	id := c.Param("id")

	taxConfig, err := h.s.Get(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, taxConfig)
}

// @Summary Update tax association
// @Description Update a tax association by ID
// @Tags Tax Configs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tax Config ID"
// @Param tax_config body dto.TaxAssociationUpdateRequest true "Tax Config Request"
// @Success 200 {object} dto.TaxAssociationResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxassociations/{id} [put]
func (h *TaxConfigHandler) UpdateTaxAssociation(c *gin.Context) {
	id := c.Param("id")

	var req dto.TaxAssociationUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	taxConfig, err := h.s.Update(c.Request.Context(), id, lo.ToPtr(req))
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, taxConfig)
}

// @Summary Delete tax association
// @Description Delete a tax association by ID
// @Tags Tax Configs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tax Config ID"
// @Success 200 {object} dto.TaxAssociationResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxassociations/{id} [delete]
func (h *TaxConfigHandler) DeleteTaxAssociation(c *gin.Context) {
	id := c.Param("id")

	err := h.s.Delete(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}
}

// @Summary List tax associations
// @Description List tax associations
// @Tags Tax Configs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param tax_association body types.TaxAssociationFilter true "Tax Association Filter"
// @Success 200 {object} dto.ListTaxAssociationsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxassociations [get]
func (h *TaxConfigHandler) ListTaxAssociations(c *gin.Context) {
	var filter types.TaxAssociationFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	taxConfigs, err := h.s.List(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, taxConfigs)
}
