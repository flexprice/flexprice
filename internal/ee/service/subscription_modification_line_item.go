package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

const (
	lineItemChangeDisplayName = "Line item change"
	lineItemChangeReason      = "line item change"
)

type lineItemChangeRequest struct {
	subscription  *subscription.Subscription
	modifications []*lineItemChangeMod
}

func NewLineItemChangeRequest(sub *subscription.Subscription, mods []*lineItemChangeMod) *lineItemChangeRequest {
	if mods == nil {
		mods = []*lineItemChangeMod{}
	}
	return &lineItemChangeRequest{
		subscription:  sub,
		modifications: mods,
	}
}

func (r *lineItemChangeRequest) GetSubscriptionID() string {
	if r == nil || r.subscription == nil {
		return ""
	}
	return r.subscription.ID
}

func (r *lineItemChangeRequest) GetSubscription() *subscription.Subscription {
	if r == nil {
		return nil
	}
	return r.subscription
}

func (r *lineItemChangeRequest) GetModifications() []*lineItemChangeMod {
	if r == nil {
		return nil
	}
	return r.modifications
}

type lineItemChangeMod struct {
	lineItemID      string
	updatedQuantity *decimal.Decimal
	newPriceReq     *dto.CreatePriceRequest
	amount          *decimal.Decimal
	effectiveDate   time.Time
	oldLineItem     *subscription.SubscriptionLineItem
	oldPrice        *dto.PriceResponse
	newEndDate      time.Time
}

func newLineItemChangeMod(
	lineItemID string,
	updatedQuantity *decimal.Decimal,
	newPriceReq *dto.CreatePriceRequest,
	change dto.LineItemChange,
	effectiveDate time.Time,
	oldLineItem *subscription.SubscriptionLineItem,
	oldPrice *dto.PriceResponse,
	newEndDate time.Time,
) *lineItemChangeMod {
	return &lineItemChangeMod{
		lineItemID:      lineItemID,
		updatedQuantity: updatedQuantity,
		newPriceReq:     newPriceReq,
		amount:          change.Amount,
		effectiveDate:   effectiveDate,
		oldLineItem:     oldLineItem,
		oldPrice:        oldPrice,
		newEndDate:      newEndDate,
	}
}

func (m *lineItemChangeMod) getLineItemID() string {
	if m == nil {
		return ""
	}
	return m.lineItemID
}

func (m *lineItemChangeMod) getUpdatedQuantity() *decimal.Decimal {
	if m == nil {
		return nil
	}
	return m.updatedQuantity
}

func (m *lineItemChangeMod) getTargetQuantity() decimal.Decimal {
	if m == nil {
		return decimal.Zero
	}
	if m.updatedQuantity != nil {
		return *m.updatedQuantity
	}
	if m.oldLineItem != nil {
		return m.oldLineItem.Quantity
	}
	return decimal.Zero
}

func (m *lineItemChangeMod) getNewPriceRequest() *dto.CreatePriceRequest {
	if m == nil {
		return nil
	}
	return m.newPriceReq
}

func (m *lineItemChangeMod) hasPriceChange() bool {
	return m != nil && m.newPriceReq != nil
}

func (m *lineItemChangeMod) getAmount() *decimal.Decimal {
	if m == nil {
		return nil
	}
	return m.amount
}

func (m *lineItemChangeMod) getEffectiveDate() time.Time {
	if m == nil {
		return time.Time{}
	}
	return m.effectiveDate
}

func (m *lineItemChangeMod) getOldLineItem() *subscription.SubscriptionLineItem {
	if m == nil {
		return nil
	}
	return m.oldLineItem
}

func (m *lineItemChangeMod) getOldPrice() *dto.PriceResponse {
	if m == nil {
		return nil
	}
	return m.oldPrice
}

func (m *lineItemChangeMod) getNewEndDate() time.Time {
	if m == nil {
		return time.Time{}
	}
	return m.newEndDate
}

func (s *subscriptionModificationService) resolveLineItemChangeMod(
	ctx context.Context,
	sub *subscription.Subscription,
	change dto.LineItemChange,
	effectiveDate time.Time,
	allowAlreadyApplied bool,
) (*lineItemChangeMod, error) {
	sp := s.serviceParams
	lineItemID := change.ID
	updatedQuantity := change.Quantity

	lineItem, err := sp.SubscriptionLineItemRepo.Get(ctx, lineItemID)
	if err != nil {
		return nil, err
	}

	if lineItem.SubscriptionID != sub.ID {
		return nil, ierr.NewError("line item does not belong to subscription").
			WithHint("The specified line item ID must belong to the given subscription").
			WithReportableDetails(map[string]interface{}{"line_item_id": lineItemID, "subscription_id": sub.ID}).
			Mark(ierr.ErrValidation)
	}

	if lineItem.Status != types.StatusPublished {
		return nil, ierr.NewError("line item is not active").
			WithHint("Only published line items can be changed").
			WithReportableDetails(map[string]interface{}{"line_item_id": lineItemID}).
			Mark(ierr.ErrValidation)
	}

	if lineItem.PriceType != types.PRICE_TYPE_FIXED {
		return nil, ierr.NewError("line item is not a fixed-price item").
			WithHint("Line item changes are only supported for fixed-price line items; reprice usage charges through the line item update endpoint").
			WithReportableDetails(map[string]interface{}{"line_item_id": lineItemID, "price_type": lineItem.PriceType}).
			Mark(ierr.ErrValidation)
	}

	alreadyApplied := allowAlreadyApplied && !lineItem.EndDate.IsZero() && lineItem.EndDate.Equal(effectiveDate)
	if !alreadyApplied {
		if err := validateChangeEffectiveDateWithinLineItemWindow(effectiveDate, sub, lineItem, lineItemID); err != nil {
			return nil, err
		}
	}

	oldPrice, err := NewPriceService(sp).GetPrice(ctx, lineItem.PriceID)
	if err != nil {
		return nil, err
	}

	if updatedQuantity != nil && updatedQuantity.Equal(lineItem.Quantity) {
		updatedQuantity = nil
	}

	var newPriceReq *dto.CreatePriceRequest
	if change.PriceChange() {
		if err := ValidatePriceChange(oldPrice, lineItemID); err != nil {
			return nil, err
		}

		override := change.ToOverrideLineItemRequest(lineItem.PriceID)
		priceMap := map[string]*dto.PriceResponse{lineItem.PriceID: oldPrice}
		lineItemsByPriceID := map[string]*subscription.SubscriptionLineItem{lineItem.PriceID: lineItem}
		if err := override.Validate(priceMap, lineItemsByPriceID, sub.PlanID); err != nil {
			return nil, err
		}

		newPriceReq, err = buildOverridePriceRequest(oldPrice, override, sub.ID)
		if err != nil {
			return nil, err
		}
	}

	if updatedQuantity == nil && newPriceReq == nil {
		sp.Logger.Debug(ctx, "skipping line item change: nothing changed", "line_item_id", lineItemID)
		return nil, nil
	}

	newEndDate := sub.CurrentPeriodEnd
	if !lineItem.EndDate.IsZero() && lineItem.EndDate.After(effectiveDate) {
		newEndDate = lineItem.EndDate
	}

	return newLineItemChangeMod(
		lineItemID, updatedQuantity, newPriceReq, change, effectiveDate, lineItem, oldPrice, newEndDate,
	), nil
}

func ValidatePriceChange(p *dto.PriceResponse, lineItemID string) error {
	if p.BillingModel != types.BILLING_MODEL_FLAT_FEE {
		return ierr.NewError("price cannot be changed for this billing model").
			WithHint("Only flat fee charges can be repriced today; package and tiered support is planned").
			WithReportableDetails(map[string]interface{}{
				"line_item_id":  lineItemID,
				"billing_model": p.BillingModel,
			}).
			Mark(ierr.ErrValidation)
	}

	if p.PriceUnitType == types.PRICE_UNIT_TYPE_CUSTOM {
		return ierr.NewError("price cannot be changed for a custom price unit").
			WithHint("Charges priced in a custom unit are not supported yet").
			WithReportableDetails(map[string]interface{}{
				"line_item_id":    lineItemID,
				"price_unit_type": p.PriceUnitType,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (s *subscriptionModificationService) buildLineItemChangeRequest(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyLineItemChangeRequest,
) (*lineItemChangeRequest, error) {
	if params == nil {
		return nil, ierr.NewError("line_item_change_params is required").
			Mark(ierr.ErrValidation)
	}

	sub, err := loadActiveSubscription(ctx, s.serviceParams, subscriptionID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	mods := make([]*lineItemChangeMod, 0, len(params.LineItems))

	for i := range params.LineItems {
		change := &params.LineItems[i]

		effectiveDate := now
		if change.EffectiveDate != nil {
			effectiveDate = change.EffectiveDate.UTC()
		}
		if err := validateEffectiveDateWithinCurrentPeriod(effectiveDate, sub); err != nil {
			return nil, err
		}

		mod, err := s.resolveLineItemChangeMod(ctx, sub, *change, effectiveDate, false)
		if err != nil {
			return nil, err
		}
		if mod == nil {
			continue
		}
		mods = append(mods, mod)
	}

	return NewLineItemChangeRequest(sub, mods), nil
}

// conver checkout session saved line item change params to line item change request
func (s *subscriptionModificationService) requestFromLineItemChangeParams(
	ctx context.Context,
	params *types.ModifySubscriptionParams,
) (*lineItemChangeRequest, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	sub, err := loadActiveSubscription(ctx, s.serviceParams, params.SubscriptionID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	mods := make([]*lineItemChangeMod, 0, len(params.LineItemModifications))

	for _, m := range params.LineItemModifications {
		effectiveDate := now
		if m.EffectiveDate != nil {
			effectiveDate = m.EffectiveDate.UTC()
		}

		change := dto.LineItemChange{ID: m.LineItemID, Quantity: m.Quantity, Amount: m.Amount}

		mod, err := s.resolveLineItemChangeMod(ctx, sub, change, effectiveDate, true)
		if err != nil {
			return nil, err
		}
		if mod == nil {
			continue
		}
		mods = append(mods, mod)
	}

	return NewLineItemChangeRequest(sub, mods), nil
}

func validateEffectiveDateWithinCurrentPeriod(effectiveDate time.Time, sub *subscription.Subscription) error {
	if effectiveDate.Before(sub.CurrentPeriodStart) {
		return ierr.NewError("effective_date cannot be before the current period start").
			WithHint("Set effective_date to a time within the current billing period").
			WithReportableDetails(map[string]interface{}{
				"effective_date":       effectiveDate,
				"current_period_start": sub.CurrentPeriodStart,
			}).
			Mark(ierr.ErrValidation)
	}

	if !effectiveDate.Before(sub.CurrentPeriodEnd) {
		return ierr.NewError("effective_date must be before the current period end").
			WithHint("Set effective_date to a time within the current billing period").
			WithReportableDetails(map[string]interface{}{
				"effective_date":     effectiveDate,
				"current_period_end": sub.CurrentPeriodEnd,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func changedLineItemPair(
	ended *subscription.SubscriptionLineItem,
	created *subscription.SubscriptionLineItem,
	effectiveDate time.Time,
	createdEndDate time.Time,
) []dto.ChangedLineItem {
	oldStart := ended.StartDate
	oldEnd := effectiveDate
	newStart := effectiveDate

	return []dto.ChangedLineItem{
		{
			ID:           ended.ID,
			PriceID:      ended.PriceID,
			Quantity:     ended.Quantity,
			StartDate:    &oldStart,
			EndDate:      &oldEnd,
			ChangeAction: dto.ChangedLineItemActionEnded,
		},
		{
			ID:           created.ID,
			PriceID:      created.PriceID,
			Quantity:     created.Quantity,
			StartDate:    &newStart,
			EndDate:      &createdEndDate,
			ChangeAction: dto.ChangedLineItemActionCreated,
		},
	}
}

// to params that are peristed in checkout config to know the modifcations to apply post checkout
func (r *lineItemChangeRequest) toModifySubscriptionParams() *types.ModifySubscriptionParams {
	if r == nil {
		return nil
	}

	mods := r.GetModifications()
	lineMods := make([]types.ModifySubscriptionLineItem, 0, len(mods))
	for _, m := range mods {
		if m == nil {
			continue
		}

		ed := m.getEffectiveDate()
		lineMods = append(lineMods, types.ModifySubscriptionLineItem{
			LineItemID:    m.getLineItemID(),
			Quantity:      m.getUpdatedQuantity(),
			Amount:        m.getAmount(),
			EffectiveDate: &ed,
		})
	}

	return &types.ModifySubscriptionParams{
		SubscriptionID:        r.GetSubscriptionID(),
		ModifyType:            types.ModifySubscriptionTypeLineItemChange,
		LineItemModifications: lineMods,
	}
}

func (r *lineItemChangeRequest) previewChangedLineItems() []dto.ChangedLineItem {
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

		ended := subscription.NewSubscriptionLineItemBuilder(old).
			WithID(previewEndedLineItemID).
			WithEndDate(effectiveDate).
			Build()
		created := subscription.NewSubscriptionLineItemBuilder(ended).
			WithID(previewCreatedID).
			WithQuantity(m.getTargetQuantity()).
			WithStartDate(effectiveDate).
			Build()
		if m.hasPriceChange() {
			created.PriceID = previewPriceID
		}

		out = append(out, changedLineItemPair(ended, created, effectiveDate, m.getNewEndDate())...)
	}
	return out
}

func (s *subscriptionModificationService) applyLineItemChange(
	ctx context.Context,
	request *lineItemChangeRequest,
) ([]dto.ChangedLineItem, error) {
	if request == nil {
		return nil, ierr.NewError("line item change request is required").
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
		priceSvc := NewPriceService(sp)

		for _, mod := range mods {
			if mod == nil {
				continue
			}
			effectiveDate := mod.getEffectiveDate()
			lineItemID := mod.getLineItemID()

			lineItem, err := sp.SubscriptionLineItemRepo.GetForUpdate(txCtx, lineItemID)
			if err != nil {
				return err
			}

			if !lineItem.EndDate.IsZero() && !lineItem.EndDate.After(effectiveDate) {
				if lineItem.EndDate.Equal(effectiveDate) {
					sp.Logger.Debug(txCtx, "skipping line item change apply: already applied",
						"line_item_id", lineItemID, "effective_date", effectiveDate)
					continue
				}
				return ierr.NewError("line item already ended before effective_date").
					WithHint("Cannot apply a line item change to a line item that ended before the effective date").
					WithReportableDetails(map[string]any{
						"line_item_id":   lineItemID,
						"line_item_end":  lineItem.EndDate,
						"effective_date": effectiveDate,
					}).
					Mark(ierr.ErrValidation)
			}

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

			builder := subscription.NewSubscriptionLineItemBuilder(endedItem).
				WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM)).
				WithQuantity(mod.getTargetQuantity()).
				WithStartDate(effectiveDate).
				WithEndDate(newItemEndDate).
				WithBaseModel(types.GetDefaultBaseModel(txCtx))

			if priceReq := mod.getNewPriceRequest(); priceReq != nil {
				newPrice, err := priceSvc.CreatePrice(txCtx, *priceReq)
				if err != nil {
					return err
				}
				builder = builder.WithPrice(newPrice.Price)
			}

			newItem := builder.Build()

			setPredecessorLineItemID(newItem, lineItemID)
			clearSuccessorLineItemID(newItem)

			if err := sp.SubscriptionLineItemRepo.Create(txCtx, newItem); err != nil {
				return err
			}

			setSuccessorLineItemID(endedItem, newItem.ID)
			if err := sp.SubscriptionLineItemRepo.Update(txCtx, endedItem); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to link the superseded line item to its successor").
					Mark(ierr.ErrDatabase)
			}

			changedLineItems = append(changedLineItems,
				changedLineItemPair(endedItem, newItem, effectiveDate, mod.getNewEndDate())...)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return changedLineItems, nil
}

// the replay method when checkout succeeds to persist the line item modifications
func (s *subscriptionModificationService) applyModifySubscriptionParams(
	ctx context.Context,
	params *types.ModifySubscriptionParams,
) error {
	if params != nil && params.ModifyType == types.ModifySubscriptionTypeLineItemChange {
		request, err := s.requestFromLineItemChangeParams(ctx, params)
		if err != nil {
			return err
		}
		_, err = s.applyLineItemChange(ctx, request)
		return err
	}

	request, err := s.requestFromModifySubscriptionParams(ctx, params)
	if err != nil {
		return err
	}
	_, err = s.applyQuantityChange(ctx, request)
	return err
}

func (s *subscriptionModificationService) quoteLineItemChange(
	ctx context.Context,
	request *lineItemChangeRequest,
) (*SettleProrationRequest, error) {
	sub := request.GetSubscription()
	if sub == nil {
		return nil, ierr.NewError("line item change request has no subscription").
			Mark(ierr.ErrValidation)
	}

	prorationSvc := NewLineItemProrationService(s.serviceParams)
	summary := emptyProrationSummary(sub)
	entries := make([]LineItemProrationEntry, 0, len(request.GetModifications()))
	combinedPeriodStart := sub.CurrentPeriodEnd

	for _, mod := range request.GetModifications() {
		if mod == nil {
			continue
		}

		oldLineItem, oldPrice := mod.getOldLineItem(), mod.getOldPrice()
		if oldLineItem == nil || oldPrice == nil {
			return nil, ierr.NewError("line item change modification is not resolved").
				WithHint("Build the line item change request before quoting it").
				Mark(ierr.ErrValidation)
		}

		entry := LineItemProrationEntry{
			LineItem:        oldLineItem,
			Action:          types.ProrationActionQuantityChange,
			CurrentPrice:    oldPrice.Price,
			CurrentQuantity: oldLineItem.Quantity,
			NewPrice:        oldPrice.Price,
			NewQuantity:     mod.getTargetQuantity(),
		}
		if priceReq := mod.getNewPriceRequest(); priceReq != nil {
			newPrice, err := priceReq.ToPrice(ctx)
			if err != nil {
				return nil, err
			}
			entry.Action = types.ProrationActionPriceChange
			entry.NewPrice = newPrice
		}

		effectiveDate := mod.getEffectiveDate()
		computed, err := prorationSvc.Compute(ctx, LineItemProrationRequest{
			Subscription:  sub,
			Entries:       []LineItemProrationEntry{entry},
			EffectiveDate: effectiveDate,
			Behavior:      types.ProrationBehaviorCreateProrations,
			Reason:        lineItemChangeReason,
		})
		if err != nil {
			return nil, err
		}

		summary.Merge(computed)
		entries = append(entries, entry)
		if effectiveDate.Before(combinedPeriodStart) {
			combinedPeriodStart = effectiveDate
		}
	}

	idempotencyKey := prorationChargeInvoiceKey(LineItemProrationRequest{
		Subscription:  sub,
		Entries:       entries,
		EffectiveDate: combinedPeriodStart,
	})

	req := NewSettleProrationRequest(
		sub,
		summary,
		combinedPeriodStart,
		sub.CurrentPeriodEnd,
		lineItemChangeDisplayName,
		idempotencyKey,
		SettleModePreview,
	)
	req.Reason = lineItemChangeReason

	return req, nil
}

func (s *subscriptionModificationService) settleLineItemChangePayLater(
	ctx context.Context,
	request *lineItemChangeRequest,
	settleReq *SettleProrationRequest,
) ([]dto.ChangedLineItem, []dto.ChangedInvoice, error) {
	sp := s.serviceParams
	sub := request.GetSubscription()
	settleReq.Mode = SettleModeIssue

	var (
		changedLineItems []dto.ChangedLineItem
		settled          *SettleProrationResult
	)

	err := sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		locked, err := sp.SubRepo.GetForUpdate(txCtx, sub.ID)
		if err != nil {
			return err
		}
		if locked.SubscriptionStatus != types.SubscriptionStatusActive {
			return ierr.NewError("subscription is not active").
				WithHint("Only active subscriptions can be modified").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": sub.ID,
					"status":          locked.SubscriptionStatus,
				}).
				Mark(ierr.ErrValidation)
		}

		if changedLineItems, err = s.applyLineItemChange(txCtx, request); err != nil {
			return err
		}

		settled, err = NewLineItemProrationService(sp).Settle(txCtx, settleReq)
		return err
	})
	if err != nil {
		return nil, nil, err
	}

	attemptProrationPayments(ctx, sp, settled.GetChanged())

	return changedLineItems, settled.GetChanged(), nil
}

func (s *subscriptionModificationService) settleLineItemChangePayFirst(
	ctx context.Context,
	request *lineItemChangeRequest,
	settleReq *SettleProrationRequest,
	checkout *dto.CheckoutParams,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams
	sub := request.GetSubscription()
	settleReq.Mode = SettleModeDraft

	modifyParams := request.toModifySubscriptionParams()
	if err := modifyParams.Validate(); err != nil {
		return nil, err
	}

	var draft *dto.InvoiceResponse

	err := sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		locked, err := sp.SubRepo.GetForUpdate(txCtx, sub.ID)
		if err != nil {
			return err
		}
		if locked.SubscriptionStatus != types.SubscriptionStatusActive {
			return ierr.NewError("subscription is not active").
				WithHint("Only active subscriptions can be modified").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": sub.ID,
					"status":          locked.SubscriptionStatus,
				}).
				Mark(ierr.ErrValidation)
		}

		if err := ensureNoPendingCheckoutSession(txCtx, sp, locked.CustomerID, locked.ID); err != nil {
			return err
		}

		settled, err := NewLineItemProrationService(sp).Settle(txCtx, settleReq)
		if err != nil {
			return err
		}
		draft = settled.GetDraft()
		return nil
	})
	if err != nil {
		return nil, err
	}

	session, err := NewCheckoutSessionService(sp).StartPayFirstCheckoutSession(ctx, &dto.PayFirstCheckoutRequest{
		CustomerID: sub.CustomerID,
		Action:     types.CheckoutActionModifySubscription,
		Configuration: types.CheckoutConfiguration{
			ModifySubscriptionParams: modifyParams,
		},
		DraftInvoice: &draft.Invoice,
		Checkout:     checkout,
	})
	if err != nil {
		s.archiveLineItemChangeDraft(ctx, draft, err)
		return nil, err
	}

	subResp, err := NewSubscriptionService(sp).GetSubscription(ctx, sub.ID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			LineItems: request.previewChangedLineItems(),
			Invoices: []dto.ChangedInvoice{{
				ID:      draft.ID,
				Action:  dto.ChangedInvoiceActionCreated,
				Status:  dto.ChangedInvoiceStatusFromPaymentStatus(draft.PaymentStatus),
				Invoice: draft,
			}},
		},
		CheckoutSession: session,
	}, nil
}

func (s *subscriptionModificationService) archiveLineItemChangeDraft(
	ctx context.Context,
	draft *dto.InvoiceResponse,
	cause error,
) {
	if draft == nil {
		return
	}

	if err := s.serviceParams.InvoiceRepo.Delete(ctx, draft.ID); err != nil {
		s.serviceParams.Logger.Error(ctx, "failed to archive draft invoice after pay-first failure",
			"error", err,
			"invoice_id", draft.ID,
			"original_error", cause,
		)
	}
}
