package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// customerBillingEntity is the effective billing entity of the invoice's customer, or nil.
func (s *invoiceService) customerBillingEntity(ctx context.Context, inv *invoice.Invoice) (*billingentity.BillingEntity, error) {
	cust, err := s.CustomerRepo.Get(ctx, inv.CustomerID)
	if err != nil {
		return nil, err
	}
	return NewBillingEntityService(s.ServiceParams).EffectiveEntity(ctx, cust)
}

func (s *invoiceService) GetInvoiceBillingEntity(ctx context.Context, inv *invoice.Invoice) (*types.InvoiceBillingEntity, error) {
	if inv.BillingEntity != nil {
		return inv.BillingEntity, nil
	}
	if inv.InvoiceStatus != types.InvoiceStatusDraft {
		return nil, nil
	}
	entity, err := s.customerBillingEntity(ctx, inv)
	if err != nil {
		return nil, err
	}
	return entity.AsInvoiceBillingEntity(), nil
}

// numberPrefixFor is the customer's entity prefix, or the environment prefix for a customer without one.
func (s *invoiceService) numberPrefixFor(ctx context.Context, inv *invoice.Invoice, cfg *types.InvoiceConfig) (string, error) {
	entity, err := s.customerBillingEntity(ctx, inv)
	if err != nil {
		return "", err
	}
	if entity == nil {
		return cfg.InvoiceNumberPrefix, nil
	}
	return entity.InvoiceNumberPrefix, nil
}

// freezeBillingEntity sets the customer's entity as the invoice's billing entity once; later calls keep the first one.
func (s *invoiceService) freezeBillingEntity(ctx context.Context, inv *invoice.Invoice) error {
	if inv.BillingEntity == nil {
		entity, err := s.customerBillingEntity(ctx, inv)
		if err != nil {
			return err
		}
		inv.BillingEntity = entity.AsInvoiceBillingEntity()
	}
	if inv.BillingEntityID == nil && inv.BillingEntity != nil {
		inv.BillingEntityID = lo.ToPtr(inv.BillingEntity.BillingEntityID)
	}
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
