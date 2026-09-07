package service

import (
	"context"
	"sort"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// gatewayCapabilities mirrors three switch statements that must agree with it:
// Factory.GetCheckoutProvider, Factory.GetPaymentMethodProvider, and
// paymentProcessor.handlePaymentLinkCreation. Stripe's unattended settlement
// (processPaymentMethodCharge) is absent on purpose — it is not on the
// CheckoutProvider seam, so nothing resolved here can route to it.
var gatewayCapabilities = map[types.PaymentGatewayType][]types.IntegrationCapabilityType{
	types.PaymentGatewayTypeChargebee: {
		types.IntegrationCapabilityCheckout,
		types.IntegrationCapabilityPaymentLink,
		types.IntegrationCapabilityAutoCharge,
		types.IntegrationCapabilityPaymentMethodManagement,
		types.IntegrationCapabilitySetDefaultMethod,
	},
	types.PaymentGatewayTypeRazorpay: {
		types.IntegrationCapabilityCheckout,
		types.IntegrationCapabilityAutoCharge,
		types.IntegrationCapabilityPaymentLink,
	},
	types.PaymentGatewayTypeStripe: {
		types.IntegrationCapabilityPaymentLink,
	},
	types.PaymentGatewayTypeNomod: {
		types.IntegrationCapabilityPaymentLink,
	},
}

// PaymentProviderResolver answers which payment gateway an operation runs against,
// from the tenant's published connections intersected with the capabilities
// FlexPrice implements per gateway.
type PaymentProviderResolver struct {
	ServiceParams
}

func NewPaymentProviderResolver(params ServiceParams) *PaymentProviderResolver {
	return &PaymentProviderResolver{ServiceParams: params}
}

type ProviderCapabilities struct {
	Gateway      types.PaymentGatewayType
	Capabilities []types.IntegrationCapability
}

// ListProviders returns configured gateways with a usable capability, ordered by
// gateway name.
func (s *PaymentProviderResolver) ListProviders(ctx context.Context, customerID string) ([]ProviderCapabilities, error) {
	gateways, err := s.configuredGateways(ctx)
	if err != nil {
		return nil, err
	}

	usable := lo.Filter(gateways, func(gw types.PaymentGatewayType, _ int) bool {
		return len(gatewayCapabilities[gw]) > 0
	})

	out := make([]ProviderCapabilities, 0, len(usable))
	for _, gw := range usable {
		caps := lo.Map(gatewayCapabilities[gw], func(c types.IntegrationCapabilityType, _ int) types.IntegrationCapability {
			return types.IntegrationCapability{Type: c}
		})
		out = append(out, ProviderCapabilities{Gateway: gw, Capabilities: caps})
	}

	return out, nil
}

// ResolveProvider picks the gateway serving capability.
func (s *PaymentProviderResolver) ResolveProvider(
	ctx context.Context,
	customerID string,
	capability types.IntegrationCapabilityType,
	requested types.PaymentGatewayType,
) (types.PaymentGatewayType, error) {
	if capability == "" {
		return "", ierr.NewError("capability is required").
			WithHint("Specify which payment operation the provider must support").
			Mark(ierr.ErrValidation)
	}
	if requested != "" {
		if err := requested.Validate(); err != nil {
			return "", err
		}
	}

	gateways, err := s.configuredGateways(ctx)
	if err != nil {
		return "", err
	}

	candidates := lo.Filter(gateways, func(gw types.PaymentGatewayType, _ int) bool {
		return lo.Contains(gatewayCapabilities[gw], capability)
	})

	details := map[string]any{
		"customer_id": customerID,
		"capability":  capability,
		"candidates":  candidates,
	}

	if requested != "" {
		if lo.Contains(candidates, requested) {
			return requested, nil
		}
		details["requested"] = requested
		if !lo.Contains(gateways, requested) {
			return "", ierr.NewError("requested payment provider is not connected").
				WithHintf("%s is not configured for this environment", requested).
				WithReportableDetails(details).
				Mark(ierr.ErrValidation)
		}
		return "", ierr.NewError("requested payment provider cannot perform this operation").
			WithHintf("%s does not support %s", requested, capability).
			WithReportableDetails(details).
			Mark(ierr.ErrValidation)
	}

	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return "", ierr.NewError("no payment provider supports this operation").
			WithHintf("Connect a payment provider that supports %s", capability).
			WithReportableDetails(details).
			Mark(ierr.ErrNotFound)
	default:
		return "", ierr.NewError("payment provider is ambiguous").
			WithHint("Specify which payment provider to use").
			WithReportableDetails(details).
			Mark(ierr.ErrValidation)
	}
}

// configuredGateways deduplicates and sorts so listings and error messages are stable.
func (s *PaymentProviderResolver) configuredGateways(ctx context.Context) ([]types.PaymentGatewayType, error) {
	connections, err := s.ConnectionRepo.ListAllPublished(ctx)
	if err != nil {
		return nil, err
	}

	seen := make(map[types.PaymentGatewayType]struct{}, len(connections))
	gateways := make([]types.PaymentGatewayType, 0, len(connections))
	for _, conn := range connections {
		if conn == nil {
			continue
		}
		gw, ok := types.PaymentGatewayFromSecretProvider(conn.ProviderType)
		if !ok {
			continue
		}
		if _, dup := seen[gw]; dup {
			continue
		}
		seen[gw] = struct{}{}
		gateways = append(gateways, gw)
	}

	sort.Slice(gateways, func(i, j int) bool { return gateways[i] < gateways[j] })
	return gateways, nil
}
