package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type lineItemChange struct {
	lineItem *subscription.SubscriptionLineItem
	price    *price.Price

	// Set only for dropped addon lines (needed to close the association).
	association *addonassociation.AddonAssociation
}

func (c lineItemChange) isAddon() bool {
	return c.lineItem != nil &&
		c.lineItem.EntityType == types.SubscriptionLineItemEntityTypeAddon &&
		c.association != nil
}

type planChangeRequest struct {
	sub            *subscription.Subscription
	fromPlan       *plan.Plan
	toPlan         *plan.Plan
	effectiveAt    time.Time
	behavior       types.ProrationBehavior
	idempotencyKey string

	carried []*lineItemChange
	closing []*lineItemChange
	opening []*lineItemChange

	grants         *planChangeGrants
	staleOverrides []*entitlement.Entitlement

	changes  []*dto.EntityChangeResult
	warnings []string

	changeType types.SubscriptionChangeType
}

// planChangeGrants is the credit-grant migration a plan change performs. Plan-level
// grants are materialised per subscription at creation time.
type planChangeGrants struct {
	// cancel holds the subscription's grants materialised from the outgoing plan.
	cancel []*creditgrant.CreditGrant

	// create holds the target plan's grants mapped to subscription-scoped requests.
	create []dto.CreateCreditGrantRequest

	// createdFrom keeps the target plan grant behind each create entry, so preview can
	// name what it would materialise before any row exists.
	createdFrom []*creditgrant.CreditGrant
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

	targetPrices, err := s.ValidateAndFilterPricesForSubscription(
		ctx, toPlan.Plan.ID, types.PRICE_ENTITY_TYPE_PLAN, sub, nil,
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
		sub:            sub,
		fromPlan:       fromPlan.Plan,
		toPlan:         toPlan.Plan,
		effectiveAt:    effectiveAt,
		behavior:       req.ProrationBehavior,
		idempotencyKey: lo.FromPtr(req.IdempotencyKey),
	}

	carried, opening, closing, err := s.resolveLineItems(ctx, sub, targetPrices, toPlan, effectiveAt)
	if err != nil {
		return nil, err
	}

	addonsClosing, addonsChanges, addonsWarnings, err := s.resolveAddonChanges(ctx, sub, req)
	if err != nil {
		return nil, err
	}

	r.carried = carried
	r.opening = opening
	r.closing = append(closing, addonsClosing...)
	r.changes = addonsChanges
	r.warnings = addonsWarnings
	r.changeType = planChangeType(r)

	if err := s.resolveCreditGrantMigration(ctx, r); err != nil {
		return nil, err
	}

	if err := s.resolveStaleEntitlementOverrides(ctx, r); err != nil {
		return nil, err
	}

	return r, nil
}

// resolveCreditGrantMigration works out which grants leave with the old plan and which
// arrive with the new one. Preview and execute both call it; only execute applies it.
func (s *subscriptionService) resolveCreditGrantMigration(ctx context.Context, r *planChangeRequest) error {
	filter := types.NewNoLimitCreditGrantFilter()
	filter.SubscriptionIDs = []string{r.sub.ID}
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

	grants := &planChangeGrants{}
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
		grants.create = append(grants.create, PlanCreditGrantRequest(cg, r.sub.ID, r.toPlan.ID))
		grants.createdFrom = append(grants.createdFrom, cg.CreditGrant)
	}
	r.grants = grants

	for _, cg := range grants.cancel {
		r.changes = append(r.changes, &dto.EntityChangeResult{
			EntityType:  types.SubscriptionChangeEntityTypeCreditGrant,
			ReferenceID: cg.ID,
			EntityID:    r.fromPlan.ID,
			Behaviour:   types.EntityChangeBehaviourDrop,
		})
	}

	for _, cg := range grants.createdFrom {
		r.changes = append(r.changes, &dto.EntityChangeResult{
			EntityType:  types.SubscriptionChangeEntityTypeCreditGrant,
			ReferenceID: cg.ID,
			EntityID:    r.toPlan.ID,
			Behaviour:   types.EntityChangeBehaviourAdd,
		})
	}

	return nil
}

// resolveStaleEntitlementOverrides finds subscription-scoped entitlements whose parent is
// an entitlement of the outgoing plan. After the swap they suppress nothing and stack with
// the new plan's entitlement on the same feature, so they must be closed.
func (s *subscriptionService) resolveStaleEntitlementOverrides(ctx context.Context, r *planChangeRequest) error {
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
		WithEntityIDs([]string{r.sub.ID}).
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

		r.staleOverrides = append(r.staleOverrides, ent.Entitlement)
		r.changes = append(r.changes, &dto.EntityChangeResult{
			EntityType:  types.SubscriptionChangeEntityTypeEntitlement,
			ReferenceID: ent.ID,
			EntityID:    ent.FeatureID,
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
	if r.grants == nil {
		return nil
	}

	if len(r.grants.cancel) > 0 {
		if err := NewCreditGrantService(s.ServiceParams).CancelFutureSubscriptionGrants(ctx, dto.CancelFutureSubscriptionGrantsRequest{
			SubscriptionID: r.sub.ID,
			PlanID:         lo.ToPtr(r.fromPlan.ID),
			EffectiveDate:  lo.ToPtr(r.effectiveAt),
		}); err != nil {
			return err
		}
	}

	if err := s.handleCreditGrantsWithStart(ctx, r.sub, r.grants.create, r.effectiveAt, nil, nil); err != nil {
		return err
	}

	s.Logger.Info(ctx, "migrated credit grants for plan change",
		"subscription_id", r.sub.ID,
		"from_plan_id", r.fromPlan.ID,
		"to_plan_id", r.toPlan.ID,
		"cancelled_grants", len(r.grants.cancel),
		"created_grants", len(r.grants.create),
		"effective_at", r.effectiveAt)

	return nil
}

// closeStaleEntitlementOverrides end-dates the overrides resolved above at the change
// instant.
func (s *subscriptionService) closeStaleEntitlementOverrides(ctx context.Context, r *planChangeRequest) error {
	for _, ent := range r.staleOverrides {
		closed := entitlement.NewEntitlementBuilder(ent).
			WithEndDate(r.effectiveAt).
			WithUpdatedBy(types.GetUserID(ctx)).
			Build()

		if _, err := s.EntitlementRepo.Update(ctx, closed); err != nil {
			return err
		}

		s.Logger.Info(ctx, "closed stale subscription entitlement override on plan change",
			"subscription_id", r.sub.ID,
			"entitlement_id", closed.ID,
			"feature_id", closed.FeatureID,
			"from_plan_id", r.fromPlan.ID,
			"effective_at", r.effectiveAt)
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
) ([]*lineItemChange, []*lineItemChange, []*lineItemChange, error) {
	priceSvc := NewPriceService(s.ServiceParams)
	var carried, opening, closing []*lineItemChange

	current := make([]*lineItemChange, 0, len(sub.LineItems))
	for _, item := range sub.LineItems {
		if item.EntityType != types.SubscriptionLineItemEntityTypePlan || !item.EndDate.IsZero() {
			continue
		}
		p, err := priceSvc.GetPrice(ctx, item.PriceID)
		if err != nil {
			return nil, nil, nil, err
		}
		current = append(current, &lineItemChange{lineItem: item, price: p.Price})
	}

	matchedTarget := make([]bool, len(targetPrices))
	matchedCurrent := make([]bool, len(current))

	for i, live := range current {
		for j, target := range targetPrices {
			if matchedTarget[j] || !live.price.BillsIdenticallyTo(target.Price) {
				continue
			}

			matchedTarget[j], matchedCurrent[i] = true, true
			carried = append(carried, &lineItemChange{lineItem: live.lineItem, price: target.Price})
			break
		}
	}

	for i, live := range current {
		if !matchedCurrent[i] {
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
		opening = append(opening, &lineItemChange{lineItem: item, price: target.Price})
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
	for _, move := range r.closing {
		if move.isAddon() {
			continue
		}
		oldTotal = oldTotal.Add(move.price.Amount.Mul(move.lineItem.Quantity))
	}
	for _, move := range r.opening {
		newTotal = newTotal.Add(move.price.Amount.Mul(move.lineItem.Quantity))
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
) ([]*lineItemChange, []*dto.EntityChangeResult, []string, error) {
	var changes []*dto.EntityChangeResult
	var closing []*lineItemChange
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
			EntityType:  types.SubscriptionChangeEntityTypeAddon,
			ReferenceID: association.ID,
			EntityID:    association.AddonID,
			Behaviour:   behaviour,
		})

		if behaviour != types.EntityChangeBehaviourDrop {
			continue
		}

		dropped, err := s.addonLineItemsToClose(ctx, sub.ID, association)
		if err != nil {
			return nil, nil, nil, err
		}
		closing = append(closing, dropped...)
	}

	// Stale override keys (e.g. preview→execute race) warn instead of failing.
	if policy != nil {
		for referenceID := range policy.Overrides {
			if !seen[referenceID] {
				warnings = append(warnings,
					"ignored addon behaviour for unknown or inactive association "+referenceID)
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
) ([]*lineItemChange, error) {
	filter := types.NewSubscriptionLineItemFilter()
	filter.SubscriptionIDs = []string{subscriptionID}
	filter.AddonAssociationIDs = []string{association.ID}

	items, err := s.SubscriptionLineItemRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	priceSvc := NewPriceService(s.ServiceParams)
	closing := make([]*lineItemChange, 0, len(items))
	for _, item := range items {
		if !item.EndDate.IsZero() {
			continue
		}
		p, err := priceSvc.GetPrice(ctx, item.PriceID)
		if err != nil {
			return nil, err
		}
		closing = append(closing, &lineItemChange{
			lineItem:    item,
			price:       p.Price,
			association: association,
		})
	}
	return closing, nil
}

func (s *subscriptionService) applyDroppedAddons(ctx context.Context, r *planChangeRequest) error {
	closed := make(map[string]bool)
	for _, change := range r.closing {
		if !change.isAddon() || closed[change.association.ID] {
			continue
		}
		closed[change.association.ID] = true

		association := addonassociation.NewAddonAssociationBuilder(change.association).
			WithCancellation(r.effectiveAt, "plan change").
			Build()
		if err := s.AddonAssociationRepo.Update(ctx, association); err != nil {
			return err
		}

		// KNOWN LIMITATION: grants are tagged by addon_id only, so concurrent
		// attachments of the same addon cannot be cancelled independently.
		if err := NewCreditGrantService(s.ServiceParams).CancelFutureSubscriptionGrants(ctx, dto.CancelFutureSubscriptionGrantsRequest{
			SubscriptionID: r.sub.ID,
			AddonID:        lo.ToPtr(association.AddonID),
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
	entries := make([]LineItemProrationEntry, 0, len(r.closing)+len(r.opening))
	for _, move := range r.closing {
		entries = append(entries, LineItemProrationEntry{
			LineItem: move.lineItem,
			Price:    move.price,
			Action:   types.ProrationActionRemoveItem,
		})
	}
	for _, move := range r.opening {
		entries = append(entries, LineItemProrationEntry{
			LineItem:    move.lineItem,
			Price:       move.price,
			Action:      types.ProrationActionAddItem,
			NewQuantity: move.lineItem.Quantity,
		})
	}

	return NewLineItemProrationService(s.ServiceParams).Compute(ctx, LineItemProrationRequest{
		Subscription:  r.sub,
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
	sub, err := s.loadSubscriptionForPlanChange(ctx, subscriptionID, false)
	if err != nil {
		return nil, err
	}

	r, err := s.resolvePlanChange(ctx, sub, req, resolveEffectiveAt(sub, req))
	if err != nil {
		return nil, err
	}

	quote, err := s.computePlanChange(ctx, r)
	if err != nil {
		return nil, err
	}

	preview, err := s.previewPlanChangeSettlement(ctx, r, quote)
	if err != nil {
		return nil, err
	}

	superseded, err := s.previewPendingScheduleSupersede(ctx, subscriptionID, req)
	if err != nil {
		return nil, err
	}

	resp := buildPlanChangeResponse(r, r.sub, req)
	resp.ChangedResources.LineItems = planChangeChangedLineItems(r)
	resp.ChangedResources.Invoices = preview
	resp.SupersededSchedules = superseded
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
		sub, err := s.loadSubscriptionForPlanChange(txCtx, subscriptionID, true)
		if err != nil {
			return err
		}

		r, err := s.resolvePlanChange(txCtx, sub, req, effectiveAt)
		if err != nil {
			return err
		}

		quote, err := s.computePlanChange(txCtx, r)
		if err != nil {
			return err
		}

		superseded, err := s.resolvePendingPlanChangeSchedule(txCtx, subscriptionID, req, executingScheduleID)
		if err != nil {
			return err
		}

		if err := s.applyPlanChangeLineItems(txCtx, r); err != nil {
			return err
		}

		if err := s.applyDroppedAddons(txCtx, r); err != nil {
			return err
		}

		swapped, err := s.applyPlanSwap(txCtx, r)
		if err != nil {
			return err
		}

		if err := s.migrateCreditGrants(txCtx, r); err != nil {
			return err
		}

		if err := s.closeStaleEntitlementOverrides(txCtx, r); err != nil {
			return err
		}

		changedInvoices, err := s.settlePlanChange(txCtx, r, quote)
		if err != nil {
			return err
		}

		resp = buildPlanChangeResponse(r, swapped, req)
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
func (s *subscriptionService) loadSubscriptionForPlanChange(
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
	for _, move := range r.carried {
		updated := subscription.NewSubscriptionLineItemBuilder(move.lineItem).
			WithPlan(r.toPlan.ID, r.toPlan.Name).
			WithPrice(move.price).
			Build()

		if err := s.SubscriptionLineItemRepo.Update(ctx, updated); err != nil {
			return err
		}
	}

	for _, move := range r.closing {
		ended := subscription.NewSubscriptionLineItemBuilder(move.lineItem).
			WithEndDate(r.effectiveAt).
			Build()

		if err := s.SubscriptionLineItemRepo.Update(ctx, ended); err != nil {
			return err
		}
	}

	if len(r.opening) == 0 {
		return nil
	}
	toCreate := make([]*subscription.SubscriptionLineItem, 0, len(r.opening))
	for _, move := range r.opening {
		toCreate = append(toCreate, move.lineItem)
	}
	return s.SubscriptionLineItemRepo.CreateBulk(ctx, toCreate)
}

func (s *subscriptionService) applyPlanSwap(
	ctx context.Context,
	r *planChangeRequest,
) (*subscription.Subscription, error) {
	if err := s.SubRepo.UpdatePlan(ctx, r.sub.ID, r.toPlan.ID); err != nil {
		return nil, err
	}

	targetSeq, err := s.PlanPriceSyncRepo.CurrentPlanSequence(ctx, r.toPlan.ID)
	if err != nil {
		return nil, err
	}

	if err := s.PlanPriceSyncRepo.ReanchorSubSyncedSequence(ctx, r.sub.ID, targetSeq); err != nil {
		return nil, err
	}

	swapped := *r.sub
	swapped.PlanID = r.toPlan.ID
	swapped.SyncedPriceSequence = targetSeq
	return &swapped, nil
}

// Net charge → one invoice (credits as lines); net credit → wallet (invoice totals can't go negative).
func (s *subscriptionService) settlePlanChange(
	ctx context.Context,
	r *planChangeRequest,
	quote *LineItemProrationSummary,
) ([]dto.ChangedInvoice, error) {
	if r.behavior != types.ProrationBehaviorCreateProrations {
		return nil, nil
	}

	switch {
	case quote.NetAmount().GreaterThan(decimal.Zero):
		inv, err := NewInvoiceService(s.ServiceParams).CreateInvoice(ctx, planChangeInvoiceRequest(r, quote))
		if err != nil {
			return nil, err
		}

		return []dto.ChangedInvoice{{
			ID:      inv.ID,
			Action:  dto.ChangedInvoiceActionCreated,
			Status:  dto.ChangedInvoiceStatusFromPaymentStatus(inv.PaymentStatus),
			Invoice: inv,
		}}, nil

	case quote.NetAmount().LessThan(decimal.Zero):
		walletSvc := NewWalletService(s.ServiceParams)
		txn, err := walletSvc.TopUpWalletForProratedCharge(
			ctx, r.sub.GetInvoicingCustomerID(), quote.NetAmount().Abs(), r.sub.Currency, planChangeIdempotencyKey(r),
		)
		if err != nil {
			return nil, err
		}

		return []dto.ChangedInvoice{walletCreditChangedInvoice(txn, dto.ChangedInvoiceStatusWalletIssued)}, nil
	default:
		return nil, nil
	}
}

// One request, two uses: CreateInvoice raises it on execute, CreatePreviewInvoice
// quotes it on preview. Anything the quote must reflect belongs here.
func planChangeInvoiceRequest(r *planChangeRequest, quote *LineItemProrationSummary) dto.CreateInvoiceRequest {
	lineItems := make([]dto.CreateInvoiceLineItemRequest, 0,
		len(quote.ChargeLineItems)+len(quote.CreditLineItems))
	lineItems = append(lineItems, quote.ChargeLineItems...)
	lineItems = append(lineItems, quote.CreditLineItems...)

	billingPeriod := string(r.sub.BillingPeriod)
	periodEnd := r.sub.CurrentPeriodEnd
	idempotencyKey := planChangeIdempotencyKey(r)

	return dto.CreateInvoiceRequest{
		CustomerID:     r.sub.GetInvoicingCustomerID(),
		SubscriptionID: &r.sub.ID,
		InvoiceType:    types.InvoiceTypeOneOff,
		Currency:       r.sub.Currency,
		BillingReason:  types.InvoiceBillingReasonSubscriptionUpdate,
		AmountDue:      quote.NetAmount(),
		Total:          quote.NetAmount(),
		Subtotal:       quote.NetAmount(),
		PeriodStart:    &r.effectiveAt,
		PeriodEnd:      &periodEnd,
		BillingPeriod:  &billingPeriod,
		LineItems:      lineItems,
		IdempotencyKey: &idempotencyKey,
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
	items := make([]dto.ChangedLineItem, 0, len(r.carried)+len(r.closing)+len(r.opening))
	for _, move := range r.carried {
		items = append(items, dto.ChangedLineItem{
			ID:           move.lineItem.ID,
			PriceID:      move.price.ID,
			Quantity:     move.lineItem.Quantity,
			StartDate:    &move.lineItem.StartDate,
			ChangeAction: dto.ChangedLineItemActionUpdated,
		})
	}

	for _, move := range r.closing {
		endDate := r.effectiveAt
		items = append(items, dto.ChangedLineItem{
			ID:           move.lineItem.ID,
			PriceID:      move.lineItem.PriceID,
			Quantity:     move.lineItem.Quantity,
			StartDate:    &move.lineItem.StartDate,
			EndDate:      &endDate,
			ChangeAction: dto.ChangedLineItemActionEnded,
		})
	}

	for _, move := range r.opening {
		startDate := move.lineItem.StartDate
		items = append(items, dto.ChangedLineItem{
			ID:           move.lineItem.ID,
			PriceID:      move.lineItem.PriceID,
			Quantity:     move.lineItem.Quantity,
			StartDate:    &startDate,
			ChangeAction: dto.ChangedLineItemActionCreated,
		})
	}
	return items
}

// previewPlanChangeSettlement quotes what settlePlanChange would do, through the
// same request and the same two branches, so preview and execute cannot drift.
func (s *subscriptionService) previewPlanChangeSettlement(
	ctx context.Context,
	r *planChangeRequest,
	quote *LineItemProrationSummary,
) ([]dto.ChangedInvoice, error) {
	if r.behavior != types.ProrationBehaviorCreateProrations || quote.NetAmount().IsZero() {
		return nil, nil
	}

	// A net credit is paid to the wallet, never invoiced.
	if quote.NetAmount().IsNegative() {
		return []dto.ChangedInvoice{walletCreditChangedInvoice(&dto.WalletTransactionResponse{
			Transaction: &wallet.Transaction{
				CustomerID:        r.sub.GetInvoicingCustomerID(),
				Amount:            quote.NetAmount().Abs(),
				Currency:          r.sub.Currency,
				TransactionReason: types.TransactionReasonSubscriptionCredit,
			},
		}, dto.ChangedInvoiceStatusPreview)}, nil
	}

	inv, err := NewInvoiceService(s.ServiceParams).CreatePreviewInvoice(ctx, planChangeInvoiceRequest(r, quote))
	if err != nil {
		return nil, err
	}

	return []dto.ChangedInvoice{{
		Action:  dto.ChangedInvoiceActionCreated,
		Status:  dto.ChangedInvoiceStatusPreview,
		Invoice: inv,
	}}, nil
}

func planChangeIdempotencyKey(r *planChangeRequest) string {
	inputs := map[string]interface{}{
		"subscription_id": r.sub.ID,
		"target_plan_id":  r.toPlan.ID,
	}

	if r.idempotencyKey != "" {
		inputs["client_key"] = r.idempotencyKey
	} else {
		inputs["subscription_version"] = r.sub.Version
		inputs["subscription_updated_at"] = r.sub.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}

	return idempotency.NewGenerator().GenerateKey(idempotency.ScopePlanChange, inputs)
}

func PlanChangeV2RequestFromConfig(config *subscription.PlanChangeV2Configuration) dto.SubscriptionChangeV2Request {
	req := dto.SubscriptionChangeV2Request{
		TargetPlanID: config.TargetPlanID,
		// Pinned, not replayed from the blob: a deferred change is only ever
		// accepted with none, and the boundary invoice already bills the new plan.
		ProrationBehavior: types.ProrationBehaviorNone,
		IdempotencyKey:    config.IdempotencyKey,
		Metadata:          config.ChangeMetadata,
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

func (s *subscriptionService) resolvePendingPlanChangeSchedule(
	ctx context.Context,
	subscriptionID string,
	req dto.SubscriptionChangeV2Request,
	executingScheduleID string,
) ([]string, error) {
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

// previewPendingScheduleSupersede reports what execute would cancel, without writing.
func (s *subscriptionService) previewPendingScheduleSupersede(
	ctx context.Context,
	subscriptionID string,
	req dto.SubscriptionChangeV2Request,
) ([]string, error) {
	if !req.SupersedesPendingSchedule() {
		return nil, nil
	}

	existing, err := s.conflictingPlanChangeSchedule(ctx, subscriptionID, "")
	if err != nil || existing == nil {
		return nil, err
	}

	return []string{existing.ID}, nil
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
		sub, err := s.loadSubscriptionForPlanChange(txCtx, subscriptionID, true)
		if err != nil {
			return err
		}

		scheduledAt := sub.CurrentPeriodEnd
		r, err := s.resolvePlanChange(txCtx, sub, req, scheduledAt)
		if err != nil {
			return err
		}

		superseded, err := s.resolvePendingPlanChangeSchedule(txCtx, subscriptionID, req, "")
		if err != nil {
			return err
		}

		config := &subscription.PlanChangeV2Configuration{
			TargetPlanID:   req.TargetPlanID,
			IdempotencyKey: req.IdempotencyKey,
			ChangeMetadata: req.Metadata,
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
