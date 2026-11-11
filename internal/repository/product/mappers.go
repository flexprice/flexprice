package product

import (
	"github.com/flexprice/flexprice/ent"
	domainProduct "github.com/flexprice/flexprice/internal/domain/product"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// FromEnt maps a products row to the domain product.
func FromEnt(e *ent.Product) *domainProduct.Product {
	if e == nil {
		return nil
	}
	return domainProduct.NewBuilder(nil).
		WithID(e.ID).
		WithName(e.Name).
		WithDescription(e.Description).
		WithLookupKey(e.LookupKey).
		WithStatus(types.Status(e.Status)).
		WithCreatedAt(e.CreatedAt).
		WithUpdatedAt(e.UpdatedAt).
		Build()
}

func FromEntList(ents []*ent.Product) []*domainProduct.Product {
	return lo.Map(ents, func(e *ent.Product, _ int) *domainProduct.Product {
		return FromEnt(e)
	})
}
