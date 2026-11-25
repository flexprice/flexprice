package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/pob"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type PerformanceObligationResponse struct {
	ID                 string                            `json:"id"`
	ContractID         string                            `json:"contract_id"`
	ProductID          string                            `json:"product_id,omitempty"`
	AllocatedAmount    decimal.Decimal                   `json:"allocated_amount" swaggertype:"string"`
	Name               string                            `json:"name"`
	RecognitionMethod  types.RecognitionMethod           `json:"recognition_method"`
	AmortizationMethod types.AmortizationMethod          `json:"amortization_method"`
	ServiceStart       time.Time                         `json:"service_start"`
	ServiceEnd         *time.Time                        `json:"service_end,omitempty"`
	POBStatus          types.PerformanceObligationStatus `json:"pob_status"`
	Status             types.Status                      `json:"status"`
	CreatedAt          time.Time                         `json:"created_at"`
	UpdatedAt          time.Time                         `json:"updated_at"`
}

func NewPerformanceObligationResponse(p *pob.PerformanceObligation) *PerformanceObligationResponse {
	if p == nil {
		return nil
	}
	return &PerformanceObligationResponse{
		ID:                 p.ID(),
		ContractID:         p.ContractID(),
		ProductID:          p.ProductID(),
		AllocatedAmount:    p.AllocatedAmount(),
		Name:               p.Name(),
		RecognitionMethod:  p.RecognitionMethod(),
		AmortizationMethod: p.AmortizationMethod(),
		ServiceStart:       p.ServiceStart(),
		ServiceEnd:         p.ServiceEnd(),
		POBStatus:          p.POBStatus(),
		Status:             p.Status(),
		CreatedAt:          p.CreatedAt(),
		UpdatedAt:          p.UpdatedAt(),
	}
}

// ListPerformanceObligationsResponse represents the response for listing performance obligations
type ListPerformanceObligationsResponse = types.ListResponse[*PerformanceObligationResponse] // @name ListPerformanceObligationsResponse
