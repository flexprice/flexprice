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

// @Summary CreateTaxConfig tax config
// @Description CreateTaxConfig a new tax config
// @Tags Tax Configs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param tax_config body dto.TaxConfigCreateRequest true "Tax Config Request"
// @Success 200 {object} dto.TaxConfigResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxconfigs [post]
func (h *TaxConfigHandler) CreateTaxConfig(c *gin.Context) {
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

// @Summary GetTaxConfig tax config
// @Description GetTaxConfig a tax config by ID
// @Tags Tax Configs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tax Config ID"
// @Success 200 {object} dto.TaxConfigResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxconfigs/{id} [get]
func (h *TaxConfigHandler) GetTaxConfig(c *gin.Context) {
	id := c.Param("id")

	taxConfig, err := h.s.Get(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, taxConfig)
}

// @Summary Update tax config
// @Description Update a tax config by ID
// @Tags Tax Configs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tax Config ID"
// @Param tax_config body dto.TaxConfigUpdateRequest true "Tax Config Request"
// @Success 200 {object} dto.TaxConfigResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxconfigs/{id} [put]
func (h *TaxConfigHandler) UpdateTaxConfig(c *gin.Context) {
	id := c.Param("id")

	var req dto.TaxConfigUpdateRequest
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

// @Summary Delete tax config
// @Description Delete a tax config by ID
// @Tags Tax Configs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Tax Config ID"
// @Success 200 {object} dto.TaxConfigResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxconfigs/{id} [delete]
func (h *TaxConfigHandler) DeleteTaxConfig(c *gin.Context) {
	id := c.Param("id")

	err := h.s.Delete(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}
}

// @Summary List tax configs
// @Description List tax configs
// @Tags Tax Configs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param tax_config body types.TaxConfigFilter true "Tax Config Filter"
// @Success 200 {object} dto.ListTaxConfigsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /taxconfigs [get]
func (h *TaxConfigHandler) ListTaxConfigs(c *gin.Context) {
	var filter types.TaxConfigFilter
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
