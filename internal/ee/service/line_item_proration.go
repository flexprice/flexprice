package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/types"
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
}

type LineItemProrationSummary struct {
	ChargeLineItems   []dto.CreateInvoiceLineItemRequest
	TotalChargeAmount decimal.Decimal

	// Negative amounts. Netting callers put these on the charge invoice; Apply
	// ignores them and pays TotalCreditAmount to the wallet instead.
	CreditLineItems   []dto.CreateInvoiceLineItemRequest
	TotalCreditAmount decimal.Decimal

	Currency  string
	IsPreview bool
}

// emptyProrationSummary is the zero quote for callers that resolved to "nothing to prorate".
func emptyProrationSummary(sub *subscription.Subscription) *LineItemProrationSummary {
	summary := &LineItemProrationSummary{
		TotalChargeAmount: decimal.Zero,
		TotalCreditAmount: decimal.Zero,
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
	}

	return s
}

func (s *LineItemProrationSummary) NetAmount() decimal.Decimal {
	if s == nil {
		return decimal.Zero
	}
	return s.TotalChargeAmount.Sub(s.TotalCreditAmount)
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
	params ServiceParams
}

func NewLineItemProrationService(params ServiceParams) LineItemProrationService {
	return &lineItemProrationService{params: params}
}

func (s *lineItemProrationService) Compute(ctx context.Context, req LineItemProrationRequest) (*LineItemProrationSummary, error) {
	sub := req.Subscription
	prorationSvc := NewProrationService(s.params)

	customerTimezone := sub.Timezone
	if customerTimezone == "" {
		customerTimezone = types.DefaultTimezone
	}

	summary := &LineItemProrationSummary{
		Currency:          sub.Currency,
		IsPreview:         req.Behavior == types.ProrationBehaviorNone,
		TotalChargeAmount: decimal.Zero,
		TotalCreditAmount: decimal.Zero,
	}

	billed := s.creditBasisForInvoiceLineItems(ctx, req)

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

		// A line item is priced against its own cadence, so a monthly addon on a quarterly
		// subscription is quoted as one partial month plus the whole months that follow —
		// the same lines the opening invoice would have raised. Same-cadence items yield a
		// single window equal to the subscription period, which is the previous behaviour.
		windows, err := splitInvoicePeriodByLineItemCadence(sub.CurrentPeriodStart, sub.CurrentPeriodEnd, item, sub)
		if err != nil {
			return nil, err
		}

		originalPaid, creditsIssued := creditBasis(item, entry.CurrentPrice, billed)

		for _, w := range windows {
			if !w.End.After(req.EffectiveDate) {
				continue
			}

			params, skip := s.buildProrationParams(ctx, sub, entry, req, customerTimezone, w, originalPaid, creditsIssued)
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
				// Later windows may only credit what this line item has not been credited yet.
				creditsIssued = creditsIssued.Add(result.NetAmount.Abs())
			}
		}
	}

	return summary, nil
}

func (s *lineItemProrationService) Settle(ctx context.Context, req *SettleProrationRequest) (*SettleProrationResult, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}

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
	net := quote.NetAmount()
	periodStart, periodEnd := req.PeriodStart, req.PeriodEnd

	return dto.CreateInvoiceRequest{
		CustomerID:     sub.GetInvoicingCustomerID(),
		SubscriptionID: &sub.ID,
		InvoiceType:    types.InvoiceTypeOneOff,
		Currency:       sub.Currency,
		BillingReason:  types.InvoiceBillingReasonSubscriptionUpdate,
		AmountDue:      net,
		Total:          net,
		Subtotal:       net,
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
		BillingPeriod:  &billingPeriod,
		LineItems:      lineItems,
		IdempotencyKey: &req.IdempotencyKey,
		Metadata:       types.WithCollapsedInvoiceDisplayName(nil, req.DisplayName),
	}
}

// Cap removal credits at amounts actually billed (list price never binds).
func (s *lineItemProrationService) creditBasisForInvoiceLineItems(
	ctx context.Context,
	req LineItemProrationRequest,
) map[string]*invoice.BilledAmounts {
	lineItemIDs := make([]string, 0, len(req.Entries))
	for _, entry := range req.Entries {
		if entry.LineItem == nil {
			continue
		}
		switch entry.Action {
		case types.ProrationActionRemoveItem, types.ProrationActionPriceChange:
			lineItemIDs = append(lineItemIDs, entry.LineItem.ID)
		}
	}
	if len(lineItemIDs) == 0 {
		return nil
	}

	billed, err := s.params.InvoiceLineItemRepo.GetBilledAmountsBySubscriptionLineItem(
		ctx, lineItemIDs, req.EffectiveDate,
	)
	if err != nil {
		s.params.Logger.Info(ctx, "failed to read billed amounts for credit basis, falling back to list price",
			"error", err,
			"subscription_id", req.Subscription.ID)
		return nil
	}

	return billed
}

func (s *lineItemProrationService) buildProrationParams(
	ctx context.Context,
	sub *subscription.Subscription,
	entry LineItemProrationEntry,
	req LineItemProrationRequest,
	customerTimezone string,
	window periodWindow,
	originalPaid decimal.Decimal,
	creditsIssued decimal.Decimal,
) (proration.ProrationParams, bool) {
	item := entry.LineItem
	periodEnd := window.End.Add(-time.Second)

	// Windows that start after the change are charged or credited in full.
	prorationDate := req.EffectiveDate
	if window.Start.After(prorationDate) {
		prorationDate = window.Start
	}

	// The fraction is of one whole period of this line item. A window clipped short by a
	// calendar stub keeps the full period as its divisor, so the quote matches what the
	// invoice would charge for the same days.
	windowDuration := window.End.Sub(window.Start)
	periodStart := window.End.Add(-fullPeriodDuration(
		ctx, s.params.Logger, item, window.Start, customerTimezone, windowDuration,
	))

	base := proration.ProrationParams{
		SubscriptionID:     sub.ID,
		LineItemID:         item.ID,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		ProrationDate:      prorationDate,
		ProrationBehavior:  req.Behavior,
		ProrationStrategy:  types.StrategySecondBased,
		Currency:           sub.Currency,
		PlanDisplayName:    item.DisplayName,
		Timezone:           customerTimezone,
	}

	switch entry.Action {
	case types.ProrationActionAddItem:
		base.Action = types.ProrationActionAddItem
		base.PlanPayInAdvance = entry.NewPrice.InvoiceCadence == types.InvoiceCadenceAdvance
		base.NewPriceID = item.PriceID
		base.NewQuantity = entry.NewQuantity
		base.NewPricePerUnit = entry.NewPrice.Amount

	case types.ProrationActionRemoveItem:
		base.Action = types.ProrationActionRemoveItem
		base.PlanPayInAdvance = entry.CurrentPrice.InvoiceCadence == types.InvoiceCadenceAdvance
		base.OldPriceID = item.PriceID
		base.OldQuantity = entry.CurrentQuantity
		base.OldPricePerUnit = entry.CurrentPrice.Amount
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
		base.OldPricePerUnit = entry.CurrentPrice.Amount
		base.NewPricePerUnit = entry.NewPrice.Amount
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

	return dto.CreateInvoiceLineItemRequest{
		PriceID:                &priceID,
		PriceType:              &priceType,
		PlanDisplayName:        &planDisplayName,
		DisplayName:            &displayName,
		Amount:                 amount,
		Quantity:               quantity,
		PeriodStart:            &effectiveDate,
		PeriodEnd:              &periodEnd,
		SubscriptionLineItemID: &subscriptionLineItemID,
		Metadata:               types.Metadata{"description": description},
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
