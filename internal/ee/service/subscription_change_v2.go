package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
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

	changes  []*dto.EntityChangeResult
	warnings []string

	changeType types.SubscriptionChangeType
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
	return r, nil
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
			EntityType:  types.SubscriptionLineItemEntityTypeAddon,
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

	r, err := s.resolvePlanChange(ctx, sub, req, time.Now().UTC())
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

	resp := buildPlanChangeResponse(r, r.sub, req)
	resp.ChangedResources.LineItems = planChangeChangedLineItems(r)
	resp.ChangedResources.Invoices = preview
	return resp, nil
}

func (s *subscriptionService) ExecutePlanChange(
	ctx context.Context,
	subscriptionID string,
	req dto.SubscriptionChangeV2Request,
) (*dto.SubscriptionChangeV2Response, error) {
	var resp *dto.SubscriptionChangeV2Response

	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		sub, err := s.loadSubscriptionForPlanChange(txCtx, subscriptionID, true)
		if err != nil {
			return err
		}

		r, err := s.resolvePlanChange(txCtx, sub, req, time.Now().UTC())
		if err != nil {
			return err
		}

		quote, err := s.computePlanChange(txCtx, r)
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

		changedInvoices, err := s.settlePlanChange(txCtx, r, quote)
		if err != nil {
			return err
		}

		resp = buildPlanChangeResponse(r, swapped, req)
		resp.ChangedResources.LineItems = planChangeChangedLineItems(r)
		resp.ChangedResources.Invoices = changedInvoices
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
