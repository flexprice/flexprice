package service

import (
	"context"
	"sort"

	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

var billingProviderCapabilities = map[types.SecretProvider][]types.IntegrationCapabilityType{
	types.SecretProviderChargebee: {types.IntegrationCapabilityInvoiceSync},
}

type BillingProviderResolver struct {
	ServiceParams
}

func NewBillingProviderResolver(params ServiceParams) *BillingProviderResolver {
	return &BillingProviderResolver{ServiceParams: params}
}

type BillingProviderCapabilities struct {
	Provider     types.SecretProvider
	Capabilities []types.IntegrationCapability
}

func (s *BillingProviderResolver) ListProviders(ctx context.Context) ([]BillingProviderCapabilities, error) {
	connections, err := s.ConnectionRepo.ListAllPublished(ctx)
	if err != nil {
		return nil, err
	}

	seen := make(map[types.SecretProvider]struct{}, len(connections))
	out := make([]BillingProviderCapabilities, 0, len(connections))
	for _, conn := range connections {
		if !invoiceSyncing(conn) {
			continue
		}
		if _, dup := seen[conn.ProviderType]; dup {
			continue
		}
		seen[conn.ProviderType] = struct{}{}

		caps := lo.Map(billingProviderCapabilities[conn.ProviderType],
			func(c types.IntegrationCapabilityType, _ int) types.IntegrationCapability {
				return types.IntegrationCapability{Type: c}
			})
		out = append(out, BillingProviderCapabilities{
			Provider:     conn.ProviderType,
			Capabilities: caps,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out, nil
}

func invoiceSyncing(conn *connection.Connection) bool {
	if conn == nil || len(billingProviderCapabilities[conn.ProviderType]) == 0 {
		return false
	}

	return conn.IsInvoiceOutboundEnabled()
}
