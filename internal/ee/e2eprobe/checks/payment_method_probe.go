package checks

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
)

// PaymentMethodProbe exercises saved-method management on an ephemeral customer,
// through both the API and the customer portal:
//   - listing returns the gateway's block without a provider error;
//   - add-method (and, for Stripe, setup intent) issues a hosted page;
//   - operations the gateway does not support are refused with a 4xx.
type PaymentMethodProbe struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	lg     *logger.Logger
	opts   PaymentProbeOpts
}

func NewPaymentMethodProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string, lg *logger.Logger, opts PaymentProbeOpts) *PaymentMethodProbe {
	return &PaymentMethodProbe{client: c, reg: r, runID: runID, lg: lg, opts: opts}
}

func (p *PaymentMethodProbe) Name() string        { return "payment-method-probe-" + p.opts.Provider.Provider }
func (p *PaymentMethodProbe) Kind() e2eprobe.Kind { return e2eprobe.KindScenario }

func (p *PaymentMethodProbe) Run(ctx context.Context) error {
	if !p.opts.supports(capListMethods) {
		return nil
	}
	f := newPaymentFlow(p.client, p.reg, p.runID, p.opts, "ephemeral-payment-method")
	if err := f.createCustomer(ctx); err != nil {
		return err
	}
	provider := f.provider()
	pay := p.client.Payments()

	if _, err := pay.ListSavedMethods(ctx, f.customerID, provider); err != nil {
		return f.fail("list_methods_api", nil, "list saved methods: %w", err)
	}
	token, err := pay.CreatePortalSession(ctx, f.externalID)
	if err != nil {
		return f.fail("portal_session", nil, "create portal session: %w", err)
	}
	if _, err := pay.PortalListSavedMethods(ctx, token, provider); err != nil {
		return f.fail("list_methods_portal", nil, "portal list saved methods: %w", err)
	}

	if provider == "stripe" {
		link, err := pay.CreateSetupLink(ctx, f.customerID, provider, paymentReturnURL)
		if err != nil {
			return f.fail("setup_link", nil, "create setup intent: %w", err)
		}
		if link == "" {
			return f.fail("setup_link", nil, "setup intent returned no checkout_url")
		}
	}

	if !p.opts.supports(capAddMethod) {
		_, err := pay.PortalAddMethod(ctx, token, provider, paymentReturnURL)
		if err == nil || !isClientRejection(err) {
			return f.fail("add_method_assert_rejected", nil, "want a 4xx for a gateway without add-method, got: %v", err)
		}
		_, err = pay.PortalDeleteMethod(ctx, token, provider, "e2eprobe_nonexistent_method")
		if err == nil || !isClientRejection(err) {
			return f.fail("delete_method_assert_rejected", nil, "want a 4xx for a gateway without delete-method, got: %v", err)
		}
		return nil
	}

	link, err := pay.PortalAddMethod(ctx, token, provider, paymentReturnURL)
	if err != nil {
		return f.fail("add_method_link", nil, "portal add method: %w", err)
	}
	if link == "" {
		return f.fail("add_method_link", nil, "portal add method returned no redirect URL")
	}
	return nil
}
