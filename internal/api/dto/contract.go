package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type ContractResponse struct {
	ID             string                         `json:"id"`
	CustomerID     string                         `json:"customer_id"`
	Currency       string                         `json:"currency"`
	TermStart      *time.Time                     `json:"term_start,omitempty"`
	TermEnd        *time.Time                     `json:"term_end,omitempty"`
	EndedAt        *time.Time                     `json:"ended_at,omitempty"`
	ContractStatus types.ContractStatus           `json:"contract_status"`
	Status         types.Status                   `json:"status"`
	CreatedAt      time.Time                      `json:"created_at"`
	UpdatedAt      time.Time                      `json:"updated_at"`
	Associations   []*ContractAssociationResponse `json:"associations,omitempty"`
}

type ContractAssociationResponse struct {
	ID         string                 `json:"id"`
	EntityType types.RevRecEntityType `json:"entity_type"`
	EntityID   string                 `json:"entity_id"`
	CreatedAt  time.Time              `json:"created_at"`
}

func NewContractResponse(c *contract.Contract) *ContractResponse {
	if c == nil {
		return nil
	}
	return &ContractResponse{
		ID:             c.ID(),
		CustomerID:     c.CustomerID(),
		Currency:       c.Currency(),
		TermStart:      c.TermStart(),
		TermEnd:        c.TermEnd(),
		EndedAt:        c.EndedAt(),
		ContractStatus: c.ContractStatus(),
		Status:         c.Status(),
		CreatedAt:      c.CreatedAt(),
		UpdatedAt:      c.UpdatedAt(),
	}
}

// WithAssociations attaches the contract's associations to the response.
func (r *ContractResponse) WithAssociations(associations []*contract.Association) *ContractResponse {
	r.Associations = lo.Map(associations, func(a *contract.Association, _ int) *ContractAssociationResponse {
		return &ContractAssociationResponse{
			ID:         a.ID(),
			EntityType: a.EntityType(),
			EntityID:   a.EntityID(),
			CreatedAt:  a.CreatedAt(),
		}
	})
	return r
}

// ListContractsResponse represents the response for listing contracts
type ListContractsResponse = types.ListResponse[*ContractResponse] // @name ListContractsResponse
