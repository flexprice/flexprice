package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
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

func (r *quantityChangeProration) getNetCharge() decimal.Decimal {
	if r == nil {
		return decimal.Zero
	}
	return r.netCharge
}

func (r *quantityChangeProration) getNetCredit() decimal.Decimal {
	if r == nil {
		return decimal.Zero
	}
	return r.netCredit
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
