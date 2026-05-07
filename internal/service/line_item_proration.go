package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// LineItemProrationEntry pairs a line item with its loaded price and the requested action.
type LineItemProrationEntry struct {
	LineItem    *subscription.SubscriptionLineItem
	Price       *price.Price
	Action      types.ProrationAction
	OldQuantity decimal.Decimal // non-zero only for quantity changes
	NewQuantity decimal.Decimal // zero for remove; equals line item qty for add
}

// LineItemProrationRequest is the unified input for Compute and Apply.
type LineItemProrationRequest struct {
	Subscription   *subscription.Subscription
	Entries        []LineItemProrationEntry
	EffectiveDate  time.Time
	Behavior       types.ProrationBehavior
	Reason         string // shown in wallet credit description
	IdempotencyKey string // required when Behavior == create_prorations and credits may be issued
}

// LineItemProrationSummary is returned by Compute and used internally by Apply.
type LineItemProrationSummary struct {
	// Per-item results in the same order as Entries (skipped entries are omitted).
	Results []proration.ProrationResult

	// Positive-net items aggregated into a single one-off invoice.
	ChargeLineItems   []dto.CreateInvoiceLineItemRequest
	TotalChargeAmount decimal.Decimal

	// Absolute value of negative-net items; used for a single wallet credit.
	TotalCreditAmount decimal.Decimal

	Currency  string
	IsPreview bool
}

// LineItemProrationService centralises compute + settlement for mid-period
// add/remove of individual subscription line items.
type LineItemProrationService interface {
	// Compute calculates proration for all entries and returns a summary.
	// No invoices or wallet credits are created.
	Compute(ctx context.Context, req LineItemProrationRequest) (*LineItemProrationSummary, error)

	// Apply calls Compute and then settles:
	//   net > 0  → creates a single InvoiceTypeOneOff and attempts payment
	//   net < 0  → issues a single wallet credit using req.IdempotencyKey
	//   net == 0 → no-op
	// If req.Behavior != ProrationBehaviorCreateProrations, Apply is a no-op.
	Apply(ctx context.Context, req LineItemProrationRequest) error
}

type lineItemProrationService struct {
	params ServiceParams
}

// NewLineItemProrationService creates a new LineItemProrationService.
func NewLineItemProrationService(params ServiceParams) LineItemProrationService {
	return &lineItemProrationService{params: params}
}

func (s *lineItemProrationService) Compute(ctx context.Context, req LineItemProrationRequest) (*LineItemProrationSummary, error) {
	sub := req.Subscription
	prorationSvc := NewProrationService(s.params)

	customerTimezone := sub.CustomerTimezone
	if customerTimezone == "" {
		customerTimezone = "UTC"
	}

	summary := &LineItemProrationSummary{
		Currency:          sub.Currency,
		IsPreview:         req.Behavior == types.ProrationBehaviorNone,
		TotalChargeAmount: decimal.Zero,
		TotalCreditAmount: decimal.Zero,
	}

	for _, entry := range req.Entries {
		item := entry.LineItem
		p := entry.Price

		// Skip usage prices — future consumption is unknown at change time.
		if item.PriceType == types.PRICE_TYPE_USAGE {
			continue
		}

		// Skip remove of onetime addons — non-refundable (EndDate non-zero means onetime cadence).
		if entry.Action == types.ProrationActionRemoveItem && !item.EndDate.IsZero() {
			continue
		}

		params, skip := s.buildProrationParams(sub, entry, req, customerTimezone)
		if skip {
			continue
		}

		result, err := prorationSvc.CalculateProration(ctx, params)
		if err != nil {
			return nil, err
		}
		if result == nil {
			// ProrationBehavior == none returns nil
			continue
		}

		summary.Results = append(summary.Results, *result)

		if result.NetAmount.GreaterThan(decimal.Zero) {
			lineItem := s.buildChargeLineItem(sub, entry, result.NetAmount, req.EffectiveDate, p)
			summary.ChargeLineItems = append(summary.ChargeLineItems, lineItem)
			summary.TotalChargeAmount = summary.TotalChargeAmount.Add(result.NetAmount)
		} else if result.NetAmount.LessThan(decimal.Zero) {
			summary.TotalCreditAmount = summary.TotalCreditAmount.Add(result.NetAmount.Abs())
		}
	}

	return summary, nil
}

func (s *lineItemProrationService) Apply(ctx context.Context, req LineItemProrationRequest) error {
	if req.Behavior != types.ProrationBehaviorCreateProrations {
		return nil
	}

	summary, err := s.Compute(ctx, req)
	if err != nil {
		return err
	}

	sub := req.Subscription

	if summary.TotalChargeAmount.GreaterThan(decimal.Zero) && len(summary.ChargeLineItems) > 0 {
		invoiceSvc := NewInvoiceService(s.params)
		if err := s.settleCharge(ctx, sub, summary, req.EffectiveDate, invoiceSvc); err != nil {
			return err
		}
	}

	if summary.TotalCreditAmount.GreaterThan(decimal.Zero) {
		walletSvc := NewWalletService(s.params)
		billingCustomer := sub.GetInvoicingCustomerID()
		if _, err := walletSvc.TopUpWalletForProratedCharge(
			ctx, billingCustomer, summary.TotalCreditAmount, sub.Currency, req.IdempotencyKey,
		); err != nil {
			s.params.Logger.ErrorwCtx(ctx, "failed to issue wallet credit for proration", "error", err)
			return err
		}
	}

	return nil
}

// buildProrationParams constructs the ProrationParams for a single entry.
// Returns (params, skip=true) when the entry should be skipped entirely.
func (s *lineItemProrationService) buildProrationParams(
	sub *subscription.Subscription,
	entry LineItemProrationEntry,
	req LineItemProrationRequest,
	customerTimezone string,
) (proration.ProrationParams, bool) {
	item := entry.LineItem
	p := entry.Price
	periodEnd := sub.CurrentPeriodEnd.Add(-time.Second)

	base := proration.ProrationParams{
		SubscriptionID:     sub.ID,
		LineItemID:         item.ID,
		PlanPayInAdvance:   p.InvoiceCadence == types.InvoiceCadenceAdvance,
		CurrentPeriodStart: sub.CurrentPeriodStart,
		CurrentPeriodEnd:   periodEnd,
		ProrationDate:      req.EffectiveDate,
		ProrationBehavior:  req.Behavior,
		ProrationStrategy:  types.StrategySecondBased,
		Currency:           sub.Currency,
		PlanDisplayName:    item.DisplayName,
		CustomerTimezone:   customerTimezone,
	}

	switch entry.Action {
	case types.ProrationActionAddItem:
		base.Action = types.ProrationActionAddItem
		base.NewPriceID = item.PriceID
		base.NewQuantity = item.Quantity
		base.NewPricePerUnit = p.Amount

	case types.ProrationActionRemoveItem:
		base.Action = types.ProrationActionRemoveItem
		base.OldPriceID = item.PriceID
		base.OldQuantity = item.Quantity
		base.OldPricePerUnit = p.Amount
		base.CancellationType = types.CancellationTypeImmediate
		base.CancellationReason = req.Reason
		base.RefundEligible = true
		base.OriginalAmountPaid = p.Amount.Mul(item.Quantity)
		base.PreviousCreditsIssued = decimal.Zero

	default:
		return proration.ProrationParams{}, true
	}

	return base, false
}

// buildChargeLineItem constructs the invoice line item for a positive proration result.
func (s *lineItemProrationService) buildChargeLineItem(
	sub *subscription.Subscription,
	entry LineItemProrationEntry,
	amount decimal.Decimal,
	effectiveDate time.Time,
	p *price.Price,
) dto.CreateInvoiceLineItemRequest {
	item := entry.LineItem
	periodEnd := sub.CurrentPeriodEnd
	priceID := item.PriceID
	priceType := string(p.Type)
	displayName := item.DisplayName

	qty := item.Quantity
	if entry.Action == types.ProrationActionAddItem && !entry.NewQuantity.IsZero() {
		qty = entry.NewQuantity
	}

	var description string
	if entry.Action == types.ProrationActionAddItem {
		description = fmt.Sprintf("Proration charge: %s × %s %s %s/unit (%s – %s)",
			qty.String(), displayName,
			strings.ToUpper(sub.Currency), p.Amount.String(),
			effectiveDate.Format("2 Jan 2006"), periodEnd.Format("2 Jan 2006"))
	} else {
		description = fmt.Sprintf("Proration credit: %s × %s %s %s/unit (%s – %s)",
			qty.String(), displayName,
			strings.ToUpper(sub.Currency), p.Amount.String(),
			effectiveDate.Format("2 Jan 2006"), periodEnd.Format("2 Jan 2006"))
	}

	return dto.CreateInvoiceLineItemRequest{
		PriceID:     &priceID,
		PriceType:   &priceType,
		DisplayName: &displayName,
		Amount:      amount,
		Quantity:    qty,
		PeriodStart: &effectiveDate,
		PeriodEnd:   &periodEnd,
		Metadata:    types.Metadata{"description": description},
	}
}

// settleCharge creates a one-off invoice for the aggregated charge amount.
func (s *lineItemProrationService) settleCharge(
	ctx context.Context,
	sub *subscription.Subscription,
	summary *LineItemProrationSummary,
	effectiveDate time.Time,
	invoiceSvc InvoiceService,
) error {
	billingCustomer := sub.GetInvoicingCustomerID()
	billingPeriod := string(sub.BillingPeriod)
	periodEnd := sub.CurrentPeriodEnd

	inv, err := invoiceSvc.CreateInvoice(ctx, dto.CreateInvoiceRequest{
		CustomerID:     billingCustomer,
		SubscriptionID: &sub.ID,
		InvoiceType:    types.InvoiceTypeOneOff,
		Currency:       sub.Currency,
		BillingReason:  types.InvoiceBillingReasonSubscriptionUpdate,
		AmountDue:      summary.TotalChargeAmount,
		Total:          summary.TotalChargeAmount,
		Subtotal:       summary.TotalChargeAmount,
		PeriodStart:    &effectiveDate,
		PeriodEnd:      &periodEnd,
		BillingPeriod:  &billingPeriod,
		LineItems:      summary.ChargeLineItems,
	})
	if err != nil {
		s.params.Logger.ErrorwCtx(ctx, "failed to create proration charge invoice", "error", err)
		return err
	}

	if err := invoiceSvc.AttemptPayment(ctx, inv.ID); err != nil {
		s.params.Logger.WarnwCtx(ctx, "failed to attempt payment for proration charge invoice",
			"error", err, "invoice_id", inv.ID)
	}

	return nil
}
