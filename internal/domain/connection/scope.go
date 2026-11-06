package connection

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// IsStorageProvider reports whether a provider is storage, which is never owned by a billing entity.
func IsStorageProvider(provider types.SecretProvider) bool {
	return provider == types.SecretProviderS3 || provider == types.SecretProviderGCS
}

// InScope keeps the connections usable for the billing entity in ctx. Storage is always kept.
// Without an entity, any owned connection drops every non-storage connection (fail closed).
func InScope(ctx context.Context, conns []*Connection) []*Connection {
	entityID := types.GetBillingEntityID(ctx)
	if entityID == "" {
		anyOwned := lo.ContainsBy(conns, func(c *Connection) bool {
			return !IsStorageProvider(c.ProviderType) && lo.FromPtr(c.BillingEntityID) != ""
		})
		if !anyOwned {
			return conns
		}
	}

	return lo.Filter(conns, func(c *Connection, _ int) bool {
		if IsStorageProvider(c.ProviderType) {
			return true
		}
		return entityID != "" && lo.FromPtr(c.BillingEntityID) == entityID
	})
}
