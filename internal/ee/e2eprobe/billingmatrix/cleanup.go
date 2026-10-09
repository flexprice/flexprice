package billingmatrix

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Name and lookup-key prefixes every billing-matrix resource carries, so the orphan sweep can find them.
const (
	planNamePrefix   = "e2eprobe-bm "
	customerIDPrefix = "e2eprobe-cust-eph-bm-"
)

// tracker records what a scenario created, for cleanup in dependency order.
type tracker struct {
	mu            sync.Mutex
	customers     []string
	subscriptions []string
	plans         []string
	prices        []string
	addons        []string
	features      []string
	entitlements  []string
}

// Resource kinds a tracker records.
const (
	kindCustomer     = "customer"
	kindSubscription = "subscription"
	kindPlan         = "plan"
	kindPrice        = "price"
	kindAddon        = "addon"
	kindFeature      = "feature"
	kindEntitlement  = "entitlement"
)

// add records a created resource; a nil tracker records nothing.
func (t *tracker) add(kind, id string) {
	if t == nil || id == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	switch kind {
	case kindCustomer:
		t.customers = append(t.customers, id)
	case kindSubscription:
		t.subscriptions = append(t.subscriptions, id)
	case kindPlan:
		t.plans = append(t.plans, id)
	case kindPrice:
		t.prices = append(t.prices, id)
	case kindAddon:
		t.addons = append(t.addons, id)
	case kindFeature:
		t.features = append(t.features, id)
	case kindEntitlement:
		t.entitlements = append(t.entitlements, id)
	}
}

// cleanup removes a scenario's resources: addons are detached and subscriptions cancelled, wallets terminated so the
// customer can be deleted, then entitlements, addons, plans and features. It is best effort; it
// returns the deletes that failed so the caller can report them without failing the scenario.
func (c client) cleanup(ctx context.Context, t *tracker) []string {
	var failed []string
	note := func(what, id string, err error) {
		if err != nil && statusOf(err) != http.StatusNotFound {
			failed = append(failed, fmt.Sprintf("%s %s: %v", what, id, err))
		}
	}
	for _, id := range t.subscriptions {
		// An ended sub keeps its addon associations active, which blocks deleting the addon.
		assocs, _ := c.addonAssociations(ctx, id)
		for _, a := range assocs {
			remove := map[string]any{"addon_association_id": a.ID, "proration_behavior": "none", "change_at": "immediate"}
			if a.StartDate.After(time.Now()) { // a future-dated addon is removed at its own start
				delete(remove, "change_at")
				remove["effective_date"] = a.StartDate.UTC().Format(time.RFC3339)
			}
			_, err := c.modify(ctx, id, map[string]any{"type": "addon", "addon_params": map[string]any{
				"action": "remove", "remove": remove,
			}}, false)
			note("remove addon association", a.ID, err)
		}
		err := c.post(ctx, "/subscriptions/"+url.PathEscape(id)+"/cancel", map[string]any{
			"cancellation_type": "immediate", "proration_behavior": "none", "reason": "e2eprobe billing matrix cleanup",
		}, nil)
		if err != nil && !isClientError(err) { // already cancelled is a 4xx
			note("cancel subscription", id, err)
		}
	}
	for _, id := range t.customers {
		note("delete customer", id, c.deleteCustomer(ctx, id))
	}
	for _, id := range t.entitlements {
		note("delete entitlement", id, c.api.Do(ctx, http.MethodDelete, "/entitlements/"+url.PathEscape(id), nil, nil))
	}
	for _, id := range t.addons {
		note("delete addon", id, c.api.Do(ctx, http.MethodDelete, "/addons/"+url.PathEscape(id), nil, nil))
	}
	for _, id := range t.plans {
		note("delete plan", id, c.api.Do(ctx, http.MethodDelete, "/plans/"+url.PathEscape(id), nil, nil))
	}
	for _, id := range t.prices {
		note("delete price", id, c.api.Do(ctx, http.MethodDelete, "/prices/"+url.PathEscape(id), map[string]any{}, nil))
	}
	for _, id := range t.features {
		note("delete feature", id, c.api.Do(ctx, http.MethodDelete, "/features/"+url.PathEscape(id), nil, nil))
	}
	return failed
}

// deleteCustomer terminates the customer's active wallets, which block deletion, then deletes it.
func (c client) deleteCustomer(ctx context.Context, id string) error {
	var raw json.RawMessage
	if err := c.get(ctx, "/customers/"+url.PathEscape(id)+"/wallets", &raw); err != nil {
		return err
	}
	var wallets []idOnly
	if err := decodeItems(raw, &wallets); err != nil {
		return err
	}
	for _, w := range wallets {
		if err := c.post(ctx, "/wallets/"+url.PathEscape(w.ID)+"/terminate", map[string]any{}, nil); err != nil && statusOf(err) != http.StatusNotFound {
			return fmt.Errorf("terminate wallet %s: %w", w.ID, err)
		}
	}
	return c.api.Do(ctx, http.MethodDelete, "/customers/"+url.PathEscape(id), nil, nil)
}

type catalogEntry struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	LookupKey  string    `json:"lookup_key"`
	ExternalID string    `json:"external_id"`
	CreatedAt  time.Time `json:"created_at"`
}

// listAll pages through a search endpoint.
func (c client) listAll(ctx context.Context, path string) ([]catalogEntry, error) {
	const limit = 100
	var all []catalogEntry
	for offset := 0; ; offset += limit {
		var raw json.RawMessage
		if err := c.post(ctx, path, map[string]any{"limit": limit, "offset": offset}, &raw); err != nil {
			return nil, err
		}
		var page []catalogEntry
		if err := decodeItems(raw, &page); err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < limit {
			return all, nil
		}
	}
}

// SweepOrphans cancels and deletes billing-matrix customers and plans older than cutoff that a
// crashed or restarted probe never cleaned up. Shared fixtures are left alone. It returns how many
// it removed and the deletes that failed.
func (e *Engine) SweepOrphans(ctx context.Context, cutoff time.Time) (int, []string) {
	c := client{api: e.api}
	orphan := func(x catalogEntry) bool {
		return x.CreatedAt.Before(cutoff) && !strings.HasPrefix(x.LookupKey, fixtureKeyPrefix)
	}
	t := &tracker{}
	var failed []string
	collect := func(path string, match func(catalogEntry) bool, list *[]string) {
		items, err := c.listAll(ctx, path)
		if err != nil {
			failed = append(failed, fmt.Sprintf("list %s: %v", path, err))
			return
		}
		for _, x := range items {
			if match(x) && orphan(x) {
				*list = append(*list, x.ID)
			}
		}
	}
	collect("/customers/search", func(x catalogEntry) bool { return strings.HasPrefix(x.ExternalID, customerIDPrefix) }, &t.customers)
	collect("/plans/search", func(x catalogEntry) bool { return strings.HasPrefix(x.Name, planNamePrefix) }, &t.plans)

	for _, custID := range t.customers {
		subs, err := c.customerSubscriptions(ctx, custID)
		if err != nil {
			failed = append(failed, fmt.Sprintf("list subscriptions of %s: %v", custID, err))
			continue
		}
		t.subscriptions = append(t.subscriptions, subs...)
	}
	failed = append(failed, c.cleanup(ctx, t)...)
	removed := len(t.customers) + len(t.plans)
	return removed, failed
}

func (c client) customerSubscriptions(ctx context.Context, customerID string) ([]string, error) {
	var raw json.RawMessage
	// Search defaults to active subscriptions only; ask for every status that can still bill.
	statuses := []string{"active", "trialing", "incomplete", "paused", "draft"}
	if err := c.post(ctx, "/subscriptions/search", map[string]any{
		"customer_id": customerID, "subscription_status": statuses, "limit": 100,
	}, &raw); err != nil {
		return nil, err
	}
	var subs []idOnly
	if err := decodeItems(raw, &subs); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(subs))
	for _, s := range subs {
		ids = append(ids, s.ID)
	}
	return ids, nil
}
