package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	ierr "github.com/flexprice/flexprice/internal/errors"
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

func (s *invoiceService) GetInvoiceIssuerBranding(ctx context.Context, inv *invoice.Invoice) (*IssuerBranding, error) {
	ctx = types.SetEnvironmentID(ctx, inv.EnvironmentID)

	issuer, err := s.GetInvoiceBillingEntity(ctx, inv)
	if err != nil {
		return nil, err
	}
	if issuer == nil {
		return tenantIssuerBranding(ctx, s.ServiceParams, inv.EnvironmentID)
	}

	// An entity no longer published reads as none.
	entity, err := s.BillingEntityRepo.Get(ctx, issuer.BillingEntityID)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}
	return issuerBranding(ctx, s.ServiceParams, issuer.Name, entity, inv.EnvironmentID)
}

// IssuerBranding is an issuer's name and branding image keys.
type IssuerBranding struct {
	name    string
	logoKey *string
	iconKey *string
}

func (b *IssuerBranding) Name() string {
	if b == nil {
		return ""
	}
	return b.name
}

func (b *IssuerBranding) LogoKey() *string {
	if b == nil {
		return nil
	}
	return b.logoKey
}

func (b *IssuerBranding) IconKey() *string {
	if b == nil {
		return nil
	}
	return b.iconKey
}

// tenantIssuerBranding is the tenant's name with its environment's pair.
func tenantIssuerBranding(ctx context.Context, params ServiceParams, environmentID string) (*IssuerBranding, error) {
	t, err := params.TenantRepo.GetByID(ctx, types.GetTenantID(ctx))
	if err != nil {
		return nil, err
	}
	return issuerBranding(ctx, params, t.Name, nil, environmentID)
}

// issuerBranding is the issuer's name with the entity's own pair when it has any asset, else its environment's.
func issuerBranding(ctx context.Context, params ServiceParams, name string, entity *billingentity.BillingEntity, environmentID string) (*IssuerBranding, error) {
	cfg, err := environmentBranding(ctx, params.SettingsRepo, environmentID)
	if err != nil {
		return nil, err
	}
	logoKey, iconKey := selectBrandingKeys(entity, cfg)
	return &IssuerBranding{name: name, logoKey: logoKey, iconKey: iconKey}, nil
}
