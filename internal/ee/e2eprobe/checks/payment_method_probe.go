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
//   - with gateway test credentials, a vaulted card is listed as auto-chargeable,
//     can be made default, and deleting it removes it;
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

	if p.opts.Driver == nil {
		return nil
	}
	return p.vaultedCardLifecycle(ctx, f, token)
}

// vaultedCardLifecycle vaults a card on the gateway and walks it through
// listing, set-default and delete.
func (p *PaymentMethodProbe) vaultedCardLifecycle(ctx context.Context, f *paymentFlow, token string) error {
	provider := f.provider()
	pay := p.client.Payments()

	gatewayCustomerID, err := f.resolveGatewayCustomer(ctx)
	if err != nil {
		return err
	}
	methodID, err := p.opts.Driver.AttachCard(ctx, gatewayCustomerID, TestCardSuccess)
	if err != nil {
		return f.fail("vault_card", map[string]string{"gateway_customer_id": gatewayCustomerID}, "vault test card: %w", err)
	}
	ids := map[string]string{"gateway_customer_id": gatewayCustomerID, "payment_method_id": methodID}

	m, err := f.waitMethodListed(ctx, methodID)
	if err != nil {
		return err
	}
	if !m.CanAutoCharge {
		return f.fail("assert_auto_chargeable", ids, "vaulted card listed with can_auto_charge=false")
	}

	if p.opts.supports(capSetDefault) {
		methods, err := pay.PortalSetDefaultMethod(ctx, token, provider, methodID)
		if err != nil {
			return f.fail("set_default", ids, "portal set default: %w", err)
		}
		if got, ok := findSavedMethod(methods, methodID); !ok || !got.IsDefault {
			return f.fail("assert_default", ids, "method not marked default after set-default")
		}
	}

	if !p.opts.supports(capDeleteMethod) {
		return nil
	}
	if _, err := pay.PortalDeleteMethod(ctx, token, provider, methodID); err != nil {
		return f.fail("delete_method", ids, "portal delete method: %w", err)
	}
	methods, err := pay.ListSavedMethods(ctx, f.customerID, provider)
	if err != nil {
		return f.fail("list_methods_after_delete", ids, "list saved methods: %w", err)
	}
	if got, ok := findSavedMethod(methods, methodID); ok && got.Status == "active" {
		return f.fail("assert_deleted", ids, "method still listed as active after delete")
	}
	return nil
}
