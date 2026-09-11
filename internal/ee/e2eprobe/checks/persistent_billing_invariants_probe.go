package checks

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// persistentBillingInvariantsProbe queries cycle invoices for persistent
// cust #0 (tax-attached) and cust #1 (coupon-attached), asserting that
// each contains the seeded tax rate / coupon association.
//
// This complements cycle-invoice-probe (which checks freshness) with a
// content check — catching divergence where preview flows correctly attach
// the tax / coupon but the real cycle invoicing code path silently drops it.
//
// The shared coupon is ONCE cadence, so only the first cycle invoice carries
// it. Asserting the latest invoice would false-alarm after the coupon is
// consumed; the coupon check therefore loads the oldest subscription invoice
// (created_at asc, limit 1). Tax still applies every cycle, so we assert it
// on the latest invoice — fetched by ID because list responses omit taxes.
//
// Soft-skips when the seed hasn't run, when persistent customers haven't
// been provisioned, when no invoice exists yet, or when cust #1's sub was
// created before the coupon seed (association is attach-at-create only).
type persistentBillingInvariantsProbe struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	lg     *logger.Logger
}

func NewPersistentBillingInvariantsProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string, lg *logger.Logger) e2eprobe.Check {
	return &persistentBillingInvariantsProbe{client: c, reg: r, runID: runID, lg: lg}
}

func (p *persistentBillingInvariantsProbe) Name() string {
	return "persistent-billing-invariants-probe"
}
func (p *persistentBillingInvariantsProbe) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }

func (p *persistentBillingInvariantsProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	if len(seeds.PersistentCustomerIDs) < 2 {
		return nil
	}

	// Persistent cust #0 → tax invariant. Match by ID when available, else
	// by nested TaxRate.Code (SharedTaxRateID may be empty when the SDK's
	// broken GetTaxRates list forced create-only idempotency in the seed).
	if seeds.SharedTaxRateID != "" || seeds.SharedTaxRateCode != "" {
		cust0 := seeds.PersistentCustomerIDs[0]
		listed, err := p.subscriptionInvoice(ctx, cust0, types.InvoiceFilterOrderDesc)
		if err != nil {
			return e2eprobe.Errorf(map[string]string{"step": "load_invoice_cust0", "external_customer_id": cust0}, "load invoice: %w", err)
		}
		if listed != nil {
			inv, err := p.invoiceWithTaxes(ctx, *listed)
			if err != nil {
				return e2eprobe.Errorf(map[string]string{"step": "load_invoice_cust0", "external_customer_id": cust0}, "load invoice: %w", err)
			}
			if inv != nil && !invoiceHasTaxRate(inv, seeds.SharedTaxRateID, seeds.SharedTaxRateCode) {
				return e2eprobe.Errorf(map[string]string{"step": "assert_tax_present_cust0", "external_customer_id": cust0, "tax_rate_id": seeds.SharedTaxRateID, "tax_rate_code": seeds.SharedTaxRateCode}, "latest invoice for cust #0 does not include our tax rate (checked id %q AND code %q)", seeds.SharedTaxRateID, seeds.SharedTaxRateCode)
			}
		}
	}

	// Persistent cust #1 → coupon invariant. ONCE cadence applies only on
	// the first cycle invoice, so query the oldest subscription invoice
	// (created_at asc, limit 1) rather than scanning newest-N.
	if seeds.SharedCouponID != "" {
		cust1 := seeds.PersistentCustomerIDs[1]
		inv, err := p.subscriptionInvoice(ctx, cust1, types.InvoiceFilterOrderAsc)
		if err != nil {
			return e2eprobe.Errorf(map[string]string{"step": "load_invoice_cust1", "external_customer_id": cust1}, "load invoice: %w", err)
		}
		if inv != nil && !invoiceHasCoupon(inv, seeds.SharedCouponID) {
			attached, err := p.couponAssociated(ctx, seeds)
			if err != nil {
				return e2eprobe.Errorf(map[string]string{"step": "list_coupon_associations", "external_customer_id": cust1, "coupon_id": seeds.SharedCouponID}, "list coupon associations: %w", err)
			}
			if !attached {
				return nil
			}
			return e2eprobe.Errorf(map[string]string{"step": "assert_coupon_present_cust1", "external_customer_id": cust1, "coupon_id": seeds.SharedCouponID}, "first subscription invoice for cust #1 does not include coupon %s", seeds.SharedCouponID)
		}
	}
	return nil
}

func (p *persistentBillingInvariantsProbe) subscriptionInvoice(ctx context.Context, extID string, order types.InvoiceFilterOrder) (*types.InvoiceResponse, error) {
	invType := types.InvoiceTypeSubscription
	limit := int64(1)
	resp, err := p.client.Invoices().Query(ctx, types.InvoiceFilter{
		ExternalCustomerID: &extID,
		InvoiceType:        &invType,
		Limit:              &limit,
		Order:              &order,
	})
	if err != nil {
		return nil, err
	}
	if resp.ListInvoicesResponse == nil || len(resp.ListInvoicesResponse.Items) == 0 {
		return nil, nil
	}
	listed := resp.ListInvoicesResponse.Items[0]
	if listed.ID == nil || *listed.ID == "" {
		return nil, nil
	}

	// Search/list never expands tax_applied; only GET attaches Taxes.
	got, err := p.client.Invoices().Get(ctx, *listed.ID)
	if err != nil {
		return nil, err
	}
	if got == nil || got.InvoiceResponse == nil {
		return nil, nil
	}
	return got.InvoiceResponse, nil
}

func (p *persistentBillingInvariantsProbe) invoiceWithTaxes(ctx context.Context, inv types.InvoiceResponse) (*types.InvoiceResponse, error) {
	if len(inv.Taxes) > 0 {
		return &inv, nil
	}
	if inv.ID == nil || *inv.ID == "" {
		return &inv, nil
	}

	// Search/list never expands tax_applied; only GET attaches Taxes.
	got, err := p.client.Invoices().Get(ctx, *inv.ID)
	if err != nil {
		return nil, err
	}
	if got == nil || got.InvoiceResponse == nil {
		return &inv, nil
	}
	return got.InvoiceResponse, nil
}

func (p *persistentBillingInvariantsProbe) couponAssociated(ctx context.Context, seeds e2eprobe.Seeds) (bool, error) {
	if len(seeds.PersistentSubIDs) < 2 {
		return false, nil
	}
	resp, err := p.client.CouponAssociations().List(ctx, dtos.ListCouponAssociationsRequest{
		CouponIds:       []string{seeds.SharedCouponID},
		SubscriptionIds: []string{seeds.PersistentSubIDs[1]},
	})
	if err != nil {
		return false, err
	}
	if resp.ListCouponAssociationsResponse == nil {
		return false, nil
	}
	return len(resp.ListCouponAssociationsResponse.Items) > 0, nil
}

func invoiceHasTaxRate(inv *types.InvoiceResponse, taxRateID, taxRateCode string) bool {
	for _, tx := range inv.Taxes {
		if taxRateID != "" && tx.TaxRateID != nil && *tx.TaxRateID == taxRateID {
			return true
		}
		if taxRateCode != "" && tx.TaxRate != nil && tx.TaxRate.Code != nil && *tx.TaxRate.Code == taxRateCode {
			return true
		}
	}
	return false
}

func invoiceHasCoupon(inv *types.InvoiceResponse, couponID string) bool {
	for _, ca := range inv.CouponApplications {
		if ca.CouponID != nil && *ca.CouponID == couponID {
			return true
		}
	}
	return false
}
