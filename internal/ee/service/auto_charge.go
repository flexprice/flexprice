package service

import (
	"context"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

const defaultAutoChargeCooldown = time.Hour

// fetchGatewayWithAutoChargeSupport returns a gateway that can charge this customer
// off-session, or "" when none can. A provider that cannot be reached is an error,
// not an empty result: charging is refused on evidence, never on a failed read.
func fetchGatewayWithAutoChargeSupport(
	ctx context.Context,
	params ServiceParams,
	customerSvc interfaces.CustomerService,
	customerID string,
) (types.PaymentGatewayType, error) {
	providers, err := NewPaymentProviderResolver(params).ListProviders(ctx, customerID)
	if err != nil {
		return "", err
	}

	for _, p := range providers {
		if !hasCapability(p.Capabilities, types.IntegrationCapabilityAutoCharge) {
			continue
		}
		// A gateway that charges from something other than a saved method (a Razorpay
		// mandate) exposes nothing to read, so it is taken on capability alone and the
		// charge itself decides.
		if !hasCapability(p.Capabilities, types.IntegrationCapabilityPaymentMethodManagement) {
			return p.Gateway, nil
		}

		provider, err := params.IntegrationFactory.GetPaymentMethodProvider(ctx, p.Gateway, customerSvc)
		if err != nil {
			return "", ierr.WithError(err).
				WithHint("The payment provider could not be reached; try again shortly").
				Mark(ierr.ErrHTTPClient)
		}

		methods, err := provider.ListSavedMethods(ctx, customerID)
		if err != nil {
			return "", ierr.WithError(err).
				WithHint("The payment provider could not be reached; try again shortly").
				Mark(ierr.ErrHTTPClient)
		}

		// Not IsDefault: the adapters charge the primary method when there is one and
		// the first valid one otherwise, so requiring a default would refuse customers
		// they can charge.
		if lo.ContainsBy(methods, func(m interfaces.ProviderPaymentMethod) bool { return m.Active }) {
			return p.Gateway, nil
		}
	}

	return "", nil
}

func hasCapability(caps []types.IntegrationCapability, want types.IntegrationCapabilityType) bool {
	return lo.ContainsBy(caps, func(c types.IntegrationCapability) bool { return c.Type == want })
}
