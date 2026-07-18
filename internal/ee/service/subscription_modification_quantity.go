package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// quantityChangePlan is the validated, ready-to-apply intent for a quantity change.
// Built by buildQuantityChangePlan (read-only). No line-item or money side effects.
type quantityChangePlan struct {
	subscriptionID string
	subscription   *subscription.Subscription
	modifications  []*quantityChangeLineItemMod
}

func NewQuantityChangePlan(subscriptionID string, sub *subscription.Subscription, mods []*quantityChangeLineItemMod) *quantityChangePlan {
	if mods == nil {
		mods = []*quantityChangeLineItemMod{}
	}
	return &quantityChangePlan{
		subscriptionID: subscriptionID,
		subscription:   sub,
		modifications:  mods,
	}
}

func (p *quantityChangePlan) GetSubscriptionID() string {
	if p == nil {
		return ""
	}
	return p.subscriptionID
}

func (p *quantityChangePlan) GetSubscription() *subscription.Subscription {
	if p == nil {
		return nil
	}
	return p.subscription
}

func (p *quantityChangePlan) GetModifications() []*quantityChangeLineItemMod {
	if p == nil {
		return nil
	}
	return p.modifications
}

// toModifySubscriptionParams maps the validated plan into checkout configuration
// for payment-gated apply on webhook complete.
func (p *quantityChangePlan) toModifySubscriptionParams() *types.ModifySubscriptionParams {
	if p == nil {
		return nil
	}
	mods := p.GetModifications()
	lineMods := make([]types.ModifySubscriptionLineItem, 0, len(mods))
	for _, m := range mods {
		if m == nil {
			continue
		}
		effectiveDate := m.getEffectiveDate()
		ed := effectiveDate
		lineMods = append(lineMods, types.ModifySubscriptionLineItem{
			LineItemID:    m.getLineItemID(),
			Quantity:      m.getQuantity(),
			EffectiveDate: &ed,
		})
	}
	return &types.ModifySubscriptionParams{
		SubscriptionID:        p.GetSubscriptionID(),
		LineItemModifications: lineMods,
	}
}

// previewChangedLineItems returns placeholder changed_resources line items
// (preview-ended / preview-created). Real ids exist only after apply.
func (p *quantityChangePlan) previewChangedLineItems() []dto.ChangedLineItem {
	if p == nil {
		return nil
	}

	out := make([]dto.ChangedLineItem, 0, len(p.modifications)*2)
	for _, m := range p.modifications {
		if m == nil {
			continue
		}
		old := m.getOldLineItem()
		if old == nil {
			continue
		}
		effectiveDate := m.getEffectiveDate()
		oldStart := old.StartDate
		endDate := effectiveDate
		startDate := effectiveDate
		newEndDate := m.getNewEndDate()
		out = append(out,
			dto.ChangedLineItem{
				ID:           "(preview-ended)",
				PriceID:      old.PriceID,
				Quantity:     old.Quantity,
				StartDate:    &oldStart,
				EndDate:      &endDate,
				ChangeAction: dto.ChangedLineItemActionEnded,
			},
			dto.ChangedLineItem{
				ID:           "(preview-created)",
				PriceID:      old.PriceID,
				Quantity:     m.getQuantity(),
				StartDate:    &startDate,
				EndDate:      &newEndDate,
				ChangeAction: dto.ChangedLineItemActionCreated,
			},
		)
	}
	return out
}

// quantityChangeLineItemMod is one validated line-item quantity change.
type quantityChangeLineItemMod struct {
	lineItemID    string
	quantity      decimal.Decimal
	effectiveDate time.Time
	oldLineItem   *subscription.SubscriptionLineItem
	newEndDate    time.Time
}

func newQuantityChangeLineItemMod(
	lineItemID string,
	quantity decimal.Decimal,
	effectiveDate time.Time,
	oldLineItem *subscription.SubscriptionLineItem,
	newEndDate time.Time,
) *quantityChangeLineItemMod {
	return &quantityChangeLineItemMod{
		lineItemID:    lineItemID,
		quantity:      quantity,
		effectiveDate: effectiveDate,
		oldLineItem:   oldLineItem,
		newEndDate:    newEndDate,
	}
}

func (m *quantityChangeLineItemMod) getLineItemID() string {
	if m == nil {
		return ""
	}
	return m.lineItemID
}

func (m *quantityChangeLineItemMod) getQuantity() decimal.Decimal {
	if m == nil {
		return decimal.Zero
	}
	return m.quantity
}

func (m *quantityChangeLineItemMod) getEffectiveDate() time.Time {
	if m == nil {
		return time.Time{}
	}
	return m.effectiveDate
}

func (m *quantityChangeLineItemMod) getOldLineItem() *subscription.SubscriptionLineItem {
	if m == nil {
		return nil
	}
	return m.oldLineItem
}

func (m *quantityChangeLineItemMod) getNewEndDate() time.Time {
	if m == nil {
		return time.Time{}
	}
	return m.newEndDate
}

// buildQuantityChangePlan loads the subscription and validates each requested
// line-item change. It performs no writes. No-op quantity changes are skipped.
func (s *subscriptionModificationService) buildQuantityChangePlan(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyQuantityChangeRequest,
) (*quantityChangePlan, error) {
	sp := s.serviceParams

	if params == nil {
		return nil, ierr.NewError("quantity_change_params is required").
			Mark(ierr.ErrValidation)
	}

	sub, _, err := sp.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Only active subscriptions can have quantity changes applied").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID, "status": sub.SubscriptionStatus}).
			Mark(ierr.ErrValidation)
	}

	now := time.Now().UTC()
	mods := make([]*quantityChangeLineItemMod, 0, len(params.LineItems))

	for _, change := range params.LineItems {
		effectiveDate := now
		if change.EffectiveDate != nil {
			effectiveDate = change.EffectiveDate.UTC()
		}
		if effectiveDate.Before(sub.CurrentPeriodStart) {
			return nil, ierr.NewError("effective_date cannot be before the current period start").
				WithHint("Set effective_date to a time within the current billing period").
				WithReportableDetails(map[string]interface{}{
					"effective_date":       effectiveDate,
					"current_period_start": sub.CurrentPeriodStart,
				}).
				Mark(ierr.ErrValidation)
		}
		if !effectiveDate.Before(sub.CurrentPeriodEnd) {
			return nil, ierr.NewError("effective_date must be before the current period end").
				WithHint("Set effective_date to a time within the current billing period").
				WithReportableDetails(map[string]interface{}{
					"effective_date":     effectiveDate,
					"current_period_end": sub.CurrentPeriodEnd,
				}).
				Mark(ierr.ErrValidation)
		}

		lineItem, err := sp.SubscriptionLineItemRepo.Get(ctx, change.ID)
		if err != nil {
			return nil, err
		}

		if lineItem.SubscriptionID != subscriptionID {
			return nil, ierr.NewError("line item does not belong to subscription").
				WithHint("The specified line item ID must belong to the given subscription").
				WithReportableDetails(map[string]interface{}{"line_item_id": change.ID, "subscription_id": subscriptionID}).
				Mark(ierr.ErrValidation)
		}

		if lineItem.Status != types.StatusPublished {
			return nil, ierr.NewError("line item is not active").
				WithHint("Only published line items can have their quantity changed").
				WithReportableDetails(map[string]interface{}{"line_item_id": change.ID}).
				Mark(ierr.ErrValidation)
		}

		if lineItem.PriceType != types.PRICE_TYPE_FIXED {
			return nil, ierr.NewError("line item is not a fixed-price item").
				WithHint("Quantity changes are only supported for fixed-price line items").
				WithReportableDetails(map[string]interface{}{"line_item_id": change.ID, "price_type": lineItem.PriceType}).
				Mark(ierr.ErrValidation)
		}

		if err := validateQuantityChangeEffectiveDateWithinLineItemWindow(effectiveDate, sub, lineItem, change.ID); err != nil {
			return nil, err
		}

		if change.Quantity.Equal(lineItem.Quantity) {
			sp.Logger.Debug(ctx, "skipping quantity change: quantity is unchanged",
				"line_item_id", change.ID, "quantity", change.Quantity)
			continue
		}

		newEndDate := sub.CurrentPeriodEnd
		if !lineItem.EndDate.IsZero() {
			newEndDate = lineItem.EndDate
		}

		// we will create a new line item following change.ID but different line item id to change.Quantity at effectiveDate till newEndDate
		mods = append(mods, newQuantityChangeLineItemMod(
			change.ID,
			change.Quantity,
			effectiveDate,
			lineItem,
			newEndDate,
		))
	}

	return NewQuantityChangePlan(subscriptionID, sub, mods), nil
}

// ─────────────────────────────────────────────
// Module B — proration calculation (no money writes)
// ─────────────────────────────────────────────

// quantityChangeProration is the calc-only money outcome for a quantityChangePlan.
type quantityChangeProration struct {
	items     []*quantityChangeProrationItem
	netCharge decimal.Decimal
	netCredit decimal.Decimal
}

func newQuantityChangeProration(items []*quantityChangeProrationItem, netCharge, netCredit decimal.Decimal) *quantityChangeProration {
	if items == nil {
		items = []*quantityChangeProrationItem{}
	}
	return &quantityChangeProration{
		items:     items,
		netCharge: netCharge,
		netCredit: netCredit,
	}
}

func (r *quantityChangeProration) getItems() []*quantityChangeProrationItem {
	if r == nil {
		return nil
	}
	return r.items
}

func (r *quantityChangeProration) GetNetCharge() decimal.Decimal {
	if r == nil {
		return decimal.Zero
	}
	return r.netCharge
}

func (r *quantityChangeProration) GetNetCredit() decimal.Decimal {
	if r == nil {
		return decimal.Zero
	}
	return r.netCredit
}

// GetNetAmount is charges minus credits across the batch (can be negative).
func (r *quantityChangeProration) GetNetAmount() decimal.Decimal {
	if r == nil {
		return decimal.Zero
	}
	return r.netCharge.Sub(r.netCredit)
}

// quantityChangeProrationItem is one ADVANCE line-item's non-zero proration result.
type quantityChangeProrationItem struct {
	mod       *quantityChangeLineItemMod
	netAmount decimal.Decimal
	price     *dto.PriceResponse
}

func newQuantityChangeProrationItem(mod *quantityChangeLineItemMod, netAmount decimal.Decimal, price *dto.PriceResponse) *quantityChangeProrationItem {
	return &quantityChangeProrationItem{
		mod:       mod,
		netAmount: netAmount,
		price:     price,
	}
}

func (i *quantityChangeProrationItem) getMod() *quantityChangeLineItemMod {
	if i == nil {
		return nil
	}
	return i.mod
}

func (i *quantityChangeProrationItem) getNetAmount() decimal.Decimal {
	if i == nil {
		return decimal.Zero
	}
	return i.netAmount
}

func (i *quantityChangeProrationItem) getPrice() *dto.PriceResponse {
	if i == nil {
		return nil
	}
	return i.price
}

// calculateProrationForPlan computes proration amounts for ADVANCE mods on the plan.
// It does not create invoices, attempt payment, or issue wallet credits.
func (s *subscriptionModificationService) calculateProrationForPlan(
	ctx context.Context,
	plan *quantityChangePlan,
) (*quantityChangeProration, error) {
	if plan == nil {
		return newQuantityChangeProration(nil, decimal.Zero, decimal.Zero), nil
	}

	sp := s.serviceParams
	sub := plan.GetSubscription()
	if sub == nil {
		return nil, ierr.NewError("quantity change plan has no subscription").
			Mark(ierr.ErrValidation)
	}

	prorationSvc := NewProrationService(sp)
	priceSvc := NewPriceService(sp)

	customerTimezone := sub.Timezone
	if customerTimezone == "" {
		customerTimezone = types.DefaultTimezone
	}

	items := make([]*quantityChangeProrationItem, 0)
	netCharge := decimal.Zero
	netCredit := decimal.Zero

	for _, mod := range plan.GetModifications() {
		if mod == nil {
			continue
		}
		oldItem := mod.getOldLineItem()
		if oldItem == nil {
			continue
		}
		if oldItem.InvoiceCadence != types.InvoiceCadenceAdvance {
			continue
		}

		price, err := priceSvc.GetPrice(ctx, oldItem.PriceID)
		if err != nil {
			return nil, err
		}

		result, err := prorationSvc.CalculateProration(ctx, proration.ProrationParams{
			SubscriptionID:     sub.ID,
			LineItemID:         oldItem.ID,
			PlanPayInAdvance:   price.Price.InvoiceCadence == types.InvoiceCadenceAdvance,
			CurrentPeriodStart: sub.CurrentPeriodStart,
			CurrentPeriodEnd:   sub.CurrentPeriodEnd.Add(-time.Second),
			Action:             types.ProrationActionQuantityChange,
			NewPriceID:         oldItem.PriceID,
			OldQuantity:        oldItem.Quantity,
			NewQuantity:        mod.getQuantity(),
			NewPricePerUnit:    price.Price.Amount,
			OldPricePerUnit:    price.Price.Amount,
			ProrationDate:      mod.getEffectiveDate(),
			ProrationBehavior:  types.ProrationBehaviorCreateProrations,
			ProrationStrategy:  types.StrategySecondBased,
			Currency:           sub.Currency,
			PlanDisplayName:    oldItem.PlanDisplayName,
			Timezone:           customerTimezone,
		})
		if err != nil {
			return nil, err
		}
		if result == nil || result.NetAmount.IsZero() {
			continue
		}

		items = append(items, newQuantityChangeProrationItem(mod, result.NetAmount, price))
		if result.NetAmount.GreaterThan(decimal.Zero) {
			netCharge = netCharge.Add(result.NetAmount)
		} else {
			netCredit = netCredit.Add(result.NetAmount.Abs())
		}
	}

	return newQuantityChangeProration(items, netCharge, netCredit), nil
}

// ─────────────────────────────────────────────
// Module C — apply plan (line-item writes only)
// ─────────────────────────────────────────────

// applyQuantityChangePlan ends old line items and creates replacements inside one
// transaction. New LI UUIDs are generated at apply time. No invoices, payments, or wallets.
func (s *subscriptionModificationService) applyQuantityChangePlan(
	ctx context.Context,
	plan *quantityChangePlan,
) ([]dto.ChangedLineItem, error) {
	if plan == nil {
		return nil, ierr.NewError("quantity change plan is required").
			Mark(ierr.ErrValidation)
	}

	sp := s.serviceParams
	mods := plan.GetModifications()
	if len(mods) == 0 {
		return []dto.ChangedLineItem{}, nil
	}

	changedLineItems := make([]dto.ChangedLineItem, 0, len(mods)*2)

	err := sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		changedLineItems = nil

		for _, mod := range mods {
			if mod == nil {
				continue
			}
			effectiveDate := mod.getEffectiveDate()
			lineItemID := mod.getLineItemID()

			lineItem, err := sp.SubscriptionLineItemRepo.Get(txCtx, lineItemID)
			if err != nil {
				return err
			}

			lineItem.EndDate = effectiveDate
			if err := sp.SubscriptionLineItemRepo.Update(txCtx, lineItem); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to end existing line item").
					Mark(ierr.ErrDatabase)
			}

			// Copy from the ended line item, then override identity / window / quantity.
			// Clear EndDate so the replacement is open-ended (or later clipped via newEndDate in response only).
			newItem := subscription.NewSubscriptionLineItemBuilder(lineItem).
				WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM)).
				WithQuantity(mod.getQuantity()).
				WithStartDate(effectiveDate).
				WithEndDate(time.Time{}).
				WithBaseModel(types.GetDefaultBaseModel(txCtx)).
				Build()
			if err := sp.SubscriptionLineItemRepo.Create(txCtx, newItem); err != nil {
				return err
			}

			oldStart := lineItem.StartDate
			endDate := effectiveDate
			startDate := effectiveDate
			newEndDate := mod.getNewEndDate()
			changedLineItems = append(changedLineItems,
				dto.ChangedLineItem{
					ID:           lineItem.ID,
					PriceID:      lineItem.PriceID,
					Quantity:     lineItem.Quantity,
					StartDate:    &oldStart,
					EndDate:      &endDate,
					ChangeAction: dto.ChangedLineItemActionEnded,
				},
				dto.ChangedLineItem{
					ID:           newItem.ID,
					PriceID:      newItem.PriceID,
					Quantity:     newItem.Quantity,
					StartDate:    &startDate,
					EndDate:      &newEndDate,
					ChangeAction: dto.ChangedLineItemActionCreated,
				},
			)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return changedLineItems, nil
}

// ─────────────────────────────────────────────
// Module D1 — money settlement helpers (pay-later)
// ─────────────────────────────────────────────

// settlePayLater applies the plan (C) then settles each proration item (charge or wallet credit).
func (s *subscriptionModificationService) settlePayLater(
	ctx context.Context,
	plan *quantityChangePlan,
	prorationResult *quantityChangeProration,
) ([]dto.ChangedLineItem, []dto.ChangedInvoice, error) {
	changedLineItems, err := s.applyQuantityChangePlan(ctx, plan)
	if err != nil {
		return nil, nil, err
	}

	sub := plan.GetSubscription()
	changedInvoices := make([]dto.ChangedInvoice, 0)
	for _, item := range prorationResult.getItems() {
		if item == nil {
			continue
		}
		netAmount := item.getNetAmount()
		if netAmount.IsZero() {
			continue
		}

		var inv *dto.ChangedInvoice
		if netAmount.GreaterThan(decimal.Zero) {
			inv, err = s.createProrationChargeInvoice(ctx, sub, item, false)
		} else {
			inv, err = s.issueProrationWalletCredit(ctx, sub, item)
		}
		if err != nil {
			return nil, nil, err
		}
		if inv != nil {
			changedInvoices = append(changedInvoices, *inv)
		}
	}

	return changedLineItems, changedInvoices, nil
}

// ─────────────────────────────────────────────
// Module D3 — pay-first settlement (no LI apply)
// ─────────────────────────────────────────────

// settlePayFirst persists the plan on a checkout session, locks the batch net
// (charges − credits) on one DRAFT ONE_OFF, and returns a payment link.
// Line items are applied on payment success — do not also issue wallet credits
// for downgrade LIs already netted into that invoice.
func (s *subscriptionModificationService) settlePayFirst(
	ctx context.Context,
	plan *quantityChangePlan,
	prorationResult *quantityChangeProration,
	checkout *dto.CheckoutParams,
) (*dto.SubscriptionModifyResponse, error) {
	if plan == nil || prorationResult == nil || checkout == nil {
		return nil, ierr.NewError("pay-first settlement requires plan, proration, and checkout").
			Mark(ierr.ErrValidation)
	}

	sp := s.serviceParams
	sub := plan.GetSubscription()
	if sub == nil {
		return nil, ierr.NewError("quantity change plan has no subscription").
			Mark(ierr.ErrValidation)
	}

	modifyParams := plan.toModifySubscriptionParams()
	if err := modifyParams.Validate(); err != nil {
		return nil, err
	}

	if err := s.guardPendingModifyCheckout(ctx, sub.CustomerID, plan.GetSubscriptionID()); err != nil {
		return nil, err
	}

	providerCfg := &types.CheckoutPaymentProviderConfig{}
	if checkout.PaymentProviderConfig != nil {
		providerCfg = checkout.PaymentProviderConfig
	}
	if providerCfg.CollectionMethod == "" {
		providerCfg.CollectionMethod = types.CollectionMethodSendInvoice
	}
	if err := providerCfg.Validate(); err != nil {
		return nil, err
	}

	draftInvoice, err := s.createAggregatedProrationDraftInvoice(ctx, sub, prorationResult)
	if err != nil {
		return nil, err
	}

	var meta types.Metadata
	if len(checkout.Metadata) > 0 {
		meta = types.Metadata(checkout.Metadata)
	}

	session := &domainCheckout.CheckoutSession{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		CustomerID:      sub.CustomerID,
		Action:          types.CheckoutActionModifySubscription,
		CheckoutStatus:  types.CheckoutStatusInitiated,
		PaymentProvider: checkout.PaymentProvider,
		Configuration: domainCheckout.ToJSONBCheckoutConfiguration(types.CheckoutConfiguration{
			ModifySubscriptionParams: modifyParams,
		}),
		PaymentProviderConfig: domainCheckout.ToJSONBCheckoutPaymentProviderConfig(providerCfg),
		IdempotencyKey:        checkout.IdempotencyKey,
		SuccessURL:            checkout.SuccessURL,
		FailureURL:            checkout.FailureURL,
		CancelURL:             checkout.CancelURL,
		ExpiresAt:             time.Now().UTC().Add(checkout.PaymentProvider.SessionExpiry()),
		Metadata:              meta,
		BaseModel:             types.GetDefaultBaseModel(ctx),
	}

	if err := sp.CheckoutSessionRepo.Create(ctx, session); err != nil {
		return nil, err
	}

	checkoutSvc := &checkoutSessionService{ServiceParams: sp}

	// Fulfill: payment + provider link. On failure, best-effort cleanup.
	if err := s.fulfillModifySubscriptionCheckout(ctx, checkoutSvc, session, &draftInvoice.Invoice); err != nil {
		if cleanupErr := checkoutSvc.cleanupCheckoutSession(ctx, session, err); cleanupErr != nil {
			sp.Logger.Error(ctx, "checkout cleanup failed after pay-first fulfillment error",
				"session_id", session.ID,
				"error", cleanupErr,
				"original_err", err,
			)
		}
		return nil, err
	}

	sessionResp := dto.ToCheckoutSessionResponse(session)
	checkoutSvc.publishCheckoutEvent(ctx, sessionResp, types.WebhookEventCheckoutSessionInitiated)

	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, plan.GetSubscriptionID())
	if err != nil {
		return nil, err
	}

	// Re-fetch draft invoice for response (amounts after compute).
	invoiceSvc := NewInvoiceService(sp)
	latestInv, err := invoiceSvc.GetInvoice(ctx, draftInvoice.ID)
	if err != nil {
		latestInv = draftInvoice
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			LineItems: plan.previewChangedLineItems(),
			Invoices: []dto.ChangedInvoice{
				{
					ID:      latestInv.ID,
					Action:  dto.ChangedInvoiceActionCreated,
					Status:  dto.ChangedInvoiceStatusFromPaymentStatus(latestInv.PaymentStatus),
					Invoice: latestInv,
				},
			},
		},
		CheckoutSession: sessionResp,
	}, nil
}

func (s *subscriptionModificationService) fulfillModifySubscriptionCheckout(
	ctx context.Context,
	checkoutSvc *checkoutSessionService,
	session *domainCheckout.CheckoutSession,
	inv *invoice.Invoice,
) error {
	payResp, err := checkoutSvc.createCheckoutPayment(ctx, inv, session.PaymentProvider)
	if err != nil {
		return err
	}
	session.CheckoutInvoiceID = &inv.ID
	session.CheckoutPaymentID = &payResp.ID

	providerResult, err := checkoutSvc.callCheckoutProvider(ctx, session, payResp)
	if err != nil {
		return err
	}
	session.ProviderResult = (*domainCheckout.JSONBCheckoutProviderResult)(providerResult)
	session.CheckoutStatus = types.CheckoutStatusPending
	return s.serviceParams.CheckoutSessionRepo.Update(ctx, session)
}

func (s *subscriptionModificationService) guardPendingModifyCheckout(
	ctx context.Context,
	customerID string,
	subscriptionID string,
) error {
	filter := &types.CheckoutSessionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		CustomerIDs: []string{customerID},
		Actions:     []types.CheckoutAction{types.CheckoutActionModifySubscription},
		CheckoutStatuses: []types.CheckoutStatus{
			types.CheckoutStatusInitiated,
			types.CheckoutStatusPending,
		},
	}
	sessions, err := s.serviceParams.CheckoutSessionRepo.List(ctx, filter)
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		cfg := sess.Configuration.ToCheckoutConfiguration()
		if cfg.ModifySubscriptionParams != nil &&
			cfg.ModifySubscriptionParams.SubscriptionID == subscriptionID {
			return ierr.NewError("a pending checkout session already exists for this subscription").
				WithHint("Complete or cancel the existing checkout before starting another payment-gated modification").
				WithReportableDetails(map[string]any{
					"subscription_id":     subscriptionID,
					"checkout_session_id": sess.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
	}
	return nil
}

// createAggregatedProrationDraftInvoice locks the batch net (charges − credits) on one DRAFT ONE_OFF.
// Line items keep per-LI charge/credit amounts; AmountDue is their sum (the net).
func (s *subscriptionModificationService) createAggregatedProrationDraftInvoice(
	ctx context.Context,
	sub *subscription.Subscription,
	prorationResult *quantityChangeProration,
) (*dto.InvoiceResponse, error) {
	if !prorationResult.GetNetAmount().GreaterThan(decimal.Zero) {
		return nil, ierr.NewError("no proration charge to collect via checkout").
			Mark(ierr.ErrValidation)
	}

	items := make([]*quantityChangeProrationItem, 0)
	for _, item := range prorationResult.getItems() {
		if item != nil && !item.getNetAmount().IsZero() {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return nil, ierr.NewError("no proration charge to collect via checkout").
			Mark(ierr.ErrValidation)
	}

	req := buildAggregatedProrationChargeInvoiceRequest(sub, items)
	invoiceSvc := NewInvoiceService(s.serviceParams)
	draftResp, err := invoiceSvc.CreateEmptyDraftInvoice(ctx, req.ToDraftRequest())
	if err != nil {
		return nil, err
	}
	computeReq := req.ToComputeRequest()
	skipped, err := invoiceSvc.ComputeInvoice(ctx, draftResp.ID, &computeReq)
	if err != nil {
		return nil, err
	}
	if skipped {
		return nil, ierr.NewError("proration draft invoice was skipped").
			WithHint("Expected a non-zero proration charge").
			WithReportableDetails(map[string]any{"invoice_id": draftResp.ID}).
			Mark(ierr.ErrValidation)
	}
	return invoiceSvc.GetInvoice(ctx, draftResp.ID)
}

func buildAggregatedProrationChargeInvoiceRequest(
	sub *subscription.Subscription,
	items []*quantityChangeProrationItem,
) dto.CreateInvoiceRequest {
	lineItems := make([]dto.CreateInvoiceLineItemRequest, 0, len(items))
	total := decimal.Zero
	var periodStart *time.Time
	periodEnd := sub.CurrentPeriodEnd
	billingPeriod := string(sub.BillingPeriod)

	for _, item := range items {
		single := buildProrationChargeInvoiceRequest(sub, item)
		total = total.Add(single.AmountDue)
		lineItems = append(lineItems, single.LineItems...)
		if single.PeriodStart != nil && (periodStart == nil || single.PeriodStart.Before(*periodStart)) {
			periodStart = single.PeriodStart
		}
	}

	return dto.CreateInvoiceRequest{
		CustomerID:     sub.GetInvoicingCustomerID(),
		SubscriptionID: &sub.ID,
		InvoiceType:    types.InvoiceTypeOneOff,
		Currency:       sub.Currency,
		BillingReason:  types.InvoiceBillingReasonSubscriptionUpdate,
		AmountDue:      total,
		Total:          total,
		Subtotal:       total,
		PeriodStart:    periodStart,
		PeriodEnd:      &periodEnd,
		BillingPeriod:  &billingPeriod,
		LineItems:      lineItems,
	}
}

// createProrationChargeInvoice creates a ONE_OFF proration charge for one B item.
// draft=false: finalize + AttemptPayment (pay-later). draft=true: leave DRAFT (pay-first).
func (s *subscriptionModificationService) createProrationChargeInvoice(
	ctx context.Context,
	sub *subscription.Subscription,
	item *quantityChangeProrationItem,
	draft bool,
) (*dto.ChangedInvoice, error) {
	if item == nil || sub == nil {
		return nil, nil
	}
	mod := item.getMod()
	oldItem := mod.getOldLineItem()
	price := item.getPrice()
	netAmount := item.getNetAmount()
	if mod == nil || oldItem == nil || price == nil || !netAmount.GreaterThan(decimal.Zero) {
		return nil, nil
	}

	sp := s.serviceParams
	invoiceSvc := NewInvoiceService(sp)
	req := buildProrationChargeInvoiceRequest(sub, item)

	if draft {
		draftResp, err := invoiceSvc.CreateEmptyDraftInvoice(ctx, req.ToDraftRequest())
		if err != nil {
			sp.Logger.Error(ctx, "failed to create draft proration invoice for quantity change", "error", err)
			return nil, err
		}
		computeReq := req.ToComputeRequest()
		skipped, err := invoiceSvc.ComputeInvoice(ctx, draftResp.ID, &computeReq)
		if err != nil {
			return nil, err
		}
		if skipped {
			return nil, ierr.NewError("proration draft invoice was skipped").
				WithHint("Expected a non-zero proration charge").
				WithReportableDetails(map[string]any{"invoice_id": draftResp.ID}).
				Mark(ierr.ErrValidation)
		}
		latest, err := invoiceSvc.GetInvoice(ctx, draftResp.ID)
		if err != nil {
			return nil, err
		}
		return &dto.ChangedInvoice{
			ID:      latest.ID,
			Action:  dto.ChangedInvoiceActionCreated,
			Status:  dto.ChangedInvoiceStatusFromPaymentStatus(latest.PaymentStatus),
			Invoice: latest,
		}, nil
	}

	inv, err := invoiceSvc.CreateInvoice(ctx, req)
	if err != nil {
		sp.Logger.Error(ctx, "failed to create delta proration invoice for quantity change", "error", err)
		return nil, err
	}
	if err := invoiceSvc.AttemptPayment(ctx, inv.ID); err != nil {
		sp.Logger.Info(context.Background(), "failed to attempt payment for delta proration invoice", "error", err, "invoice_id", inv.ID)
	}
	latest, fetchErr := invoiceSvc.GetInvoice(ctx, inv.ID)
	if fetchErr != nil {
		latest = inv
	}
	return &dto.ChangedInvoice{
		ID:      latest.ID,
		Action:  dto.ChangedInvoiceActionCreated,
		Status:  dto.ChangedInvoiceStatusFromPaymentStatus(latest.PaymentStatus),
		Invoice: latest,
	}, nil
}

// issueProrationWalletCredit issues a wallet credit for a negative proration item.
func (s *subscriptionModificationService) issueProrationWalletCredit(
	ctx context.Context,
	sub *subscription.Subscription,
	item *quantityChangeProrationItem,
) (*dto.ChangedInvoice, error) {
	if item == nil || sub == nil {
		return nil, nil
	}
	mod := item.getMod()
	oldItem := mod.getOldLineItem()
	netAmount := item.getNetAmount()
	if mod == nil || oldItem == nil || !netAmount.LessThan(decimal.Zero) {
		return nil, nil
	}

	sp := s.serviceParams
	walletSvc := NewWalletService(sp)
	creditAmount := netAmount.Abs()
	billingCustomer := sub.GetInvoicingCustomerID()
	effectiveDate := mod.getEffectiveDate()
	idempotencyKey := fmt.Sprintf("proration_credit_%s_%s_%s", sub.ID, oldItem.ID, effectiveDate.Format(time.RFC3339))
	walletTx, err := walletSvc.TopUpWalletForProratedCharge(ctx, billingCustomer, creditAmount, sub.Currency, idempotencyKey)
	if err != nil {
		sp.Logger.Error(ctx, "failed to top up wallet for downgrade proration", "error", err)
		return nil, err
	}
	changedID := "(wallet_credit)"
	if walletTx != nil && walletTx.Transaction != nil && walletTx.ID != "" {
		changedID = walletTx.ID
	}
	return &dto.ChangedInvoice{
		ID:                changedID,
		Action:            dto.ChangedInvoiceActionWalletCredit,
		Status:            dto.ChangedInvoiceStatusWalletIssued,
		WalletTransaction: walletTx,
	}, nil
}

func buildProrationChargeInvoiceRequest(
	sub *subscription.Subscription,
	item *quantityChangeProrationItem,
) dto.CreateInvoiceRequest {
	mod := item.getMod()
	oldItem := mod.getOldLineItem()
	price := item.getPrice()
	netAmount := item.getNetAmount()
	effectiveDate := mod.getEffectiveDate()
	newQuantity := mod.getQuantity()
	periodEnd := sub.CurrentPeriodEnd
	billingPeriod := string(sub.BillingPeriod)
	billingCustomer := sub.GetInvoicingCustomerID()

	qtyDelta := newQuantity.Sub(oldItem.Quantity)
	displayName := fmt.Sprintf("%s — Quantity Change Proration (%s – %s)",
		oldItem.DisplayName,
		effectiveDate.Format("2 Jan 2006"),
		periodEnd.Format("2 Jan 2006"))
	priceID := oldItem.PriceID
	priceType := string(price.Price.Type)
	planDisplayName := oldItem.PlanDisplayName
	lineItemDescription := fmt.Sprintf("Proration for quantity change: %s → %s units × %s %s/unit (%s – %s)",
		oldItem.Quantity.String(), newQuantity.String(),
		strings.ToUpper(sub.Currency), price.Price.Amount.String(),
		effectiveDate.Format("2 Jan 2006"), periodEnd.Format("2 Jan 2006"))

	return dto.CreateInvoiceRequest{
		CustomerID:     billingCustomer,
		SubscriptionID: &sub.ID,
		InvoiceType:    types.InvoiceTypeOneOff,
		Currency:       sub.Currency,
		BillingReason:  types.InvoiceBillingReasonSubscriptionUpdate,
		AmountDue:      netAmount,
		Total:          netAmount,
		Subtotal:       netAmount,
		PeriodStart:    &effectiveDate,
		PeriodEnd:      &periodEnd,
		BillingPeriod:  &billingPeriod,
		LineItems: []dto.CreateInvoiceLineItemRequest{
			{
				PriceID:         &priceID,
				PriceType:       &priceType,
				PlanDisplayName: &planDisplayName,
				DisplayName:     &displayName,
				Amount:          netAmount,
				Quantity:        qtyDelta,
				PeriodStart:     &effectiveDate,
				PeriodEnd:       &periodEnd,
				Metadata:        types.Metadata{"description": lineItemDescription},
			},
		},
	}
}

// validateQuantityChangeEffectiveDateWithinLineItemWindow ensures effectiveDate lies in
// [lineItem.StartDate, lineEnd), where lineEnd is lineItem.EndDate when set, otherwise
// sub.CurrentPeriodEnd (open-ended line item). Subscription period bounds are validated separately.
func validateQuantityChangeEffectiveDateWithinLineItemWindow(
	effectiveDate time.Time,
	sub *subscription.Subscription,
	lineItem *subscription.SubscriptionLineItem,
	lineItemID string,
) error {
	if !lineItem.StartDate.IsZero() && effectiveDate.Before(lineItem.StartDate) {
		return ierr.NewError("effective_date cannot be before the line item start date").
			WithHint("Set effective_date to a time when the line item is active").
			WithReportableDetails(map[string]interface{}{
				"effective_date":  effectiveDate,
				"line_item_id":    lineItemID,
				"line_item_start": lineItem.StartDate,
			}).
			Mark(ierr.ErrValidation)
	}
	lineEnd := sub.CurrentPeriodEnd
	if !lineItem.EndDate.IsZero() {
		lineEnd = lineItem.EndDate
	}
	if !effectiveDate.Before(lineEnd) {
		return ierr.NewError("effective_date must be before the line item end date").
			WithHint("Set effective_date to a time before the line item's active window ends").
			WithReportableDetails(map[string]interface{}{
				"effective_date": effectiveDate,
				"line_item_id":   lineItemID,
				"line_item_end":  lineEnd,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}
