package v1

import (
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
)

type TaxRateHandler struct {
	service service.TaxRateService
	logger  *logger.Logger
}

func NewTaxRateHandler(service service.TaxRateService, logger *logger.Logger) *TaxRateHandler {
	return &TaxRateHandler{
		service: service,
		logger:  logger,
	}
}
