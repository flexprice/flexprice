package v1

import (
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
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

func (h *TaxConfigHandler) Create(c *gin.Context) {

}

func (h *TaxConfigHandler) Get(c *gin.Context) {

}

func (h *TaxConfigHandler) Update(c *gin.Context) {
}

func (h *TaxConfigHandler) Delete(c *gin.Context) {

}

func (h *TaxConfigHandler) List(c *gin.Context) {

}
