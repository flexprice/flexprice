package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// quantityChangeRequest is the validated, ready-to-apply intent for a quantity change.
// Built by buildQuantityChangeRequest (read-only). No line-item or money side effects.
type quantityChangeRequest struct {
	subscriptionID string
	subscription   *subscription.Subscription
	modifications  []*quantityChangeLineItemMod
}

func NewQuantityChangeRequest(subscriptionID string, sub *subscription.Subscription, mods []*quantityChangeLineItemMod) *quantityChangeRequest {
	if mods == nil {
		mods = []*quantityChangeLineItemMod{}
	}
	return &quantityChangeRequest{
		subscriptionID: subscriptionID,
		subscription:   sub,
		modifications:  mods,
	}
}

func (r *quantityChangeRequest) GetSubscriptionID() string {
	if r == nil {
		return ""
	}
	return r.subscriptionID
}

func (r *quantityChangeRequest) GetSubscription() *subscription.Subscription {
	if r == nil {
		return nil
	}
	return r.subscription
}

func (r *quantityChangeRequest) GetModifications() []*quantityChangeLineItemMod {
	if r == nil {
		return nil
	}
	return r.modifications
}

// requestFromModifySubscriptionParams rebuilds an apply request from checkout configuration.
// Used on payment success — does not recompute proration. Revalidates that the
// subscription and line items are still safe to apply; already-applied LIs
// (ended at effective_date) are allowed through for idempotent complete.
func (s *subscriptionModificationService) requestFromModifySubscriptionParams(
	ctx context.Context,
	params *types.ModifySubscriptionParams,
) (*quantityChangeRequest, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	sp := s.serviceParams
	sub, _, err := sp.SubRepo.GetWithLineItems(ctx, params.SubscriptionID)
	if err != nil {
		return nil, err
	}

	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Cannot apply a paid quantity change to a non-active subscription; refund or reconcile manually").
			WithReportableDetails(map[string]any{
				"subscription_id": params.SubscriptionID,
				"status":          sub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	now := time.Now().UTC()
	mods := make([]*quantityChangeLineItemMod, 0, len(params.LineItemModifications))
	for _, m := range params.LineItemModifications {
		lineItem, err := sp.SubscriptionLineItemRepo.Get(ctx, m.LineItemID)
		if err != nil {
			return nil, err
		}
		if lineItem.SubscriptionID != params.SubscriptionID {
			return nil, ierr.NewError("line item does not belong to subscription").
				WithHint("Checkout modify_subscription_params line item must belong to the subscription").
				WithReportableDetails(map[string]any{
					"line_item_id":    m.LineItemID,
					"subscription_id": params.SubscriptionID,
				}).
				Mark(ierr.ErrValidation)
		}
		if lineItem.Status != types.StatusPublished {
			return nil, ierr.NewError("line item is not active").
				WithHint("Cannot apply quantity change to an unpublished line item; refund or reconcile manually").
				WithReportableDetails(map[string]any{"line_item_id": m.LineItemID}).
				Mark(ierr.ErrValidation)
		}
		if lineItem.PriceType != types.PRICE_TYPE_FIXED {
			return nil, ierr.NewError("line item is not a fixed-price item").
				WithHint("Quantity changes are only supported for fixed-price line items").
				WithReportableDetails(map[string]any{
					"line_item_id": m.LineItemID,
					"price_type":   lineItem.PriceType,
				}).
				Mark(ierr.ErrValidation)
		}

		effectiveDate := now
		if m.EffectiveDate != nil {
			effectiveDate = m.EffectiveDate.UTC()
		}

		alreadyApplied := !lineItem.EndDate.IsZero() && lineItem.EndDate.Equal(effectiveDate)
		if !alreadyApplied {
			if err := validateQuantityChangeEffectiveDateWithinLineItemWindow(
				effectiveDate, sub, lineItem, m.LineItemID,
			); err != nil {
				return nil, err
			}
		}

		newEndDate := sub.CurrentPeriodEnd
		if !lineItem.EndDate.IsZero() && lineItem.EndDate.After(effectiveDate) {
			newEndDate = lineItem.EndDate
		}

		mods = append(mods, newQuantityChangeLineItemMod(
			m.LineItemID,
			m.Quantity,
			effectiveDate,
			lineItem,
			newEndDate,
		))
	}

	return NewQuantityChangeRequest(params.SubscriptionID, sub, mods), nil
}

// toModifySubscriptionParams maps the validated request into checkout configuration
// for payment-gated apply on webhook complete.
func (r *quantityChangeRequest) toModifySubscriptionParams() *types.ModifySubscriptionParams {
	if r == nil {
		return nil
	}
	mods := r.GetModifications()
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
		SubscriptionID:        r.GetSubscriptionID(),
		LineItemModifications: lineMods,
	}
}

// previewChangedLineItems returns placeholder changed_resources line items
// (preview-ended / preview-created). Real ids exist only after apply.
func (r *quantityChangeRequest) previewChangedLineItems() []dto.ChangedLineItem {
	if r == nil {
		return nil
	}

	out := make([]dto.ChangedLineItem, 0, len(r.modifications)*2)
	for _, m := range r.modifications {
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

// buildQuantityChangeRequest loads the subscription and validates each requested
// line-item change. It performs no writes. No-op quantity changes are skipped.
func (s *subscriptionModificationService) buildQuantityChangeRequest(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyQuantityChangeRequest,
) (*quantityChangeRequest, error) {
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

	return NewQuantityChangeRequest(subscriptionID, sub, mods), nil
}

// ─────────────────────────────────────────────
// Module B — proration calculation (no money writes)
// ─────────────────────────────────────────────

// quantityChangeProration is the calc-only money outcome for a quantityChangeRequest.
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

// calculateProration computes proration amounts for ADVANCE mods on the request.
// It does not create invoices, attempt payment, or issue wallet credits.
func (s *subscriptionModificationService) calculateProration(
	ctx context.Context,
	request *quantityChangeRequest,
) (*quantityChangeProration, error) {
	if request == nil {
		return newQuantityChangeProration(nil, decimal.Zero, decimal.Zero), nil
	}

	sp := s.serviceParams
	sub := request.GetSubscription()
	if sub == nil {
		return nil, ierr.NewError("quantity change request has no subscription").
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

	for _, mod := range request.GetModifications() {
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
			PlanPayInAdvance:   oldItem.InvoiceCadence == types.InvoiceCadenceAdvance,
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
// Module C — apply request (line-item writes only)
// ─────────────────────────────────────────────

// applyQuantityChange ends old line items and creates replacements inside one
// transaction (reuses an ambient tx when present). New LI UUIDs are generated at
// apply time. No invoices, payments, or wallets.
// Idempotent: if a line item is already ended at effective_date, that mod is skipped
// (safe for duplicate checkout-complete webhooks).
func (s *subscriptionModificationService) applyQuantityChange(
	ctx context.Context,
	request *quantityChangeRequest,
) ([]dto.ChangedLineItem, error) {
	if request == nil {
		return nil, ierr.NewError("quantity change request is required").
			Mark(ierr.ErrValidation)
	}

	sp := s.serviceParams
	mods := request.GetModifications()
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

			// Already applied: EndDate was set to effective_date on a prior complete.
			// Pre-apply finite LIs have EndDate after effective_date (validated at request build).
			if !lineItem.EndDate.IsZero() && !lineItem.EndDate.After(effectiveDate) {
				if lineItem.EndDate.Equal(effectiveDate) {
					sp.Logger.Debug(txCtx, "skipping quantity change apply: already applied",
						"line_item_id", lineItemID, "effective_date", effectiveDate)
					continue
				}
				return ierr.NewError("line item already ended before effective_date").
					WithHint("Cannot apply quantity change to a line item that ended before the effective date").
					WithReportableDetails(map[string]any{
						"line_item_id":   lineItemID,
						"line_item_end":  lineItem.EndDate,
						"effective_date": effectiveDate,
					}).
					Mark(ierr.ErrValidation)
			}

			// Preserve a finite original window on the replacement; open-ended stays open-ended.
			newItemEndDate := time.Time{}
			if !lineItem.EndDate.IsZero() {
				newItemEndDate = lineItem.EndDate
			}

			endedItem := subscription.NewSubscriptionLineItemBuilder(lineItem).
				WithEndDate(effectiveDate).
				Build()
			if err := sp.SubscriptionLineItemRepo.Update(txCtx, endedItem); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to end existing line item").
					Mark(ierr.ErrDatabase)
			}

			// Copy from the ended line item, then override identity / window / quantity.
			newItem := subscription.NewSubscriptionLineItemBuilder(endedItem).
				WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM)).
				WithQuantity(mod.getQuantity()).
				WithStartDate(effectiveDate).
				WithEndDate(newItemEndDate).
				WithBaseModel(types.GetDefaultBaseModel(txCtx)).
				Build()
			if err := sp.SubscriptionLineItemRepo.Create(txCtx, newItem); err != nil {
				return err
			}

			oldStart := endedItem.StartDate
			endDate := effectiveDate
			startDate := effectiveDate
			newEndDate := mod.getNewEndDate()
			changedLineItems = append(changedLineItems,
				dto.ChangedLineItem{
					ID:           endedItem.ID,
					PriceID:      endedItem.PriceID,
					Quantity:     endedItem.Quantity,
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

// settlePayLater applies the request (C) then settles each proration item (charge or wallet credit).
// LI apply and invoice/wallet DB writes share one transaction so a settlement failure
// rolls back seat changes. AttemptPayment runs after commit (external side effects).
func (s *subscriptionModificationService) settlePayLater(
	ctx context.Context,
	request *quantityChangeRequest,
	prorationResult *quantityChangeProration,
) ([]dto.ChangedLineItem, []dto.ChangedInvoice, error) {
	sp := s.serviceParams
	var changedLineItems []dto.ChangedLineItem
	changedInvoices := make([]dto.ChangedInvoice, 0)

	err := sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		var err error
		changedLineItems, err = s.applyQuantityChange(txCtx, request)
		if err != nil {
			return err
		}

		sub := request.GetSubscription()
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
				inv, err = s.createProrationChargeInvoice(txCtx, sub, item, false)
			} else {
				inv, err = s.issueProrationWalletCredit(txCtx, sub, item)
			}
			if err != nil {
				return err
			}
			if inv != nil {
				changedInvoices = append(changedInvoices, *inv)
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Best-effort collection after LI + invoice rows are committed together.
	invoiceSvc := NewInvoiceService(sp)
	for i := range changedInvoices {
		ci := &changedInvoices[i]
		if ci.Action != dto.ChangedInvoiceActionCreated || ci.ID == "" {
			continue
		}

		if payErr := invoiceSvc.AttemptPayment(ctx, ci.ID); payErr != nil {
			sp.Logger.Info(ctx, "failed to attempt payment for delta proration invoice",
				"error", payErr, "invoice_id", ci.ID)
		}

		if latest, fetchErr := invoiceSvc.GetInvoice(ctx, ci.ID); fetchErr == nil {
			ci.Invoice = latest
			ci.Status = dto.ChangedInvoiceStatusFromPaymentStatus(latest.PaymentStatus)
		}
	}

	return changedLineItems, changedInvoices, nil
}

// ─────────────────────────────────────────────
// Module D3 — pay-first settlement (no LI apply)
// ─────────────────────────────────────────────

// settlePayFirst persists the request on a checkout session, locks the batch net
// (charges − credits) on one DRAFT ONE_OFF, and returns a payment link.
// Line items are applied on payment success — do not also issue wallet credits
// for downgrade LIs already netted into that invoice.
func (s *subscriptionModificationService) settlePayFirst(
	ctx context.Context,
	request *quantityChangeRequest,
	prorationResult *quantityChangeProration,
	checkout *dto.CheckoutParams,
) (*dto.SubscriptionModifyResponse, error) {
	if request == nil || prorationResult == nil || checkout == nil {
		return nil, ierr.NewError("pay-first settlement requires request, proration, and checkout").
			Mark(ierr.ErrValidation)
	}

	sp := s.serviceParams
	sub := request.GetSubscription()
	if sub == nil {
		return nil, ierr.NewError("quantity change request has no subscription").
			Mark(ierr.ErrValidation)
	}

	modifyParams := request.toModifySubscriptionParams()
	if err := modifyParams.Validate(); err != nil {
		return nil, err
	}

	subscriptionID := sub.ID
	checkoutSvc := NewCheckoutSessionService(sp)
	if err := checkoutSvc.CheckIfAnyCheckoutSessionPending(
		ctx,
		sub.CustomerID,
		types.CheckoutActionModifySubscription,
		func(cfg *types.CheckoutConfiguration) bool {
			return cfg != nil &&
				cfg.ModifySubscriptionParams != nil &&
				cfg.ModifySubscriptionParams.SubscriptionID == subscriptionID
		},
		dto.PendingCheckoutConflict{
			Message: "a pending checkout session already exists for this subscription",
			Hint:    "Complete or cancel the existing checkout before starting another payment-gated modification",
			Details: map[string]any{"subscription_id": subscriptionID},
		},
	); err != nil {
		return nil, err
	}

	draftInvoice, err := s.createAggregatedProrationDraftInvoice(ctx, sub, prorationResult)
	if err != nil {
		return nil, err
	}

	sessionResp, err := checkoutSvc.StartPayFirstCheckoutSession(ctx, &dto.PayFirstCheckoutRequest{
		CustomerID: sub.CustomerID,
		Action:     types.CheckoutActionModifySubscription,
		Configuration: types.CheckoutConfiguration{
			ModifySubscriptionParams: modifyParams,
		},
		DraftInvoice: &draftInvoice.Invoice,
		Checkout:     checkout,
	})
	if err != nil {
		return nil, err
	}

	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
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
			LineItems: request.previewChangedLineItems(),
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
	return invoiceSvc.CreateComputedDraftInvoice(ctx, req)
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
// draft=false: create + finalize (pay-later DB write; caller runs AttemptPayment after commit).
// draft=true: leave DRAFT (unused by current pay-first aggregated draft path).
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
		latest, err := invoiceSvc.CreateComputedDraftInvoice(ctx, req)
		if err != nil {
			sp.Logger.Error(ctx, "failed to create draft proration invoice for quantity change", "error", err)
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

	return &dto.ChangedInvoice{
		ID:      inv.ID,
		Action:  dto.ChangedInvoiceActionCreated,
		Status:  dto.ChangedInvoiceStatusFromPaymentStatus(inv.PaymentStatus),
		Invoice: inv,
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
