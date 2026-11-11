package testutil

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/addonpricesync"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryAddonPriceSyncStore implements addonpricesync.Repository against the
// other in-memory stores. It mirrors internal/repository/ent/addon_price_sync.go
// closely enough for service-level unit tests — keyed on the addon association,
// with the watermark on association.SyncedPriceSequence.
type InMemoryAddonPriceSyncStore struct {
	priceStore    *InMemoryPriceStore
	subStore      *InMemorySubscriptionStore
	lineItemStore *InMemorySubscriptionLineItemStore
	assocStore    *InMemoryAddonAssociationStore
}

func NewInMemoryAddonPriceSyncStore(
	priceStore *InMemoryPriceStore,
	subStore *InMemorySubscriptionStore,
	lineItemStore *InMemorySubscriptionLineItemStore,
	assocStore *InMemoryAddonAssociationStore,
) *InMemoryAddonPriceSyncStore {
	return &InMemoryAddonPriceSyncStore{
		priceStore:    priceStore,
		subStore:      subStore,
		lineItemStore: lineItemStore,
		assocStore:    assocStore,
	}
}

// Clear is a no-op — this store owns no state, only refs to other stores.
func (r *InMemoryAddonPriceSyncStore) Clear() {}

// --- snapshot helpers -------------------------------------------------------

func isAddonUsagePrice(p *price.Price, addonID string) bool {
	return p != nil &&
		p.Status == types.StatusPublished &&
		p.EntityType == types.PRICE_ENTITY_TYPE_ADDON &&
		p.EntityID == addonID &&
		p.Type == types.PRICE_TYPE_USAGE
}

func (r *InMemoryAddonPriceSyncStore) addonUsagePrices(ctx context.Context, addonID string) []*price.Price {
	if r.priceStore == nil {
		return nil
	}
	r.priceStore.mu.RLock()
	defer r.priceStore.mu.RUnlock()
	var out []*price.Price
	for _, p := range r.priceStore.items {
		if isAddonUsagePrice(p, addonID) && scopedByCtx(ctx, p.TenantID, p.EnvironmentID) {
			out = append(out, p)
		}
	}
	return out
}

func (r *InMemoryAddonPriceSyncStore) subByID() map[string]*subscription.Subscription {
	out := make(map[string]*subscription.Subscription)
	if r.subStore == nil {
		return out
	}
	r.subStore.mu.RLock()
	defer r.subStore.mu.RUnlock()
	for id, s := range r.subStore.items {
		out[id] = s
	}
	return out
}

// stillBillingAssociations returns published associations to addonID on
// active/trialing published subscriptions that are still billing: active, or
// cancelled with a future end date (cancel at period end), scoped by ctx.
func (r *InMemoryAddonPriceSyncStore) stillBillingAssociations(ctx context.Context, addonID string) []*addonassociation.AddonAssociation {
	if r.assocStore == nil {
		return nil
	}
	subs := r.subByID()
	now := time.Now().UTC()

	r.assocStore.mu.RLock()
	defer r.assocStore.mu.RUnlock()

	var out []*addonassociation.AddonAssociation
	for _, aa := range r.assocStore.items {
		if aa == nil || aa.Status != types.StatusPublished {
			continue
		}
		if aa.EntityType != types.AddonAssociationEntityTypeSubscription {
			continue
		}
		if aa.AddonID != addonID {
			continue
		}
		billing := false
		switch aa.AddonStatus {
		case types.AddonStatusActive:
			billing = aa.EndDate == nil || aa.EndDate.After(now)
		case types.AddonStatusCancelled:
			billing = aa.EndDate != nil && aa.EndDate.After(now)
		}
		if !billing {
			continue
		}
		if !scopedByCtx(ctx, aa.TenantID, aa.EnvironmentID) {
			continue
		}
		sub := subs[aa.EntityID]
		if sub == nil || sub.Status != types.StatusPublished {
			continue
		}
		if sub.SubscriptionStatus != types.SubscriptionStatusActive &&
			sub.SubscriptionStatus != types.SubscriptionStatusTrialing {
			continue
		}
		out = append(out, aa)
	}
	return out
}

// --- addonpricesync.Repository implementation -------------------------------

// CurrentAddonSequence returns max(prices.sequence) for the addon's published
// usage prices. 0 when none qualify.
func (r *InMemoryAddonPriceSyncStore) CurrentAddonSequence(ctx context.Context, addonID string) (int64, error) {
	if addonID == "" {
		return 0, ierr.NewError("addon_id is required").Mark(ierr.ErrValidation)
	}
	var maxSeq int64
	for _, p := range r.addonUsagePrices(ctx, addonID) {
		if p.Sequence > maxSeq {
			maxSeq = p.Sequence
		}
	}
	return maxSeq, nil
}

// ListBehindAssociations returns active recurring associations whose watermark is
// below TargetSeq, ordered by (synced_price_sequence, id) and limited.
func (r *InMemoryAddonPriceSyncStore) ListBehindAssociations(
	ctx context.Context,
	p addonpricesync.ListBehindAssociationsParams,
) ([]addonpricesync.AddonAssociationRef, error) {
	if p.AddonID == "" {
		return nil, ierr.NewError("addon_id is required").Mark(ierr.ErrValidation)
	}

	var behind []*addonassociation.AddonAssociation
	for _, aa := range r.stillBillingAssociations(ctx, p.AddonID) {
		if aa.SyncedPriceSequence < p.TargetSeq {
			behind = append(behind, aa)
		}
	}
	sort.Slice(behind, func(i, j int) bool {
		if behind[i].SyncedPriceSequence != behind[j].SyncedPriceSequence {
			return behind[i].SyncedPriceSequence < behind[j].SyncedPriceSequence
		}
		return behind[i].ID < behind[j].ID
	})
	if p.Limit > 0 && len(behind) > p.Limit {
		behind = behind[:p.Limit]
	}

	refs := make([]addonpricesync.AddonAssociationRef, 0, len(behind))
	for _, aa := range behind {
		refs = append(refs, addonpricesync.AddonAssociationRef{
			AssociationID:  aa.ID,
			SubscriptionID: aa.EntityID,
			EndDate:        aa.EndDate,
		})
	}
	return refs, nil
}

// TerminateEndedAddonLineItems caps addon usage line items belonging to the given
// associations to their catalog price's end date, shortening any line that is open
// or outlives the ended price. Idempotent.
func (r *InMemoryAddonPriceSyncStore) TerminateEndedAddonLineItems(
	ctx context.Context,
	p addonpricesync.TerminateEndedAddonLineItemsParams,
) (int, error) {
	if p.AddonID == "" {
		return 0, ierr.NewError("addon_id is required").Mark(ierr.ErrValidation)
	}
	if len(p.AssociationIDs) == 0 || r.lineItemStore == nil {
		return 0, nil
	}

	endedPrice := make(map[string]*price.Price)
	for _, pp := range r.addonUsagePrices(ctx, p.AddonID) {
		if pp.EndDate != nil {
			endedPrice[pp.ID] = pp
		}
	}
	if len(endedPrice) == 0 {
		return 0, nil
	}

	assocSet := make(map[string]struct{}, len(p.AssociationIDs))
	for _, id := range p.AssociationIDs {
		assocSet[id] = struct{}{}
	}

	// Associations in this page that are cancelled at period end; their lines that
	// outlive the ended price must be shortened, not just the open ones.
	cancelledAssoc := make(map[string]bool, len(p.AssociationIDs))
	if r.assocStore != nil {
		r.assocStore.mu.RLock()
		for _, id := range p.AssociationIDs {
			if aa := r.assocStore.items[id]; aa != nil && aa.EndDate != nil {
				cancelledAssoc[id] = true
			}
		}
		r.assocStore.mu.RUnlock()
	}

	r.lineItemStore.mu.Lock()
	defer r.lineItemStore.mu.Unlock()

	updated := 0
	for _, li := range r.lineItemStore.items {
		if li == nil || li.Status != types.StatusPublished {
			continue
		}
		if li.EntityType != types.SubscriptionLineItemEntityTypeAddon || li.AddonAssociationID == nil {
			continue
		}
		if _, ok := assocSet[*li.AddonAssociationID]; !ok {
			continue
		}
		pp, ok := endedPrice[li.PriceID]
		if !ok || pp.EndDate == nil {
			continue
		}
		if !scopedByCtx(ctx, li.TenantID, li.EnvironmentID) {
			continue
		}
		// Open lines always close at the price's end; a line that outlives the price is
		// only shortened when its association is cancelled at period end.
		if !li.EndDate.IsZero() {
			if !cancelledAssoc[*li.AddonAssociationID] || !li.EndDate.After(*pp.EndDate) {
				continue
			}
		}
		endDate := *pp.EndDate
		if !li.StartDate.IsZero() && li.StartDate.After(endDate) {
			endDate = li.StartDate
		}
		li.EndDate = endDate
		li.UpdatedAt = time.Now().UTC()
		if uid := types.GetUserID(ctx); uid != "" {
			li.UpdatedBy = uid
		}
		updated++
	}
	return updated, nil
}

// StampAssociations sets synced_price_sequence on the given associations.
// Forward-only: never lowers an existing higher value.
func (r *InMemoryAddonPriceSyncStore) StampAssociations(
	ctx context.Context,
	p addonpricesync.StampAssociationsParams,
) (int, error) {
	if len(p.AssociationIDs) == 0 || r.assocStore == nil {
		return 0, nil
	}

	r.assocStore.mu.Lock()
	defer r.assocStore.mu.Unlock()

	updated := 0
	for _, id := range p.AssociationIDs {
		aa, ok := r.assocStore.items[id]
		if !ok || aa == nil {
			continue
		}
		if !scopedByCtx(ctx, aa.TenantID, aa.EnvironmentID) {
			continue
		}
		if aa.SyncedPriceSequence >= p.TargetSeq {
			continue
		}
		aa.SyncedPriceSequence = p.TargetSeq
		aa.UpdatedAt = time.Now().UTC()
		if uid := types.GetUserID(ctx); uid != "" {
			aa.UpdatedBy = uid
		}
		updated++
	}
	return updated, nil
}

// CountUnsyncedAssociations returns how many still-billing associations are
// behind targetSeq.
func (r *InMemoryAddonPriceSyncStore) CountUnsyncedAssociations(ctx context.Context, addonID string, targetSeq int64) (int64, error) {
	if addonID == "" {
		return 0, ierr.NewError("addon_id is required").Mark(ierr.ErrValidation)
	}
	var count int64
	for _, aa := range r.stillBillingAssociations(ctx, addonID) {
		if aa.SyncedPriceSequence < targetSeq {
			count++
		}
	}
	return count, nil
}
