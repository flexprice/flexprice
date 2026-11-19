package scenarios

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/world"
)

// paymentMethods manages saved methods on an ephemeral customer through the API and
// the portal: listing, the add-method (and Stripe setup) hosted page, and 4xx
// refusals for what the gateway lacks.
type paymentMethods struct {
	poll time.Duration
}

func newPaymentMethods(scenario.Deps) *paymentMethods {
	return &paymentMethods{poll: defaultPaymentPoll}
}

func (*paymentMethods) Name() string               { return "payment-methods" }
func (*paymentMethods) Kind() e2eprobe.Kind        { return e2eprobe.KindFlow }
func (*paymentMethods) Requires() []e2eprobe.Trait { return []e2eprobe.Trait{e2eprobe.TraitPayment} }

func (p *paymentMethods) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	opts, stop := paymentPrelude(ctx, sc, p.poll, capListMethods)
	if stop != nil {
		return *stop
	}
	eph, err := sc.Clone(ctx)
	if err != nil {
		return sc.Fail("clone", err)
	}
	sc.Attr("ephemeral_customer", eph.ExternalID())
	f := newPaymentFlow(sc, opts, "methods")
	legs := &legResults{}
	legs.run("methods", func() error { return p.manageMethods(ctx, f, eph) })
	return legs.result(sc, "payment-methods")
}

func (p *paymentMethods) manageMethods(ctx context.Context, f *paymentFlow, eph *world.Ephemeral) error {
	if err := f.useEphemeral(ctx, eph); err != nil {
		return err
	}
	provider := f.provider()
	pay := f.client.Payments()

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

	if !f.opts.supports(capAddMethod) {
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
