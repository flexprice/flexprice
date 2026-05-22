package testutil

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/domain/planpricesync"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryPlanPriceSyncStore implements planpricesync.Repository against the
// other in-memory stores (prices, subscriptions, subscription line items).
// The behavior mirrors internal/repository/ent/plan_price_sync*.go closely
// enough for unit tests of code that only consumes the interface — it is
// intentionally not a clone-for-clone replica of the Postgres queries.
type InMemoryPlanPriceSyncStore struct {
	priceStore    *InMemoryPriceStore
	subStore      *InMemorySubscriptionStore
	lineItemStore *InMemorySubscriptionLineItemStore
}

func NewInMemoryPlanPriceSyncStore(
	priceStore *InMemoryPriceStore,
	subStore *InMemorySubscriptionStore,
	lineItemStore *InMemorySubscriptionLineItemStore,
) *InMemoryPlanPriceSyncStore {
	return &InMemoryPlanPriceSyncStore{
		priceStore:    priceStore,
		subStore:      subStore,
		lineItemStore: lineItemStore,
	}
}

// Clear is a no-op — this store owns no state, only refs to other stores.
func (r *InMemoryPlanPriceSyncStore) Clear() {}

// --- snapshot helpers -------------------------------------------------------

func (r *InMemoryPlanPriceSyncStore) allPrices() []*price.Price {
	if r.priceStore == nil {
		return nil
	}
	r.priceStore.mu.RLock()
	defer r.priceStore.mu.RUnlock()
	out := make([]*price.Price, 0, len(r.priceStore.items))
	for _, p := range r.priceStore.items {
		out = append(out, p)
	}
	return out
}

func (r *InMemoryPlanPriceSyncStore) allSubscriptions() []*subscription.Subscription {
	if r.subStore == nil {
		return nil
	}
	r.subStore.mu.RLock()
	defer r.subStore.mu.RUnlock()
	out := make([]*subscription.Subscription, 0, len(r.subStore.items))
	for _, s := range r.subStore.items {
		out = append(out, s)
	}
	return out
}

func (r *InMemoryPlanPriceSyncStore) allLineItems() []*subscription.SubscriptionLineItem {
	if r.lineItemStore == nil {
		return nil
	}
	r.lineItemStore.mu.RLock()
	defer r.lineItemStore.mu.RUnlock()
	out := make([]*subscription.SubscriptionLineItem, 0, len(r.lineItemStore.items))
	for _, li := range r.lineItemStore.items {
		out = append(out, li)
	}
	return out
}

// scopedByCtx matches the tenant_id + environment_id scoping the Postgres
// queries enforce. Items with empty tenant/env pass (callers may set neither).
func scopedByCtx(ctx context.Context, tenantID, envID string) bool {
	ctxTenant := types.GetTenantID(ctx)
	if ctxTenant != "" && tenantID != "" && ctxTenant != tenantID {
		return false
	}
	return CheckEnvironmentFilter(ctx, envID)
}

// isPlanUsagePrice reports whether p is a published USAGE plan price for planID.
// This mirrors the WHERE clause used by every plan-price-sync query.
func isPlanUsagePrice(p *price.Price, planID string) bool {
	return p != nil &&
		p.Status == types.StatusPublished &&
		p.EntityType == types.PRICE_ENTITY_TYPE_PLAN &&
		p.EntityID == planID &&
		p.Type == types.PRICE_TYPE_USAGE
}

// --- planpricesync.Repository implementation --------------------------------

// CurrentPlanSequence returns max(prices.sequence) for the plan's published
// USAGE prices. 0 when no qualifying prices exist.
func (r *InMemoryPlanPriceSyncStore) CurrentPlanSequence(
	ctx context.Context,
	planID string,
) (int64, error) {
	if planID == "" {
		return 0, ierr.NewError("plan_id is required").Mark(ierr.ErrValidation)
	}

	var maxSeq int64
	for _, p := range r.allPrices() {
		if !isPlanUsagePrice(p, planID) {
			continue
		}
		if !scopedByCtx(ctx, p.TenantID, p.EnvironmentID) {
			continue
		}
		if p.Sequence > maxSeq {
			maxSeq = p.Sequence
		}
	}
	return maxSeq, nil
}

// StampSubsAsSynced sets synced_price_sequence on the given subs.
// Forward-only: never lowers an existing higher value.
func (r *InMemoryPlanPriceSyncStore) StampSubsAsSynced(
	ctx context.Context,
	p planpricesync.StampSubsAsSyncedParams,
) (int, error) {
	if len(p.SubIDs) == 0 || r.subStore == nil {
		return 0, nil
	}

	r.subStore.mu.Lock()
	defer r.subStore.mu.Unlock()

	updated := 0
	for _, id := range p.SubIDs {
		sub, ok := r.subStore.items[id]
		if !ok || sub == nil {
			continue
		}
		if !scopedByCtx(ctx, sub.TenantID, sub.EnvironmentID) {
			continue
		}
		if sub.SyncedPriceSequence >= p.TargetSeq {
			continue
		}
		sub.SyncedPriceSequence = p.TargetSeq
		sub.UpdatedAt = time.Now().UTC()
		if uid := types.GetUserID(ctx); uid != "" {
			sub.UpdatedBy = uid
		}
		updated++
	}
	return updated, nil
}

// ListPlanLineItemsToCreateV2 returns missing (sub, price) pairs and the full
// set of stale sub IDs for a plan. A sub is stale when
// synced_price_sequence < TargetSeq. A pair is missing when a candidate plan
// price has no matching plan-derived line item on the sub.
func (r *InMemoryPlanPriceSyncStore) ListPlanLineItemsToCreateV2(
	ctx context.Context,
	p planpricesync.ListPlanLineItemsToCreateV2Params,
) ([]planpricesync.PlanLineItemCreationDelta, []string, error) {
	if p.PlanID == "" {
		return nil, nil, ierr.NewError("plan_id is required").Mark(ierr.ErrValidation)
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 1000
	}

	planPrices := r.planPrices(ctx, p.PlanID)
	subPricesBySub := r.subscriptionPricesBySub(ctx)
	lineItemsBySub := r.planLineItemsBySub(ctx)

	staleSubs := r.staleSubsForPlan(ctx, p.PlanID, p.TargetSeq, limit)
	staleSubIDs := make([]string, 0, len(staleSubs))
	var items []planpricesync.PlanLineItemCreationDelta

	for _, sub := range staleSubs {
		staleSubIDs = append(staleSubIDs, sub.ID)
		for _, pp := range planPrices {
			if !priceMatchesSub(pp, sub) {
				continue
			}
			if pp.Sequence <= sub.SyncedPriceSequence {
				continue
			}
			if pp.EndDate != nil && sub.StartDate.After(*pp.EndDate) {
				continue
			}
			if hasSubscriptionPriceFor(subPricesBySub[sub.ID], pp) {
				continue
			}
			if hasPlanLineItemFor(lineItemsBySub[sub.ID], pp.ID) {
				continue
			}
			items = append(items, planpricesync.PlanLineItemCreationDelta{
				SubscriptionID: sub.ID,
				PriceID:        pp.ID,
				CustomerID:     sub.CustomerID,
			})
		}
	}
	return items, staleSubIDs, nil
}

// TerminatePlanPricesLineItemsV2 sets end_date on live plan-derived line items
// belonging to the given subs whose plan price has been ended. Idempotent —
// items with non-nil end_date are skipped.
func (r *InMemoryPlanPriceSyncStore) TerminatePlanPricesLineItemsV2(
	ctx context.Context,
	p planpricesync.TerminatePlanPricesLineItemsV2Params,
) (int, error) {
	if p.PlanID == "" {
		return 0, ierr.NewError("plan_id is required").Mark(ierr.ErrValidation)
	}
	if len(p.SubIDs) == 0 || r.lineItemStore == nil {
		return 0, nil
	}

	priceByID := make(map[string]*price.Price)
	for _, pp := range r.planPrices(ctx, p.PlanID) {
		if pp.EndDate == nil {
			continue
		}
		priceByID[pp.ID] = pp
	}
	if len(priceByID) == 0 {
		return 0, nil
	}

	subSet := lo.SliceToMap(p.SubIDs, func(id string) (string, struct{}) { return id, struct{}{} })

	r.lineItemStore.mu.Lock()
	defer r.lineItemStore.mu.Unlock()

	updated := 0
	for _, li := range r.lineItemStore.items {
		if li == nil || li.Status != types.StatusPublished {
			continue
		}
		if li.EntityType != types.SubscriptionLineItemEntityTypePlan {
			continue
		}
		if !li.EndDate.IsZero() {
			continue
		}
		if _, ok := subSet[li.SubscriptionID]; !ok {
			continue
		}
		pp, ok := priceByID[li.PriceID]
		if !ok || pp.EndDate == nil {
			continue
		}
		if !scopedByCtx(ctx, li.TenantID, li.EnvironmentID) {
			continue
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

// TerminateExpiredPlanPricesLineItems is the V1 termination — sets end_date on
// every live plan-derived line item whose price has been ended, plan-wide.
func (r *InMemoryPlanPriceSyncStore) TerminateExpiredPlanPricesLineItems(
	ctx context.Context,
	p planpricesync.TerminateExpiredPlanPricesLineItemsParams,
) (int, error) {
	if p.PlanID == "" {
		return 0, ierr.NewError("plan_id is required").Mark(ierr.ErrValidation)
	}
	subIDs := lo.Map(r.allSubscriptions(), func(s *subscription.Subscription, _ int) string { return s.ID })
	return r.TerminatePlanPricesLineItemsV2(ctx, planpricesync.TerminatePlanPricesLineItemsV2Params{
		PlanID: p.PlanID,
		SubIDs: subIDs,
	})
}

// ListPlanLineItemsToTerminate returns the (line item, price.end_date) pairs
// that TerminateExpiredPlanPricesLineItems would terminate.
func (r *InMemoryPlanPriceSyncStore) ListPlanLineItemsToTerminate(
	ctx context.Context,
	p planpricesync.ListPlanLineItemsToTerminateParams,
) ([]planpricesync.PlanLineItemTerminationDelta, error) {
	if p.PlanID == "" {
		return nil, ierr.NewError("plan_id is required").Mark(ierr.ErrValidation)
	}

	priceByID := make(map[string]*price.Price)
	for _, pp := range r.planPrices(ctx, p.PlanID) {
		if pp.EndDate == nil {
			continue
		}
		priceByID[pp.ID] = pp
	}

	var out []planpricesync.PlanLineItemTerminationDelta
	for _, li := range r.allLineItems() {
		if li == nil || li.Status != types.StatusPublished {
			continue
		}
		if li.EntityType != types.SubscriptionLineItemEntityTypePlan {
			continue
		}
		if !li.EndDate.IsZero() {
			continue
		}
		pp, ok := priceByID[li.PriceID]
		if !ok || pp.EndDate == nil {
			continue
		}
		if !scopedByCtx(ctx, li.TenantID, li.EnvironmentID) {
			continue
		}
		out = append(out, planpricesync.PlanLineItemTerminationDelta{
			LineItemID:     li.ID,
			SubscriptionID: li.SubscriptionID,
			PriceID:        li.PriceID,
			TargetEndDate:  *pp.EndDate,
		})
	}
	limit := p.Limit
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ListPlanLineItemsToCreate returns missing (sub, price) pairs for a plan,
// V1 style (no sequence narrowing, cursor-driven via AfterSubID).
func (r *InMemoryPlanPriceSyncStore) ListPlanLineItemsToCreate(
	ctx context.Context,
	p planpricesync.ListPlanLineItemsToCreateParams,
) ([]planpricesync.PlanLineItemCreationDelta, error) {
	if p.PlanID == "" {
		return nil, ierr.NewError("plan_id is required").Mark(ierr.ErrValidation)
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 1000
	}

	planPrices := r.planPrices(ctx, p.PlanID)
	subPricesBySub := r.subscriptionPricesBySub(ctx)
	lineItemsBySub := r.planLineItemsBySub(ctx)

	subs := r.candidateSubsForPlan(ctx, p.PlanID)
	sort.Slice(subs, func(i, j int) bool { return subs[i].ID < subs[j].ID })

	pagedSubs := make([]*subscription.Subscription, 0, len(subs))
	for _, sub := range subs {
		if p.AfterSubID != "" && sub.ID <= p.AfterSubID {
			continue
		}
		pagedSubs = append(pagedSubs, sub)
		if len(pagedSubs) >= limit {
			break
		}
	}

	var items []planpricesync.PlanLineItemCreationDelta
	for _, sub := range pagedSubs {
		for _, pp := range planPrices {
			if !priceMatchesSub(pp, sub) {
				continue
			}
			if pp.EndDate != nil && sub.StartDate.After(*pp.EndDate) {
				continue
			}
			if hasSubscriptionPriceFor(subPricesBySub[sub.ID], pp) {
				continue
			}
			if hasPlanLineItemFor(lineItemsBySub[sub.ID], pp.ID) {
				continue
			}
			items = append(items, planpricesync.PlanLineItemCreationDelta{
				SubscriptionID: sub.ID,
				PriceID:        pp.ID,
				CustomerID:     sub.CustomerID,
			})
		}
	}
	return items, nil
}

// GetLastSubscriptionIDInBatch returns the last sub ID in the candidate batch
// after AfterSubID, used by V1 to advance the cursor when no pairs were found.
func (r *InMemoryPlanPriceSyncStore) GetLastSubscriptionIDInBatch(
	ctx context.Context,
	p planpricesync.ListPlanLineItemsToCreateParams,
) (*string, error) {
	if p.PlanID == "" {
		return nil, ierr.NewError("plan_id is required").Mark(ierr.ErrValidation)
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 1000
	}

	subs := r.candidateSubsForPlan(ctx, p.PlanID)
	sort.Slice(subs, func(i, j int) bool { return subs[i].ID < subs[j].ID })

	var last string
	count := 0
	for _, sub := range subs {
		if p.AfterSubID != "" && sub.ID <= p.AfterSubID {
			continue
		}
		last = sub.ID
		count++
		if count >= limit {
			break
		}
	}
	if last == "" || last == p.AfterSubID {
		return nil, nil
	}
	return &last, nil
}

// --- helpers ----------------------------------------------------------------

func (r *InMemoryPlanPriceSyncStore) planPrices(ctx context.Context, planID string) []*price.Price {
	var out []*price.Price
	for _, p := range r.allPrices() {
		if !isPlanUsagePrice(p, planID) {
			continue
		}
		if !scopedByCtx(ctx, p.TenantID, p.EnvironmentID) {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (r *InMemoryPlanPriceSyncStore) subscriptionPricesBySub(ctx context.Context) map[string][]*price.Price {
	out := make(map[string][]*price.Price)
	for _, p := range r.allPrices() {
		if p == nil || p.Status != types.StatusPublished {
			continue
		}
		if p.EntityType != types.PRICE_ENTITY_TYPE_SUBSCRIPTION {
			continue
		}
		if !scopedByCtx(ctx, p.TenantID, p.EnvironmentID) {
			continue
		}
		out[p.EntityID] = append(out[p.EntityID], p)
	}
	return out
}

func (r *InMemoryPlanPriceSyncStore) planLineItemsBySub(ctx context.Context) map[string][]*subscription.SubscriptionLineItem {
	out := make(map[string][]*subscription.SubscriptionLineItem)
	for _, li := range r.allLineItems() {
		if li == nil || li.Status != types.StatusPublished {
			continue
		}
		if li.EntityType != types.SubscriptionLineItemEntityTypePlan {
			continue
		}
		if !scopedByCtx(ctx, li.TenantID, li.EnvironmentID) {
			continue
		}
		out[li.SubscriptionID] = append(out[li.SubscriptionID], li)
	}
	return out
}

// candidateSubsForPlan returns published subs on planID in a status/type that
// owns plan line items (matches the V1/V2 query's eligibility filter).
func (r *InMemoryPlanPriceSyncStore) candidateSubsForPlan(ctx context.Context, planID string) []*subscription.Subscription {
	eligibleStatus := map[types.SubscriptionStatus]bool{
		types.SubscriptionStatusActive:     true,
		types.SubscriptionStatusTrialing:   true,
		types.SubscriptionStatusDraft:      true,
		types.SubscriptionStatusIncomplete: true,
	}
	eligibleType := lo.SliceToMap(types.SubscriptionTypesWithLineItems, func(t types.SubscriptionType) (types.SubscriptionType, struct{}) {
		return t, struct{}{}
	})

	var out []*subscription.Subscription
	for _, sub := range r.allSubscriptions() {
		if sub == nil || sub.Status != types.StatusPublished {
			continue
		}
		if sub.PlanID != planID {
			continue
		}
		if !eligibleStatus[sub.SubscriptionStatus] {
			continue
		}
		if _, ok := eligibleType[sub.SubscriptionType]; !ok {
			continue
		}
		if !scopedByCtx(ctx, sub.TenantID, sub.EnvironmentID) {
			continue
		}
		out = append(out, sub)
	}
	return out
}

func (r *InMemoryPlanPriceSyncStore) staleSubsForPlan(ctx context.Context, planID string, targetSeq int64, limit int) []*subscription.Subscription {
	subs := r.candidateSubsForPlan(ctx, planID)
	stale := subs[:0]
	for _, sub := range subs {
		if sub.SyncedPriceSequence < targetSeq {
			stale = append(stale, sub)
		}
	}
	sort.Slice(stale, func(i, j int) bool { return stale[i].ID < stale[j].ID })
	if limit > 0 && len(stale) > limit {
		stale = stale[:limit]
	}
	return stale
}

func priceMatchesSub(p *price.Price, sub *subscription.Subscription) bool {
	return p != nil && sub != nil &&
		p.Currency == sub.Currency &&
		p.BillingPeriod == sub.BillingPeriod &&
		p.BillingPeriodCount == sub.BillingPeriodCount
}

// hasSubscriptionPriceFor reports whether the sub already has a sub-scoped
// price linked to the plan price (directly via parent_price_id, or as a
// sibling via shared parent_price_id).
func hasSubscriptionPriceFor(subPrices []*price.Price, planPrice *price.Price) bool {
	for _, sp := range subPrices {
		if sp.ParentPriceID == "" {
			continue
		}
		if sp.ParentPriceID == planPrice.ID {
			return true
		}
		if planPrice.ParentPriceID != "" && sp.ParentPriceID == planPrice.ParentPriceID {
			return true
		}
	}
	return false
}

func hasPlanLineItemFor(items []*subscription.SubscriptionLineItem, planPriceID string) bool {
	for _, li := range items {
		if li.PriceID == planPriceID {
			return true
		}
	}
	return false
}
