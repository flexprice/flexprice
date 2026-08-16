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
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type LineItemProrationEntry struct {
	LineItem    *subscription.SubscriptionLineItem
	Price       *price.Price
	Action      types.ProrationAction
	OldQuantity decimal.Decimal // non-zero only for quantity changes
	NewQuantity decimal.Decimal // zero for remove; equals line item qty for add
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
	Results []proration.ProrationResult

	ChargeLineItems   []dto.CreateInvoiceLineItemRequest
	TotalChargeAmount decimal.Decimal

	// Negative amounts. Netting callers put these on the charge invoice; Apply
	// ignores them and pays TotalCreditAmount to the wallet instead.
	CreditLineItems []dto.CreateInvoiceLineItemRequest

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

func (s *LineItemProrationSummary) NetAmount() decimal.Decimal {
	if s == nil {
		return decimal.Zero
	}
	return s.TotalChargeAmount.Sub(s.TotalCreditAmount)
}

type LineItemProrationService interface {
	Compute(ctx context.Context, req LineItemProrationRequest) (*LineItemProrationSummary, error)
	// Apply settles Compute: charge invoice if net > 0, wallet credit if net < 0.
	// No-op when Behavior != CreateProrations.
	Apply(ctx context.Context, req LineItemProrationRequest) ([]dto.ChangedInvoice, error)
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
		p := entry.Price

		if item.PriceType == types.PRICE_TYPE_USAGE {
			continue
		}

		// Onetime addons (EndDate set) are non-refundable.
		if entry.Action == types.ProrationActionRemoveItem && !item.EndDate.IsZero() {
			continue
		}

		params, skip := s.buildProrationParams(sub, entry, req, customerTimezone, billed)
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

		summary.Results = append(summary.Results, *result)

		if result.NetAmount.GreaterThan(decimal.Zero) {
			lineItem := s.buildChargeLineItem(sub, entry, result.NetAmount, req.EffectiveDate, p)
			summary.ChargeLineItems = append(summary.ChargeLineItems, lineItem)
			summary.TotalChargeAmount = summary.TotalChargeAmount.Add(result.NetAmount)
		} else if result.NetAmount.LessThan(decimal.Zero) {
			lineItem := s.buildChargeLineItem(sub, entry, result.NetAmount, req.EffectiveDate, p)
			summary.CreditLineItems = append(summary.CreditLineItems, lineItem)
			summary.TotalCreditAmount = summary.TotalCreditAmount.Add(result.NetAmount.Abs())
		}
	}

	return summary, nil
}

func (s *lineItemProrationService) Apply(ctx context.Context, req LineItemProrationRequest) ([]dto.ChangedInvoice, error) {
	if req.Behavior != types.ProrationBehaviorCreateProrations {
		return nil, nil
	}

	summary, err := s.Compute(ctx, req)
	if err != nil {
		return nil, err
	}

	sub := req.Subscription
	settled := make([]dto.ChangedInvoice, 0, 2)

	if summary.TotalChargeAmount.GreaterThan(decimal.Zero) && len(summary.ChargeLineItems) > 0 {
		invoiceSvc := NewInvoiceService(s.params)
		inv, err := s.settleCharge(ctx, sub, summary, req.EffectiveDate, prorationChargeInvoiceKey(req), invoiceSvc)
		if err != nil {
			return nil, err
		}

		settled = append(settled, dto.ChangedInvoice{
			ID:      inv.ID,
			Action:  dto.ChangedInvoiceActionCreated,
			Status:  dto.ChangedInvoiceStatusFromPaymentStatus(inv.PaymentStatus),
			Invoice: inv,
		})
	}

	if summary.TotalCreditAmount.GreaterThan(decimal.Zero) {
		walletSvc := NewWalletService(s.params)
		billingCustomer := sub.GetInvoicingCustomerID()
		walletTx, err := walletSvc.TopUpWalletForProratedCharge(
			ctx, billingCustomer, summary.TotalCreditAmount, sub.Currency, req.IdempotencyKey,
		)
		if err != nil {
			s.params.Logger.Error(ctx, "failed to issue wallet credit for proration", "error", err)
			return settled, err
		}
		settled = append(settled, walletCreditChangedInvoice(walletTx, dto.ChangedInvoiceStatusWalletIssued))
	}

	return settled, nil
}

// Cap removal credits at amounts actually billed (list price never binds).
func (s *lineItemProrationService) creditBasisForInvoiceLineItems(
	ctx context.Context,
	req LineItemProrationRequest,
) map[string]*invoice.BilledAmounts {
	lineItemIDs := make([]string, 0, len(req.Entries))
	for _, entry := range req.Entries {
		if entry.Action == types.ProrationActionRemoveItem && entry.LineItem != nil {
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
	sub *subscription.Subscription,
	entry LineItemProrationEntry,
	req LineItemProrationRequest,
	customerTimezone string,
	billed map[string]*invoice.BilledAmounts,
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
		Timezone:           customerTimezone,
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
		base.OriginalAmountPaid, base.PreviousCreditsIssued = creditBasis(item, p, billed)

	default:
		return proration.ProrationParams{}, true
	}

	return base, false
}

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
	subscriptionLineItemID := item.ID

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
		PriceID:                &priceID,
		PriceType:              &priceType,
		DisplayName:            &displayName,
		Amount:                 amount,
		Quantity:               qty,
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

func buildLineItemProrationChargeInvoiceRequest(
	sub *subscription.Subscription,
	summary *LineItemProrationSummary,
	effectiveDate time.Time,
	idempotencyKey string,
) dto.CreateInvoiceRequest {
	billingCustomer := sub.GetInvoicingCustomerID()
	billingPeriod := string(sub.BillingPeriod)
	periodEnd := sub.CurrentPeriodEnd

	return dto.CreateInvoiceRequest{
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
		IdempotencyKey: &idempotencyKey,
	}
}

func (s *lineItemProrationService) settleCharge(
	ctx context.Context,
	sub *subscription.Subscription,
	summary *LineItemProrationSummary,
	effectiveDate time.Time,
	idempotencyKey string,
	invoiceSvc InvoiceService,
) (*dto.InvoiceResponse, error) {
	inv, err := invoiceSvc.CreateInvoice(ctx, buildLineItemProrationChargeInvoiceRequest(sub, summary, effectiveDate, idempotencyKey))
	if err != nil {
		s.params.Logger.Error(ctx, "failed to create proration charge invoice", "error", err)
		return nil, err
	}

	if err := invoiceSvc.AttemptPayment(ctx, inv.ID); err != nil {
		s.params.Logger.Info(ctx, "failed to attempt payment for proration charge invoice",
			"error", err, "invoice_id", inv.ID)
	}

	if latest, err := s.params.InvoiceRepo.Get(ctx, inv.ID); err == nil && latest != nil {
		inv.InvoiceStatus = latest.InvoiceStatus
		inv.PaymentStatus = latest.PaymentStatus
		inv.AmountPaid = latest.AmountPaid
		inv.AmountRemaining = latest.AmountRemaining
	}

	return inv, nil
}
