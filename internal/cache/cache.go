package cache

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Cache defines the interface for caching operations
type Cache interface {
	// Get retrieves a value from the cache
	// Returns the value and a boolean indicating whether the key was found
	Get(ctx context.Context, key string) (interface{}, bool)

	// Set adds a value to the cache with the specified expiration
	// If expiration is 0, the item never expires (but may be evicted)
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration)

	// Delete removes a key from the cache
	Delete(ctx context.Context, key string)

	// DeleteByPrefix removes all keys with the given prefix
	DeleteByPrefix(ctx context.Context, prefix string)

	// Flush removes all items from the cache
	Flush(ctx context.Context)
}

// Predefined cache key prefixes for different entity types
const (
	PrefixSecret                   = "secret:v1:"
	PrefixCustomer                 = "customer:v1:"
	PrefixUser                     = "user:v1:"
	PrefixTenant                   = "tenant:v1:"
	PrefixPlan                     = "plan:v1:"
	PrefixSubscription             = "subscription:v1:"
	PrefixPrice                    = "price:v1:"
	PrefixMeter                    = "meter:v1:"
	PrefixEvent                    = "event:v1:"
	PrefixWallet                   = "wallet:v1:"
	PrefixInvoice                  = "invoice:v1:"
	PrefixFeature                  = "feature:v1:"
	PrefixEntitlement              = "entitlement:v1:"
	PrefixPayment                  = "payment:v1:"
	PrefixCreditGrantApplication   = "creditgrantapplication:v1:"
	PrefixCreditNote               = "creditnote:v1:"
	PrefixTaxRate                  = "taxrate:v1:"
	PrefixTaxAssociation           = "taxassociation:v1:"
	PrefixTaxApplied               = "taxapplied:v1:"
	PrefixCoupon                   = "coupon:v1:"
	PrefixCouponAssociation        = "couponassociation:v1:"
	PrefixCouponApplication        = "couponapplication:v1:"
	PrefixAddon                    = "addon:v1:"
	PrefixAddonAssociation         = "addonassociation:v1:"
	PrefixEntityIntegrationMapping = "entity_integration_mapping:v1:"
	PrefixConnection               = "connection:v1:"
	PrefixSettings                 = "settings:v1:"
	PrefixSubscriptionLineItem     = "subscription_line_item:v1:"
)

// GenerateKey creates a cache key from a prefix and a set of parameters
// It joins all parameters with a colon and appends them to the prefix
func GenerateKey(prefix string, params ...interface{}) string {
	parts := make([]string, len(params)+1)
	parts[0] = prefix

	for i, param := range params {
		parts[i+1] = fmt.Sprintf("%v", param)
	}

	return strings.Join(parts, ":")
}
