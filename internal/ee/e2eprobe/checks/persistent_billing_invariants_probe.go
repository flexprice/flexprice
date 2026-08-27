package checks

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// persistentBillingInvariantsProbe queries the latest cycle invoice for
// persistent cust #0 (tax-attached) and cust #1 (coupon-attached), asserting
// that each invoice contains the seeded tax rate / coupon association.
//
// This complements cycle-invoice-probe (which checks freshness) with a
// content check — catching divergence where preview flows correctly attach
// the tax / coupon but the real cycle invoicing code path silently drops it.
//
// Soft-skips when the seed hasn't run, when persistent customers haven't
// been provisioned, or when no invoice exists yet for the customer.
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
		inv, err := p.latestInvoice(ctx, cust0)
		if err != nil {
			return e2eprobe.Errorf(map[string]string{"step": "query_invoice_cust0", "external_customer_id": cust0}, "query invoice: %w", err)
		}
		if inv != nil {
			if !invoiceHasTaxRate(inv, seeds.SharedTaxRateID, seeds.SharedTaxRateCode) {
				return e2eprobe.Errorf(map[string]string{"step": "assert_tax_present_cust0", "external_customer_id": cust0, "tax_rate_id": seeds.SharedTaxRateID, "tax_rate_code": seeds.SharedTaxRateCode}, "latest invoice for cust #0 does not include our tax rate (checked id %q AND code %q)", seeds.SharedTaxRateID, seeds.SharedTaxRateCode)
			}
		}
	}

	// Persistent cust #1 → coupon invariant.
	if seeds.SharedCouponID != "" {
		cust1 := seeds.PersistentCustomerIDs[1]
		inv, err := p.latestInvoice(ctx, cust1)
		if err != nil {
			return e2eprobe.Errorf(map[string]string{"step": "query_invoice_cust1", "external_customer_id": cust1}, "query invoice: %w", err)
		}
		if inv != nil {
			if !invoiceHasCoupon(inv, seeds.SharedCouponID) {
				return e2eprobe.Errorf(map[string]string{"step": "assert_coupon_present_cust1", "external_customer_id": cust1, "coupon_id": seeds.SharedCouponID}, "latest invoice for cust #1 does not include coupon %s", seeds.SharedCouponID)
			}
		}
	}
	return nil
}

func (p *persistentBillingInvariantsProbe) latestInvoice(ctx context.Context, extID string) (*types.InvoiceResponse, error) {
	limit := int64(1)
	resp, err := p.client.Invoices().Query(ctx, types.InvoiceFilter{
		ExternalCustomerID: &extID,
		Limit:              &limit,
	})
	if err != nil {
		return nil, err
	}
	if resp.ListInvoicesResponse == nil || len(resp.ListInvoicesResponse.Items) == 0 {
		return nil, nil // no invoice yet — soft skip at caller
	}
	inv := resp.ListInvoicesResponse.Items[0]
	return &inv, nil
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
