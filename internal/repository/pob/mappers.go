package pob

import (
	"github.com/flexprice/flexprice/ent"
	domainPOB "github.com/flexprice/flexprice/internal/domain/pob"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// FromEnt maps a performance_obligations row to the domain performance obligation.
func FromEnt(e *ent.PerformanceObligation) *domainPOB.PerformanceObligation {
	if e == nil {
		return nil
	}
	return domainPOB.NewBuilder(nil).
		WithID(e.ID).
		WithContractID(e.ContractID).
		WithName(e.Name).
		WithRecognitionMethod(types.RecognitionMethod(e.RecognitionMethod)).
		WithAmortizationMethod(types.AmortizationMethod(e.AmortizationMethod)).
		WithServiceStart(e.ServiceStart).
		WithServiceEnd(e.ServiceEnd).
		WithPOBStatus(types.PerformanceObligationStatus(e.PobStatus)).
		WithStatus(types.Status(e.Status)).
		WithCreatedAt(e.CreatedAt).
		WithUpdatedAt(e.UpdatedAt).
		Build()
}

func FromEntList(ents []*ent.PerformanceObligation) []*domainPOB.PerformanceObligation {
	return lo.Map(ents, func(e *ent.PerformanceObligation, _ int) *domainPOB.PerformanceObligation {
		return FromEnt(e)
	})
}

// AssociationFromEnt maps a performance_obligation_associations row to the domain association.
func AssociationFromEnt(e *ent.PerformanceObligationAssociation) *domainPOB.Association {
	if e == nil {
		return nil
	}
	return domainPOB.NewAssociationBuilder(nil).
		WithID(e.ID).
		WithPOBID(e.PobID).
		WithEntityType(types.RevRecEntityType(e.EntityType)).
		WithEntityID(e.EntityID).
		WithStatus(types.Status(e.Status)).
		WithCreatedAt(e.CreatedAt).
		Build()
}

func AssociationFromEntList(ents []*ent.PerformanceObligationAssociation) []*domainPOB.Association {
	return lo.Map(ents, func(e *ent.PerformanceObligationAssociation, _ int) *domainPOB.Association {
		return AssociationFromEnt(e)
	})
}
