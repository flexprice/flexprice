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
		WithProductID(e.ProductID).
		WithAllocatedAmount(e.AllocatedAmount).
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
