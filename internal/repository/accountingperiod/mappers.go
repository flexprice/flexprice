package accountingperiod

import (
	"github.com/flexprice/flexprice/ent"
	domainAccountingPeriod "github.com/flexprice/flexprice/internal/domain/accountingperiod"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// FromEnt maps an accounting_periods row to the domain accounting period.
func FromEnt(e *ent.AccountingPeriod) *domainAccountingPeriod.AccountingPeriod {
	if e == nil {
		return nil
	}
	return domainAccountingPeriod.NewBuilder(nil).
		WithID(e.ID).
		WithPeriodStart(e.PeriodStart).
		WithPeriodStatus(types.AccountingPeriodStatus(e.PeriodStatus)).
		WithStatus(types.Status(e.Status)).
		WithCreatedAt(e.CreatedAt).
		WithUpdatedAt(e.UpdatedAt).
		Build()
}

func FromEntList(ents []*ent.AccountingPeriod) []*domainAccountingPeriod.AccountingPeriod {
	return lo.Map(ents, func(e *ent.AccountingPeriod, _ int) *domainAccountingPeriod.AccountingPeriod {
		return FromEnt(e)
	})
}
