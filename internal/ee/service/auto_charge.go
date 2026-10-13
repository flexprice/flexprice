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
// off-session for the requested amount, or "" when none can. A provider that cannot
// be reached is an error, not an empty result: charging is refused on evidence,
// never on a failed read.
func fetchGatewayWithAutoChargeSupport(
	ctx context.Context,
	params ServiceParams,
	customerSvc interfaces.CustomerService,
	req interfaces.HasAutoChargeableMethodRequest,
) (types.PaymentGatewayType, error) {
	if params.IntegrationFactory == nil || params.ConnectionRepo == nil {
		return "", nil
	}

	gateways, err := NewPaymentProviderResolver(params).ConfiguredGateways(ctx)
	if err != nil {
		return "", err
	}

	for _, gw := range gateways {
		if !lo.Contains(gatewayCapabilities[gw], types.IntegrationCapabilityAutoCharge) {
			continue
		}

		checkoutProvider, ok := types.CheckoutProviderFromGateway(gw)
		if !ok {
			continue
		}

		provider, err := params.IntegrationFactory.GetCheckoutProvider(ctx, checkoutProvider, customerSvc, nil)
		if err != nil {
			if ierr.IsValidation(err) || ierr.IsNotImplemented(err) {
				continue
			}
			return "", ierr.WithError(err).
				WithHint("The payment provider could not be reached; try again shortly").
				Mark(ierr.ErrHTTPClient)
		}

		hasMethod, err := provider.HasAutoChargeableMethod(ctx, req)
		if err != nil {
			return "", ierr.WithError(err).
				WithHint("The payment provider could not be reached; try again shortly").
				Mark(ierr.ErrHTTPClient)
		}

		if hasMethod {
			return gw, nil
		}
	}

	return "", nil
}
