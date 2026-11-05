package billingentity

import (
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type billingEntityBuilder struct {
	e *BillingEntity
}

// NewBillingEntityBuilder starts from a copy of e (or an empty entity when e is nil).
func NewBillingEntityBuilder(e *BillingEntity) *billingEntityBuilder {
	if e == nil {
		return &billingEntityBuilder{e: &BillingEntity{}}
	}

	cp := *e
	cp.Metadata = lo.Assign(map[string]string{}, e.Metadata)

	return &billingEntityBuilder{e: &cp}
}

func (b *billingEntityBuilder) WithName(v string) *billingEntityBuilder {
	b.e.Name = v
	return b
}

func (b *billingEntityBuilder) WithInvoiceNumberPrefix(v string) *billingEntityBuilder {
	b.e.InvoiceNumberPrefix = v
	return b
}

func (b *billingEntityBuilder) WithBillingDetails(v types.BillingDetails) *billingEntityBuilder {
	b.e.BillingDetails = v
	return b
}

func (b *billingEntityBuilder) WithMetadata(v map[string]string) *billingEntityBuilder {
	b.e.Metadata = lo.Assign(map[string]string{}, v)
	return b
}

func (b *billingEntityBuilder) Build() *BillingEntity {
	return b.e
}
