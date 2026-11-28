package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/pob"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type PerformanceObligationResponse struct {
	ID                 string                                      `json:"id"`
	ContractID         string                                      `json:"contract_id"`
	Name               string                                      `json:"name"`
	RecognitionMethod  types.RecognitionMethod                     `json:"recognition_method"`
	AmortizationMethod types.AmortizationMethod                    `json:"amortization_method"`
	ServiceStart       time.Time                                   `json:"service_start"`
	ServiceEnd         *time.Time                                  `json:"service_end,omitempty"`
	POBStatus          types.PerformanceObligationStatus           `json:"pob_status"`
	Status             types.Status                                `json:"status"`
	CreatedAt          time.Time                                   `json:"created_at"`
	UpdatedAt          time.Time                                   `json:"updated_at"`
	Associations       []*PerformanceObligationAssociationResponse `json:"associations,omitempty"`
}

type PerformanceObligationAssociationResponse struct {
	ID         string                 `json:"id"`
	EntityType types.RevRecEntityType `json:"entity_type"`
	EntityID   string                 `json:"entity_id"`
	CreatedAt  time.Time              `json:"created_at"`
}

func NewPerformanceObligationResponse(p *pob.PerformanceObligation) *PerformanceObligationResponse {
	if p == nil {
		return nil
	}
	return &PerformanceObligationResponse{
		ID:                 p.ID(),
		ContractID:         p.ContractID(),
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

// WithAssociations attaches the performance obligation's associations to the response.
func (r *PerformanceObligationResponse) WithAssociations(associations []*pob.Association) *PerformanceObligationResponse {
	r.Associations = lo.Map(associations, func(a *pob.Association, _ int) *PerformanceObligationAssociationResponse {
		return &PerformanceObligationAssociationResponse{
			ID:         a.ID(),
			EntityType: a.EntityType(),
			EntityID:   a.EntityID(),
			CreatedAt:  a.CreatedAt(),
		}
	})
	return r
}

// ListPerformanceObligationsResponse represents the response for listing performance obligations
type ListPerformanceObligationsResponse = types.ListResponse[*PerformanceObligationResponse] // @name ListPerformanceObligationsResponse
