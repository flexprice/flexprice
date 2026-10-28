package service

import (
	"context"
	"sort"

	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

var gatewayCapabilities = map[types.PaymentGatewayType][]types.IntegrationCapabilityType{
	types.PaymentGatewayTypeChargebee: {
		types.IntegrationCapabilityCheckout,
		types.IntegrationCapabilityPaymentLink,
		types.IntegrationCapabilityAutoCharge,
		types.IntegrationCapabilityPaymentMethodManagement,
		types.IntegrationCapabilitySetDefaultMethod,
		types.IntegrationCapabilityListPaymentMethods,
		types.IntegrationCapabilityAddPaymentMethod,
		types.IntegrationCapabilityDeletePaymentMethod,
	},
	types.PaymentGatewayTypeRazorpay: {
		types.IntegrationCapabilityCheckout,
		types.IntegrationCapabilityAutoCharge,
		types.IntegrationCapabilityPaymentLink,
		types.IntegrationCapabilityListPaymentMethods,
	},
	types.PaymentGatewayTypeStripe: {
		types.IntegrationCapabilityCheckout,
		types.IntegrationCapabilityPaymentLink,
		types.IntegrationCapabilityAutoCharge,
		types.IntegrationCapabilityPaymentMethodManagement,
		types.IntegrationCapabilitySetDefaultMethod,
		types.IntegrationCapabilityListPaymentMethods,
		types.IntegrationCapabilityAddPaymentMethod,
		types.IntegrationCapabilityDeletePaymentMethod,
	},
	types.PaymentGatewayTypeNomod: {
		types.IntegrationCapabilityPaymentLink,
	},
}

type PaymentProviderResolver struct {
	ServiceParams
}

func NewPaymentProviderResolver(params ServiceParams) *PaymentProviderResolver {
	return &PaymentProviderResolver{ServiceParams: params}
}

// ProviderCapabilities holds a payment gateway and its list of supported capabilities.
type ProviderCapabilities struct {
	// Gateway is the payment gateway type.
	Gateway types.PaymentGatewayType
	// Capabilities is the list of capabilities supported by the gateway.
	Capabilities []types.IntegrationCapability
}

func (s *PaymentProviderResolver) ListProviders(ctx context.Context, customerID string) ([]ProviderCapabilities, error) {
	ctx, err := s.scopeToCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}

	gateways, err := s.ConfiguredGateways(ctx)
	if err != nil {
		return nil, err
	}
	gateways, err = s.preferredFirst(ctx, customerID, gateways)
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

	ctx, err := s.scopeToCustomer(ctx, customerID)
	if err != nil {
		return "", err
	}

	gateways, err := s.ConfiguredGateways(ctx)
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

	if preferred, ok, err := s.preferredGateway(ctx, customerID); err != nil {
		return "", err
	} else if ok && lo.Contains(candidates, preferred) {
		return preferred, nil
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

// ConfiguredGateways lists the payment gateways connected for the billing entity in ctx.
func (s *PaymentProviderResolver) ConfiguredGateways(ctx context.Context) ([]types.PaymentGatewayType, error) {
	connections, err := s.ConnectionRepo.ListPublishedInScope(ctx)
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

// scopeToCustomer puts the customer's billing entity in ctx; an empty customer leaves ctx unchanged.
func (s *PaymentProviderResolver) scopeToCustomer(ctx context.Context, customerID string) (context.Context, error) {
	if customerID == "" {
		return ctx, nil
	}
	return NewBillingEntityService(s.ServiceParams).ScopeToCustomer(ctx, customerID)
}

// preferredGateway returns the gateway of the customer's preferred connection while that connection is still in scope.
func (s *PaymentProviderResolver) preferredGateway(ctx context.Context, customerID string) (types.PaymentGatewayType, bool, error) {
	if customerID == "" {
		return "", false, nil
	}
	filter := types.NewNoLimitConnectionAssociationFilter()
	filter.EntityType = types.ConnectionAssociationEntityTypeCustomer
	filter.EntityIDs = []string{customerID}
	associations, err := s.ConnectionAssociationRepo.List(ctx, filter)
	if err != nil || len(associations) == 0 {
		return "", false, err
	}
	connections, err := s.ConnectionRepo.ListPublishedInScope(ctx)
	if err != nil {
		return "", false, err
	}
	conn, found := lo.Find(connections, func(c *connection.Connection) bool {
		return c != nil && c.ID == associations[0].ConnectionID
	})
	if !found {
		return "", false, nil
	}
	gw, ok := types.PaymentGatewayFromSecretProvider(conn.ProviderType)
	return gw, ok, nil
}

// preferredFirst moves the customer's preferred gateway to the front, keeping the rest in order.
func (s *PaymentProviderResolver) preferredFirst(ctx context.Context, customerID string, gateways []types.PaymentGatewayType) ([]types.PaymentGatewayType, error) {
	preferred, ok, err := s.preferredGateway(ctx, customerID)
	if err != nil || !ok || !lo.Contains(gateways, preferred) {
		return gateways, err
	}
	rest := lo.Without(gateways, preferred)
	return append([]types.PaymentGatewayType{preferred}, rest...), nil
}
