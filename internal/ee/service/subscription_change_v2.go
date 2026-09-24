package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type planChangeRequest struct {
	currentSub *subscription.Subscription
	updatedSub *subscription.Subscription

	fromPlan       *plan.Plan
	toPlan         *plan.Plan
	effectiveAt    time.Time
	behavior       types.ProrationBehavior
	idempotencyKey string

	carriedLineItems []LineItemProrationEntry
	closingLineItems []LineItemProrationEntry
	openingLineItems []LineItemProrationEntry

	creditGrants                *planChangeCreditGrants
	closingEntitlementOverrides []*entitlement.Entitlement
	closingEntitlementGrants    []*entitlementgrant.EntitlementGrant

	anchorAtEffect bool

	// outgoingUsage bills the outgoing plan's usage and is set only under a reset; settlementQuote is
	// the net of credits against charges, nil when the change settles no money.
	outgoingUsage   *dto.CreateInvoiceRequest
	settlementQuote *LineItemProrationSummary

	changes  []*dto.EntityChangeResult
	warnings []string

	changeType types.SubscriptionChangeType
}

// planChangeCreditGrants is the credit-grant migration a plan change performs. Plan-level
// grants are materialised per subscription at creation time.
type planChangeCreditGrants struct {
	// cancel holds the subscription's grants materialised from the outgoing plan.
	cancel []*creditgrant.CreditGrant

	// opening holds the target plan's grants.
	opening []*dto.CreditGrantResponse
}

func (s *subscriptionService) resolvePlanChange(
	ctx context.Context,
	sub *subscription.Subscription,
	req dto.SubscriptionChangeV2Request,
	effectiveAt time.Time,
) (*planChangeRequest, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	planSvc := NewPlanService(s.ServiceParams)
	toPlan, err := planSvc.GetPlan(ctx, req.TargetPlanID)
	if err != nil {
		return nil, err
	}
	fromPlan, err := planSvc.GetPlan(ctx, sub.PlanID)
	if err != nil {
		return nil, err
	}

	if err := s.checkPlanChangePreconditions(sub, req); err != nil {
		return nil, err
	}

	// Plan change v2 does not currently support caller-driven price selection —
	// pass nil so the strict-equal default applies (matches historical behavior
	// on this path).
	targetPrices, err := s.ValidateAndFilterPricesForSubscription(
		ctx, toPlan.Plan.ID, types.PRICE_ENTITY_TYPE_PLAN, sub, nil, nil,
	)
	if err != nil {
		return nil, err
	}
	if len(targetPrices) == 0 {
		return nil, ierr.NewError("target plan has no prices compatible with this subscription").
			WithHint("The target plan must have prices matching the subscription's currency and billing period. Use the v1 change endpoint to change interval or currency.").
			WithReportableDetails(map[string]any{
				"subscription_id": sub.ID,
				"target_plan_id":  toPlan.Plan.ID,
				"currency":        sub.Currency,
				"billing_period":  sub.BillingPeriod,
			}).
			Mark(ierr.ErrValidation)
	}

	r := &planChangeRequest{
		currentSub:     sub,
		fromPlan:       fromPlan.Plan,
		toPlan:         toPlan.Plan,
		effectiveAt:    effectiveAt,
		behavior:       req.ProrationBehavior,
		idempotencyKey: lo.FromPtr(req.IdempotencyKey),
		anchorAtEffect: req.AnchorAtEffect() && !req.IsDeferred(),
	}

	updatedSub, err := projectSubscriptionAfterChange(r)
	if err != nil {
		return nil, err
	}
	if updatedSub.SyncedPriceSequence, err = s.PlanPriceSyncRepo.CurrentPlanSequence(ctx, r.toPlan.ID); err != nil {
		return nil, err
	}
	r.updatedSub = updatedSub

	carriedLineItems, openingLineItems, closingLineItems, err := s.resolveLineItems(ctx, sub, targetPrices, toPlan, effectiveAt)
	if err != nil {
		return nil, err
	}

	closingAddonLineItems, addonsChanges, addonsWarnings, err := s.resolveAddonChanges(ctx, sub, req)
	if err != nil {
		return nil, err
	}

	r.carriedLineItems = carriedLineItems
	r.openingLineItems = openingLineItems
	r.closingLineItems = append(closingLineItems, closingAddonLineItems...)
	r.changes = addonsChanges
	r.warnings = addonsWarnings
	r.changeType = planChangeType(r)

	// The settlement is resolved here, before any write, so preview and execute quote the
	// same documents and settling them is order-independent.
	if r.anchorAtEffect {
		if r.outgoingUsage, err = s.outgoingUsageInvoiceRequest(ctx, r); err != nil {
			return nil, err
		}
		if r.settlementQuote, err = s.resetQuote(ctx, r); err != nil {
			return nil, err
		}
	} else if r.behavior == types.ProrationBehaviorCreateProrations {
		if r.settlementQuote, err = s.computePlanChange(ctx, r); err != nil {
			return nil, err
		}
	}

	if err := s.resolveCreditGrantMigration(ctx, r); err != nil {
		return nil, err
	}

	if err := s.resolveClosingEntitlements(ctx, r); err != nil {
		return nil, err
	}

	return r, nil
}

// resolveCreditGrantMigration works out which grants leave with the old plan and which
// arrive with the new one. Preview and execute both call it; only execute applies it.
func (s *subscriptionService) resolveCreditGrantMigration(ctx context.Context, r *planChangeRequest) error {
	filter := types.NewNoLimitCreditGrantFilter()
	filter.SubscriptionIDs = []string{r.currentSub.ID}
	filter.PlanIDs = []string{r.fromPlan.ID}
	filter.WithStatus(types.StatusPublished)

	outgoing, err := s.CreditGrantRepo.List(ctx, filter)
	if err != nil {
		return err
	}

	targetGrants, err := NewCreditGrantService(s.ServiceParams).GetCreditGrantsByPlan(ctx, r.toPlan.ID)
	if err != nil {
		return err
	}

	grants := &planChangeCreditGrants{}
	for _, cg := range outgoing {
		// Already closed at or before the change instant: nothing future to cancel.
		if cg.EndDate != nil && !cg.EndDate.After(r.effectiveAt) {
			continue
		}
		grants.cancel = append(grants.cancel, cg)
	}

	for _, cg := range targetGrants.Items {
		if cg == nil || cg.CreditGrant == nil {
			continue
		}
		grants.opening = append(grants.opening, cg)
	}
	r.creditGrants = grants

	for _, cg := range grants.cancel {
		r.changes = append(r.changes, &dto.EntityChangeResult{
			EntityType:  types.SubscriptionChangeEntityTypeCreditGrant,
			ReferenceID: r.fromPlan.ID,
			EntityID:    cg.ID,
			Behaviour:   types.EntityChangeBehaviourDrop,
		})
	}

	for _, cg := range grants.opening {
		r.changes = append(r.changes, &dto.EntityChangeResult{
			EntityType:  types.SubscriptionChangeEntityTypeCreditGrant,
			ReferenceID: r.toPlan.ID,
			EntityID:    cg.ID,
			Behaviour:   types.EntityChangeBehaviourAdd,
		})
	}

	return nil
}

// resolveClosingEntitlements finds what the outgoing plan leaves behind on the subscription:
// subscription-scoped entitlements whose parent is one of its entitlements, and the grant
// windows opened from either. After the swap they suppress nothing and stack with the new
// plan's entitlement on the same feature, so they must be closed.
func (s *subscriptionService) resolveClosingEntitlements(ctx context.Context, r *planChangeRequest) error {
	entitlementSvc := NewEntitlementService(s.ServiceParams)

	fromPlanEnts, err := entitlementSvc.ListEntitlements(ctx, types.NewNoLimitEntitlementFilter().
		WithEntityIDs([]string{r.fromPlan.ID}).
		WithEntityType(types.ENTITLEMENT_ENTITY_TYPE_PLAN).
		WithStatus(types.StatusPublished))
	if err != nil {
		return err
	}
	if len(fromPlanEnts.Items) == 0 {
		return nil
	}

	fromPlanEntIDs := make(map[string]bool, len(fromPlanEnts.Items))
	for _, ent := range fromPlanEnts.Items {
		if ent != nil {
			fromPlanEntIDs[ent.ID] = true
		}
	}

	subEnts, err := entitlementSvc.ListEntitlements(ctx, types.NewNoLimitEntitlementFilter().
		WithEntityIDs([]string{r.currentSub.ID}).
		WithEntityType(types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION).
		WithStatus(types.StatusPublished))
	if err != nil {
		return err
	}

	for _, ent := range subEnts.Items {
		if ent == nil || ent.Entitlement == nil {
			continue
		}
		if !fromPlanEntIDs[lo.FromPtr(ent.ParentEntitlementID)] {
			continue
		}
		// Already closed at or before the change instant: nothing left to suppress.
		if ent.EndDate != nil && !ent.EndDate.After(r.effectiveAt) {
			continue
		}

		r.closingEntitlementOverrides = append(r.closingEntitlementOverrides, ent.Entitlement)
		r.changes = append(r.changes, &dto.EntityChangeResult{
			EntityType:  types.SubscriptionChangeEntityTypeEntitlement,
			ReferenceID: ent.FeatureID,
			EntityID:    ent.ID,
			Behaviour:   types.EntityChangeBehaviourDrop,
		})
	}

	return s.resolveClosingEntitlementGrants(ctx, r, fromPlanEntIDs)
}

// resolveClosingEntitlementGrants collects the subscription's grant windows still open at the
// change instant whose config is an outgoing-plan entitlement or an override closing with it.
func (s *subscriptionService) resolveClosingEntitlementGrants(
	ctx context.Context,
	r *planChangeRequest,
	fromPlanEntIDs map[string]bool,
) error {
	// Grants key off the entitlement the subscription actually resolves to, which is the
	// override where one is active and the plan entitlement otherwise.

	// NOTE: This logic doesnot close entitlement grants that are additive and derived from
	// some entities which are not being closed like addon. There is no way to be certain on
	// what qouta had been used in that grant and associate it to different constituent grant qoutas.
	// If needed, later on a prorated handling could be added to close the existing grant and open a
	// new one with prorated qouta.

	configIDs := lo.Keys(fromPlanEntIDs)
	for _, ent := range r.closingEntitlementOverrides {
		configIDs = append(configIDs, ent.ID)
	}

	filter := types.NewNoLimitEntitlementGrantFilter().
		WithSubscriptionIDs(r.currentSub.ID).
		WithEntitlementConfigIDs(configIDs...)
	filter.ValidToAfter = &r.effectiveAt

	grants, err := s.EntitlementGrantRepo.List(ctx, filter)
	if err != nil {
		return err
	}

	for _, g := range grants {
		if g == nil {
			continue
		}

		r.closingEntitlementGrants = append(r.closingEntitlementGrants, g)
		r.changes = append(r.changes, &dto.EntityChangeResult{
			EntityType:  types.SubscriptionChangeEntityTypeEntitlementGrant,
			ReferenceID: g.FeatureID(),
			EntityID:    g.ID,
			Behaviour:   types.EntityChangeBehaviourDrop,
		})
	}

	return nil
}

// migrateCreditGrants cancels the outgoing plan's grants and materialises the target
// plan's, both dated at the change instant.
//
// TODO: a change landing mid-period under the default billing_period_behaviour
// ("unchanged") grants the target plan's first period in full even though only part of
// that period remains. addonCreditGrantProration (subscription.go) already solves this
// exact shape for addons and is the intended reuse.
func (s *subscriptionService) migrateCreditGrants(ctx context.Context, r *planChangeRequest) error {
	if r.creditGrants == nil {
		return nil
	}

	if len(r.creditGrants.cancel) > 0 {
		if err := NewCreditGrantService(s.ServiceParams).CancelFutureSubscriptionGrants(ctx, dto.CancelFutureSubscriptionGrantsRequest{
			SubscriptionID: r.currentSub.ID,
			PlanID:         lo.ToPtr(r.fromPlan.ID),
			EffectiveDate:  lo.ToPtr(r.effectiveAt),
		}); err != nil {
			return err
		}
	}

	create := make([]dto.CreateCreditGrantRequest, 0, len(r.creditGrants.opening))
	for _, cg := range r.creditGrants.opening {
		create = append(create, dto.NewSubscriptionScopedCreditGrantRequest(cg, r.currentSub.ID, r.toPlan.ID))
	}

	if err := NewCreditGrantService(s.ServiceParams).CreateSubscriptionCreditGrants(ctx, dto.CreateSubscriptionCreditGrantsRequest{
		Subscription: r.updatedSub,
		Grants:       create,
		StartDate:    r.effectiveAt,
	}); err != nil {
		return err
	}

	s.Logger.Info(ctx, "migrated credit grants for plan change",
		"subscription_id", r.currentSub.ID,
		"from_plan_id", r.fromPlan.ID,
		"to_plan_id", r.toPlan.ID,
		"cancelled_grants", len(r.creditGrants.cancel),
		"created_grants", len(r.creditGrants.opening),
		"effective_at", r.effectiveAt)

	return nil
}

// closeEntitlementOverrides end-dates the overrides resolved above at the change instant.
func (s *subscriptionService) closeEntitlementOverrides(ctx context.Context, r *planChangeRequest) error {
	for _, ent := range r.closingEntitlementOverrides {
		closed := entitlement.NewEntitlementBuilder(ent).
			WithEndDate(r.effectiveAt).
			WithUpdatedBy(types.GetUserID(ctx)).
			Build()

		if _, err := s.EntitlementRepo.Update(ctx, closed); err != nil {
			return err
		}

		s.Logger.Info(ctx, "closed subscription entitlement override on plan change",
			"subscription_id", r.currentSub.ID,
			"entitlement_id", closed.ID,
			"feature_id", closed.FeatureID,
			"from_plan_id", r.fromPlan.ID,
			"effective_at", r.effectiveAt)
	}

	return nil
}

// closeEntitlementGrants ends the outgoing plan's open grant windows at the change instant.
// Closing shrinks valid_to, which the evaluator treats as a window owed one final usage
// refresh, so events landing before the swap still reach the billing reads.
func (s *subscriptionService) closeEntitlementGrants(ctx context.Context, r *planChangeRequest) error {
	for _, g := range r.closingEntitlementGrants {
		// A window opening after the swap has nothing to keep; close it empty rather than inverted.
		closeAt := r.effectiveAt
		if closeAt.Before(g.ValidFrom) {
			closeAt = g.ValidFrom
		}

		closed := entitlementgrant.NewEntitlementGrantBuilder(g).
			WithWindow(g.ValidFrom, closeAt).
			Build()

		if _, err := s.EntitlementGrantRepo.Update(ctx, closed); err != nil {
			return err
		}

		s.Logger.Info(ctx, "closed entitlement grant window on plan change",
			"subscription_id", r.currentSub.ID,
			"grant_id", closed.ID,
			"entitlement_config_id", closed.EntitlementConfigID,
			"feature_id", closed.FeatureID(),
			"from_plan_id", r.fromPlan.ID,
			"valid_to", closeAt)
	}

	return nil
}

func (s *subscriptionService) checkPlanChangePreconditions(
	sub *subscription.Subscription,
	req dto.SubscriptionChangeV2Request,
) error {
	fail := func(msg, hint string, details map[string]any) error {
		return ierr.NewError(msg).WithHint(hint).WithReportableDetails(details).Mark(ierr.ErrValidation)
	}

	if sub.PlanID == req.TargetPlanID {
		return fail("subscription is already on the target plan",
			"The subscription is already on this plan",
			map[string]any{"subscription_id": sub.ID, "plan_id": sub.PlanID})
	}

	if sub.SubscriptionStatus != types.SubscriptionStatusActive &&
		sub.SubscriptionStatus != types.SubscriptionStatusTrialing {
		return fail("subscription is not active",
			"Only active or trialing subscriptions can change plan",
			map[string]any{"subscription_id": sub.ID, "subscription_status": sub.SubscriptionStatus})
	}

	if sub.PauseStatus == types.PauseStatusActive || sub.PauseStatus == types.PauseStatusScheduled {
		return fail("subscription is paused",
			"Resume the subscription before changing its plan",
			map[string]any{"subscription_id": sub.ID, "pause_status": sub.PauseStatus})
	}

	if sub.SubscriptionType != types.SubscriptionTypeStandalone {
		return fail("subscription is part of a hierarchy",
			"Hierarchy subscriptions are not supported by this endpoint. Use the v1 change endpoint.",
			map[string]any{"subscription_id": sub.ID, "subscription_type": sub.SubscriptionType})
	}

	if sub.CancelAtPeriodEnd || sub.CancelAt != nil {
		return fail("subscription has a pending cancellation",
			"Clear the pending cancellation before changing plan",
			map[string]any{"subscription_id": sub.ID})
	}

	if req.AnchorAtEffect() && sub.BillingCycle == types.BillingCycleCalendar {
		return fail("the billing period cannot be reset on a calendar-aligned subscription",
			"Calendar billing keeps every period on a calendar boundary. Use billing_period_behaviour='unchanged', or move the subscription to anniversary billing.",
			map[string]any{"subscription_id": sub.ID, "billing_cycle": sub.BillingCycle})
	}

	// TODO: Handle mixed billing periods correctly.
	if req.ProrationBehavior == types.ProrationBehaviorCreateProrations && sub.HasMixedBillingPeriods() {
		return fail("proration is not supported for subscriptions with mixed billing periods",
			"Set proration_behavior to 'none' to change this subscription's plan",
			map[string]any{"subscription_id": sub.ID})
	}

	for _, item := range sub.LineItems {
		if item.SubscriptionPhaseID != nil {
			return fail("subscription has phases",
				"Phased subscriptions are not supported by this endpoint. Use the v1 change endpoint.",
				map[string]any{"subscription_id": sub.ID})
		}
	}

	return nil
}

func (s *subscriptionService) resolveLineItems(
	ctx context.Context,
	sub *subscription.Subscription,
	targetPrices []*dto.PriceResponse,
	toPlan *dto.PlanResponse,
	effectiveAt time.Time,
) ([]LineItemProrationEntry, []LineItemProrationEntry, []LineItemProrationEntry, error) {
	priceSvc := NewPriceService(s.ServiceParams)
	var carried, opening, closing []LineItemProrationEntry

	current := make([]LineItemProrationEntry, 0, len(sub.LineItems))
	for _, item := range sub.LineItems {
		if item.EntityType != types.SubscriptionLineItemEntityTypePlan || !item.EndDate.IsZero() {
			continue
		}
		p, err := priceSvc.GetPrice(ctx, item.PriceID)
		if err != nil {
			return nil, nil, nil, err
		}
		current = append(current, LineItemProrationEntry{
			LineItem:        item,
			CurrentPrice:    p.Price,
			CurrentQuantity: item.Quantity,
		})
	}

	matchedTarget := make([]bool, len(targetPrices))
	matchedCurrent := make([]bool, len(current))

	for i, live := range current {
		for j, target := range targetPrices {
			if matchedTarget[j] || !live.CurrentPrice.BillsIdenticallyTo(target.Price) {
				continue
			}

			matchedTarget[j], matchedCurrent[i] = true, true
			carried = append(carried, LineItemProrationEntry{
				LineItem:        live.LineItem,
				CurrentPrice:    target.Price,
				CurrentQuantity: live.LineItem.Quantity,
				NewPrice:        target.Price,
				NewQuantity:     live.LineItem.Quantity,
			})
			break
		}
	}

	for i, live := range current {
		// If the current line item is not matched to a target price, it is being removed otherwise it is being carried.
		if !matchedCurrent[i] {
			live.Action = types.ProrationActionRemoveItem
			closing = append(closing, live)
		}
	}

	subResp := &dto.SubscriptionResponse{Subscription: sub}
	for j, target := range targetPrices {
		if matchedTarget[j] {
			continue
		}
		item, err := buildPlanChangeLineItem(ctx, subResp, target, toPlan, effectiveAt)
		if err != nil {
			return nil, nil, nil, err
		}

		opening = append(opening, LineItemProrationEntry{
			LineItem:    item,
			Action:      types.ProrationActionAddItem,
			NewPrice:    target.Price,
			NewQuantity: item.Quantity,
		})
	}

	return carried, opening, closing, nil
}

func buildPlanChangeLineItem(
	ctx context.Context,
	subResp *dto.SubscriptionResponse,
	target *dto.PriceResponse,
	toPlan *dto.PlanResponse,
	effectiveAt time.Time,
) (*subscription.SubscriptionLineItem, error) {
	lineItemReq := &dto.CreateSubscriptionLineItemRequest{
		PriceID:   target.Price.ID,
		StartDate: &effectiveAt,
	}
	if err := lineItemReq.Validate(target.Price, subResp.Subscription); err != nil {
		return nil, err
	}

	item := lineItemReq.ToSubscriptionLineItem(ctx, dto.LineItemParams{
		Subscription: subResp,
		Price:        target,
		Plan:         toPlan,
		EntityType:   types.SubscriptionLineItemEntityTypePlan,
	})

	item.BillingPeriodCount = target.Price.BillingPeriodCount
	return item, nil
}

// Uses recurring amounts (not prorated net) so timing/proration don't change the type.
func planChangeType(r *planChangeRequest) types.SubscriptionChangeType {
	oldTotal, newTotal := decimal.Zero, decimal.Zero
	for _, item := range r.closingLineItems {
		if item.LineItem.EntityType == types.SubscriptionLineItemEntityTypeAddon {
			continue
		}
		oldTotal = oldTotal.Add(item.CurrentPrice.Amount.Mul(item.LineItem.Quantity))
	}

	for _, item := range r.openingLineItems {
		newTotal = newTotal.Add(item.NewPrice.Amount.Mul(item.LineItem.Quantity))
	}

	switch {
	case newTotal.GreaterThan(oldTotal):
		return types.SubscriptionChangeTypeUpgrade
	case newTotal.LessThan(oldTotal):
		return types.SubscriptionChangeTypeDowngrade
	default:
		return types.SubscriptionChangeTypeLateral
	}
}

func (s *subscriptionService) resolveAddonChanges(
	ctx context.Context,
	sub *subscription.Subscription,
	req dto.SubscriptionChangeV2Request,
) ([]LineItemProrationEntry, []*dto.EntityChangeResult, []string, error) {
	var changes []*dto.EntityChangeResult
	var closing []LineItemProrationEntry
	var warnings []string

	var policy *dto.EntityChangePolicy
	if req.EntityPolicies != nil {
		policy = req.EntityPolicies.Addons
	}

	associations, err := s.activeAddonAssociations(ctx, sub.ID)
	if err != nil {
		return nil, nil, nil, err
	}

	seen := make(map[string]bool, len(associations))
	for _, association := range associations {
		seen[association.ID] = true

		behaviour := policy.BehaviourFor(association.ID)
		changes = append(changes, &dto.EntityChangeResult{
			EntityType:  types.SubscriptionChangeEntityTypeAddonAssociation,
			ReferenceID: association.AddonID,
			EntityID:    association.ID,
			Behaviour:   behaviour,
		})

		if behaviour != types.EntityChangeBehaviourDrop {
			continue
		}

		droppedItems, err := s.addonLineItemsToClose(ctx, sub.ID, association)
		if err != nil {
			return nil, nil, nil, err
		}
		closing = append(closing, droppedItems...)
	}

	// Stale override keys (e.g. preview→execute race) warn instead of failing.
	if policy != nil {
		for entityID := range policy.Overrides {
			if !seen[entityID] {
				warnings = append(warnings,
					"ignored addon behaviour for unknown or inactive association "+entityID)
			}
		}
	}

	return closing, changes, warnings, nil
}

func (s *subscriptionService) activeAddonAssociations(
	ctx context.Context,
	subscriptionID string,
) ([]*addonassociation.AddonAssociation, error) {
	resp, err := NewAddonService(s.ServiceParams).GetActiveAddonAssociation(ctx, dto.GetActiveAddonAssociationRequest{
		EntityID:   subscriptionID,
		EntityType: types.AddonAssociationEntityTypeSubscription,
	})
	if err != nil {
		return nil, err
	}

	associations := make([]*addonassociation.AddonAssociation, 0, len(resp.Items))
	for _, item := range resp.Items {
		if item.AddonAssociation != nil {
			associations = append(associations, item.AddonAssociation)
		}
	}
	return associations, nil
}

func (s *subscriptionService) addonLineItemsToClose(
	ctx context.Context,
	subscriptionID string,
	association *addonassociation.AddonAssociation,
) ([]LineItemProrationEntry, error) {
	filter := types.NewSubscriptionLineItemFilter()
	filter.SubscriptionIDs = []string{subscriptionID}
	filter.AddonAssociationIDs = []string{association.ID}

	items, err := s.SubscriptionLineItemRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	priceSvc := NewPriceService(s.ServiceParams)
	closing := make([]LineItemProrationEntry, 0, len(items))
	for _, item := range items {
		if !item.EndDate.IsZero() {
			continue
		}
		p, err := priceSvc.GetPrice(ctx, item.PriceID)
		if err != nil {
			return nil, err
		}
		closing = append(closing, LineItemProrationEntry{
			LineItem:        item,
			Action:          types.ProrationActionRemoveItem,
			CurrentPrice:    p.Price,
			CurrentQuantity: item.Quantity,
		})
	}
	return closing, nil
}

func (s *subscriptionService) applyDroppedAddons(ctx context.Context, r *planChangeRequest) error {
	ids := make([]string, 0, len(r.changes))
	for _, change := range r.changes {
		if change.EntityType == types.SubscriptionChangeEntityTypeAddonAssociation &&
			change.Behaviour == types.EntityChangeBehaviourDrop {
			ids = append(ids, change.EntityID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	dropped, err := s.AddonAssociationRepo.GetByIDs(ctx, ids)
	if err != nil {
		return err
	}

	for _, existing := range dropped {
		association := addonassociation.NewAddonAssociationBuilder(existing).
			WithCancellation(r.effectiveAt, "plan change").
			Build()
		if err := s.AddonAssociationRepo.Update(ctx, association); err != nil {
			return err
		}

		// KNOWN LIMITATION: grants are tagged by addon_id only, so concurrent
		// attachments of the same addon cannot be cancelled independently.
		if err := NewCreditGrantService(s.ServiceParams).CancelFutureSubscriptionGrants(ctx, dto.CancelFutureSubscriptionGrantsRequest{
			SubscriptionID: r.currentSub.ID,
			AddonIDs:       []string{association.AddonID},
			EffectiveDate:  lo.ToPtr(r.effectiveAt),
		}); err != nil {
			return err
		}
	}

	return nil
}

func (s *subscriptionService) computePlanChange(
	ctx context.Context,
	r *planChangeRequest,
) (*LineItemProrationSummary, error) {
	entries := make([]LineItemProrationEntry, 0, len(r.closingLineItems)+len(r.openingLineItems))
	entries = append(entries, r.closingLineItems...)
	entries = append(entries, r.openingLineItems...)

	if len(entries) == 0 {
		return emptyProrationSummary(r.currentSub), nil
	}

	return NewLineItemProrationService(s.ServiceParams).Compute(ctx, LineItemProrationRequest{
		Subscription:  r.currentSub,
		Entries:       entries,
		EffectiveDate: r.effectiveAt,
		Behavior:      r.behavior,
		Reason:        "plan change",
	})
}

func (s *subscriptionService) PreviewPlanChange(
	ctx context.Context,
	subscriptionID string,
	req dto.SubscriptionChangeV2Request,
) (*dto.SubscriptionChangeV2Response, error) {
	sub, err := s.loadSubscriptionForChange(ctx, subscriptionID, false)
	if err != nil {
		return nil, err
	}

	superseded, err := s.planChangeScheduleToSupersede(ctx, subscriptionID, req, "")
	if err != nil {
		return nil, err
	}

	r, err := s.resolvePlanChange(ctx, sub, req, resolveEffectiveAt(sub, req))
	if err != nil {
		return nil, err
	}

	preview, err := s.settlePlanChange(ctx, r, true)
	if err != nil {
		return nil, err
	}

	// A deferred request only records a schedule, so the subscription it leaves behind
	// is the untouched one and nothing settles until the boundary.
	responseSub := r.updatedSub
	if req.IsDeferred() {
		responseSub = sub
	}

	resp := buildPlanChangeResponse(r, responseSub, req)
	resp.ChangedResources.LineItems = planChangeChangedLineItems(r)
	resp.ChangedResources.Invoices = preview
	if superseded != nil {
		resp.SupersededSchedules = []string{superseded.ID}
	}

	if req.IsDeferred() {
		resp.IsScheduled = true
		resp.ScheduledAt = lo.ToPtr(r.effectiveAt)
	}

	return resp, nil
}

func (s *subscriptionService) ExecutePlanChange(
	ctx context.Context,
	subscriptionID string,
	req dto.SubscriptionChangeV2Request,
	effectiveAt time.Time,
) (*dto.SubscriptionChangeV2Response, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	if req.IsDeferred() {
		return s.schedulePlanChangeForPeriodEnd(ctx, subscriptionID, req)
	}

	return s.executePlanChangeAt(ctx, subscriptionID, req, effectiveAt, "")
}

func (s *subscriptionService) executePlanChangeAt(
	ctx context.Context,
	subscriptionID string,
	req dto.SubscriptionChangeV2Request,
	effectiveAt time.Time,
	executingScheduleID string,
) (*dto.SubscriptionChangeV2Response, error) {
	var resp *dto.SubscriptionChangeV2Response

	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		sub, err := s.loadSubscriptionForChange(txCtx, subscriptionID, true)
		if err != nil {
			return err
		}

		superseded, err := s.resolvePendingPlanChangeSchedule(txCtx, subscriptionID, req, executingScheduleID)
		if err != nil {
			return err
		}

		r, err := s.resolvePlanChange(txCtx, sub, req, effectiveAt)
		if err != nil {
			return err
		}

		if err := s.applyPlanChangeLineItems(txCtx, r); err != nil {
			return err
		}

		if err := s.applyDroppedAddons(txCtx, r); err != nil {
			return err
		}

		swappedSub, err := s.applyPlanSwap(txCtx, r)
		if err != nil {
			return err
		}

		anchoredSub, err := s.applyAnchorReset(txCtx, r, swappedSub)
		if err != nil {
			return err
		}
		r.updatedSub = anchoredSub

		if err := s.migrateCreditGrants(txCtx, r); err != nil {
			return err
		}

		if err := s.closeEntitlementOverrides(txCtx, r); err != nil {
			return err
		}

		if err := s.closeEntitlementGrants(txCtx, r); err != nil {
			return err
		}

		changedInvoices, err := s.settlePlanChange(txCtx, r, false)
		if err != nil {
			return err
		}

		resp = buildPlanChangeResponse(r, anchoredSub, req)
		resp.ChangedResources.LineItems = planChangeChangedLineItems(r)
		resp.ChangedResources.Invoices = changedInvoices
		resp.SupersededSchedules = superseded
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "subscription plan changed",
		"subscription_id", subscriptionID,
		"from_plan_id", resp.FromPlan.ID,
		"to_plan_id", resp.ToPlan.ID,
		"change_type", resp.ChangeType)

	s.attemptPlanChangePayment(ctx, resp)

	// After commit so webhook failure cannot roll back a completed change.
	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionPlanChanged, subscriptionID)
	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

	return resp, nil
}

func (s *subscriptionService) attemptPlanChangePayment(ctx context.Context, resp *dto.SubscriptionChangeV2Response) {
	invoiceSvc := NewInvoiceService(s.ServiceParams)
	for _, changed := range resp.ChangedResources.Invoices {
		if changed.Invoice == nil || changed.Invoice.ID == "" {
			continue
		}

		if err := invoiceSvc.AttemptPayment(ctx, changed.Invoice.ID); err != nil {
			s.Logger.Info(ctx, "plan change invoice created but payment attempt failed; invoice remains collectable",
				"error", err, "invoice_id", changed.Invoice.ID)
		}
	}
}

// forUpdate takes the row lock as the first read so concurrent changes serialize.
func (s *subscriptionService) loadSubscriptionForChange(
	ctx context.Context,
	subscriptionID string,
	forUpdate bool,
) (*subscription.Subscription, error) {
	var (
		sub *subscription.Subscription
		err error
	)
	if forUpdate {
		sub, err = s.SubRepo.GetForUpdate(ctx, subscriptionID)
	} else {
		sub, err = s.SubRepo.Get(ctx, subscriptionID)
	}
	if err != nil {
		return nil, err
	}

	lineItems, err := s.SubscriptionLineItemRepo.ListBySubscription(ctx, sub)
	if err != nil {
		return nil, err
	}
	sub.LineItems = lineItems
	return sub, nil
}

func (s *subscriptionService) applyPlanChangeLineItems(ctx context.Context, r *planChangeRequest) error {
	for _, move := range r.carriedLineItems {
		updated := subscription.NewSubscriptionLineItemBuilder(move.LineItem).
			WithPlan(r.toPlan.ID, r.toPlan.Name).
			WithPrice(move.NewPrice).
			Build()

		if err := s.SubscriptionLineItemRepo.Update(ctx, updated); err != nil {
			return err
		}
	}

	for _, move := range r.closingLineItems {
		ended := subscription.NewSubscriptionLineItemBuilder(move.LineItem).
			WithEndDate(r.effectiveAt).
			Build()

		if err := s.SubscriptionLineItemRepo.Update(ctx, ended); err != nil {
			return err
		}
	}

	if len(r.openingLineItems) == 0 {
		return nil
	}
	toCreate := make([]*subscription.SubscriptionLineItem, 0, len(r.openingLineItems))
	for _, move := range r.openingLineItems {
		toCreate = append(toCreate, move.LineItem)
	}
	return s.SubscriptionLineItemRepo.CreateBulk(ctx, toCreate)
}

func (s *subscriptionService) applyPlanSwap(
	ctx context.Context,
	r *planChangeRequest,
) (*subscription.Subscription, error) {
	if err := s.SubRepo.UpdatePlan(ctx, r.currentSub.ID, r.toPlan.ID); err != nil {
		return nil, err
	}

	targetSeq := r.updatedSub.SyncedPriceSequence
	if err := s.PlanPriceSyncRepo.ReanchorSubSyncedSequence(ctx, r.currentSub.ID, targetSeq); err != nil {
		return nil, err
	}

	swapped := *r.updatedSub
	swapped.PlanID = r.toPlan.ID
	return &swapped, nil
}

func (s *subscriptionService) applyAnchorReset(
	ctx context.Context,
	r *planChangeRequest,
	swapped *subscription.Subscription,
) (*subscription.Subscription, error) {
	if !r.anchorAtEffect {
		return swapped, nil
	}

	reset := *swapped
	reset.BillingAnchor = r.updatedSub.BillingAnchor
	reset.CurrentPeriodStart = r.updatedSub.CurrentPeriodStart
	reset.CurrentPeriodEnd = r.updatedSub.CurrentPeriodEnd

	if err := s.SubRepo.Update(ctx, &reset); err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "reset subscription billing anchor on plan change",
		"subscription_id", reset.ID,
		"billing_anchor", reset.BillingAnchor,
		"current_period_start", reset.CurrentPeriodStart,
		"current_period_end", reset.CurrentPeriodEnd,
		"previous_period_start", r.currentSub.CurrentPeriodStart,
		"previous_period_end", r.currentSub.CurrentPeriodEnd)

	return &reset, nil
}

// projectSubscriptionAfterChange is the subscription the change would leave behind,
// computed without writing anything. Under anchor_at_effect the term restarts at
// effectiveAt, so the new period is a full period and never the remainder of the old one.
func projectSubscriptionAfterChange(r *planChangeRequest) (*subscription.Subscription, error) {
	projected := *r.currentSub
	projected.PlanID = r.toPlan.ID

	if !r.anchorAtEffect {
		return &projected, nil
	}

	periodEnd, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart:  r.effectiveAt,
		BillingAnchor:       r.effectiveAt,
		Unit:                projected.BillingPeriodCount,
		Period:              projected.BillingPeriod,
		SubscriptionEndDate: projected.EndDate,
		Timezone:            projected.Timezone,
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to calculate the new billing period end after resetting the anchor").
			WithReportableDetails(map[string]any{
				"subscription_id": projected.ID,
				"effective_at":    r.effectiveAt,
			}).
			Mark(ierr.ErrSystem)
	}

	projected.BillingAnchor = r.effectiveAt
	projected.CurrentPeriodStart = r.effectiveAt
	projected.CurrentPeriodEnd = periodEnd
	return &projected, nil
}

// resetQuote prices the term restart against both snapshots: removals against the period
// the change interrupts (credit for time paid for but no longer covered), additions
// against the period it opens (a full period, because effectiveAt is its start).
func (s *subscriptionService) resetQuote(
	ctx context.Context,
	r *planChangeRequest,
) (*LineItemProrationSummary, error) {
	prorationSvc := NewLineItemProrationService(s.ServiceParams)

	creditEntries := make([]LineItemProrationEntry, 0, len(r.closingLineItems)+len(r.carriedLineItems))
	creditEntries = append(creditEntries, r.closingLineItems...)
	chargeEntries := make([]LineItemProrationEntry, 0, len(r.openingLineItems)+len(r.carriedLineItems))
	chargeEntries = append(chargeEntries, r.openingLineItems...)

	for _, carried := range r.carriedLineItems {
		credit := carried
		credit.Action = types.ProrationActionRemoveItem
		creditEntries = append(creditEntries, credit)

		carried.Action = types.ProrationActionAddItem
		chargeEntries = append(chargeEntries, carried)
	}

	var credit *LineItemProrationSummary
	if r.behavior == types.ProrationBehaviorCreateProrations {
		var err error
		credit, err = prorationSvc.Compute(ctx, LineItemProrationRequest{
			Subscription:  r.currentSub,
			Entries:       creditEntries,
			EffectiveDate: r.effectiveAt,
			Behavior:      types.ProrationBehaviorCreateProrations,
			Reason:        "plan change with billing period reset",
		})
		if err != nil {
			return nil, err
		}
	}

	charge, err := prorationSvc.Compute(ctx, LineItemProrationRequest{
		Subscription:  r.updatedSub,
		Entries:       chargeEntries,
		EffectiveDate: r.effectiveAt,
		Behavior:      types.ProrationBehaviorCreateProrations,
		Reason:        "plan change with billing period reset",
	})
	if err != nil {
		return nil, err
	}

	return emptyProrationSummary(r.currentSub).Merge(credit, charge), nil
}

// outgoingUsageInvoiceRequest bills the usage consumed on the outgoing plan before the
// reset cut its period short. Without it that usage is never invoiced: the period never
// reaches its boundary, and the next roll's window starts at effectiveAt.
func (s *subscriptionService) outgoingUsageInvoiceRequest(
	ctx context.Context,
	r *planChangeRequest,
) (*dto.CreateInvoiceRequest, error) {
	billingSvc := NewBillingService(s.ServiceParams)

	prepared, err := billingSvc.PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   r.currentSub,
		PeriodStart:    r.currentSub.CurrentPeriodStart,
		PeriodEnd:      r.effectiveAt,
		ReferencePoint: types.ReferencePointCancel,
	})
	if err != nil {
		return nil, err
	}

	usageCharges := lo.Filter(prepared.LineItems, func(line dto.CreateInvoiceLineItemRequest, _ int) bool {
		return lo.FromPtr(line.PriceType) == string(types.PRICE_TYPE_USAGE)
	})
	if len(usageCharges) == 0 {
		return nil, nil
	}

	usageChargesTotal := decimal.Zero
	for _, charge := range usageCharges {
		usageChargesTotal = usageChargesTotal.Add(charge.Amount)
	}

	req, err := billingSvc.CreateInvoiceRequestForCharges(ctx, &dto.CreateInvoiceRequestForChargesParams{
		Subscription:  r.currentSub,
		Result:        &dto.BillingCalculationResult{UsageCharges: usageCharges, TotalAmount: usageChargesTotal, Currency: r.currentSub.Currency},
		PeriodStart:   r.currentSub.CurrentPeriodStart,
		PeriodEnd:     r.effectiveAt,
		Description:   "Usage on the previous plan up to the change",
		Metadata:      types.Metadata{},
		InvoiceType:   types.InvoiceTypeOneOff,
		BillingReason: types.InvoiceBillingReasonSubscriptionUpdate,
	})
	if err != nil {
		return nil, err
	}
	if !req.Total.IsPositive() {
		return nil, nil
	}

	req.IdempotencyKey = lo.ToPtr(planChangeIdempotencyKey(r, "outgoing_usage"))
	return req, nil
}

// settlePlanChange raises the settlement resolved for this change: the outgoing plan's
// usage as its own invoice, and the net of credits against charges as either an invoice or
// a wallet credit. With preview set it quotes the same documents without writing, so the
// two cannot describe different outcomes.
func (s *subscriptionService) settlePlanChange(
	ctx context.Context,
	r *planChangeRequest,
	preview bool,
) ([]dto.ChangedInvoice, error) {
	invoiceSvc := NewInvoiceService(s.ServiceParams)
	changed := make([]dto.ChangedInvoice, 0, 2)

	raiseInvoice := func(req dto.CreateInvoiceRequest) error {
		if preview {
			inv, err := invoiceSvc.CreatePreviewInvoice(ctx, req)
			if err != nil {
				return err
			}

			changed = append(changed, dto.ChangedInvoice{
				Action:  dto.ChangedInvoiceActionCreated,
				Status:  dto.ChangedInvoiceStatusPreview,
				Invoice: inv,
			})
			return nil
		}

		inv, err := invoiceSvc.CreateInvoice(ctx, req)
		if err != nil {
			return err
		}

		changed = append(changed, dto.ChangedInvoice{
			ID:      inv.ID,
			Action:  dto.ChangedInvoiceActionCreated,
			Status:  dto.ChangedInvoiceStatusFromPaymentStatus(inv.PaymentStatus),
			Invoice: inv,
		})
		return nil
	}

	if r.outgoingUsage != nil {
		if err := raiseInvoice(*r.outgoingUsage); err != nil {
			return nil, err
		}
	}

	if r.settlementQuote == nil {
		return changed, nil
	}

	settleReq := NewSettleProrationRequest(
		r.currentSub, r.settlementQuote, r.effectiveAt, r.updatedSub.CurrentPeriodEnd,
		planChangeCollapsedInvoiceDisplayName(r), planChangeIdempotencyKey(r, ""),
		lo.Ternary(preview, SettleModePreview, SettleModeIssue),
	)
	settleReq.BillingPeriod = r.updatedSub.BillingPeriod

	settled, err := NewLineItemProrationService(s.ServiceParams).Settle(ctx, settleReq)
	if err != nil {
		return nil, err
	}

	changed = append(changed, settled.Changed...)

	return changed, nil
}

func planChangeCollapsedInvoiceDisplayName(r *planChangeRequest) string {
	from, to := "Plan", "Plan"
	if r.fromPlan != nil && strings.TrimSpace(r.fromPlan.Name) != "" {
		from = strings.TrimSpace(r.fromPlan.Name)
	}
	if r.toPlan != nil && strings.TrimSpace(r.toPlan.Name) != "" {
		to = strings.TrimSpace(r.toPlan.Name)
	}
	switch r.changeType {
	case types.SubscriptionChangeTypeUpgrade:
		return fmt.Sprintf("Upgrade: %s → %s", from, to)
	case types.SubscriptionChangeTypeDowngrade:
		return fmt.Sprintf("Downgrade: %s → %s", from, to)
	default:
		return fmt.Sprintf("Plan change: %s → %s", from, to)
	}
}

func buildPlanChangeResponse(
	r *planChangeRequest,
	sub *subscription.Subscription,
	req dto.SubscriptionChangeV2Request,
) *dto.SubscriptionChangeV2Response {
	return &dto.SubscriptionChangeV2Response{
		Subscription: &dto.SubscriptionResponse{Subscription: sub},
		ChangeType:   r.changeType,
		EffectiveAt:  r.effectiveAt,
		FromPlan:     planChangeSummary(r.fromPlan),
		ToPlan:       planChangeSummary(r.toPlan),
		BillingPeriod: dto.SubscriptionChangeBillingPeriodResult{
			Behaviour:          req.EffectiveBillingPeriodBehaviour(),
			BillingAnchor:      sub.BillingAnchor,
			CurrentPeriodStart: sub.CurrentPeriodStart,
			CurrentPeriodEnd:   sub.CurrentPeriodEnd,
		},
		EntityChanges: r.changes,
		Warnings:      r.warnings,
		Metadata:      req.Metadata,
	}
}

func planChangeSummary(p *plan.Plan) dto.PlanSummary {
	if p == nil {
		return dto.PlanSummary{}
	}
	return dto.PlanSummary{
		ID:          p.ID,
		Name:        p.Name,
		LookupKey:   p.LookupKey,
		Description: p.Description,
	}
}

// A wallet credit and a charge invoice are the two shapes of one settlement, so
// both are reported through ChangedInvoice (Action tells them apart).
func walletCreditChangedInvoice(txn *dto.WalletTransactionResponse, status dto.ChangedInvoiceStatus) dto.ChangedInvoice {
	credit := dto.ChangedInvoice{
		Action:            dto.ChangedInvoiceActionWalletCredit,
		Status:            status,
		WalletTransaction: txn,
	}

	if txn != nil && txn.Transaction != nil {
		credit.ID = txn.ID
	}

	return credit
}

func planChangeChangedLineItems(r *planChangeRequest) []dto.ChangedLineItem {
	items := make([]dto.ChangedLineItem, 0, len(r.carriedLineItems)+len(r.closingLineItems)+len(r.openingLineItems))
	for _, move := range r.carriedLineItems {
		items = append(items, dto.ChangedLineItem{
			ID:           move.LineItem.ID,
			PriceID:      move.NewPrice.ID,
			Quantity:     move.LineItem.Quantity,
			StartDate:    &move.LineItem.StartDate,
			ChangeAction: dto.ChangedLineItemActionUpdated,
		})
	}

	for _, move := range r.closingLineItems {
		endDate := r.effectiveAt
		items = append(items, dto.ChangedLineItem{
			ID:           move.LineItem.ID,
			PriceID:      move.LineItem.PriceID,
			Quantity:     move.LineItem.Quantity,
			StartDate:    &move.LineItem.StartDate,
			EndDate:      &endDate,
			ChangeAction: dto.ChangedLineItemActionEnded,
		})
	}

	for _, move := range r.openingLineItems {
		startDate := move.LineItem.StartDate
		items = append(items, dto.ChangedLineItem{
			ID:           move.LineItem.ID,
			PriceID:      move.LineItem.PriceID,
			Quantity:     move.LineItem.Quantity,
			StartDate:    &startDate,
			ChangeAction: dto.ChangedLineItemActionCreated,
		})
	}
	return items
}

// The key is what stops a retry double-billing: CreateInvoice returns an existing draft
// under the same key and rejects an existing finalized one, so two documents sharing a key
// would make the second fail as already-existing.
func planChangeIdempotencyKey(r *planChangeRequest, document string) string {
	inputs := map[string]interface{}{
		"subscription_id": r.currentSub.ID,
		"target_plan_id":  r.toPlan.ID,
	}

	if document != "" {
		inputs["document"] = document
	}

	if r.idempotencyKey != "" {
		inputs["client_key"] = r.idempotencyKey
	} else {
		inputs["subscription_version"] = r.currentSub.Version
		inputs["subscription_updated_at"] = r.currentSub.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}

	return idempotency.NewGenerator().GenerateKey(idempotency.ScopePlanChange, inputs)
}

func PlanChangeV2RequestFromConfig(config *subscription.PlanChangeV2Configuration) dto.SubscriptionChangeV2Request {
	req := dto.SubscriptionChangeV2Request{
		TargetPlanID: config.TargetPlanID,
		// Pinned, not replayed from the blob: a deferred change is only ever
		// accepted with none, and the boundary invoice already bills the new plan.
		ProrationBehavior:      types.ProrationBehaviorNone,
		IdempotencyKey:         config.IdempotencyKey,
		Metadata:               config.ChangeMetadata,
		BillingPeriodBehaviour: config.BillingPeriodBehaviour,
		ChangeAt:               lo.ToPtr(types.ScheduleTypePeriodEnd),
	}

	if config.EntityPolicies != nil && config.EntityPolicies.Addons != nil {
		req.EntityPolicies = &dto.SubscriptionChangeEntityPolicies{
			Addons: &dto.EntityChangePolicy{
				DefaultBehaviour: config.EntityPolicies.Addons.DefaultBehaviour,
				Overrides:        config.EntityPolicies.Addons.Overrides,
			},
		}
	}

	return req
}

func IsTerminalPlanChangeError(err error) bool {
	return ierr.IsValidation(err) || ierr.IsNotFound(err)
}

func (s *subscriptionService) ExecuteScheduledPlanChangeV2(
	ctx context.Context,
	schedule *subscription.SubscriptionSchedule,
	config *subscription.PlanChangeV2Configuration,
	sub *subscription.Subscription,
) error {
	if schedule.IsStaleFor(sub) {
		err := ierr.NewError("scheduled plan change missed its billing period boundary").
			WithHint("The boundary has already been invoiced. Schedule the change again to apply it at the next period end.").
			WithReportableDetails(map[string]any{
				"schedule_id":          schedule.ID,
				"subscription_id":      schedule.SubscriptionID,
				"scheduled_at":         schedule.ScheduledAt,
				"current_period_start": sub.CurrentPeriodStart,
			}).
			Mark(ierr.ErrValidation)

		s.failPlanChangeSchedule(ctx, schedule, err)
		return err
	}

	resp, err := s.executePlanChangeAt(
		ctx, schedule.SubscriptionID, PlanChangeV2RequestFromConfig(config), schedule.ScheduledAt, schedule.ID,
	)
	if err != nil {
		if !IsTerminalPlanChangeError(err) {
			s.Logger.Error(ctx, "scheduled plan change failed, leaving schedule pending for retry",
				"error", err,
				"schedule_id", schedule.ID,
				"subscription_id", schedule.SubscriptionID)
			return err
		}

		s.failPlanChangeSchedule(ctx, schedule, err)
		return err
	}

	schedule.Status = types.ScheduleStatusExecuted
	schedule.ExecutedAt = lo.ToPtr(time.Now().UTC())
	if err := schedule.SetPlanChangeV2Result(&subscription.PlanChangeV2Result{
		SubscriptionID: schedule.SubscriptionID,
		FromPlanID:     resp.FromPlan.ID,
		ToPlanID:       resp.ToPlan.ID,
		ChangeType:     string(resp.ChangeType),
		EffectiveDate:  resp.EffectiveAt,
	}); err != nil {
		s.Logger.Error(ctx, "failed to set scheduled plan change result",
			"error", err, "schedule_id", schedule.ID)
	}

	if err := s.SubScheduleRepo.Update(ctx, schedule); err != nil {
		s.Logger.Error(ctx, "failed to update schedule after plan change",
			"error", err, "schedule_id", schedule.ID)
		return err
	}

	return nil
}

func (s *subscriptionService) failPlanChangeSchedule(
	ctx context.Context,
	schedule *subscription.SubscriptionSchedule,
	cause error,
) {
	schedule.Status = types.ScheduleStatusFailed
	schedule.ExecutedAt = lo.ToPtr(time.Now().UTC())
	schedule.ErrorMessage = lo.ToPtr(cause.Error())

	if err := s.SubScheduleRepo.Update(ctx, schedule); err != nil {
		s.Logger.Error(ctx, "failed to mark scheduled plan change as failed",
			"error", err,
			"schedule_id", schedule.ID,
			"subscription_id", schedule.SubscriptionID,
			"original_error", cause)
	}
}

func (s *subscriptionService) conflictingPlanChangeSchedule(
	ctx context.Context,
	subscriptionID string,
	executingScheduleID string,
) (*subscription.SubscriptionSchedule, error) {
	existing, err := s.SubScheduleRepo.GetPendingBySubscriptionAndType(
		ctx, subscriptionID, types.SubscriptionScheduleChangeTypePlanChange,
	)
	if err != nil {
		return nil, err
	}
	if existing == nil || existing.ID == executingScheduleID {
		return nil, nil
	}

	return existing, nil
}

// planChangeScheduleToSupersede returns the pending plan-change schedule this request has to
// clear, or the conflict error when the policy does not allow clearing it.
func (s *subscriptionService) planChangeScheduleToSupersede(
	ctx context.Context,
	subscriptionID string,
	req dto.SubscriptionChangeV2Request,
	executingScheduleID string,
) (*subscription.SubscriptionSchedule, error) {
	existing, err := s.conflictingPlanChangeSchedule(ctx, subscriptionID, executingScheduleID)
	if err != nil || existing == nil {
		return nil, err
	}

	if !req.SupersedesPendingSchedule() {
		return nil, ierr.NewError("a plan change is already scheduled for this subscription").
			WithHint("Cancel the existing schedule first, or set on_conflict_policies.on_pending_schedule to 'supersede' to replace it with this request.").
			WithReportableDetails(map[string]any{
				"subscription_id": subscriptionID,
				"schedule_id":     existing.ID,
				"scheduled_at":    existing.ScheduledAt,
				"policy_field":    "on_conflict_policies.on_pending_schedule",
				"policy_value":    types.OnPendingSchedulePolicySupersede.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	return existing, nil
}

func (s *subscriptionService) resolvePendingPlanChangeSchedule(
	ctx context.Context,
	subscriptionID string,
	req dto.SubscriptionChangeV2Request,
	executingScheduleID string,
) ([]string, error) {
	existing, err := s.planChangeScheduleToSupersede(ctx, subscriptionID, req, executingScheduleID)
	if err != nil || existing == nil {
		return nil, err
	}

	superseded := subscription.NewSubscriptionScheduleBuilder(existing).
		WithStatus(types.ScheduleStatusCancelled).
		WithCancelledAt(time.Now().UTC()).
		WithUpdatedBy(types.GetUserID(ctx)).
		Build()

	if err := s.SubScheduleRepo.Update(ctx, superseded); err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "cancelled pending plan change superseded by a newer request",
		"subscription_id", subscriptionID,
		"schedule_id", superseded.ID,
		"scheduled_at", superseded.ScheduledAt,
		"target_plan_id", req.TargetPlanID)

	return []string{superseded.ID}, nil
}

// resolveEffectiveAt is the instant a request would take effect: the period boundary
// when deferred, otherwise now. Preview uses it so the quote is priced at the same
// instant the change will actually be applied.
func resolveEffectiveAt(sub *subscription.Subscription, req dto.SubscriptionChangeV2Request) time.Time {
	if req.IsDeferred() {
		return sub.CurrentPeriodEnd
	}

	return time.Now().UTC()
}

// schedulePlanChangeForPeriodEnd records a deferred change.
func (s *subscriptionService) schedulePlanChangeForPeriodEnd(
	ctx context.Context,
	subscriptionID string,
	req dto.SubscriptionChangeV2Request,
) (*dto.SubscriptionChangeV2Response, error) {
	var (
		resp       *dto.SubscriptionChangeV2Response
		scheduleID string
		changeType types.SubscriptionChangeType
	)

	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		sub, err := s.loadSubscriptionForChange(txCtx, subscriptionID, true)
		if err != nil {
			return err
		}

		superseded, err := s.resolvePendingPlanChangeSchedule(txCtx, subscriptionID, req, "")
		if err != nil {
			return err
		}

		scheduledAt := sub.CurrentPeriodEnd
		r, err := s.resolvePlanChange(txCtx, sub, req, scheduledAt)
		if err != nil {
			return err
		}

		config := &subscription.PlanChangeV2Configuration{
			TargetPlanID:           req.TargetPlanID,
			IdempotencyKey:         req.IdempotencyKey,
			ChangeMetadata:         req.Metadata,
			BillingPeriodBehaviour: req.BillingPeriodBehaviour,
		}
		if req.EntityPolicies != nil && req.EntityPolicies.Addons != nil {
			config.EntityPolicies = &subscription.EntityChangePoliciesConfig{
				Addons: &subscription.EntityChangePolicyConfig{
					DefaultBehaviour: req.EntityPolicies.Addons.DefaultBehaviour,
					Overrides:        req.EntityPolicies.Addons.Overrides,
				},
			}
		}

		schedule := subscription.NewPendingScheduleBuilder(
			txCtx, sub, types.SubscriptionScheduleChangeTypePlanChange, scheduledAt,
		).Build()
		if err := schedule.SetPlanChangeV2Config(config); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to serialize the plan change configuration").
				Mark(ierr.ErrInternal)
		}

		if err := s.SubScheduleRepo.Create(txCtx, schedule); err != nil {
			if ierr.IsAlreadyExists(err) {
				return ierr.WithError(err).
					WithHint("A plan change is already scheduled for this subscription. Cancel it before requesting another one.").
					WithReportableDetails(map[string]any{"subscription_id": subscriptionID}).
					Mark(ierr.ErrValidation)
			}
			return err
		}

		scheduleID = schedule.ID
		changeType = r.changeType

		resp = buildPlanChangeResponse(r, sub, req)
		resp.ChangedResources.LineItems = planChangeChangedLineItems(r)
		resp.IsScheduled = true
		resp.ScheduleID = &schedule.ID
		resp.ScheduledAt = &schedule.ScheduledAt
		resp.SupersededSchedules = superseded

		return nil
	})
	if err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "plan change scheduled for period end",
		"subscription_id", subscriptionID,
		"schedule_id", scheduleID,
		"scheduled_at", *resp.ScheduledAt,
		"target_plan_id", req.TargetPlanID,
		"change_type", changeType)

	return resp, nil
}
