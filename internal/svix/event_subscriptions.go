package svix

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	svix "github.com/svix/svix-webhooks/go"
)

// subscriptionCacheTTL bounds how long a tenant's Svix endpoint subscription
// map is trusted before we re-list. 1m matches typical UI-driven change latency.
const subscriptionCacheTTL = 1 * time.Minute

// subscriptionCachePrefix namespaces the cache entries for endpoint subscriptions.
const subscriptionCachePrefix = "svix_subs:v1:"

// appSubscriptions is the cached result of listing an app's endpoints.
// subscribeAll=true when at least one endpoint has no filterTypes (Svix treats
// an empty filterTypes list as "receive every event type").
type appSubscriptions struct {
	subscribeAll bool
	types        map[string]struct{}
}

// newAppSubscriptions returns an empty, ready-to-use aggregate.
func newAppSubscriptions() *appSubscriptions {
	return &appSubscriptions{types: map[string]struct{}{}}
}

// addEndpoint folds one endpoint's filterTypes into the aggregate. An empty or
// nil filterTypes list means "subscribe to every event", matching Svix
// semantics.
func (s *appSubscriptions) addEndpoint(filterTypes []string) {
	if s == nil {
		return
	}
	if len(filterTypes) == 0 {
		s.subscribeAll = true
		return
	}
	if s.types == nil {
		s.types = map[string]struct{}{}
	}
	for _, t := range filterTypes {
		s.types[t] = struct{}{}
	}
}

func (s *appSubscriptions) has(eventType string) bool {
	if s == nil {
		return false
	}
	if s.subscribeAll {
		return true
	}
	_, ok := s.types[eventType]
	return ok
}

// IsEventSubscribed returns true if the tenant's Svix application has at least
// one endpoint subscribed to eventType. An endpoint without filterTypes counts
// as subscribed to every event. Results are cached per app for
// subscriptionCacheTTL. When Svix is disabled the check is a no-op (returns
// true) so callers keep their existing behaviour.
//
// Fail-open on transient list errors: callers should not silently drop webhooks
// because Svix's control-plane is momentarily unhappy — the message send itself
// will retry via the caller's existing error path.
func (c *Client) IsEventSubscribed(ctx context.Context, applicationID, eventType string) (bool, error) {
	if !c.enabled || c.client == nil {
		return true, nil
	}

	cacheKey := subscriptionCachePrefix + applicationID
	cacheClient := cache.GetInMemoryCache()
	if cacheClient != nil {
		if cached, found := cacheClient.ForceCacheGet(ctx, cacheKey); found {
			if subs, ok := cached.(*appSubscriptions); ok {
				return subs.has(eventType), nil
			}
		}
	}

	subs, err := c.listSubscribedEvents(ctx, applicationID)
	if err != nil {
		return true, err
	}

	if cacheClient != nil {
		cacheClient.ForceCacheSet(ctx, cacheKey, subs, subscriptionCacheTTL)
	}
	return subs.has(eventType), nil
}

// listSubscribedEvents walks every endpoint on the app and folds their
// filterTypes into a single appSubscriptions. ponytail: no pagination limit
// guard; add a page cap if a tenant ever fans out past a few thousand endpoints.
func (c *Client) listSubscribedEvents(ctx context.Context, applicationID string) (*appSubscriptions, error) {
	subs := newAppSubscriptions()
	var iterator *string
	for {
		page, err := c.client.Endpoint.List(ctx, applicationID, &svix.EndpointListOptions{
			Iterator: iterator,
		})
		if err != nil {
			if err.Error() == "application not found" {
				return subs, nil
			}
			return nil, fmt.Errorf("failed to list endpoints: %w", err)
		}
		for _, ep := range page.Data {
			if ep.Disabled != nil && *ep.Disabled {
				continue
			}
			subs.addEndpoint(ep.FilterTypes)
		}
		if page.Done || page.Iterator == nil || *page.Iterator == "" {
			break
		}
		iterator = page.Iterator
	}
	return subs, nil
}
