package contract

import (
	"github.com/flexprice/flexprice/ent"
	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// FromEnt maps a contracts row to the domain contract.
func FromEnt(e *ent.Contract) *domainContract.Contract {
	if e == nil {
		return nil
	}
	return domainContract.NewBuilder(nil).
		WithID(e.ID).
		WithCustomerID(e.CustomerID).
		WithCurrency(e.Currency).
		WithTermStart(e.TermStart).
		WithTermEnd(e.TermEnd).
		WithEndedAt(e.EndedAt).
		WithContractStatus(types.ContractStatus(e.ContractStatus)).
		WithStatus(types.Status(e.Status)).
		WithCreatedAt(e.CreatedAt).
		WithUpdatedAt(e.UpdatedAt).
		Build()
}

func FromEntList(ents []*ent.Contract) []*domainContract.Contract {
	return lo.Map(ents, func(e *ent.Contract, _ int) *domainContract.Contract {
		return FromEnt(e)
	})
}

// AssociationFromEnt maps a contract_associations row to the domain association.
func AssociationFromEnt(e *ent.ContractAssociation) *domainContract.Association {
	if e == nil {
		return nil
	}
	return domainContract.NewAssociationBuilder(nil).
		WithID(e.ID).
		WithContractID(e.ContractID).
		WithEntityType(types.RevRecEntityType(e.EntityType)).
		WithEntityID(e.EntityID).
		WithStatus(types.Status(e.Status)).
		WithCreatedAt(e.CreatedAt).
		Build()
}

func AssociationFromEntList(ents []*ent.ContractAssociation) []*domainContract.Association {
	return lo.Map(ents, func(e *ent.ContractAssociation, _ int) *domainContract.Association {
		return AssociationFromEnt(e)
	})
}
