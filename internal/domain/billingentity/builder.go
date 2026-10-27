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
	cp.TaxIdentifiers = append([]types.TaxIdentifier(nil), e.TaxIdentifiers...)
	cp.Metadata = lo.Assign(map[string]string{}, e.Metadata)

	return &billingEntityBuilder{e: &cp}
}

func (b *billingEntityBuilder) WithName(v string) *billingEntityBuilder {
	b.e.Name = v
	return b
}

func (b *billingEntityBuilder) WithLegalName(v string) *billingEntityBuilder {
	b.e.LegalName = v
	return b
}

func (b *billingEntityBuilder) WithInvoiceNumberPrefix(v string) *billingEntityBuilder {
	b.e.InvoiceNumberPrefix = v
	return b
}

func (b *billingEntityBuilder) WithSellerDetails(v types.SellerDetails) *billingEntityBuilder {
	b.e.SellerDetails = v
	return b
}

func (b *billingEntityBuilder) WithTaxIdentifiers(v []types.TaxIdentifier) *billingEntityBuilder {
	b.e.TaxIdentifiers = append([]types.TaxIdentifier(nil), v...)
	return b
}

func (b *billingEntityBuilder) WithMetadata(v map[string]string) *billingEntityBuilder {
	b.e.Metadata = lo.Assign(map[string]string{}, v)
	return b
}

func (b *billingEntityBuilder) Build() *BillingEntity {
	return b.e
}
