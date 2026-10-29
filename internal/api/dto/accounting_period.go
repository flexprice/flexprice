package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/accountingperiod"
	"github.com/flexprice/flexprice/internal/types"
)

type AccountingPeriodResponse struct {
	ID           string                       `json:"id"`
	PeriodStart  time.Time                    `json:"period_start"`
	PeriodEnd    time.Time                    `json:"period_end"`
	PeriodStatus types.AccountingPeriodStatus `json:"period_status"`
	Status       types.Status                 `json:"status"`
	CreatedAt    time.Time                    `json:"created_at"`
	UpdatedAt    time.Time                    `json:"updated_at"`
}

func NewAccountingPeriodResponse(p *accountingperiod.AccountingPeriod) *AccountingPeriodResponse {
	if p == nil {
		return nil
	}
	return &AccountingPeriodResponse{
		ID:           p.ID(),
		PeriodStart:  p.PeriodStart(),
		PeriodEnd:    p.PeriodEnd(),
		PeriodStatus: p.PeriodStatus(),
		Status:       p.Status(),
		CreatedAt:    p.CreatedAt(),
		UpdatedAt:    p.UpdatedAt(),
	}
}

// ListAccountingPeriodsResponse represents the response for listing accounting periods
type ListAccountingPeriodsResponse = types.ListResponse[*AccountingPeriodResponse] // @name ListAccountingPeriodsResponse
