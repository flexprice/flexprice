package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
)

// invoiceBillingEntity is the effective billing entity of the invoice's customer, or nil.
func (s *invoiceService) invoiceBillingEntity(ctx context.Context, inv *invoice.Invoice) (*billingentity.BillingEntity, error) {
	cust, err := s.CustomerRepo.Get(ctx, inv.CustomerID)
	if err != nil {
		return nil, err
	}
	return NewBillingEntityService(s.ServiceParams).EffectiveEntity(ctx, cust)
}

// sellerFor is the frozen seller of a finalized invoice, else the customer's entity now; nil means the tenant sells.
func (s *invoiceService) sellerFor(ctx context.Context, inv *invoice.Invoice) (*types.SellerSnapshot, error) {
	if inv.InvoiceStatus == types.InvoiceStatusFinalized && inv.SellerSnapshot != nil {
		return inv.SellerSnapshot, nil
	}
	entity, err := s.invoiceBillingEntity(ctx, inv)
	if err != nil {
		return nil, err
	}
	return entity.SellerSnapshot(), nil
}

// numberPrefixFor is the customer's entity prefix, or the environment prefix for a customer without one.
func (s *invoiceService) numberPrefixFor(ctx context.Context, inv *invoice.Invoice, cfg *types.InvoiceConfig) (string, error) {
	entity, err := s.invoiceBillingEntity(ctx, inv)
	if err != nil {
		return "", err
	}
	if entity == nil {
		return cfg.InvoiceNumberPrefix, nil
	}
	return entity.InvoiceNumberPrefix, nil
}

// applySellerSnapshot freezes the customer's entity as seller once; later calls keep the first snapshot.
func (s *invoiceService) applySellerSnapshot(ctx context.Context, inv *invoice.Invoice) error {
	if inv.SellerSnapshot != nil {
		return nil
	}
	entity, err := s.invoiceBillingEntity(ctx, inv)
	if err != nil {
		return err
	}
	inv.SellerSnapshot = entity.SellerSnapshot()
	return nil
}
