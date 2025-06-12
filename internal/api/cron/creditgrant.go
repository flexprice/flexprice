package cron

import (
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
)

type CreditGrantCronHandler struct {
	creditGrantService service.CreditGrantService
	log                *logger.Logger
}

func NewCreditGrantCronHandler(creditGrantService service.CreditGrantService, log *logger.Logger) *CreditGrantCronHandler {
	return &CreditGrantCronHandler{
		creditGrantService: creditGrantService,
		log:                log,
	}
}
