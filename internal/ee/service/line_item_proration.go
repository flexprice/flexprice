package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// The credited side (Current*) is set by every action that credits, the charged side (New*)
// by every action that charges. An action that only does one carries nil on the other.
type LineItemProrationEntry struct {
	LineItem *subscription.SubscriptionLineItem
	Action   types.ProrationAction

	CurrentPrice    *price.Price
	CurrentQuantity decimal.Decimal
	NewPrice        *price.Price
	NewQuantity     decimal.Decimal
}

type LineItemProrationRequest struct {
	Subscription   *subscription.Subscription
	Entries        []LineItemProrationEntry
	EffectiveDate  time.Time
	Behavior       types.ProrationBehavior
	Reason         string // shown in wallet credit description
	IdempotencyKey string

	// Coupons discount the matching charge lines before credits are netted.
	Coupons []dto.InvoiceLineItemCoupon
}

type LineItemProrationSummary struct {
	ChargeLineItems   []dto.CreateInvoiceLineItemRequest
	TotalChargeAmount decimal.Decimal

	// Negative amounts. Netting callers put these on the charge invoice; Apply
	// ignores them and pays TotalCreditAmount to the wallet instead.
	CreditLineItems   []dto.CreateInvoiceLineItemRequest
	TotalCreditAmount decimal.Decimal

	// Coupons matched to charge lines and their discount, netted before credits.
	LineItemCoupons     []dto.InvoiceLineItemCoupon
	TotalDiscountAmount decimal.Decimal

	Currency  string
	IsPreview bool
}

// emptyProrationSummary is the zero quote for callers that resolved to "nothing to prorate".
func emptyProrationSummary(sub *subscription.Subscription) *LineItemProrationSummary {
	summary := &LineItemProrationSummary{
		TotalChargeAmount:   decimal.Zero,
		TotalCreditAmount:   decimal.Zero,
		TotalDiscountAmount: decimal.Zero,
	}

	if sub != nil {
		summary.Currency = sub.Currency
	}

	return summary
}

func (s *LineItemProrationSummary) Merge(others ...*LineItemProrationSummary) *LineItemProrationSummary {
	for _, other := range others {
		if other == nil {
			continue
		}

		s.ChargeLineItems = append(s.ChargeLineItems, other.ChargeLineItems...)
		s.CreditLineItems = append(s.CreditLineItems, other.CreditLineItems...)
		s.TotalChargeAmount = s.TotalChargeAmount.Add(other.TotalChargeAmount)
		s.TotalCreditAmount = s.TotalCreditAmount.Add(other.TotalCreditAmount)
		s.LineItemCoupons = append(s.LineItemCoupons, other.LineItemCoupons...)
		s.TotalDiscountAmount = s.TotalDiscountAmount.Add(other.TotalDiscountAmount)
	}

	return s
}

// Subtotal is charges minus credits, before any discount.
func (s *LineItemProrationSummary) Subtotal() decimal.Decimal {
	if s == nil {
		return decimal.Zero
	}
	return s.TotalChargeAmount.Sub(s.TotalCreditAmount)
}

func (s *LineItemProrationSummary) NetAmount() decimal.Decimal {
	return s.Subtotal().Sub(s.getTotalDiscountAmount())
}

func (s *LineItemProrationSummary) getTotalDiscountAmount() decimal.Decimal {
	if s == nil {
		return decimal.Zero
	}
	return s.TotalDiscountAmount
}

type SettleMode int

const (
	SettleModePreview SettleMode = iota // quotes, writes nothing
	SettleModeIssue                     // real invoice / real wallet top-up
	SettleModeDraft                     // pay-first: a DRAFT invoice to collect against
)

type SettleProrationRequest struct {
	Subscription *subscription.Subscription
	Quote        *LineItemProrationSummary

	PeriodStart time.Time
	PeriodEnd   time.Time

	DisplayName   string
	BillingPeriod types.BillingPeriod

	IdempotencyKey string
	Reason         string
	Mode           SettleMode
}

func NewSettleProrationRequest(
	sub *subscription.Subscription,
	quote *LineItemProrationSummary,
	periodStart time.Time,
	periodEnd time.Time,
	displayName string,
	idempotencyKey string,
	mode SettleMode,
) *SettleProrationRequest {
	return &SettleProrationRequest{
		Subscription:   sub,
		Quote:          quote,
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
		DisplayName:    displayName,
		IdempotencyKey: idempotencyKey,
		Mode:           mode,
	}
}

func (r *SettleProrationRequest) validate() error {
	if r == nil {
		return ierr.NewError("settlement request is required").Mark(ierr.ErrValidation)
	}
	if r.Subscription == nil {
		return ierr.NewError("settlement subscription is required").
			WithHint("A proration document must belong to a subscription").
			Mark(ierr.ErrValidation)
	}
	if r.Quote == nil {
		return ierr.NewError("settlement quote is required").
			WithHint("Compute the proration before settling it").
			Mark(ierr.ErrValidation)
	}
	if r.DisplayName == "" {
		return ierr.NewError("settlement display name is required").
			WithHint("Every proration document must be titled by its caller").
			Mark(ierr.ErrValidation)
	}

	switch r.Mode {
	case SettleModePreview, SettleModeIssue, SettleModeDraft:
	default:
		return ierr.NewError("unknown settle mode").
			WithHint("Settle mode must be preview, issue or draft").
			WithReportableDetails(map[string]any{"mode": int(r.Mode)}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

type SettleProrationResult struct {
	Changed []dto.ChangedInvoice
	Draft   *dto.InvoiceResponse
}

func (r *SettleProrationResult) GetChanged() []dto.ChangedInvoice {
	if r == nil {
		return nil
	}
	return r.Changed
}

func (r *SettleProrationResult) GetDraft() *dto.InvoiceResponse {
	if r == nil {
		return nil
	}
	return r.Draft
}

type LineItemProrationService interface {
	Compute(ctx context.Context, req LineItemProrationRequest) (*LineItemProrationSummary, error)

	Settle(ctx context.Context, req *SettleProrationRequest) (*SettleProrationResult, error)
}

type lineItemProrationService struct {
	params       ServiceParams
	priceService PriceService
}

func NewLineItemProrationService(params ServiceParams) LineItemProrationService {
	return &lineItemProrationService{params: params, priceService: NewPriceService(params)}
}

func (s *lineItemProrationService) Compute(ctx context.Context, req LineItemProrationRequest) (*LineItemProrationSummary, error) {
	sub := req.Subscription
	prorationSvc := NewProrationService(s.params)

	summary := &LineItemProrationSummary{
		Currency:            sub.Currency,
		IsPreview:           req.Behavior == types.ProrationBehaviorNone,
		TotalChargeAmount:   decimal.Zero,
		TotalCreditAmount:   decimal.Zero,
		TotalDiscountAmount: decimal.Zero,
	}

	for _, entry := range req.Entries {
		item := entry.LineItem

		if item.PriceType == types.PRICE_TYPE_USAGE {
			continue
		}

		// Onetime addons (EndDate set) are non-refundable.
		if entry.Action == types.ProrationActionRemoveItem && !item.EndDate.IsZero() {
			continue
		}

		// Arrear items are billed by the regular invoice at the end of each period. Charging
		// for them here would bill the same period twice, since the period-end invoice only
		// deduplicates against other subscription invoices, not against this one-off.
		if entry.Action == types.ProrationActionAddItem && entry.NewPrice.InvoiceCadence == types.InvoiceCadenceArrear {
			continue
		}

		// One-time prices are never prorated: an advance one is charged in full when added.
		if item.BillingPeriod == types.BILLING_PERIOD_ONETIME {
			if entry.Action == types.ProrationActionAddItem {
				amount := s.priceService.CalculateCost(ctx, entry.NewPrice, entry.NewQuantity)
				if amount.IsPositive() {
					summary.ChargeLineItems = append(summary.ChargeLineItems, buildProrationLineItem(
						sub, item, entry.NewPrice, entry.NewQuantity, amount, "One-time charge", req.EffectiveDate, req.EffectiveDate,
					))
					summary.TotalChargeAmount = summary.TotalChargeAmount.Add(amount)
				}
			}
			continue
		}

		// A line item is priced against its own cadence, so a monthly addon on a quarterly
		// subscription is quoted as one partial month plus the whole months that follow —
		// the same lines the opening invoice would have raised. Same-cadence items yield a
		// single window equal to the subscription period, which is the previous behaviour.
		// A longer-cadence item has one window: its own period containing the change, on its own grid.
		// e.g. annual item on a monthly sub, removed Jun 10 → credit [Jun 10, Jan 1) of [Jan 1, Jan 1).
		gridSub := sub
		var windows []periodWindow
		if types.IsLongerCadence(item.BillingPeriod, item.BillingPeriodCount, sub.BillingPeriod, sub.BillingPeriodCount) {
			// A removal on an item boundary closes the period ending there, not the next unbilled one.
			at := req.EffectiveDate
			if entry.Action == types.ProrationActionRemoveItem || entry.Action == types.ProrationActionCancellation {
				at = at.Add(-time.Nanosecond)
			}
			itemPeriod, err := longerItemPeriod(sub, item, at)
			if err != nil {
				return nil, err
			}
			itemSub := *sub
			itemSub.BillingAnchor = longerItemAnchor(sub, item)
			gridSub = &itemSub
			windows = []periodWindow{{Start: itemPeriod.Start, End: itemPeriod.End}}
		} else {
			var err error
			windows, err = splitInvoicePeriodByLineItemCadence(sub.CurrentPeriodStart, sub.CurrentPeriodEnd, item, sub)
			if err != nil {
				return nil, err
			}
		}

		creditedEarlier := decimal.Zero
		for _, w := range windows {
			if !w.End.After(req.EffectiveDate) {
				continue
			}
			// Skip items ended before the cancel; stop at a period-end removal.
			if entry.Action == types.ProrationActionCancellation && !item.EndDate.IsZero() {
				if !item.EndDate.After(req.EffectiveDate) {
					continue
				}
				if !item.EndDate.Before(sub.CurrentPeriodEnd) && item.EndDate.Before(w.End) {
					w.End = item.EndDate
				}
			}

			originalPaid, creditsIssued := s.creditBasisForWindow(ctx, req, entry, w)
			// A merged invoice row spans every window, so earlier windows' credits draw on it too.
			// e.g. one $30 row for a monthly item's quarter: windows credit 13.33 + 16.67 + 0, not 3 × up to $30.
			if sub.LineItemGrouping.MergesIntoBillingPeriod() {
				creditsIssued = creditsIssued.Add(creditedEarlier)
			}
			params, skip := s.buildProrationParams(ctx, gridSub, entry, req, w, originalPaid, creditsIssued)
			if skip {
				continue
			}

			result, err := prorationSvc.CalculateProration(ctx, params)
			if err != nil {
				return nil, err
			}
			if result == nil {
				continue
			}

			if result.NetAmount.GreaterThan(decimal.Zero) {
				lineItem := buildProrationLineItem(
					sub,
					entry.LineItem,
					entry.NewPrice,
					entry.NewQuantity,
					result.NetAmount,
					"Proration charge",
					params.ProrationDate,
					w.End,
				)

				summary.ChargeLineItems = append(summary.ChargeLineItems, lineItem)
				summary.TotalChargeAmount = summary.TotalChargeAmount.Add(result.NetAmount)
			} else if result.NetAmount.LessThan(decimal.Zero) {
				lineItem := buildProrationLineItem(
					sub,
					entry.LineItem,
					entry.CurrentPrice,
					entry.CurrentQuantity,
					result.NetAmount,
					"Proration credit",
					params.ProrationDate,
					w.End,
				)

				summary.CreditLineItems = append(summary.CreditLineItems, lineItem)
				summary.TotalCreditAmount = summary.TotalCreditAmount.Add(result.NetAmount.Abs())
				creditedEarlier = creditedEarlier.Add(result.NetAmount.Abs())
			}
		}
	}

	if err := s.applyCoupons(ctx, summary, req.Coupons); err != nil {
		return nil, err
	}

	return summary, nil
}

func (s *lineItemProrationService) Settle(ctx context.Context, req *SettleProrationRequest) (*SettleProrationResult, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}

	invoicingSub, err := invoicingSubscription(ctx, s.params, req.Subscription)
	if err != nil {
		return nil, err
	}
	req.Subscription = invoicingSub

	result := &SettleProrationResult{Changed: make([]dto.ChangedInvoice, 0, 1)}
	net := req.Quote.NetAmount()

	if req.Mode == SettleModeDraft && !net.IsPositive() {
		return nil, ierr.NewError("no proration charge to collect via checkout").
			WithHint("Expected a positive proration charge").
			Mark(ierr.ErrValidation)
	}

	switch {
	case net.IsPositive():
		invoiceReq := buildNettedProrationInvoiceRequest(req)

		switch req.Mode {
		case SettleModePreview:
			inv, err := NewInvoiceService(s.params).CreatePreviewInvoice(ctx, invoiceReq)
			if err != nil {
				return nil, err
			}
			result.Changed = append(result.Changed, dto.ChangedInvoice{
				ID:      previewInvoiceID,
				Action:  dto.ChangedInvoiceActionCreated,
				Status:  dto.ChangedInvoiceStatusPreview,
				Invoice: inv,
			})

		case SettleModeDraft:
			invoiceReq.SourceType = types.InvoiceSourceTypeCheckout
			inv, skipped, err := NewInvoiceService(s.params).CreateComputedDraftInvoice(ctx, invoiceReq)
			if err != nil {
				return nil, err
			}
			if skipped {
				return nil, ierr.NewError("draft invoice was skipped").
					WithHint("Expected a non-zero invoice amount").
					WithReportableDetails(map[string]any{"invoice_id": inv.GetId()}).
					Mark(ierr.ErrValidation)
			}
			result.Draft = inv

		default:
			inv, err := NewInvoiceService(s.params).CreateInvoice(ctx, invoiceReq)
			if err != nil {
				s.params.Logger.Error(ctx, "failed to create proration charge invoice", "error", err)
				return nil, err
			}
			result.Changed = append(result.Changed, dto.ChangedInvoice{
				ID:      inv.ID,
				Action:  dto.ChangedInvoiceActionCreated,
				Status:  dto.ChangedInvoiceStatusFromPaymentStatus(inv.PaymentStatus),
				Invoice: inv,
			})
		}

	case net.IsNegative():
		// No invoice means no coupon application row, so a repeated coupon does not use up a period here.
		credit, err := s.creditWallet(ctx, req, net.Abs())
		if err != nil {
			return nil, err
		}
		result.Changed = append(result.Changed, credit)
	}

	return result, nil
}

func (s *lineItemProrationService) creditWallet(
	ctx context.Context,
	req *SettleProrationRequest,
	amount decimal.Decimal,
) (dto.ChangedInvoice, error) {
	sub := req.Subscription

	if req.Mode == SettleModePreview {
		credit := walletCreditChangedInvoice(&dto.WalletTransactionResponse{
			Transaction: &wallet.Transaction{
				CustomerID:        sub.GetInvoicingCustomerID(),
				Amount:            amount,
				Currency:          sub.Currency,
				TransactionReason: types.TransactionReasonSubscriptionCredit,
			},
		}, dto.ChangedInvoiceStatusPreview)
		credit.ID = previewWalletCreditID
		return credit, nil
	}

	walletTx, err := NewWalletService(s.params).TopUpWalletForProratedCharge(
		ctx, sub.GetInvoicingCustomerID(), amount, sub.Currency, req.IdempotencyKey,
	)
	if err != nil {
		s.params.Logger.Error(ctx, "failed to issue wallet credit for proration", "error", err)
		return dto.ChangedInvoice{}, err
	}

	return walletCreditChangedInvoice(walletTx, dto.ChangedInvoiceStatusWalletIssued), nil
}

func buildNettedProrationInvoiceRequest(req *SettleProrationRequest) dto.CreateInvoiceRequest {
	sub, quote := req.Subscription, req.Quote

	lineItems := make([]dto.CreateInvoiceLineItemRequest, 0,
		len(quote.ChargeLineItems)+len(quote.CreditLineItems))
	lineItems = append(lineItems, quote.ChargeLineItems...)
	lineItems = append(lineItems, quote.CreditLineItems...)

	billingPeriod := string(req.BillingPeriod)
	if billingPeriod == "" {
		billingPeriod = string(sub.BillingPeriod)
	}
	// Pre-discount: invoice compute subtracts the coupons itself.
	subtotal := quote.Subtotal()
	periodStart, periodEnd := req.PeriodStart, req.PeriodEnd

	return dto.CreateInvoiceRequest{
		CustomerID:      sub.GetInvoicingCustomerID(),
		SubscriptionID:  &sub.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		Currency:        sub.Currency,
		BillingReason:   types.InvoiceBillingReasonSubscriptionUpdate,
		AmountDue:       subtotal,
		Total:           subtotal,
		Subtotal:        subtotal,
		PeriodStart:     &periodStart,
		PeriodEnd:       &periodEnd,
		BillingPeriod:   &billingPeriod,
		LineItems:       lineItems,
		LineItemCoupons: quote.LineItemCoupons,
		IdempotencyKey:  &req.IdempotencyKey,
		Metadata:        types.WithCollapsedInvoiceDisplayName(nil, req.DisplayName),
	}
}

func (s *lineItemProrationService) applyCoupons(
	ctx context.Context,
	quote *LineItemProrationSummary,
	coupons []dto.InvoiceLineItemCoupon,
) error {
	// Only charge lines are discounted; a credit line never carries a coupon.
	chargedItemIDs := lo.FilterMap(quote.ChargeLineItems, func(li dto.CreateInvoiceLineItemRequest, _ int) (string, bool) {
		return lo.FromPtr(li.SubscriptionLineItemID), li.SubscriptionLineItemID != nil
	})
	coupons = lo.Filter(coupons, func(c dto.InvoiceLineItemCoupon, _ int) bool {
		return lo.Contains(chargedItemIDs, lo.FromPtr(c.SubscriptionLineItemID))
	})
	if len(coupons) == 0 {
		return nil
	}

	inv, err := (&dto.CreateInvoiceRequest{Currency: quote.Currency, LineItems: quote.ChargeLineItems}).ToInvoice(ctx)
	if err != nil {
		return err
	}

	result, err := NewCouponApplicationService(s.params).CalculateCouponsForInvoice(ctx, dto.ApplyCouponsToInvoiceRequest{
		Invoice:         inv,
		LineItemCoupons: coupons,
	})
	if err != nil {
		return err
	}

	quote.LineItemCoupons = coupons
	quote.TotalDiscountAmount = result.TotalDiscountAmount
	return nil
}

// creditBasisForWindow caps a window's credit at what was billed for that window.
// e.g. monthly item on a quarterly sub removed Apr 11: Apr reads April's $20 row, May its own, June its own.
func (s *lineItemProrationService) creditBasisForWindow(
	ctx context.Context,
	req LineItemProrationRequest,
	entry LineItemProrationEntry,
	window periodWindow,
) (originalAmountPaid, previousCredits decimal.Decimal) {
	listTotal := s.priceService.CalculateCost(ctx, entry.CurrentPrice, entry.LineItem.Quantity)

	switch entry.Action {
	case types.ProrationActionRemoveItem, types.ProrationActionPriceChange, types.ProrationActionCancellation:
	default:
		return listTotal, decimal.Zero
	}

	asOf := req.EffectiveDate
	if window.Start.After(asOf) {
		asOf = window.Start
	}

	billed, err := s.params.InvoiceLineItemRepo.GetBilledAmountsBySubscriptionLineItem(
		ctx, []string{entry.LineItem.ID}, asOf,
	)
	if err != nil {
		s.params.Logger.Info(ctx, "failed to read billed amounts for credit basis, falling back to list price",
			"error", err,
			"subscription_id", req.Subscription.ID,
			"line_item_id", entry.LineItem.ID)
	}

	return creditBasis(entry.LineItem, billed, listTotal)
}

func (s *lineItemProrationService) buildProrationParams(
	ctx context.Context,
	sub *subscription.Subscription,
	entry LineItemProrationEntry,
	req LineItemProrationRequest,
	window periodWindow,
	originalPaid decimal.Decimal,
	creditsIssued decimal.Decimal,
) (proration.ProrationParams, bool) {
	item := entry.LineItem

	// Windows that start after the change are charged or credited in full.
	prorationDate := req.EffectiveDate
	if window.Start.After(prorationDate) {
		prorationDate = window.Start
	}

	base := proration.ProrationParams{
		Subscription:       sub,
		BillingPeriod:      item.BillingPeriod,
		BillingPeriodCount: item.BillingPeriodCount,
		SubscriptionID:     sub.ID,
		LineItemID:         item.ID,
		CurrentPeriodStart: window.Start,
		CurrentPeriodEnd:   window.End,
		ProrationDate:      prorationDate,
		ProrationBehavior:  req.Behavior,
		Currency:           sub.Currency,
		PlanDisplayName:    item.DisplayName,
	}

	switch entry.Action {
	case types.ProrationActionAddItem:
		base.Action = types.ProrationActionAddItem
		base.PlanPayInAdvance = entry.NewPrice.InvoiceCadence == types.InvoiceCadenceAdvance
		base.NewPriceID = item.PriceID
		base.NewQuantity = entry.NewQuantity
		base.NewPricePerUnit = s.priceService.CalculateUnitCost(ctx, entry.NewPrice, entry.NewQuantity)

	case types.ProrationActionRemoveItem, types.ProrationActionCancellation:
		base.Action = entry.Action
		base.PlanPayInAdvance = entry.CurrentPrice.InvoiceCadence == types.InvoiceCadenceAdvance
		base.OldPriceID = item.PriceID
		base.OldQuantity = entry.CurrentQuantity
		base.OldPricePerUnit = s.priceService.CalculateUnitCost(ctx, entry.CurrentPrice, entry.CurrentQuantity)
		base.CancellationType = types.CancellationTypeImmediate
		base.CancellationReason = req.Reason
		base.RefundEligible = true
		base.OriginalAmountPaid, base.PreviousCreditsIssued = originalPaid, creditsIssued

	case types.ProrationActionQuantityChange, types.ProrationActionPriceChange:
		if entry.CurrentPrice.InvoiceCadence != types.InvoiceCadenceAdvance {
			return proration.ProrationParams{}, true
		}
		base.Action = entry.Action
		base.PlanPayInAdvance = true
		base.OldPriceID = item.PriceID
		base.NewPriceID = item.PriceID
		base.OldQuantity = entry.CurrentQuantity
		base.NewQuantity = entry.NewQuantity
		base.OldPricePerUnit = s.priceService.CalculateUnitCost(ctx, entry.CurrentPrice, entry.CurrentQuantity)
		base.NewPricePerUnit = s.priceService.CalculateUnitCost(ctx, entry.NewPrice, entry.NewQuantity)
		base.OriginalAmountPaid, base.PreviousCreditsIssued = originalPaid, creditsIssued

	default:
		return proration.ProrationParams{}, true
	}

	return base, false
}

func buildProrationLineItem(
	sub *subscription.Subscription,
	item *subscription.SubscriptionLineItem,
	p *price.Price,
	quantity decimal.Decimal,
	amount decimal.Decimal,
	label string,
	effectiveDate time.Time,
	periodEnd time.Time,
) dto.CreateInvoiceLineItemRequest {
	priceID := item.PriceID
	priceType := string(p.Type)
	subscriptionLineItemID := item.ID
	planDisplayName := item.PlanDisplayName

	displayName := fmt.Sprintf("%s — %s (%s – %s)",
		item.DisplayName,
		label,
		effectiveDate.Format("2 Jan 2006"), periodEnd.Format("2 Jan 2006"))

	description := fmt.Sprintf("%s: %s × %s %s %s/unit (%s – %s)",
		label,
		quantity.String(),
		item.DisplayName,
		strings.ToUpper(sub.Currency),
		p.Amount.String(),
		effectiveDate.Format("2 Jan 2006"), periodEnd.Format("2 Jan 2006"))

	metadata := types.Metadata{"description": description}
	if sub.SubscriptionType == types.SubscriptionTypeGroupedInvoicing {
		metadata[types.InvoiceLineItemMetadataKeyChildCustomerID] = sub.CustomerID
	}

	return dto.CreateInvoiceLineItemRequest{
		SubscriptionID:         &sub.ID,
		PriceID:                &priceID,
		PriceType:              &priceType,
		PlanDisplayName:        &planDisplayName,
		DisplayName:            &displayName,
		Amount:                 amount,
		Quantity:               quantity,
		PeriodStart:            &effectiveDate,
		PeriodEnd:              &periodEnd,
		SubscriptionLineItemID: &subscriptionLineItemID,
		Metadata:               metadata,
	}
}

func prorationChargeInvoiceKey(req LineItemProrationRequest) string {
	source := req.IdempotencyKey
	if source == "" {
		ids := make([]string, 0, len(req.Entries))
		for _, entry := range req.Entries {
			ids = append(ids, entry.LineItem.ID)
			ids = append(ids, string(entry.Action))
		}
		sort.Strings(ids)
		source = strings.Join(ids, ",")
	}

	return idempotency.NewGenerator().GenerateKey(idempotency.ScopeProrationCharge, map[string]interface{}{
		"subscription_id": req.Subscription.ID,
		"effective_date":  req.EffectiveDate.UTC().Format(time.RFC3339Nano),
		"source":          source,
	})
}

// collectableProrationInvoice reports whether an invoice in this payment status still owes
// money. Settle raises its invoices as PENDING, so the other statuses only arise when a
// caller hands back an invoice that was already paid, voided or refunded — charging one of
// those again would take money twice.
func collectableProrationInvoice(status types.PaymentStatus) bool {
	switch status {
	case types.PaymentStatusPending, types.PaymentStatusFailed:
		return true
	default:
		return false
	}
}

// attemptProrationPayments collects settled proration invoices and refreshes what changed. It
// does outbound I/O — wallet debits and a gateway charge — so it runs only after the caller's
// transaction has committed.
func attemptProrationPayments(ctx context.Context, params ServiceParams, changed []dto.ChangedInvoice) {
	invoiceSvc := NewInvoiceService(params)

	for i := range changed {
		inv := changed[i].Invoice
		if inv == nil || inv.ID == "" {
			continue
		}

		if !collectableProrationInvoice(inv.PaymentStatus) {
			continue
		}

		if err := invoiceSvc.AttemptPayment(ctx, inv.ID); err != nil {
			params.Logger.Info(ctx, "proration invoice created but payment attempt failed; invoice remains collectable",
				"error", err, "invoice_id", inv.ID)
		}

		latest, err := params.InvoiceRepo.Get(ctx, inv.ID)
		if err != nil || latest == nil {
			continue
		}

		inv.InvoiceStatus = latest.InvoiceStatus
		inv.PaymentStatus = latest.PaymentStatus
		inv.AmountPaid = latest.AmountPaid
		inv.AmountRemaining = latest.AmountRemaining
		changed[i].Status = dto.ChangedInvoiceStatusFromPaymentStatus(latest.PaymentStatus)
	}
}
