package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/types"
)

// storedBillingEntity is the billing entity stored on the invoice, or nil when the tenant bills it.
func (s *invoiceService) storedBillingEntity(ctx context.Context, inv *invoice.Invoice) (*billingentity.BillingEntity, error) {
	if inv.BillingEntityID == nil {
		return nil, nil
	}
	return s.BillingEntityRepo.Get(ctx, *inv.BillingEntityID)
}

func (s *invoiceService) GetInvoiceBillingEntity(ctx context.Context, inv *invoice.Invoice) (*types.InvoiceBillingEntity, error) {
	if inv.BillingEntity != nil {
		return inv.BillingEntity, nil
	}
	if inv.InvoiceStatus != types.InvoiceStatusDraft {
		return nil, nil
	}
	entity, err := s.storedBillingEntity(ctx, inv)
	if err != nil {
		return nil, err
	}
	return entity.AsInvoiceBillingEntity(), nil
}

// numberPrefixFor is the invoice's entity prefix, or the environment prefix for an invoice without one.
func (s *invoiceService) numberPrefixFor(ctx context.Context, inv *invoice.Invoice, cfg *types.InvoiceConfig) (string, error) {
	entity, err := s.storedBillingEntity(ctx, inv)
	if err != nil {
		return "", err
	}
	if entity == nil {
		return cfg.InvoiceNumberPrefix, nil
	}
	return entity.InvoiceNumberPrefix, nil
}

// freezeBillingEntity snapshots the invoice's entity once; later calls keep the first snapshot.
func (s *invoiceService) freezeBillingEntity(ctx context.Context, inv *invoice.Invoice) error {
	if inv.BillingEntity != nil {
		return nil
	}
	entity, err := s.storedBillingEntity(ctx, inv)
	if err != nil {
		return err
	}
	inv.BillingEntity = entity.AsInvoiceBillingEntity()
	return nil
}

// billingEntityAsTenant presents a billing entity in the tenant shape the biller block and hosted page render.
func billingEntityAsTenant(t *tenant.Tenant, billingEntity *types.InvoiceBillingEntity) *tenant.Tenant {
	biller := &tenant.Tenant{}
	if t != nil {
		*biller = *t
	}
	biller.Name = billingEntity.Name
	biller.BillingDetails = billingentity.ToTenantBillingDetails(billingEntity.BillingDetails)
	return biller
}
